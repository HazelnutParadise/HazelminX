package iam

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/google/uuid"
)

// Config tunes the service. Zero values take the production defaults.
type Config struct {
	PasswordParams PasswordParams
	Limiter        LimiterPolicy
	SessionIdle    time.Duration // slide window; default 12h
	SessionMax     time.Duration // absolute lifetime; default 7d
	SetupCodeTTL   time.Duration // default 1h
	ChallengeTTL   time.Duration // second login step; default 5m
	ReauthWindow   time.Duration // how long a login or reauthentication counts as recent; default 10m
	// Secrets holds TOTP seeds and the recovery-code key (ticket 011). Nil
	// disables TOTP and recovery codes (ErrUnavailable).
	Secrets *secrets.Service
	Now     func() time.Time
}

func (c Config) withDefaults() Config {
	if c.PasswordParams == (PasswordParams{}) {
		c.PasswordParams = DefaultPasswordParams
	}
	if c.Limiter == (LimiterPolicy{}) {
		c.Limiter = DefaultLimiterPolicy
	}
	if c.SessionIdle == 0 {
		c.SessionIdle = 12 * time.Hour
	}
	if c.SessionMax == 0 {
		c.SessionMax = 7 * 24 * time.Hour
	}
	if c.SetupCodeTTL == 0 {
		c.SetupCodeTTL = time.Hour
	}
	if c.ChallengeTTL == 0 {
		c.ChallengeTTL = 5 * time.Minute
	}
	if c.ReauthWindow == 0 {
		c.ReauthWindow = 10 * time.Minute
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Service implements accounts, sessions, authorization data and audit on top
// of the platform database.
type Service struct {
	db      *sql.DB
	cfg     Config
	limiter *Limiter

	setupMu      sync.Mutex
	setupCode    string
	setupExpires time.Time

	dummyHash string // verified against for unknown usernames so timing matches

	challenges *challengeStore // pending second login steps
	ceremonies *ceremonyStore  // pending passkey ceremonies

	recoveryMu   sync.Mutex
	recoveryHMAC []byte // cached recovery-code key
}

var randRead = rand.Read

// New wires the service to the store's database (migrations already applied).
func New(db *sql.DB, cfg Config) (*Service, error) {
	cfg = cfg.withDefaults()
	dummy, err := HashPassword("hazelminx-dummy-password-for-timing", cfg.PasswordParams)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, cfg: cfg, limiter: NewLimiter(cfg.Limiter, cfg.Now), dummyHash: dummy,
		challenges: newChallengeStore(), ceremonies: newCeremonyStore()}, nil
}

// Limiter exposes the attempt limiter so ticket 014 can throttle recovery codes with the same policy.
func (s *Service) Limiter() *Limiter { return s.limiter }

func (s *Service) now() time.Time { return s.cfg.Now().UTC() }

// ---- setup (first owner) ------------------------------------------------

// NeedsSetup is true until the first owner exists.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// SetupCode returns the current one-time activation code, minting a new one
// when none is valid. It returns "" once setup is complete. The caller prints
// it to the installer's stdout (39.1); it must not reach shared logs.
func (s *Service) SetupCode(ctx context.Context) (string, time.Time, error) {
	need, err := s.NeedsSetup(ctx)
	if err != nil || !need {
		return "", time.Time{}, err
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupCode == "" || !s.now().Before(s.setupExpires) {
		code, err := randomToken(24)
		if err != nil {
			return "", time.Time{}, err
		}
		s.setupCode, s.setupExpires = code, s.now().Add(s.cfg.SetupCodeTTL)
	}
	return s.setupCode, s.setupExpires, nil
}

// CompleteSetup creates the first owner and signs them in. The code is
// consumed atomically under the mutex and the user, assignment, session and
// audit rows land in one transaction that re-checks "no users yet", so two
// racing callers cannot both become owner and a failed transaction never
// leaves a role-less owner behind (which would lock everyone out). The
// session is held to second-factor enrolment until the owner adds one.
func (s *Service) CompleteSetup(ctx context.Context, code, username, password string, src Source) (*LoginResult, error) {
	if ok, retry := s.limiter.Check(setupKey, src.IP, false); !ok {
		return &LoginResult{RetryAfter: retry}, ErrThrottled
	}
	need, err := s.NeedsSetup(ctx)
	if err != nil {
		return nil, err
	}
	if !need {
		return nil, ErrSetupDone
	}
	if err := validateNewUser(username, password); err != nil {
		return nil, err
	}
	s.setupMu.Lock()
	validCode := s.setupCode != "" && code == s.setupCode && s.now().Before(s.setupExpires)
	if validCode {
		s.setupCode = "" // single use, whatever happens next
	}
	s.setupMu.Unlock()
	if !validCode {
		s.limiter.Failure(setupKey, src.IP)
		s.record(ctx, s.db, AuditEvent{Action: "setup.owner", Scope: Global, Result: ResultFailed, Message: "invalid or expired setup code", TraceID: src.TraceID, SourceIP: src.IP})
		return nil, ErrSetupCode
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrSetupDone
	}
	u, err := s.insertUser(ctx, tx, username, password, "")
	if err != nil {
		return nil, err
	}
	if _, err := s.insertAssignment(ctx, tx, u.ID, RoleOwner, Global, u.ID); err != nil {
		return nil, err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: u.ID, ActorName: u.Username, Action: "setup.owner", Scope: Global,
		ResourceKind: "user", ResourceID: u.ID, Result: ResultOK, After: map[string]any{"username": u.Username, "role": RoleOwner},
		TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return nil, err
	}
	sess, err := s.openSessionTx(ctx, tx, u, src, "")
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &LoginResult{Session: sess, User: u, EnrollmentRequired: true}, nil
}

// setupKey is the limiter account key for setup attempts; '#' cannot occur
// in a username, so it never collides with a real account.
const setupKey = "#setup"

// ---- login & sessions ---------------------------------------------------

// Login verifies a password. Unknown usernames, wrong passwords and
// disabled accounts all return ErrInvalidCredentials after the same hashing
// work; throttling returns ErrThrottled with RetryAfter set. A user without
// a second factor is signed in (Session set); a user with one gets a
// Challenge to finish with CompleteSecondFactor.
func (s *Service) Login(ctx context.Context, username, password string, src Source) (*LoginResult, error) {
	username = strings.TrimSpace(username)
	trusted, err := s.trustedSource(ctx, username, src.IP)
	if err != nil {
		return nil, err
	}
	if ok, retry := s.limiter.Check(username, src.IP, trusted); !ok {
		// Not audited: the failures that led here are already on the ledger,
		// and a throttled source must not be able to grow it at wire speed.
		return &LoginResult{RetryAfter: retry}, ErrThrottled
	}

	u, hash, err := s.userByUsername(ctx, username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if u == nil {
		hash = s.dummyHash
	}
	match, verr := VerifyPassword(password, hash)
	if verr != nil {
		return nil, verr
	}
	if u == nil || !match || u.Status != StatusActive {
		s.limiter.Failure(username, src.IP)
		s.record(ctx, s.db, AuditEvent{Action: "login", Scope: Global, Result: ResultFailed, Message: "invalid credentials",
			After: map[string]any{"username": username}, TraceID: src.TraceID, SourceIP: src.IP})
		return nil, ErrInvalidCredentials
	}

	f, err := s.factors(ctx, s.db, u.ID)
	if err != nil {
		return nil, err
	}
	if f.any() {
		ch, err := s.newChallenge(ctx, u, f, src)
		if err != nil {
			return nil, err
		}
		s.limiter.Success(username)
		if err := s.record(ctx, s.db, AuditEvent{ActorID: u.ID, ActorName: u.Username, Action: "login.password", Scope: Global,
			ResourceKind: "user", ResourceID: u.ID, Result: ResultOK, Message: "second factor required",
			After: map[string]any{"methods": ch.Methods}, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
			return nil, err
		}
		return &LoginResult{User: u, Challenge: ch}, nil
	}

	pr, err := s.principal(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	sess, err := s.openSession(ctx, u, src)
	if err != nil {
		return nil, err
	}
	s.limiter.Success(username)
	return &LoginResult{Session: sess, User: u, EnrollmentRequired: RequiresSecondFactor(pr)}, nil
}

func (s *Service) openSession(ctx context.Context, u *User, src Source) (*Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	sess, err := s.openSessionTx(ctx, tx, u, src, "")
	if err != nil {
		return nil, err
	}
	return sess, tx.Commit()
}

// openSessionTx inserts a session and its audit row inside tx. factor is the
// second-factor method that completed the login, "" for none.
func (s *Service) openSessionTx(ctx context.Context, tx *sql.Tx, u *User, src Source, factor string) (*Session, error) {
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	idle, _, err := s.userIdle(ctx, tx, u.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	sess := &Session{ID: uuid.NewString(), UserID: u.ID, Token: token, CSRFToken: csrf, SourceIP: src.IP, UserAgent: src.UserAgent,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(idle), SecondFactor: factor}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_token,source_ip,user_agent,created_at,last_seen_at,expires_at,second_factor)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, sess.ID, u.ID, hashToken(token), csrf, src.IP, src.UserAgent, ts(now), ts(now), ts(sess.ExpiresAt), factor); err != nil {
		return nil, err
	}
	var after map[string]any
	if factor != "" {
		after = map[string]any{"secondFactor": factor}
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: u.ID, ActorName: u.Username, Action: "login", Scope: Global,
		ResourceKind: "session", ResourceID: sess.ID, Result: ResultOK, After: after, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return nil, err
	}
	return sess, nil
}

// Authenticate resolves a session token to a principal, sliding the idle
// window. Revoked, expired and disabled-user sessions fail identically.
func (s *Service) Authenticate(ctx context.Context, token string) (*Principal, *Session, error) {
	if token == "" {
		return nil, nil, ErrSessionInvalid
	}
	var sess Session
	var created, lastSeen, expires string
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id,csrf_token,source_ip,user_agent,created_at,last_seen_at,expires_at,revoked_at,second_factor
		FROM sessions WHERE token_hash=?`, hashToken(token)).
		Scan(&sess.ID, &sess.UserID, &sess.CSRFToken, &sess.SourceIP, &sess.UserAgent, &created, &lastSeen, &expires, &revoked, &sess.SecondFactor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, nil, err
	}
	sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt = pt(created), pt(lastSeen), pt(expires)
	now := s.now()
	if revoked.Valid || !now.Before(sess.ExpiresAt) || !now.Before(sess.CreatedAt.Add(s.cfg.SessionMax)) {
		return nil, nil, ErrSessionInvalid
	}
	p, err := s.principal(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil, ErrSessionInvalid
		}
		return nil, nil, err
	}
	if p.User.Status != StatusActive {
		return nil, nil, ErrSessionInvalid
	}
	p.Kind, p.SessionID = "session", sess.ID
	if RequiresSecondFactor(p) {
		f, err := s.factors(ctx, s.db, p.User.ID)
		if err != nil {
			return nil, nil, err
		}
		p.EnrollmentRequired = !f.any()
	}
	if now.Sub(sess.LastSeenAt) > time.Minute {
		idle, _, err := s.userIdle(ctx, s.db, sess.UserID)
		if err != nil {
			return nil, nil, err
		}
		sess.LastSeenAt, sess.ExpiresAt = now, now.Add(idle)
		if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=?, expires_at=? WHERE id=?`, ts(now), ts(sess.ExpiresAt), sess.ID); err != nil {
			return nil, nil, err
		}
	}
	return p, &sess, nil
}

// Logout revokes the session; later requests with the cookie fail.
func (s *Service) Logout(ctx context.Context, p *Principal, src Source) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, ts(s.now()), p.SessionID); err != nil {
		return err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "logout", Scope: Global,
		ResourceKind: "session", ResourceID: p.SessionID, Result: ResultOK, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return err
	}
	return tx.Commit()
}

// trustedSource is true when ip has logged into username before (a
// successful session exists), which exempts it from the account cool-down.
func (s *Service) trustedSource(ctx context.Context, username, ip string) (bool, error) {
	if ip == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE u.username=? COLLATE NOCASE AND s.source_ip=?`, username, ip).Scan(&n)
	return n > 0, err
}

// ---- users & roles ------------------------------------------------------

// CreateUserInput is what the owner supplies for a new account.
type CreateUserInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{2,63}$`)

// CreateUser adds a local account with no roles (IAM-001: nothing is
// granted until a role is assigned).
func (s *Service) CreateUser(ctx context.Context, actor *Principal, in CreateUserInput, src Source) (*User, error) {
	if !Authorize(actor, ActionUsersWrite, Global) {
		s.RecordDenied(ctx, actor, ActionUsersWrite, Global, src)
		return nil, ErrForbidden
	}
	if err := validateNewUser(in.Username, in.Password); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	u, err := s.insertUser(ctx, tx, in.Username, in.Password, in.DisplayName)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: actor.User.ID, ActorName: actor.User.Username, Action: "user.create", Scope: Global,
		ResourceKind: "user", ResourceID: u.ID, Result: ResultOK,
		After:   map[string]any{"username": u.Username, "displayName": u.DisplayName, "status": u.Status, "password": in.Password},
		TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return nil, err
	}
	return u, tx.Commit()
}

func validateNewUser(username, password string) error {
	if !usernameRe.MatchString(strings.TrimSpace(username)) {
		return fmt.Errorf("%w: username must be 3-64 characters of letters, digits, '.', '_' or '-'", ErrValidation)
	}
	return ValidatePassword(password)
}

func (s *Service) insertUser(ctx context.Context, ex execer, username, password, displayName string) (*User, error) {
	username = strings.TrimSpace(username)
	hash, err := HashPassword(password, s.cfg.PasswordParams)
	if err != nil {
		return nil, err
	}
	now := s.now()
	u := &User{ID: uuid.NewString(), Username: username, DisplayName: displayName, Status: StatusActive, CreatedAt: now, UpdatedAt: now}
	_, err = ex.ExecContext(ctx, `INSERT INTO users(id,username,display_name,password_hash,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.DisplayName, hash, u.Status, ts(now), ts(now))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("%w: username already exists", ErrConflict)
		}
		return nil, err
	}
	return u, nil
}

// AssignRole grants role on scope to a user. Only principals with
// roles:assign (owner) may do it; owner may only be granted globally.
func (s *Service) AssignRole(ctx context.Context, actor *Principal, userID string, role Role, scope Scope, src Source) (*Assignment, error) {
	if !Authorize(actor, ActionRolesAssign, Global) {
		s.RecordDenied(ctx, actor, ActionRolesAssign, Global, src)
		return nil, ErrForbidden
	}
	if !ValidRole(role) {
		return nil, fmt.Errorf("%w: unknown role %q", ErrValidation, role)
	}
	if err := validScope(scope); err != nil {
		return nil, err
	}
	if role == RoleOwner && scope.Kind != ScopeGlobal {
		return nil, fmt.Errorf("%w: owner can only be assigned globally", ErrValidation)
	}
	target, err := s.GetUser(ctx, actor, userID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	as, err := s.insertAssignment(ctx, tx, userID, role, scope, actor.User.ID)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: actor.User.ID, ActorName: actor.User.Username, Action: "role.assign", Scope: scope,
		ResourceKind: "user", ResourceID: userID, Result: ResultOK,
		After: map[string]any{"username": target.Username, "role": role, "scope": scope}, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return nil, err
	}
	return as, tx.Commit()
}

func (s *Service) insertAssignment(ctx context.Context, tx *sql.Tx, userID string, role Role, scope Scope, by string) (*Assignment, error) {
	now := s.now()
	as := &Assignment{ID: uuid.NewString(), UserID: userID, Role: role, Scope: scope, CreatedAt: now, CreatedBy: by}
	_, err := tx.ExecContext(ctx, `INSERT INTO role_assignments(id,user_id,role,scope_kind,scope_id,created_at,created_by) VALUES(?,?,?,?,?,?,?)`,
		as.ID, userID, string(role), string(scope.Kind), scope.ID, ts(now), by)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("%w: assignment already exists", ErrConflict)
		}
		return nil, err
	}
	return as, nil
}

// RevokeRole removes one assignment.
func (s *Service) RevokeRole(ctx context.Context, actor *Principal, assignmentID string, src Source) error {
	if !Authorize(actor, ActionRolesAssign, Global) {
		s.RecordDenied(ctx, actor, ActionRolesAssign, Global, src)
		return ErrForbidden
	}
	var as Assignment
	var kind string
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id,role,scope_kind,scope_id FROM role_assignments WHERE id=?`, assignmentID).
		Scan(&as.ID, &as.UserID, &as.Role, &kind, &as.Scope.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	as.Scope.Kind = ScopeKind(kind)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_assignments WHERE id=?`, assignmentID); err != nil {
		return err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: actor.User.ID, ActorName: actor.User.Username, Action: "role.revoke", Scope: as.Scope,
		ResourceKind: "user", ResourceID: as.UserID, Result: ResultOK,
		Before: map[string]any{"role": as.Role, "scope": as.Scope}, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return err
	}
	return tx.Commit()
}

// UserDetail is a user with their assignments.
type UserDetail struct {
	User
	Assignments []Assignment `json:"assignments"`
}

// ListUsers returns every account with assignments; requires users:read.
func (s *Service) ListUsers(ctx context.Context, actor *Principal) ([]UserDetail, error) {
	if !Authorize(actor, ActionUsersRead, Global) {
		return nil, ErrForbidden
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,display_name,status,created_at,updated_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	var out []UserDetail
	for rows.Next() {
		var u User
		var c, m string
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Status, &c, &m); err != nil {
			rows.Close()
			return nil, err
		}
		u.CreatedAt, u.UpdatedAt = pt(c), pt(m)
		out = append(out, UserDetail{User: u})
	}
	rows.Close()
	for i := range out {
		as, err := s.assignments(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Assignments = as
	}
	if out == nil {
		out = []UserDetail{}
	}
	return out, nil
}

// GetUser returns one account; ErrNotFound both when missing and when the
// actor may not read users.
func (s *Service) GetUser(ctx context.Context, actor *Principal, id string) (*UserDetail, error) {
	if !Authorize(actor, ActionUsersRead, Global) {
		return nil, ErrNotFound
	}
	p, err := s.principal(ctx, id)
	if err != nil {
		return nil, err
	}
	return &UserDetail{User: p.User, Assignments: p.Assignments}, nil
}

func (s *Service) userByUsername(ctx context.Context, username string) (*User, string, error) {
	var u User
	var hash, c, m string
	err := s.db.QueryRowContext(ctx, `SELECT id,username,display_name,password_hash,status,created_at,updated_at FROM users WHERE username=? COLLATE NOCASE`, username).
		Scan(&u.ID, &u.Username, &u.DisplayName, &hash, &u.Status, &c, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	u.CreatedAt, u.UpdatedAt = pt(c), pt(m)
	return &u, hash, nil
}

func (s *Service) principal(ctx context.Context, userID string) (*Principal, error) {
	var u User
	var c, m string
	err := s.db.QueryRowContext(ctx, `SELECT id,username,display_name,status,created_at,updated_at FROM users WHERE id=?`, userID).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Status, &c, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt, u.UpdatedAt = pt(c), pt(m)
	as, err := s.assignments(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Principal{User: u, Assignments: as}, nil
}

func (s *Service) assignments(ctx context.Context, userID string) ([]Assignment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,role,scope_kind,scope_id,created_at,coalesce(created_by,'') FROM role_assignments WHERE user_id=? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		var a Assignment
		var kind, c string
		if err := rows.Scan(&a.ID, &a.UserID, &a.Role, &kind, &a.Scope.ID, &c, &a.CreatedBy); err != nil {
			return nil, err
		}
		a.Scope.Kind, a.CreatedAt = ScopeKind(kind), pt(c)
		out = append(out, a)
	}
	return out, rows.Err()
}

func validScope(sc Scope) error {
	switch sc.Kind {
	case ScopeGlobal:
		if sc.ID != "*" {
			return fmt.Errorf("%w: global scope id must be '*'", ErrValidation)
		}
	case ScopeNode, ScopeProject, ScopeDatabase:
		if sc.ID == "" || sc.ID == "*" {
			return fmt.Errorf("%w: %s scope needs a concrete id", ErrValidation, sc.Kind)
		}
	default:
		return fmt.Errorf("%w: unknown scope kind %q", ErrValidation, sc.Kind)
	}
	return nil
}

// ---- audit --------------------------------------------------------------

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Record appends an event from another package (jobs, nodes). It fills in
// id and time, masks summaries and never returns rows to the caller.
func (s *Service) Record(ctx context.Context, ev AuditEvent) error {
	return s.record(ctx, s.db, ev)
}

// RecordDenied writes the audit row for a refused call (403/404).
func (s *Service) RecordDenied(ctx context.Context, p *Principal, a Action, on Scope, src Source) {
	ev := AuditEvent{Action: "authz.deny", Scope: on, Result: ResultDenied, Message: string(a), TraceID: src.TraceID, SourceIP: src.IP}
	if p != nil {
		ev.ActorID, ev.ActorName = p.User.ID, p.User.Username
	}
	_ = s.record(ctx, s.db, ev)
}

func (s *Service) record(ctx context.Context, ex execer, ev AuditEvent) error {
	if ev.ID == "" {
		ev.ID = uuid.NewString()
	}
	if ev.At.IsZero() {
		ev.At = s.now()
	}
	if ev.Scope.Kind == "" {
		ev.Scope = Global
	}
	if ev.Result == "" {
		ev.Result = ResultOK
	}
	before, err := marshalMasked(ev.Before)
	if err != nil {
		return err
	}
	after, err := marshalMasked(ev.After)
	if err != nil {
		return err
	}
	var actor any
	if ev.ActorID != "" {
		actor = ev.ActorID
	}
	_, err = ex.ExecContext(ctx, `INSERT INTO audit_events(id,at,actor_id,actor_name,action,scope_kind,scope_id,resource_kind,resource_id,job_id,node_id,result,message,before_json,after_json,trace_id,source_ip)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.ID, ts(ev.At), actor, ev.ActorName, ev.Action, string(ev.Scope.Kind), ev.Scope.ID, ev.ResourceKind, ev.ResourceID, ev.JobID, ev.NodeID,
		ev.Result, ev.Message, before, after, ev.TraceID, ev.SourceIP)
	return err
}

// ListAudit pages the ledger, restricted to scopes where the actor holds
// audit:read. A filter on a scope outside that set yields an empty page, the
// same answer as for a scope that does not exist.
func (s *Service) ListAudit(ctx context.Context, actor *Principal, f AuditFilter) ([]AuditEvent, error) {
	global, scopes := ScopesFor(actor, ActionAuditRead)
	if !global && len(scopes) == 0 {
		return nil, ErrForbidden
	}
	if f.Limit <= 0 || f.Limit > MaxAuditPage {
		f.Limit = MaxAuditPage
	}
	where := []string{"seq > ?"}
	args := []any{f.AfterSeq}
	if !global {
		var ors []string
		for _, sc := range scopes {
			ors = append(ors, "(scope_kind=? AND scope_id=?)")
			args = append(args, string(sc.Kind), sc.ID)
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	if f.Scope != nil {
		where = append(where, "scope_kind=? AND scope_id=?")
		args = append(args, string(f.Scope.Kind), f.Scope.ID)
	}
	if f.ActorID != "" {
		where = append(where, "actor_id=?")
		args = append(args, f.ActorID)
	}
	if f.JobID != "" {
		where = append(where, "job_id=?")
		args = append(args, f.JobID)
	}
	if f.ResourceID != "" {
		where = append(where, "resource_id=?")
		args = append(args, f.ResourceID)
	}
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx, `SELECT seq,id,at,coalesce(actor_id,''),actor_name,action,scope_kind,scope_id,resource_kind,resource_id,job_id,node_id,result,message,before_json,after_json,trace_id,source_ip
		FROM audit_events WHERE `+strings.Join(where, " AND ")+` ORDER BY seq LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var ev AuditEvent
		var at, kind string
		var before, after *string
		if err := rows.Scan(&ev.Seq, &ev.ID, &at, &ev.ActorID, &ev.ActorName, &ev.Action, &kind, &ev.Scope.ID, &ev.ResourceKind, &ev.ResourceID,
			&ev.JobID, &ev.NodeID, &ev.Result, &ev.Message, &before, &after, &ev.TraceID, &ev.SourceIP); err != nil {
			return nil, err
		}
		ev.At, ev.Scope.Kind, ev.Before, ev.After = pt(at), ScopeKind(kind), unmarshalSummary(before), unmarshalSummary(after)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ---- helpers ------------------------------------------------------------

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func pt(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

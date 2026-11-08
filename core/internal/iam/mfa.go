package iam

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/google/uuid"
)

// Second factors of ticket 014 (IAM-002, IAM-011, IAM-012):
//
//   - Methods are TOTP (one per user), passkeys (several) and single-use
//     recovery codes. Recovery codes exist only while at least one real
//     factor does, and are issued with the first one.
//   - A password login of a user with a factor yields a challenge, not a
//     session. The challenge is single use, lives in memory for
//     Config.ChallengeTTL and allows MaxChallengeAttempts wrong answers.
//   - Wrong second-factor answers count against a per-user limiter key that
//     the trusted-source bypass does not apply to: behind a reverse proxy
//     every client shares one address, and a six-digit code must not be
//     guessable from the owner's "known" address.
//   - Holders of the roles in enforcedRoles must have a factor. Until they
//     do, their session is limited to enrolment (the API layer enforces the
//     route allow-list), and they cannot remove their last factor.
//   - Adding or removing a factor, or regenerating recovery codes, needs a
//     recent proof of identity (Reauthenticate or a fresh login).

// Second-factor methods.
const (
	MethodTOTP     = "totp"
	MethodPasskey  = "passkey"
	MethodRecovery = "recovery-code"
)

// MaxChallengeAttempts bounds wrong answers per login challenge.
const MaxChallengeAttempts = 5

// totpEnrollmentTTL is how long an unconfirmed TOTP seed may be confirmed.
const totpEnrollmentTTL = 15 * time.Minute

// Errors of the second-factor flows.
var (
	ErrChallengeInvalid   = errors.New("iam: login challenge invalid or expired")
	ErrSecondFactor       = errors.New("iam: second factor rejected")
	ErrEnrollmentRequired = errors.New("iam: second factor enrolment required")
	ErrReauthRequired     = errors.New("iam: recent reauthentication required")
	ErrLastFactor         = errors.New("iam: cannot remove the last second factor of this account")
	ErrUnavailable        = errors.New("iam: not available")
)

// enforcedRoles must hold a second factor (IAM-002 安全預設). Host operators
// are included because terminal:use and host:write amount to root on the
// managed host.
var enforcedRoles = map[Role]bool{RoleOwner: true, RolePlatformAdmin: true, RoleHostOperator: true}

// RequiresSecondFactor reports whether p holds a role that must use a
// second factor, in any scope.
func RequiresSecondFactor(p *Principal) bool {
	if p == nil {
		return false
	}
	for _, a := range p.Assignments {
		if enforcedRoles[a.Role] {
			return true
		}
	}
	return false
}

// LoginResult is the outcome of a password or second-factor step. Exactly
// one of Session (signed in) and Challenge (second step needed) is set on
// success; RetryAfter accompanies ErrThrottled.
type LoginResult struct {
	Session    *Session
	User       *User
	Challenge  *SecondFactorChallenge
	RetryAfter time.Duration
	// EnrollmentRequired: signed in, but the account must add a second
	// factor before anything else (see RequiresSecondFactor).
	EnrollmentRequired bool
}

// SecondFactorChallenge is handed to the browser between the two steps.
type SecondFactorChallenge struct {
	Token     string    `json:"challengeToken"`
	Methods   []string  `json:"methods"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// FactorSummary is what a user sees about their own second factors.
type FactorSummary struct {
	TOTP                   *TOTPInfo     `json:"totp"`
	Passkeys               []PasskeyInfo `json:"passkeys"`
	RecoveryCodesRemaining int           `json:"recoveryCodesRemaining"`
	Required               bool          `json:"required"`
	EnrollmentRequired     bool          `json:"enrollmentRequired"`
}

// TOTPInfo describes the authenticator-app factor.
type TOTPInfo struct {
	Confirmed   bool       `json:"confirmed"`
	CreatedAt   time.Time  `json:"createdAt"`
	ConfirmedAt *time.Time `json:"confirmedAt,omitempty"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

// TOTPEnrollment is shown once when a TOTP seed is generated.
type TOTPEnrollment struct {
	Secret        string    `json:"secret"` // base32 for manual entry
	URI           string    `json:"uri"`    // otpauth:// for the QR code
	Digits        int       `json:"digits"`
	PeriodSeconds int       `json:"periodSeconds"`
	ServerTime    time.Time `json:"serverTime"` // lets the user spot a wrong server clock
	ExpiresAt     time.Time `json:"expiresAt"`
}

// EnrollmentResult is returned when a factor is added. RecoveryCodes is
// non-empty only when this factor is the user's first and a fresh set was
// issued; it is never shown again.
type EnrollmentResult struct {
	RecoveryCodes []string     `json:"recoveryCodes,omitempty"`
	Passkey       *PasskeyInfo `json:"passkey,omitempty"`
}

// factorState is what a user currently has.
type factorState struct {
	totp     bool // confirmed
	passkeys int
	recovery int // unused codes
}

func (f factorState) any() bool { return f.totp || f.passkeys > 0 }

func (s *Service) factors(ctx context.Context, q queryRower, userID string) (factorState, error) {
	var f factorState
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM totp_credentials WHERE user_id=? AND confirmed_at IS NOT NULL`, userID).Scan(&n); err != nil {
		return f, err
	}
	f.totp = n > 0
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM webauthn_credentials WHERE user_id=?`, userID).Scan(&f.passkeys); err != nil {
		return f, err
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id=? AND used_at IS NULL`, userID).Scan(&f.recovery); err != nil {
		return f, err
	}
	return f, nil
}

// Factors returns the caller's own second factors.
func (s *Service) Factors(ctx context.Context, p *Principal) (*FactorSummary, error) {
	if p == nil || p.Kind != "session" {
		return nil, ErrForbidden
	}
	f, err := s.factors(ctx, s.db, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := &FactorSummary{RecoveryCodesRemaining: f.recovery, Required: RequiresSecondFactor(p), Passkeys: []PasskeyInfo{}}
	out.EnrollmentRequired = out.Required && !f.any()
	var created string
	var confirmed, used sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT created_at,confirmed_at,last_used_at FROM totp_credentials WHERE user_id=?`, p.User.ID).Scan(&created, &confirmed, &used)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		out.TOTP = &TOTPInfo{Confirmed: confirmed.Valid, CreatedAt: pt(created), ConfirmedAt: ptNull(confirmed), LastUsedAt: ptNull(used)}
	}
	if out.Passkeys, err = s.listPasskeys(ctx, p.User.ID); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- recent authentication ---------------------------------------------

// requireRecentAuth refuses unless the session signed in or reauthenticated
// within Config.ReauthWindow.
func (s *Service) requireRecentAuth(ctx context.Context, p *Principal) error {
	if p == nil || p.Kind != "session" {
		return ErrForbidden
	}
	var created string
	var reauth sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT created_at,reauthenticated_at FROM sessions WHERE id=?`, p.SessionID).Scan(&created, &reauth); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReauthRequired
		}
		return err
	}
	last := pt(created)
	if reauth.Valid && pt(reauth.String).After(last) {
		last = pt(reauth.String)
	}
	if s.now().Sub(last) > s.cfg.ReauthWindow {
		return ErrReauthRequired
	}
	return nil
}

// Proof is the second-factor part of a login or reauthentication.
type Proof struct {
	Method     string // totp | recovery-code | passkey
	Code       string // totp or recovery code
	Credential []byte // passkey assertion JSON
}

// Reauthenticate proves identity again inside a session: the password, and
// one second factor when the user has any. Failures count against the same
// limits as login.
func (s *Service) Reauthenticate(ctx context.Context, p *Principal, password string, proof Proof, rp RelyingParty, src Source) error {
	if p == nil || p.Kind != "session" {
		return ErrForbidden
	}
	unlock := s.lockUser(p.User.ID)
	defer unlock()
	key := secondFactorKey(p.User.ID)
	if ok, _ := s.limiter.Check(key, src.IP, false); !ok {
		return ErrThrottled
	}
	_, hash, err := s.userByUsername(ctx, p.User.Username)
	if err != nil {
		return err
	}
	match, err := VerifyPassword(password, hash)
	if err != nil {
		return err
	}
	if !match {
		s.limiter.Failure(key, src.IP)
		s.record(ctx, s.db, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "reauthenticate", Scope: Global,
			ResourceKind: "session", ResourceID: p.SessionID, Result: ResultFailed, Message: "invalid credentials", TraceID: src.TraceID, SourceIP: src.IP})
		return ErrInvalidCredentials
	}
	f, err := s.factors(ctx, s.db, p.User.ID)
	if err != nil {
		return err
	}
	method := ""
	spend := func(*sql.Tx) error { return nil }
	if f.any() {
		var pk *passkeyCeremony
		if proof.Method == MethodPasskey {
			pk = s.ceremonies.take("reauth:" + p.SessionID)
		}
		var cerr error
		if spend, cerr = s.checkProof(ctx, &p.User, proof, pk, rp); cerr != nil && !errors.Is(cerr, ErrSecondFactor) {
			return cerr
		} else if cerr != nil {
			spend = func(*sql.Tx) error { return ErrSecondFactor }
		}
		method = proof.Method
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := spend(tx); err != nil {
		if !errors.Is(err, ErrSecondFactor) {
			return err
		}
		tx.Rollback()
		s.limiter.Failure(key, src.IP)
		s.record(ctx, s.db, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "reauthenticate", Scope: Global,
			ResourceKind: "session", ResourceID: p.SessionID, Result: ResultFailed, Message: "second factor rejected",
			After: map[string]any{"method": proof.Method}, TraceID: src.TraceID, SourceIP: src.IP})
		return ErrSecondFactor
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET reauthenticated_at=? WHERE id=?`, ts(s.now()), p.SessionID); err != nil {
		return err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "reauthenticate", Scope: Global,
		ResourceKind: "session", ResourceID: p.SessionID, Result: ResultOK, After: map[string]any{"method": method},
		TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.limiter.Success(key)
	return nil
}

// secondFactorKey is the limiter account key for second-factor and
// reauthentication attempts; '#' never occurs in a username.
func secondFactorKey(userID string) string { return "#2fa:" + userID }

// checkProof verifies one second factor for u without spending it and
// returns the statement that spends it (TOTP step advanced, recovery code
// marked used, passkey counter stored). spend runs inside the transaction
// that grants the result and re-checks with a conditional UPDATE, so of two
// concurrent requests with the same proof only one succeeds. All checking
// happens before that transaction: the store has a single connection, and
// the secret store cannot be read while a transaction holds it.
// A rejected proof is ErrSecondFactor, from checkProof or from spend, so
// callers cannot tell a wrong code from a missing method.
func (s *Service) checkProof(ctx context.Context, u *User, proof Proof, pk *passkeyCeremony, rp RelyingParty) (spend func(*sql.Tx) error, err error) {
	switch proof.Method {
	case MethodTOTP:
		return s.checkTOTP(ctx, u.ID, proof.Code)
	case MethodRecovery:
		return s.checkRecoveryCode(ctx, u.ID, proof.Code)
	case MethodPasskey:
		if pk == nil || pk.userID != u.ID || !s.now().Before(pk.expires) {
			return nil, ErrSecondFactor
		}
		return s.checkPasskeyAssertion(ctx, u, pk, rp, proof.Credential)
	}
	return nil, ErrSecondFactor
}

// ---- login challenges ----------------------------------------------------

type loginChallenge struct {
	userID   string
	username string
	methods  []string
	expires  time.Time
	attempts int
	passkey  *passkeyCeremony
}

// challengeStore keeps pending second steps. One per user: a new password
// login replaces the previous challenge.
type challengeStore struct {
	mu     sync.Mutex
	byHash map[string]*loginChallenge
	byUser map[string]string
}

func newChallengeStore() *challengeStore {
	return &challengeStore{byHash: map[string]*loginChallenge{}, byUser: map[string]string{}}
}

func (cs *challengeStore) put(token string, c *loginChallenge, now time.Time) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if old, ok := cs.byUser[c.userID]; ok {
		delete(cs.byHash, old)
	}
	if len(cs.byHash) > pruneAbove {
		for h, v := range cs.byHash {
			if !now.Before(v.expires) {
				delete(cs.byHash, h)
				delete(cs.byUser, v.userID)
			}
		}
	}
	h := hashToken(token)
	cs.byHash[h] = c
	cs.byUser[c.userID] = h
}

// get returns a copy of a live challenge, or nil. Callers never hold the
// stored value, so concurrent requests on one challenge do not race.
func (cs *challengeStore) get(token string, now time.Time) *loginChallenge {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	h := hashToken(token)
	c := cs.byHash[h]
	if c == nil {
		return nil
	}
	if !now.Before(c.expires) {
		cs.dropLocked(h, c)
		return nil
	}
	cp := *c
	return &cp
}

// setPasskey attaches a passkey ceremony to a live challenge.
func (cs *challengeStore) setPasskey(token string, pk *passkeyCeremony) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c := cs.byHash[hashToken(token)]
	if c == nil {
		return false
	}
	c.passkey = pk
	return true
}

func (cs *challengeStore) drop(token string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	h := hashToken(token)
	if c := cs.byHash[h]; c != nil {
		cs.dropLocked(h, c)
	}
}

func (cs *challengeStore) dropLocked(h string, c *loginChallenge) {
	delete(cs.byHash, h)
	if cs.byUser[c.userID] == h {
		delete(cs.byUser, c.userID)
	}
}

// failure counts a wrong answer; true means the challenge is now spent.
func (cs *challengeStore) failure(token string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	h := hashToken(token)
	c := cs.byHash[h]
	if c == nil {
		return true
	}
	c.attempts++
	if c.attempts >= MaxChallengeAttempts {
		cs.dropLocked(h, c)
		return true
	}
	return false
}

// newChallenge starts the second step for u after a correct password.
func (s *Service) newChallenge(ctx context.Context, u *User, f factorState, src Source) (*SecondFactorChallenge, error) {
	var methods []string
	if f.passkeys > 0 && src.RPID != "" {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM webauthn_credentials WHERE user_id=? AND rp_id=?`, u.ID, src.RPID).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			methods = append(methods, MethodPasskey)
		}
	}
	if f.totp {
		methods = append(methods, MethodTOTP)
	}
	if f.recovery > 0 {
		methods = append(methods, MethodRecovery)
	}
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	exp := s.now().Add(s.cfg.ChallengeTTL)
	s.challenges.put(token, &loginChallenge{userID: u.ID, username: u.Username, methods: methods, expires: exp}, s.now())
	if methods == nil {
		methods = []string{}
	}
	return &SecondFactorChallenge{Token: token, Methods: methods, ExpiresAt: exp}, nil
}

// lockUser serializes second-factor attempts of one account. The limiter
// and the challenge's attempt count then see every answer before the next
// is checked, so a burst of parallel guesses gets no extra tries and two
// valid proofs racing on one challenge open one session.
func (s *Service) lockUser(id string) func() {
	s.userMu.Lock()
	m := s.userLocks[id]
	if m == nil {
		m = &sync.Mutex{}
		s.userLocks[id] = m
	}
	s.userMu.Unlock()
	m.Lock()
	return m.Unlock
}

// CompleteSecondFactor finishes a login started by Login. An unknown,
// expired or spent challenge is ErrChallengeInvalid; a wrong answer is
// ErrSecondFactor (the challenge survives until MaxChallengeAttempts).
func (s *Service) CompleteSecondFactor(ctx context.Context, challengeToken string, proof Proof, rp RelyingParty, src Source) (*LoginResult, error) {
	c := s.challenges.get(challengeToken, s.now())
	if c == nil {
		return nil, ErrChallengeInvalid
	}
	unlock := s.lockUser(c.userID)
	defer unlock()
	// Re-read under the lock: another answer may have spent it meanwhile.
	if c = s.challenges.get(challengeToken, s.now()); c == nil {
		return nil, ErrChallengeInvalid
	}
	key := secondFactorKey(c.userID)
	if ok, retry := s.limiter.Check(key, src.IP, false); !ok {
		return &LoginResult{RetryAfter: retry}, ErrThrottled
	}
	p, err := s.principal(ctx, c.userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrChallengeInvalid
		}
		return nil, err
	}
	if p.User.Status != StatusActive {
		s.challenges.drop(challengeToken)
		return nil, ErrChallengeInvalid
	}
	allowed := false
	for _, m := range c.methods {
		allowed = allowed || m == proof.Method
	}
	spend := func(*sql.Tx) error { return ErrSecondFactor }
	if allowed {
		sp, cerr := s.checkProof(ctx, &p.User, proof, c.passkey, rp)
		if cerr != nil && !errors.Is(cerr, ErrSecondFactor) {
			return nil, cerr
		}
		if cerr == nil {
			spend = sp
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if verr := spend(tx); verr != nil {
		tx.Rollback()
		if !errors.Is(verr, ErrSecondFactor) {
			return nil, verr
		}
		spent := s.challenges.failure(challengeToken)
		s.limiter.Failure(key, src.IP)
		s.record(ctx, s.db, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "login.second-factor", Scope: Global,
			Result: ResultFailed, Message: "second factor rejected", After: map[string]any{"method": proof.Method, "challengeSpent": spent},
			TraceID: src.TraceID, SourceIP: src.IP})
		return nil, ErrSecondFactor
	}
	sess, err := s.openSessionTx(ctx, tx, &p.User, src, proof.Method)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.challenges.drop(challengeToken)
	s.limiter.Success(key)
	return &LoginResult{Session: sess, User: &p.User}, nil
}

// ---- TOTP -----------------------------------------------------------------

func totpConsumer(userID string) string { return "iam-totp:" + userID }

func (s *Service) vault() (*secrets.Service, error) {
	if s.cfg.Secrets == nil {
		return nil, fmt.Errorf("%w: the secret store is not configured", ErrUnavailable)
	}
	return s.cfg.Secrets, nil
}

// BeginTOTP generates a seed for the caller and returns it once. It needs a
// recent authentication. A confirmed TOTP must be removed first; an
// unconfirmed one is replaced.
func (s *Service) BeginTOTP(ctx context.Context, p *Principal, src Source) (*TOTPEnrollment, error) {
	if err := s.requireRecentAuth(ctx, p); err != nil {
		return nil, err
	}
	vault, err := s.vault()
	if err != nil {
		return nil, err
	}
	var oldID, oldSecret string
	var confirmed sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT id,secret_id,confirmed_at FROM totp_credentials WHERE user_id=?`, p.User.ID).Scan(&oldID, &oldSecret, &confirmed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	case confirmed.Valid:
		return nil, fmt.Errorf("%w: remove the current authenticator app before adding another", ErrConflict)
	}
	seed, err := NewTOTPSecret()
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	sec, err := vault.Create(ctx, secrets.CreateInput{Kind: secrets.KindGeneric, Name: "iam/totp/" + p.User.ID + "/" + id, Value: seed,
		Scope: []string{totpConsumer(p.User.ID)}, Actor: "user:" + p.User.ID})
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := s.storePendingTOTP(ctx, p, oldID, id, sec.ID, now, src); err != nil {
		s.dropSecret(ctx, sec.ID, p.User.ID) // the transaction is closed by now
		return nil, err
	}
	if oldSecret != "" {
		s.dropSecret(ctx, oldSecret, p.User.ID)
	}
	return &TOTPEnrollment{Secret: EncodeTOTPSecret(seed), URI: TOTPURI("HazelminX", p.User.Username, seed), Digits: TOTPDigits,
		PeriodSeconds: int(TOTPPeriod / time.Second), ServerTime: now, ExpiresAt: now.Add(totpEnrollmentTTL)}, nil
}

func (s *Service) storePendingTOTP(ctx context.Context, p *Principal, oldID, id, secretID string, now time.Time, src Source) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if oldID != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM totp_credentials WHERE id=?`, oldID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO totp_credentials(id,user_id,secret_id,created_at) VALUES(?,?,?,?)`, id, p.User.ID, secretID, ts(now)); err != nil {
		return err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "totp.begin", Scope: Global,
		ResourceKind: "user", ResourceID: p.User.ID, Result: ResultOK, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return err
	}
	return tx.Commit()
}

// ConfirmTOTP activates the pending seed with a first code. When it is the
// user's first factor, a recovery-code set is issued and returned once.
func (s *Service) ConfirmTOTP(ctx context.Context, p *Principal, code string, src Source) (*EnrollmentResult, error) {
	if p == nil || p.Kind != "session" {
		return nil, ErrForbidden
	}
	unlock := s.lockUser(p.User.ID)
	defer unlock()
	key := secondFactorKey(p.User.ID)
	if ok, _ := s.limiter.Check(key, src.IP, false); !ok {
		return nil, ErrThrottled
	}
	var id, secretID, created string
	var confirmed sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,secret_id,created_at,confirmed_at FROM totp_credentials WHERE user_id=?`, p.User.ID).Scan(&id, &secretID, &created, &confirmed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && confirmed.Valid) {
		return nil, fmt.Errorf("%w: no authenticator app enrolment is pending", ErrConflict)
	}
	if err != nil {
		return nil, err
	}
	if s.now().Sub(pt(created)) > totpEnrollmentTTL {
		return nil, fmt.Errorf("%w: the enrolment expired; start again", ErrConflict)
	}
	seed, err := s.revealSeed(ctx, secretID, p.User.ID)
	if err != nil {
		return nil, err
	}
	step, ok := VerifyTOTP(seed, code, s.now(), 0)
	if !ok {
		s.limiter.Failure(key, src.IP)
		s.record(ctx, s.db, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "totp.enroll", Scope: Global,
			ResourceKind: "user", ResourceID: p.User.ID, Result: ResultFailed, Message: "code rejected", TraceID: src.TraceID, SourceIP: src.IP})
		return nil, ErrSecondFactor
	}
	fresh, err := s.prepareRecoveryCodes(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now()
	res, err := tx.ExecContext(ctx, `UPDATE totp_credentials SET confirmed_at=?, last_used_step=?, last_used_at=? WHERE id=? AND confirmed_at IS NULL`, ts(now), step, ts(now), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("%w: no authenticator app enrolment is pending", ErrConflict)
	}
	out := &EnrollmentResult{}
	if out.RecoveryCodes, err = s.issueRecoveryCodesIfNone(ctx, tx, p.User.ID, fresh); err != nil {
		return nil, err
	}
	ended, err := s.endPasswordOnlySessions(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "totp.enroll", Scope: Global,
		ResourceKind: "user", ResourceID: p.User.ID, Result: ResultOK, After: map[string]any{"recoveryCodesIssued": len(out.RecoveryCodes), "otherSessionsRevoked": ended},
		TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.limiter.Success(key)
	return out, nil
}

// RemoveTOTP deletes the caller's authenticator-app factor.
func (s *Service) RemoveTOTP(ctx context.Context, p *Principal, src Source) error {
	if err := s.requireRecentAuth(ctx, p); err != nil {
		return err
	}
	var id, secretID string
	var confirmed sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,secret_id,confirmed_at FROM totp_credentials WHERE user_id=?`, p.User.ID).Scan(&id, &secretID, &confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := s.removeFactor(ctx, p, src, "totp.remove", confirmed.Valid, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM totp_credentials WHERE id=?`, id)
		return err
	}, map[string]any{"factor": MethodTOTP}); err != nil {
		return err
	}
	s.dropSecret(ctx, secretID, p.User.ID)
	return nil
}

// removeFactor runs del inside a transaction after the last-factor check.
// counts says whether the removed row was a usable factor. When no factor
// remains the recovery codes go too (they only make sense beside one); the
// user's other sessions are ended either way.
func (s *Service) removeFactor(ctx context.Context, p *Principal, src Source, action string, counts bool, del func(*sql.Tx) error, after map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := s.factors(ctx, tx, p.User.ID)
	if err != nil {
		return err
	}
	remaining := f
	if counts {
		if after["factor"] == MethodTOTP {
			remaining.totp = false
		} else {
			remaining.passkeys--
		}
	}
	if !remaining.any() && f.any() && RequiresSecondFactor(p) {
		return ErrLastFactor
	}
	if err := del(tx); err != nil {
		return err
	}
	if !remaining.any() {
		if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, p.User.ID); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND id<>? AND revoked_at IS NULL`, ts(s.now()), p.User.ID, p.SessionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	after["otherSessionsRevoked"] = n
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: action, Scope: Global,
		ResourceKind: "user", ResourceID: p.User.ID, Result: ResultOK, After: after, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return err
	}
	return tx.Commit()
}

// endPasswordOnlySessions ends the user's other sessions that never passed a
// second factor. Called inside the transaction that adds a factor: until
// then an enforced account's password-only sessions were held to
// enrolment, and they must not turn into full sessions now.
func (s *Service) endPasswordOnlySessions(ctx context.Context, tx *sql.Tx, p *Principal) (int64, error) {
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND id<>? AND second_factor='' AND revoked_at IS NULL`,
		ts(s.now()), p.User.ID, p.SessionID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// dropSecret deletes a TOTP seed nothing refers to any more. The credential
// row is already gone, so a failure only leaves an unreachable ciphertext;
// it is logged (by id, never the value) for the operator to clean up.
func (s *Service) dropSecret(ctx context.Context, secretID, userID string) {
	vault, err := s.vault()
	if err == nil {
		err = vault.Delete(ctx, secretID, "user:"+userID)
	}
	if err != nil {
		slog.Warn("iam: could not delete an unused TOTP seed from the secret store", "secretId", secretID, "error", err)
	}
}

func (s *Service) revealSeed(ctx context.Context, secretID, userID string) ([]byte, error) {
	vault, err := s.vault()
	if err != nil {
		return nil, err
	}
	seed, _, err := vault.Reveal(ctx, secrets.RevealRequest{ID: secretID, Consumer: totpConsumer(userID)})
	return seed, err
}

// checkTOTP accepts a code once: the stored step only moves forward, and
// spend re-checks it so two concurrent requests with the same code cannot
// both succeed.
func (s *Service) checkTOTP(ctx context.Context, userID, code string) (func(*sql.Tx) error, error) {
	var id, secretID string
	var last int64
	err := s.db.QueryRowContext(ctx, `SELECT id,secret_id,last_used_step FROM totp_credentials WHERE user_id=? AND confirmed_at IS NOT NULL`, userID).Scan(&id, &secretID, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecondFactor
	}
	if err != nil {
		return nil, err
	}
	seed, err := s.revealSeed(ctx, secretID, userID)
	if err != nil {
		return nil, err
	}
	step, ok := VerifyTOTP(seed, code, s.now(), last)
	if !ok {
		return nil, ErrSecondFactor
	}
	return func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE totp_credentials SET last_used_step=?, last_used_at=? WHERE id=? AND last_used_step<?`, step, ts(s.now()), id, step)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrSecondFactor
		}
		return nil
	}, nil
}

// ---- recovery codes ---------------------------------------------------------

const recoveryKeyName = "iam/recovery-code-key"
const recoveryKeyConsumer = "iam-recovery-codes"

// recoveryKey is the HMAC key recovery codes are hashed with. It lives in
// the secret store, so a copy of the database alone cannot be searched
// offline for the 40-bit codes; with the master key an attacker would hold
// the TOTP seeds anyway.
func (s *Service) recoveryKey(ctx context.Context) ([]byte, error) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if s.recoveryHMAC != nil {
		return s.recoveryHMAC, nil
	}
	vault, err := s.vault()
	if err != nil {
		return nil, err
	}
	list, err := vault.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, sec := range list {
		if sec.Name == recoveryKeyName {
			k, _, err := vault.Reveal(ctx, secrets.RevealRequest{ID: sec.ID, Consumer: recoveryKeyConsumer})
			if err != nil {
				return nil, err
			}
			s.recoveryHMAC = k
			return k, nil
		}
	}
	k := make([]byte, 32)
	if _, err := randRead(k); err != nil {
		return nil, err
	}
	if _, err := vault.Create(ctx, secrets.CreateInput{Kind: secrets.KindGeneric, Name: recoveryKeyName, Value: k,
		Scope: []string{recoveryKeyConsumer}, Actor: "system:iam"}); err != nil {
		return nil, err
	}
	s.recoveryHMAC = k
	return k, nil
}

func (s *Service) hashRecoveryCode(ctx context.Context, userID, canonical string) (string, error) {
	k, err := s.recoveryKey(ctx)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, k)
	m.Write([]byte(userID + ":" + canonical))
	return "hmac-sha256:" + hex.EncodeToString(m.Sum(nil)), nil
}

// recoverySet is a freshly generated set: plain codes for one-time display
// and their hashes for storage. It is prepared outside any transaction
// because hashing may need the secret store.
type recoverySet struct {
	codes  []string
	hashes []string
}

func (s *Service) prepareRecoveryCodes(ctx context.Context, userID string) (*recoverySet, error) {
	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, err
	}
	set := &recoverySet{codes: codes}
	for _, c := range codes {
		canon, ok := NormalizeRecoveryCode(c)
		if !ok {
			return nil, fmt.Errorf("iam: generated recovery code %d does not normalize", len(set.hashes))
		}
		h, err := s.hashRecoveryCode(ctx, userID, canon)
		if err != nil {
			return nil, err
		}
		set.hashes = append(set.hashes, h)
	}
	return set, nil
}

// replaceRecoveryCodes deletes the user's codes and stores set.
func (s *Service) replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, userID string, set *recoverySet) ([]string, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
		return nil, err
	}
	now := ts(s.now())
	for _, h := range set.hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes(id,user_id,code_hash,created_at) VALUES(?,?,?,?)`, uuid.NewString(), userID, h, now); err != nil {
			return nil, err
		}
	}
	return set.codes, nil
}

// issueRecoveryCodesIfNone stores set when the user has no unused codes and
// returns the codes to show; otherwise it stores nothing and returns nil.
func (s *Service) issueRecoveryCodesIfNone(ctx context.Context, tx *sql.Tx, userID string, set *recoverySet) ([]string, error) {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id=? AND used_at IS NULL`, userID).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, nil
	}
	return s.replaceRecoveryCodes(ctx, tx, userID, set)
}

// RegenerateRecoveryCodes replaces the caller's set; old codes stop working
// at once. It needs a recent authentication and an enrolled factor.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p *Principal, src Source) ([]string, error) {
	if err := s.requireRecentAuth(ctx, p); err != nil {
		return nil, err
	}
	set, err := s.prepareRecoveryCodes(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	f, err := s.factors(ctx, tx, p.User.ID)
	if err != nil {
		return nil, err
	}
	if !f.any() {
		return nil, fmt.Errorf("%w: add an authenticator app or passkey first", ErrConflict)
	}
	codes, err := s.replaceRecoveryCodes(ctx, tx, p.User.ID, set)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "recovery-codes.regenerate", Scope: Global,
		ResourceKind: "user", ResourceID: p.User.ID, Result: ResultOK, Before: map[string]any{"unused": f.recovery},
		After: map[string]any{"unused": len(codes)}, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

// checkRecoveryCode returns the statement that marks one unused code as
// used. That conditional UPDATE is the single point of truth: a code
// accepted once can never match again.
func (s *Service) checkRecoveryCode(ctx context.Context, userID, code string) (func(*sql.Tx) error, error) {
	canon, ok := NormalizeRecoveryCode(code)
	if !ok {
		return nil, ErrSecondFactor
	}
	h, err := s.hashRecoveryCode(ctx, userID, canon)
	if err != nil {
		return nil, err
	}
	return func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE recovery_codes SET used_at=? WHERE user_id=? AND code_hash=? AND used_at IS NULL`, ts(s.now()), userID, h)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrSecondFactor
		}
		return nil
	}, nil
}

func ptNull(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t := pt(v.String)
	return &t
}

// normalizeDomain lower-cases a host name and strips a trailing dot.
func normalizeDomain(d string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
}

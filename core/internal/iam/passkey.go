package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

// Passkeys (WebAuthn L3) as a second factor, through go-webauthn.
//
// A passkey belongs to the domain (RP ID) it was created on; the browser
// refuses to use it anywhere else. Each credential row therefore records its
// rp_id, a ceremony only offers credentials of the current RP ID, and
// PasskeyDomainImpact tells an administrator, before the management domain
// changes, which passkeys would stop working and who would be left without
// any second factor (IAM-011, T-130).

// RelyingParty is where the browser is talking to us: the RP ID (a domain,
// no scheme or port) and the exact origin. The API layer derives it from
// the configured public origin or, failing that, from the request.
type RelyingParty struct {
	ID     string
	Origin string
}

// Usable reports why passkeys cannot work for rp, or nil. WebAuthn needs a
// domain name (not an IP address) and a secure context: HTTPS, or plain
// HTTP on localhost.
func (rp RelyingParty) Usable() error {
	if rp.ID == "" || rp.Origin == "" {
		return fmt.Errorf("%w: passkeys need the panel's address", ErrUnavailable)
	}
	if net.ParseIP(rp.ID) != nil {
		return fmt.Errorf("%w: passkeys need the panel to be opened by a domain name, not an IP address", ErrUnavailable)
	}
	u, err := url.Parse(rp.Origin)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("%w: invalid origin", ErrUnavailable)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (rp.ID == "localhost" || strings.HasSuffix(rp.ID, ".localhost"))) {
		return fmt.Errorf("%w: passkeys need HTTPS", ErrUnavailable)
	}
	if !domainCovers(rp.ID, normalizeDomain(u.Hostname())) {
		return fmt.Errorf("%w: origin %s is not within %s", ErrUnavailable, rp.Origin, rp.ID)
	}
	return nil
}

func (s *Service) relyingParty(rp RelyingParty) (*webauthn.WebAuthn, error) {
	if err := rp.Usable(); err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{RPID: rp.ID, RPDisplayName: "HazelminX", RPOrigins: []string{rp.Origin}})
}

// PasskeyInfo is one passkey as its owner sees it.
type PasskeyInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	RPID       string     `json:"rpId"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// waUser adapts a user to webauthn.User. The user handle is the account ID,
// which carries no personal data.
type waUser struct {
	u     *User
	creds []webauthn.Credential
}

func (w *waUser) WebAuthnID() []byte                         { return []byte(w.u.ID) }
func (w *waUser) WebAuthnName() string                       { return w.u.Username }
func (w *waUser) WebAuthnDisplayName() string                { return firstNonEmpty(w.u.DisplayName, w.u.Username) }
func (w *waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type storedCredential struct {
	id   string
	cred webauthn.Credential
	raw  string
}

// credentials loads the user's passkeys for one RP ID ("" = all).
func (s *Service) credentials(ctx context.Context, userID, rpID string) ([]storedCredential, error) {
	q := `SELECT id,credential_json FROM webauthn_credentials WHERE user_id=?`
	args := []any{userID}
	if rpID != "" {
		q += ` AND rp_id=?`
		args = append(args, rpID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedCredential
	for rows.Next() {
		var sc storedCredential
		if err := rows.Scan(&sc.id, &sc.raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(sc.raw), &sc.cred); err != nil {
			return nil, fmt.Errorf("iam: passkey %s is unreadable: %w", sc.id, err)
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Service) listPasskeys(ctx context.Context, userID string) ([]PasskeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,rp_id,created_at,last_used_at FROM webauthn_credentials WHERE user_id=? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PasskeyInfo{}
	for rows.Next() {
		var pi PasskeyInfo
		var c string
		var used sql.NullString
		if err := rows.Scan(&pi.ID, &pi.Name, &pi.RPID, &c, &used); err != nil {
			return nil, err
		}
		pi.CreatedAt, pi.LastUsedAt = pt(c), ptNull(used)
		out = append(out, pi)
	}
	return out, rows.Err()
}

// ---- ceremony state --------------------------------------------------------

// passkeyCeremony is the server half of one WebAuthn ceremony. It lives in
// memory for ceremonyTTL and is taken (removed) when the browser answers,
// so a challenge is never accepted twice.
type passkeyCeremony struct {
	userID  string
	rpID    string
	data    *webauthn.SessionData
	expires time.Time
}

const ceremonyTTL = 10 * time.Minute

type ceremonyStore struct {
	mu sync.Mutex
	m  map[string]*passkeyCeremony
}

func newCeremonyStore() *ceremonyStore { return &ceremonyStore{m: map[string]*passkeyCeremony{}} }

func (cs *ceremonyStore) put(key string, c *passkeyCeremony) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.m) > pruneAbove {
		now := time.Now()
		for k, v := range cs.m {
			if !now.Before(v.expires) {
				delete(cs.m, k)
			}
		}
	}
	cs.m[key] = c
}

func (cs *ceremonyStore) take(key string) *passkeyCeremony {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c := cs.m[key]
	delete(cs.m, key)
	return c
}

// ---- registration ------------------------------------------------------------

// BeginPasskeyRegistration returns the options for navigator.credentials.create().
// It needs a recent authentication and a usable relying party.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, p *Principal, rp RelyingParty) (json.RawMessage, error) {
	if err := s.requireRecentAuth(ctx, p); err != nil {
		return nil, err
	}
	wa, err := s.relyingParty(rp)
	if err != nil {
		return nil, err
	}
	existing, err := s.credentials(ctx, p.User.ID, "")
	if err != nil {
		return nil, err
	}
	var exclude []protocol.CredentialDescriptor
	for _, c := range existing {
		exclude = append(exclude, c.cred.Descriptor())
	}
	creation, data, err := wa.BeginRegistration(&waUser{u: &p.User},
		webauthn.WithExclusions(exclude),
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementPreferred, UserVerification: protocol.VerificationPreferred}),
	)
	if err != nil {
		return nil, err
	}
	s.ceremonies.put("register:"+p.SessionID, &passkeyCeremony{userID: p.User.ID, rpID: rp.ID, data: data, expires: s.now().Add(ceremonyTTL)})
	return json.Marshal(creation)
}

// FinishPasskeyRegistration verifies the browser's attestation and stores
// the passkey. When it is the user's first factor a recovery-code set is
// issued and returned once.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, p *Principal, rp RelyingParty, name string, credential []byte, src Source) (*EnrollmentResult, error) {
	if err := s.requireRecentAuth(ctx, p); err != nil {
		return nil, err
	}
	c := s.ceremonies.take("register:" + p.SessionID)
	if c == nil || c.userID != p.User.ID || c.rpID != rp.ID || !s.now().Before(c.expires) {
		return nil, fmt.Errorf("%w: no passkey registration is pending; start again", ErrConflict)
	}
	wa, err := s.relyingParty(rp)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return nil, fmt.Errorf("%w: the passkey response could not be read", ErrValidation)
	}
	cred, err := wa.CreateCredential(&waUser{u: &p.User}, *c.data, parsed)
	if err != nil {
		s.record(ctx, s.db, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "passkey.register", Scope: Global,
			ResourceKind: "user", ResourceID: p.User.ID, Result: ResultFailed, Message: "attestation rejected", TraceID: src.TraceID, SourceIP: src.IP})
		return nil, ErrSecondFactor
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	fresh, err := s.prepareRecoveryCodes(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	info := &PasskeyInfo{ID: uuid.NewString(), Name: name, RPID: rp.ID, CreatedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO webauthn_credentials(id,user_id,credential_id,rp_id,name,credential_json,created_at) VALUES(?,?,?,?,?,?,?)`,
		info.ID, p.User.ID, cred.ID, rp.ID, name, string(raw), ts(now)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("%w: this passkey is already registered", ErrConflict)
		}
		return nil, err
	}
	out := &EnrollmentResult{Passkey: info}
	if out.RecoveryCodes, err = s.issueRecoveryCodesIfNone(ctx, tx, p.User.ID, fresh); err != nil {
		return nil, err
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "passkey.register", Scope: Global,
		ResourceKind: "passkey", ResourceID: info.ID, Result: ResultOK,
		After: map[string]any{"name": name, "rpId": rp.ID, "recoveryCodesIssued": len(out.RecoveryCodes)}, TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// RemovePasskey deletes one of the caller's passkeys. Another user's ID is
// ErrNotFound.
func (s *Service) RemovePasskey(ctx context.Context, p *Principal, id string, src Source) error {
	if err := s.requireRecentAuth(ctx, p); err != nil {
		return err
	}
	var name, rpID string
	err := s.db.QueryRowContext(ctx, `SELECT name,rp_id FROM webauthn_credentials WHERE id=? AND user_id=?`, id, p.User.ID).Scan(&name, &rpID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.removeFactor(ctx, p, src, "passkey.remove", true, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM webauthn_credentials WHERE id=? AND user_id=?`, id, p.User.ID)
		return err
	}, map[string]any{"factor": MethodPasskey, "passkeyId": id, "name": name, "rpId": rpID})
}

// ---- assertion -----------------------------------------------------------------

// BeginPasskeyLogin returns navigator.credentials.get() options for the
// second step of a login. Only passkeys created for rp.ID are offered.
func (s *Service) BeginPasskeyLogin(ctx context.Context, challengeToken string, rp RelyingParty) (json.RawMessage, error) {
	c := s.challenges.get(challengeToken, s.now())
	if c == nil {
		return nil, ErrChallengeInvalid
	}
	pk, opts, err := s.beginAssertion(ctx, c.userID, rp)
	if err != nil {
		return nil, err
	}
	s.challenges.mu.Lock()
	c.passkey = pk
	s.challenges.mu.Unlock()
	return opts, nil
}

// BeginPasskeyReauth returns assertion options for Reauthenticate.
func (s *Service) BeginPasskeyReauth(ctx context.Context, p *Principal, rp RelyingParty) (json.RawMessage, error) {
	if p == nil || p.Kind != "session" {
		return nil, ErrForbidden
	}
	pk, opts, err := s.beginAssertion(ctx, p.User.ID, rp)
	if err != nil {
		return nil, err
	}
	s.ceremonies.put("reauth:"+p.SessionID, pk)
	return opts, nil
}

func (s *Service) beginAssertion(ctx context.Context, userID string, rp RelyingParty) (*passkeyCeremony, json.RawMessage, error) {
	wa, err := s.relyingParty(rp)
	if err != nil {
		return nil, nil, err
	}
	pr, err := s.principal(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	creds, err := s.credentials(ctx, userID, rp.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(creds) == 0 {
		return nil, nil, fmt.Errorf("%w: no passkey of this account was created for %s", ErrUnavailable, rp.ID)
	}
	u := &waUser{u: &pr.User}
	for _, c := range creds {
		u.creds = append(u.creds, c.cred)
	}
	assertion, data, err := wa.BeginLogin(u, webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		return nil, nil, err
	}
	opts, err := json.Marshal(assertion)
	if err != nil {
		return nil, nil, err
	}
	return &passkeyCeremony{userID: userID, rpID: rp.ID, data: data, expires: s.now().Add(ceremonyTTL)}, opts, nil
}

// checkPasskeyAssertion verifies an assertion against the ceremony and
// returns the statement that stores the new signature counter. A cloned
// authenticator (counter went backwards) is rejected.
func (s *Service) checkPasskeyAssertion(ctx context.Context, u *User, pk *passkeyCeremony, rp RelyingParty, body []byte) (func(*sql.Tx) error, error) {
	if pk.rpID != rp.ID {
		return nil, ErrSecondFactor
	}
	wa, err := s.relyingParty(rp)
	if err != nil {
		return nil, ErrSecondFactor
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return nil, ErrSecondFactor
	}
	creds, err := s.credentials(ctx, u.ID, rp.ID)
	if err != nil {
		return nil, err
	}
	wu := &waUser{u: u}
	for _, c := range creds {
		wu.creds = append(wu.creds, c.cred)
	}
	cred, err := wa.ValidateLogin(wu, *pk.data, parsed)
	if err != nil || cred.Authenticator.CloneWarning {
		return nil, ErrSecondFactor
	}
	var row *storedCredential
	for i := range creds {
		if string(creds[i].cred.ID) == string(cred.ID) {
			row = &creds[i]
		}
	}
	if row == nil {
		return nil, ErrSecondFactor
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	return func(tx *sql.Tx) error {
		// Compare-and-set on the stored record: a concurrent use of the same
		// assertion finds the record already moved on and fails.
		res, err := tx.ExecContext(ctx, `UPDATE webauthn_credentials SET credential_json=?, last_used_at=? WHERE id=? AND credential_json=?`,
			string(raw), ts(s.now()), row.id, row.raw)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrSecondFactor
		}
		return nil
	}, nil
}

// ---- domain change impact ------------------------------------------------------------

// DomainImpact answers "what happens to passkeys if the management domain
// becomes Domain" before anyone changes it (IAM-011, T-130).
type DomainImpact struct {
	Domain          string        `json:"domain"`
	PasskeysTotal   int           `json:"passkeysTotal"`
	PasskeysLost    int           `json:"passkeysLost"`
	Users           []UserImpact  `json:"users"`
	UsersLockedOut  int           `json:"usersWithoutSecondFactor"`
	Safe            bool          `json:"safe"`
	LostPasskeys    []LostPasskey `json:"lostPasskeys"`
	SuggestedAction []string      `json:"suggestedActions"`
}

// UserImpact is one affected user. KeepsSecondFactor is false when, after
// the change, the user has neither TOTP nor a passkey valid for the new
// domain; they could then only sign in with a recovery code, and enforced
// roles would be held to enrolment again.
type UserImpact struct {
	UserID                 string `json:"userId"`
	Username               string `json:"username"`
	PasskeysLost           int    `json:"passkeysLost"`
	PasskeysKept           int    `json:"passkeysKept"`
	HasTOTP                bool   `json:"hasTotp"`
	RecoveryCodesRemaining int    `json:"recoveryCodesRemaining"`
	KeepsSecondFactor      bool   `json:"keepsSecondFactor"`
	Enforced               bool   `json:"secondFactorRequired"`
}

// LostPasskey is one credential that would stop working.
type LostPasskey struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	RPID     string `json:"rpId"`
}

// domainCovers reports whether a credential for rpID can be used on host:
// host equals rpID or is a subdomain of it (WebAuthn RP ID rule).
func domainCovers(rpID, host string) bool {
	rpID, host = normalizeDomain(rpID), normalizeDomain(host)
	return host == rpID || strings.HasSuffix(host, "."+rpID)
}

var domainLabel = func(l string) bool {
	if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for _, r := range l {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// PasskeyDomainImpact lists the passkeys that a move to domain would
// invalidate and the users who would be left without a second factor. It
// changes nothing. Requires platform:write (the right that changes the
// management address).
func (s *Service) PasskeyDomainImpact(ctx context.Context, actor *Principal, domain string, src Source) (*DomainImpact, error) {
	if !Authorize(actor, ActionPlatformWrite, Global) {
		s.RecordDenied(ctx, actor, ActionPlatformWrite, Global, src)
		return nil, ErrForbidden
	}
	domain = normalizeDomain(domain)
	labels := strings.Split(domain, ".")
	valid := len(domain) <= 253 && len(labels) >= 1 && net.ParseIP(domain) == nil
	for _, l := range labels {
		valid = valid && domainLabel(l)
	}
	if !valid {
		return nil, fmt.Errorf("%w: domain must be a host name such as panel.example.com (no scheme, port or IP address)", ErrValidation)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT w.id,w.name,w.rp_id,u.id,u.username FROM webauthn_credentials w JOIN users u ON u.id=w.user_id ORDER BY u.username, w.created_at`)
	if err != nil {
		return nil, err
	}
	out := &DomainImpact{Domain: domain, Users: []UserImpact{}, LostPasskeys: []LostPasskey{}, SuggestedAction: []string{}}
	per := map[string]*UserImpact{}
	for rows.Next() {
		var lp LostPasskey
		if err := rows.Scan(&lp.ID, &lp.Name, &lp.RPID, &lp.UserID, &lp.Username); err != nil {
			rows.Close()
			return nil, err
		}
		out.PasskeysTotal++
		ui := per[lp.UserID]
		if ui == nil {
			ui = &UserImpact{UserID: lp.UserID, Username: lp.Username}
			per[lp.UserID] = ui
		}
		if domainCovers(lp.RPID, domain) {
			ui.PasskeysKept++
		} else {
			ui.PasskeysLost++
			out.PasskeysLost++
			out.LostPasskeys = append(out.LostPasskeys, lp)
		}
	}
	rows.Close()
	for id, ui := range per {
		if ui.PasskeysLost == 0 {
			continue
		}
		f, err := s.factors(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		pr, err := s.principal(ctx, id)
		if err != nil {
			return nil, err
		}
		ui.HasTOTP, ui.RecoveryCodesRemaining, ui.Enforced = f.totp, f.recovery, RequiresSecondFactor(pr)
		ui.KeepsSecondFactor = ui.HasTOTP || ui.PasskeysKept > 0
		if !ui.KeepsSecondFactor {
			out.UsersLockedOut++
		}
		out.Users = append(out.Users, *ui)
	}
	sort.Slice(out.Users, func(i, j int) bool { return out.Users[i].Username < out.Users[j].Username })
	out.Safe = out.UsersLockedOut == 0
	if out.PasskeysLost > 0 {
		out.SuggestedAction = append(out.SuggestedAction, "register-passkey-on-new-domain")
	}
	if out.UsersLockedOut > 0 {
		out.SuggestedAction = append(out.SuggestedAction, "enroll-totp-before-domain-change", "check-recovery-codes")
	}
	return out, nil
}

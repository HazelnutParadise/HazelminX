package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// fakeGitHub plays GitHub for the service. Tokens are plain strings mapped
// to identities; nothing reaches the network.
type fakeGitHub struct {
	mu sync.Mutex
	// users maps a token to who it is and what headers GitHub sends.
	users     map[string]User
	facts     map[string]TokenFacts
	canList   map[string]bool
	apps      map[int64]AppInfo        // by App id
	appKeys   map[int64]*rsa.PublicKey // key that signs for an App
	manifests map[string]ManifestApp   // by conversion code
	// codes maps an authorization code to the token it yields and the
	// PKCE verifier it requires ("" = none).
	codes        map[string]codeGrant
	refresh      map[string]UserToken // refresh token → next tokens
	refreshErr   error
	installs     map[int64]Installation
	userInstalls map[string][]int64 // user token → installation ids visible
	instRepos    map[int64][]int64  // installation → repositories the user reaches; unset = [100]
	instTokens   []Token            // next installation tokens, in order
	hook         HookConfig
	delivery     *HookDelivery
	down         bool

	calls map[string]int
	// lastVerifier is the PKCE verifier the last code exchange sent.
	lastVerifier string
	lastScope    InstallationTokenRequest

	r   *fakeRepos       // repository calls (fake_repos_test.go)
	now func() time.Time // the test clock
}

func (f *fakeGitHub) clockNow() time.Time {
	if f.now == nil {
		return time.Now()
	}
	return f.now()
}

type codeGrant struct {
	tok      UserToken
	verifier string // challenge source; "" = the flow had no PKCE
	used     bool
}

func newFake() *fakeGitHub {
	return &fakeGitHub{users: map[string]User{}, facts: map[string]TokenFacts{}, canList: map[string]bool{}, apps: map[int64]AppInfo{},
		appKeys: map[int64]*rsa.PublicKey{}, manifests: map[string]ManifestApp{}, codes: map[string]codeGrant{}, refresh: map[string]UserToken{},
		installs: map[int64]Installation{}, userInstalls: map[string][]int64{}, instRepos: map[int64][]int64{}, calls: map[string]int{}}
}

func (f *fakeGitHub) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeGitHub) enter(name string) error {
	f.calls[name]++
	if f.down {
		return &APIError{Kind: ErrUnavailable, Status: 503}
	}
	return nil
}

var refused = func(status int, reason string) error {
	return &APIError{Kind: ErrRejected, Status: status, Reason: reason}
}

// appFromJWT checks the JWT the way GitHub does: signature by the App's key.
func (f *fakeGitHub) appFromJWT(token string) (AppInfo, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AppInfo{}, refused(401, "bad jwt")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Iss string `json:"iss"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return AppInfo{}, refused(401, "bad jwt")
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	for id, pub := range f.appKeys {
		if claims.Iss == itoa(id) && rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig) == nil {
			if claims.Exp-claims.Iat > 600 {
				return AppInfo{}, refused(401, "jwt too long")
			}
			return f.apps[id], nil
		}
	}
	return AppInfo{}, refused(401, "A JSON web token could not be decoded")
}

func itoa(v int64) string { b, _ := json.Marshal(v); return string(b) }

func (f *fakeGitHub) ConvertManifest(_ context.Context, code string) (ManifestApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ConvertManifest"); err != nil {
		return ManifestApp{}, err
	}
	m, ok := f.manifests[code]
	if !ok {
		return ManifestApp{}, refused(404, "Not Found")
	}
	delete(f.manifests, code)
	return m, nil
}

func (f *fakeGitHub) GetApp(_ context.Context, jwt string) (AppInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetApp"); err != nil {
		return AppInfo{}, err
	}
	return f.appFromJWT(jwt)
}

func (f *fakeGitHub) GetInstallation(_ context.Context, jwt string, id int64) (Installation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetInstallation"); err != nil {
		return Installation{}, err
	}
	app, err := f.appFromJWT(jwt)
	if err != nil {
		return Installation{}, err
	}
	in, ok := f.installs[id]
	if !ok || in.AppID != app.ID {
		return Installation{}, refused(404, "Not Found")
	}
	return in, nil
}

func (f *fakeGitHub) CreateInstallationToken(_ context.Context, jwt string, id int64, req InstallationTokenRequest) (Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CreateInstallationToken"); err != nil {
		return Token{}, err
	}
	app, err := f.appFromJWT(jwt)
	if err != nil {
		return Token{}, err
	}
	if in, ok := f.installs[id]; !ok || in.AppID != app.ID {
		return Token{}, refused(404, "Not Found")
	}
	if len(f.instTokens) == 0 {
		return Token{}, errors.New("fake: no installation token queued")
	}
	t := f.instTokens[0]
	f.instTokens = f.instTokens[1:]
	f.lastScope = req
	return t, nil
}

func (f *fakeGitHub) GetAppHookConfig(_ context.Context, jwt string) (HookConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetAppHookConfig"); err != nil {
		return HookConfig{}, err
	}
	if _, err := f.appFromJWT(jwt); err != nil {
		return HookConfig{}, err
	}
	return f.hook, nil
}

func (f *fakeGitHub) LastAppHookDelivery(_ context.Context, jwt string) (*HookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("LastAppHookDelivery"); err != nil {
		return nil, err
	}
	return f.delivery, nil
}

func (f *fakeGitHub) ExchangeCode(_ context.Context, clientID, clientSecret, code, redirect, verifier string) (UserToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ExchangeCode"); err != nil {
		return UserToken{}, err
	}
	f.lastVerifier = verifier
	if clientSecret != "client-secret-1" {
		return UserToken{}, refused(0, "incorrect_client_credentials")
	}
	g, ok := f.codes[code]
	if !ok || g.used {
		return UserToken{}, refused(0, "bad_verification_code")
	}
	if g.verifier != verifier {
		return UserToken{}, refused(0, "bad_verification_code")
	}
	g.used = true
	f.codes[code] = g
	return g.tok, nil
}

func (f *fakeGitHub) RefreshUserToken(_ context.Context, _, _, refreshToken string) (UserToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("RefreshUserToken"); err != nil {
		return UserToken{}, err
	}
	if f.refreshErr != nil {
		return UserToken{}, f.refreshErr
	}
	t, ok := f.refresh[refreshToken]
	if !ok {
		return UserToken{}, refused(0, "bad_refresh_token")
	}
	delete(f.refresh, refreshToken) // single use, like GitHub
	return t, nil
}

func (f *fakeGitHub) GetUser(_ context.Context, token string) (User, TokenFacts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("GetUser"); err != nil {
		return User{}, TokenFacts{}, err
	}
	u, ok := f.users[token]
	if !ok {
		return User{}, TokenFacts{}, refused(401, "Bad credentials")
	}
	return u, f.facts[token], nil
}

func (f *fakeGitHub) UserInstallations(_ context.Context, token string) ([]Installation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("UserInstallations"); err != nil {
		return nil, err
	}
	var out []Installation
	for _, id := range f.userInstalls[token] {
		out = append(out, f.installs[id])
	}
	return out, nil
}

func (f *fakeGitHub) UserInstallationRepositories(_ context.Context, _ string, id int64) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("UserInstallationRepositories"); err != nil {
		return nil, err
	}
	if ids, ok := f.instRepos[id]; ok {
		return ids, nil
	}
	return []int64{100}, nil
}

func (f *fakeGitHub) CanListRepositories(_ context.Context, token string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CanListRepositories"); err != nil {
		return false, err
	}
	return f.canList[token], nil
}

// --- test environment -----------------------------------------------------

type recAudit struct {
	mu     sync.Mutex
	events []iam.AuditEvent
	fail   bool
}

func (a *recAudit) RecordTx(_ context.Context, _ *sql.Tx, ev iam.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail {
		return errors.New("audit store refuses")
	}
	a.events = append(a.events, ev)
	return nil
}

func (a *recAudit) actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, e := range a.events {
		out = append(out, e.Action)
	}
	return out
}

type env struct {
	t     *testing.T
	st    *store.Store
	vault *secrets.Service
	gh    *fakeGitHub
	aud   *recAudit
	svc   *Service
	clock time.Time
	mu    sync.Mutex
	alice Actor
	bob   Actor
}

func (e *env) now() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clock
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clock = e.clock.Add(d)
}

func newEnv(t *testing.T, origin string) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	kr, err := secrets.LoadOrCreateKeyring(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kr.Close)
	vault, err := secrets.Open(context.Background(), st, kr)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, vault: vault, gh: newFake(), aud: &recAudit{}, clock: time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)}
	e.gh.now = e.now
	for _, u := range []string{"usr_alice", "usr_bob"} {
		if _, err := st.DB.Exec(`INSERT INTO users(id, username, password_hash, created_at, updated_at) VALUES(?, ?, 'x', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, u, u); err != nil {
			t.Fatal(err)
		}
	}
	e.alice = Actor{UserID: "usr_alice", Username: "alice", SessionID: "ses_alice", CanManageApps: true}
	e.bob = Actor{UserID: "usr_bob", Username: "bob", SessionID: "ses_bob"}
	e.svc, err = New(Options{DB: st.DB, Vault: vault, Audit: e.aud, Client: e.gh, PublicOrigin: origin, Now: e.now,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// rsaKey makes an App key and returns its PEM.
func rsaKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

// addApp registers an App on the fake GitHub and in the service.
func (e *env) addApp(appID int64, perms map[string]string, events []string) App {
	e.t.Helper()
	k, pemText := rsaKey(e.t)
	e.gh.apps[appID] = AppInfo{ID: appID, Slug: "hx-" + itoa(appID), ClientID: "Iv1.client" + itoa(appID), Name: "HX " + itoa(appID),
		Owner: Account{ID: 9000, Login: "acme", Type: "Organization"}, Permissions: perms, Events: events}
	e.gh.appKeys[appID] = &k.PublicKey
	a, err := e.svc.CreateManualApp(context.Background(), ManualApp{AppID: appID, ClientID: "Iv1.client" + itoa(appID), ClientSecret: "client-secret-1",
		PrivateKey: pemText, WebhookSecret: "hook-secret-1"}, e.alice)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

// mustNotContain fails when any secret string appears in v's JSON or in
// any github_* table row.
func (e *env) mustNotContain(v any, secretValues ...string) {
	e.t.Helper()
	b, _ := json.Marshal(v)
	dump := string(b)
	for _, table := range []string{"github_apps", "github_connections", "github_auth_requests", "secret_changes"} {
		rows, err := e.st.DB.Query(`SELECT * FROM ` + table)
		if err != nil {
			e.t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				e.t.Fatal(err)
			}
			for _, v := range vals {
				switch x := v.(type) {
				case string:
					dump += "|" + x
				case []byte:
					dump += "|" + string(x)
				}
			}
		}
		rows.Close()
	}
	for _, a := range e.aud.events {
		ab, _ := json.Marshal(a)
		dump += string(ab)
	}
	for _, s := range secretValues {
		if strings.Contains(dump, s) {
			e.t.Fatalf("secret value %q leaked into a response, a table or the audit trail", s[:min(6, len(s))])
		}
	}
}

func (e *env) countConnections() int {
	e.t.Helper()
	var n int
	if err := e.st.DB.QueryRow(`SELECT count(*) FROM github_connections`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

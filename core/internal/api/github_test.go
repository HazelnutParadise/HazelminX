package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/github"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// apiFakeGitHub answers just enough of GitHub for the routes. It never
// reaches the network.
type apiFakeGitHub struct {
	mu    sync.Mutex
	users map[string]github.User
	codes map[string]string // code → user access token
	app   github.AppInfo
	down  bool
}

func (f *apiFakeGitHub) refuse() error {
	return &github.APIError{Kind: github.ErrRejected, Status: 401, Reason: "Bad credentials"}
}

func (f *apiFakeGitHub) ConvertManifest(context.Context, string) (github.ManifestApp, error) {
	return github.ManifestApp{}, f.refuse()
}
func (f *apiFakeGitHub) GetApp(context.Context, string) (github.AppInfo, error) { return f.app, nil }
func (f *apiFakeGitHub) GetInstallation(context.Context, string, int64) (github.Installation, error) {
	return github.Installation{}, &github.APIError{Kind: github.ErrRejected, Status: 404}
}
func (f *apiFakeGitHub) CreateInstallationToken(context.Context, string, int64, github.InstallationTokenRequest) (github.Token, error) {
	return github.Token{}, f.refuse()
}
func (f *apiFakeGitHub) GetAppHookConfig(context.Context, string) (github.HookConfig, error) {
	return github.HookConfig{}, nil
}
func (f *apiFakeGitHub) LastAppHookDelivery(context.Context, string) (*github.HookDelivery, error) {
	return nil, nil
}
func (f *apiFakeGitHub) ExchangeCode(_ context.Context, _, secret, code, _, _ string) (github.UserToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok, ok := f.codes[code]
	if !ok || secret != "app-client-secret" {
		return github.UserToken{}, &github.APIError{Kind: github.ErrRejected, Reason: "bad_verification_code"}
	}
	delete(f.codes, code)
	return github.UserToken{AccessToken: tok, RefreshToken: tok + "-refresh", ExpiresAt: time.Now().Add(8 * time.Hour)}, nil
}
func (f *apiFakeGitHub) RefreshUserToken(context.Context, string, string, string) (github.UserToken, error) {
	return github.UserToken{}, f.refuse()
}
func (f *apiFakeGitHub) GetUser(_ context.Context, token string) (github.User, github.TokenFacts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return github.User{}, github.TokenFacts{}, &github.APIError{Kind: github.ErrUnavailable, Status: 429, RetryAfter: 30 * time.Second}
	}
	u, ok := f.users[token]
	if !ok {
		return github.User{}, github.TokenFacts{}, f.refuse()
	}
	return u, github.TokenFacts{Scopes: []string{"repo"}}, nil
}
func (f *apiFakeGitHub) UserInstallations(context.Context, string) ([]github.Installation, error) {
	return nil, nil
}
func (f *apiFakeGitHub) CanListRepositories(context.Context, string) (bool, error) { return true, nil }

type githubHarness struct {
	*harness
	gh    *apiFakeGitHub
	owner *client
}

func newGitHubHarness(t *testing.T, origin string) *githubHarness {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hs := &harness{t: t, clock: time.Now().UTC().Truncate(time.Second), totp: map[string]*totpDevice{}}
	kr, err := secrets.LoadOrCreateKeyring(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kr.Close)
	vault, err := secrets.Open(context.Background(), st, kr)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := iam.New(st.DB, iam.Config{PasswordParams: iam.TestPasswordParams, Secrets: vault, Now: func() time.Time { return hs.clock }})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, nil)
	srv.MountIAM(svc, IAMOptions{PublicOrigin: origin})
	gh := &apiFakeGitHub{users: map[string]github.User{}, codes: map[string]string{}}
	sources, err := github.New(github.Options{DB: st.DB, Vault: vault, Audit: svc, Client: gh, PublicOrigin: origin, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	srv.MountGitHub(sources)
	hs.h, hs.svc, hs.srv = srv.Handler(), svc, srv
	return &githubHarness{harness: hs, gh: gh, owner: hs.bootstrap()}
}

func mustNotLeak(t *testing.T, r resp, secretValues ...string) {
	t.Helper()
	for _, s := range secretValues {
		if strings.Contains(r.Raw, s) {
			t.Fatalf("answer contains a secret: %s", r.Raw)
		}
	}
}

func TestGitHubPATConnectionAPI(t *testing.T) {
	g := newGitHubHarness(t, "")
	const tok = "ghp_api_secret_value"
	g.gh.users[tok] = github.User{ID: 77, Login: "octo", Type: "User"}
	r := g.owner.do(http.MethodPost, "/management/v1/github/connections", map[string]string{"token": tok, "name": "work"}, nil)
	if r.Code != http.StatusCreated || r.Hdr.Get("ETag") == "" {
		t.Fatalf("create: %+v", r)
	}
	mustNotLeak(t, r, tok)
	conn := r.Body["connection"].(map[string]any)
	id := conn["id"].(string)
	acct := conn["account"].(map[string]any)
	if acct["id"].(float64) != 77 || acct["login"] != "octo" || conn["tokenType"] != "classic" || conn["capabilities"].(map[string]any)["repoRead"] != "yes" {
		t.Fatalf("connection: %+v", conn)
	}
	list := g.owner.do(http.MethodGet, "/management/v1/github/connections?accountId=77", nil, nil)
	if items := list.Body["items"].([]any); len(items) != 1 || list.Body["page"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("list: %+v", list)
	}
	mustNotLeak(t, list, tok)

	// Rename needs the current version.
	if r := g.owner.do(http.MethodPatch, "/management/v1/github/connections/"+id, map[string]string{"name": "x"}, nil); r.Code != http.StatusPreconditionRequired {
		t.Fatalf("without If-Match: %+v", r)
	}
	get := g.owner.do(http.MethodGet, "/management/v1/github/connections/"+id, nil, nil)
	r = g.owner.do(http.MethodPatch, "/management/v1/github/connections/"+id, map[string]string{"name": "personal"}, map[string]string{"If-Match": get.Hdr.Get("ETag")})
	if r.Code != http.StatusOK || r.Body["connection"].(map[string]any)["name"] != "personal" {
		t.Fatalf("rename: %+v", r)
	}
	if r := g.owner.do(http.MethodPatch, "/management/v1/github/connections/"+id, map[string]string{"name": "late"}, map[string]string{"If-Match": get.Hdr.Get("ETag")}); r.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale version: %+v", r)
	}
	if r := g.owner.do(http.MethodPost, "/management/v1/github/connections/"+id+"/disable", nil, nil); r.Body["connection"].(map[string]any)["state"] != "disabled" {
		t.Fatalf("disable: %+v", r)
	}
	if r := g.owner.do(http.MethodPost, "/management/v1/github/connections/"+id+"/enable", nil, nil); r.Body["connection"].(map[string]any)["state"] != "active" {
		t.Fatalf("enable: %+v", r)
	}
	// Rotation to another account's token is refused with the reason.
	g.gh.users["ghp_other"] = github.User{ID: 78, Login: "octo"}
	r = g.owner.do(http.MethodPut, "/management/v1/github/connections/"+id+"/credential", map[string]string{"token": "ghp_other"}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeGitHubAuthInvalid || r.Body["details"].(map[string]any)["reason"] != "wrong-account" {
		t.Fatalf("rotate to another account: %+v", r)
	}
	mustNotLeak(t, r, "ghp_other")
	// GitHub rate limits the check: retryable, with GitHub's wait.
	g.gh.down = true
	r = g.owner.do(http.MethodPost, "/management/v1/github/connections/"+id+"/check", nil, nil)
	if r.Code != http.StatusBadGateway || r.Body["code"] != CodeGitHubUnavailable || r.Body["retryable"] != true || r.Body["retryAfterSeconds"].(float64) != 30 {
		t.Fatalf("check while rate limited: %+v", r)
	}
	g.gh.down = false
	// A token GitHub refuses at creation.
	r = g.owner.do(http.MethodPost, "/management/v1/github/connections", map[string]string{"token": "ghp_wrong_value"}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeGitHubRejected {
		t.Fatalf("refused token: %+v", r)
	}
	mustNotLeak(t, r, "ghp_wrong_value")
	if r := g.owner.do(http.MethodDelete, "/management/v1/github/connections/"+id, nil, nil); r.Code != http.StatusNoContent {
		t.Fatalf("delete: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, "/management/v1/github/connections/"+id, nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("deleted: %+v", r)
	}
	// The audit trail names the changes and never the token.
	audit := g.owner.do(http.MethodGet, "/management/v1/audit-events?limit=100", nil, nil)
	for _, want := range []string{"github.connection.create", "github.connection.rename", "github.connection.disable", "github.connection.delete"} {
		if !strings.Contains(audit.Raw, want) {
			t.Fatalf("audit lacks %s: %s", want, audit.Raw)
		}
	}
	mustNotLeak(t, audit, tok)
}

func TestGitHubConnectionPermissions(t *testing.T) {
	g := newGitHubHarness(t, "")
	g.gh.users["ghp_a"] = github.User{ID: 1, Login: "a"}
	id := g.owner.do(http.MethodPost, "/management/v1/github/connections", map[string]string{"token": "ghp_a"}, nil).Body["connection"].(map[string]any)["id"].(string)

	dev := g.createUser(g.owner, "dev", iam.RoleProjectAdmin, iam.Scope{Kind: iam.ScopeProject, ID: "prj_1"}, "10.0.0.21")
	auditor := g.createUser(g.owner, "auditor", iam.RoleAuditor, iam.Global, "10.0.0.22")
	// A project admin may connect GitHub, but sees only their own.
	if r := dev.do(http.MethodGet, "/management/v1/github/connections/"+id, nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("another account's connection: %+v", r)
	}
	if r := dev.do(http.MethodGet, "/management/v1/github/connections", nil, nil); len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("list leaks: %+v", r)
	}
	g.gh.users["ghp_dev"] = github.User{ID: 2, Login: "dev"}
	if r := dev.do(http.MethodPost, "/management/v1/github/connections", map[string]string{"token": "ghp_dev"}, nil); r.Code != http.StatusCreated {
		t.Fatalf("project admin connects: %+v", r)
	}
	// It does not give them platform rights: App registration is refused.
	if r := dev.do(http.MethodPost, "/management/v1/github/apps", map[string]any{"appId": 1}, nil); r.Code != http.StatusForbidden {
		t.Fatalf("project admin registers an App: %+v", r)
	}
	if r := dev.do(http.MethodGet, "/management/v1/users", nil, nil); r.Code != http.StatusForbidden {
		t.Fatalf("connecting GitHub must not widen HazelminX rights: %+v", r)
	}
	// A read-only auditor cannot use GitHub connections at all.
	if r := auditor.do(http.MethodPost, "/management/v1/github/connections", map[string]string{"token": "ghp_a"}, nil); r.Code != http.StatusForbidden {
		t.Fatalf("auditor: %+v", r)
	}
}

func TestGitHubBrowserAuthorizationAPI(t *testing.T) {
	// No public origin: browser flows answer NOT_AVAILABLE, PAT still works.
	g := newGitHubHarness(t, "")
	r := g.owner.do(http.MethodPost, "/management/v1/github/app-manifests", map[string]string{"name": "HX"}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeNotAvailable {
		t.Fatalf("manifest without origin: %+v", r)
	}

	g = newGitHubHarness(t, "https://panel.example.test")
	r = g.owner.do(http.MethodPost, "/management/v1/github/app-manifests", map[string]string{"name": "HX"}, nil)
	if r.Code != http.StatusCreated || r.Hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("manifest: %+v", r)
	}
	if !strings.Contains(r.Raw, `"default_permissions":{"contents":"read","metadata":"read"}`) {
		t.Fatalf("manifest body: %s", r.Raw)
	}

	// Register an App by hand, then authorize with it.
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
	g.gh.app = github.AppInfo{ID: 42, Slug: "hx", ClientID: "Iv1.hx", Name: "HX", Permissions: map[string]string{"contents": "read", "metadata": "read"}}
	r = g.owner.do(http.MethodPost, "/management/v1/github/apps", map[string]any{"appId": 42, "clientId": "Iv1.hx", "clientSecret": "app-client-secret", "privateKey": pemText}, nil)
	if r.Code != http.StatusCreated {
		t.Fatalf("manual app: %+v", r)
	}
	mustNotLeak(t, r, "app-client-secret", "PRIVATE KEY")
	appID := r.Body["app"].(map[string]any)["id"].(string)
	if r := g.owner.do(http.MethodGet, "/management/v1/github/apps", nil, nil); len(r.Body["items"].([]any)) != 1 {
		t.Fatalf("apps: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, "/management/v1/github/apps/"+appID+"/reachability", nil, nil); r.Body["reachability"].(map[string]any)["browserCallback"].(map[string]any)["status"] != "configured" {
		t.Fatalf("reachability: %+v", r)
	}

	r = g.owner.do(http.MethodPost, "/management/v1/github/authorizations", map[string]string{"appId": appID}, nil)
	if r.Code != http.StatusCreated {
		t.Fatalf("start: %+v", r)
	}
	start := r.Body["authorization"].(map[string]any)
	u, _ := url.Parse(start["url"].(string))
	if u.Query().Get("redirect_uri") != "https://panel.example.test"+github.CallbackPath || u.Query().Get("code_challenge") == "" {
		t.Fatalf("authorize url: %s", start["url"])
	}
	state := start["state"].(string)
	g.gh.codes["the-code"], g.gh.users["ghu_tok"] = "ghu_tok", github.User{ID: 900, Login: "real-person"}

	// Another browser of the same account cannot finish it.
	other := g.client("10.0.0.1")
	other.login("owner", ownerPW)
	r = other.do(http.MethodPost, "/management/v1/github/authorizations/complete", map[string]string{"state": state, "code": "the-code"}, nil)
	if r.Code != http.StatusConflict || r.Body["details"].(map[string]any)["reason"] != "session-mismatch" {
		t.Fatalf("other session: %+v", r)
	}
	r = g.owner.do(http.MethodPost, "/management/v1/github/authorizations/complete", map[string]string{"state": state, "code": "the-code"}, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("complete: %+v", r)
	}
	mustNotLeak(t, r, "ghu_tok", "the-code")
	conns := r.Body["result"].(map[string]any)["connections"].([]any)
	if acct := conns[0].(map[string]any)["account"].(map[string]any); acct["login"] != "real-person" || acct["id"].(float64) != 900 {
		t.Fatalf("connection shows GitHub's account: %+v", conns)
	}
	r = g.owner.do(http.MethodPost, "/management/v1/github/authorizations/complete", map[string]string{"state": state, "code": "the-code"}, nil)
	if r.Code != http.StatusConflict || r.Body["details"].(map[string]any)["reason"] != "used" {
		t.Fatalf("replay: %+v", r)
	}
	r = g.owner.do(http.MethodPost, "/management/v1/github/authorizations/cancel", map[string]string{"state": "made-up"}, nil)
	if r.Code != http.StatusConflict || r.Body["details"].(map[string]any)["reason"] != "unknown" {
		t.Fatalf("cancel unknown: %+v", r)
	}
	// The App cannot be removed while a connection uses it.
	if r := g.owner.do(http.MethodDelete, "/management/v1/github/apps/"+appID, nil, nil); r.Code != http.StatusConflict || r.Body["code"] != CodeResourceReferenced {
		t.Fatalf("delete used app: %+v", r)
	}
}


package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
	"github.com/gin-gonic/gin"
)

// harness runs the real store, service and router in-process (ENG.md test
// seam: httptest against /management/v1).
type harness struct {
	t     *testing.T
	h     http.Handler
	svc   *iam.Service
	srv   *Server
	clock time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hs := &harness{t: t, clock: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}
	svc, err := iam.New(st.DB, iam.Config{PasswordParams: iam.TestPasswordParams, Now: func() time.Time { return hs.clock }})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, nil)
	srv.MountIAM(svc, IAMOptions{})
	hs.h, hs.svc, hs.srv = srv.Handler(), svc, srv
	return hs
}

// client is one browser: it keeps the cookies and CSRF token of its login.
type client struct {
	hs      *harness
	ip      string
	cookies []*http.Cookie
	csrf    string
}

func (hs *harness) client(ip string) *client { return &client{hs: hs, ip: ip} }

type resp struct {
	Code int
	Body map[string]any
	Raw  string
	Hdr  http.Header
}

func (c *client) do(method, path string, body any, hdr map[string]string) resp {
	c.hs.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.RemoteAddr = c.ip + ":4321"
	req.Host = "panel.example.test"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	if c.csrf != "" && method != http.MethodGet {
		req.Header.Set(CSRFHeader, c.csrf)
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	c.hs.h.ServeHTTP(rec, req)
	if set := rec.Result().Cookies(); len(set) > 0 {
		c.cookies = nil
		for _, ck := range set {
			if ck.MaxAge >= 0 && ck.Value != "" {
				c.cookies = append(c.cookies, ck)
			}
		}
	}
	out := resp{Code: rec.Code, Raw: rec.Body.String(), Hdr: rec.Header()}
	json.Unmarshal(rec.Body.Bytes(), &out.Body)
	return out
}

func (c *client) login(username, password string) resp {
	r := c.do(http.MethodPost, "/management/v1/auth/login", map[string]string{"username": username, "password": password}, nil)
	if r.Code == http.StatusOK {
		c.csrf, _ = r.Body["csrfToken"].(string)
	}
	return r
}

const ownerPW = "owner-password-123"

// bootstrap completes setup and returns a logged-in owner browser.
func (hs *harness) bootstrap() *client {
	hs.t.Helper()
	anon := hs.client("10.0.0.1")
	if r := anon.do(http.MethodGet, "/management/v1/setup", nil, nil); r.Body["needsSetup"] != true {
		hs.t.Fatalf("fresh install must need setup: %+v", r)
	}
	code, _, err := hs.svc.SetupCode(context.Background())
	if err != nil {
		hs.t.Fatal(err)
	}
	if r := anon.do(http.MethodPost, "/management/v1/setup", map[string]string{"setupCode": "nope", "username": "owner", "password": ownerPW}, nil); r.Code != http.StatusForbidden {
		hs.t.Fatalf("wrong setup code: %+v", r)
	}
	if r := anon.do(http.MethodPost, "/management/v1/setup", map[string]string{"setupCode": code, "username": "owner", "password": ownerPW}, nil); r.Code != http.StatusCreated {
		hs.t.Fatalf("setup: %+v", r)
	}
	if r := anon.do(http.MethodPost, "/management/v1/setup", map[string]string{"setupCode": code, "username": "evil", "password": ownerPW}, nil); r.Code != http.StatusConflict {
		hs.t.Fatalf("setup must be one-shot: %+v", r)
	}
	owner := hs.client("10.0.0.1")
	if r := owner.login("owner", ownerPW); r.Code != http.StatusOK {
		hs.t.Fatalf("owner login: %+v", r)
	}
	return owner
}

// createUser makes an account with one assignment and returns its logged-in browser.
func (hs *harness) createUser(owner *client, username string, role iam.Role, scope iam.Scope, ip string) *client {
	hs.t.Helper()
	pw := username + "-password-123"
	r := owner.do(http.MethodPost, "/management/v1/users", map[string]string{"username": username, "displayName": username, "password": pw}, nil)
	if r.Code != http.StatusCreated {
		hs.t.Fatalf("create %s: %+v", username, r)
	}
	id := r.Body["user"].(map[string]any)["id"].(string)
	if role != "" {
		r = owner.do(http.MethodPost, "/management/v1/users/"+id+"/roles", map[string]any{"role": role, "scope": scope}, nil)
		if r.Code != http.StatusCreated {
			hs.t.Fatalf("assign %s: %+v", username, r)
		}
	}
	c := hs.client(ip)
	if r := c.login(username, pw); r.Code != http.StatusOK {
		hs.t.Fatalf("login %s: %+v", username, r)
	}
	return c
}

func TestSetupLoginAndSessionCookies(t *testing.T) {
	hs := newHarness(t)
	owner := hs.bootstrap()
	var sessCookie *http.Cookie
	for _, ck := range owner.cookies {
		if ck.Name == SessionCookie {
			sessCookie = ck
		}
	}
	if sessCookie == nil || !sessCookie.HttpOnly || sessCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie must be HttpOnly+Strict: %+v", sessCookie)
	}
	r := owner.do(http.MethodGet, "/management/v1/auth/session", nil, nil)
	if r.Code != http.StatusOK || r.Body["user"].(map[string]any)["username"] != "owner" {
		t.Fatalf("whoami: %+v", r)
	}
	if strings.Contains(r.Raw, "password") {
		t.Fatal("session payload must not carry password material")
	}
	if r := owner.do(http.MethodPost, "/management/v1/auth/logout", nil, nil); r.Code != http.StatusNoContent {
		t.Fatalf("logout: %+v", r)
	}
	if r := owner.do(http.MethodGet, "/management/v1/auth/session", nil, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %+v", r)
	}
	if r := hs.client("10.0.0.1").do(http.MethodGet, "/management/v1/users", nil, nil); r.Code != http.StatusUnauthorized || r.Body["code"] != "UNAUTHENTICATED" {
		t.Fatalf("anonymous: %+v", r)
	}
}

func TestLoginErrorsDoNotRevealAccounts(t *testing.T) {
	hs := newHarness(t)
	hs.bootstrap()
	c := hs.client("203.0.113.5")
	unknown := c.login("ghost", "whatever-password-1")
	wrong := c.login("owner", "whatever-password-1")
	if unknown.Code != http.StatusUnauthorized || wrong.Code != http.StatusUnauthorized {
		t.Fatalf("codes %d %d", unknown.Code, wrong.Code)
	}
	strip := func(r resp) string { delete(r.Body, "traceId"); b, _ := json.Marshal(r.Body); return string(b) }
	if strip(unknown) != strip(wrong) {
		t.Fatalf("bodies differ:\n%s\n%s", unknown.Raw, wrong.Raw)
	}
}

func TestCSRFProtectsCookieSessions(t *testing.T) {
	hs := newHarness(t)
	owner := hs.bootstrap()
	body := map[string]string{"username": "carol", "password": "carol-password-123"}
	if r := owner.do(http.MethodPost, "/management/v1/users", body, map[string]string{CSRFHeader: ""}); r.Code != http.StatusForbidden || r.Body["code"] != "CSRF_TOKEN_MISMATCH" {
		t.Fatalf("missing csrf header: %+v", r)
	}
	if r := owner.do(http.MethodPost, "/management/v1/users", body, map[string]string{CSRFHeader: "wrong"}); r.Code != http.StatusForbidden {
		t.Fatalf("wrong csrf header: %+v", r)
	}
	if r := owner.do(http.MethodPost, "/management/v1/users", body, map[string]string{"Origin": "https://evil.example"}); r.Code != http.StatusForbidden || r.Body["code"] != "ORIGIN_MISMATCH" {
		t.Fatalf("cross-site origin: %+v", r)
	}
	if r := owner.do(http.MethodPost, "/management/v1/users", body, map[string]string{"Referer": "https://evil.example/x"}); r.Code != http.StatusForbidden {
		t.Fatalf("cross-site referer: %+v", r)
	}
	if r := owner.do(http.MethodPost, "/management/v1/users", body, map[string]string{"Origin": "https://panel.example.test"}); r.Code != http.StatusCreated {
		t.Fatalf("same-site with token: %+v", r)
	}
}

// T-013: a read-only account calls a change API directly.
func TestT013ReadOnlyAccountCannotChange(t *testing.T) {
	hs := newHarness(t)
	owner := hs.bootstrap()
	auditor := hs.createUser(owner, "auditor", iam.RoleAuditor, iam.Global, "10.0.0.2")

	if r := auditor.do(http.MethodGet, "/management/v1/users", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("auditor may list users: %+v", r)
	}
	r := auditor.do(http.MethodPost, "/management/v1/users", map[string]string{"username": "mallory", "password": "mallory-password-1"}, nil)
	if r.Code != http.StatusForbidden || r.Body["code"] != "FORBIDDEN" {
		t.Fatalf("auditor must be refused by the backend: %+v", r)
	}
	users := owner.do(http.MethodGet, "/management/v1/users", nil, nil).Body["users"].([]any)
	for _, u := range users {
		if u.(map[string]any)["username"] == "mallory" {
			t.Fatal("refused call must not have created the user")
		}
	}
	ownerID := owner.do(http.MethodGet, "/management/v1/auth/session", nil, nil).Body["user"].(map[string]any)["id"].(string)
	if r := auditor.do(http.MethodPost, "/management/v1/users/"+ownerID+"/roles", map[string]any{"role": "owner"}, nil); r.Code != http.StatusForbidden {
		t.Fatalf("auditor must not assign roles: %+v", r)
	}
	// The refusal is on the ledger.
	evs := owner.do(http.MethodGet, "/management/v1/audit-events", nil, nil).Body["events"].([]any)
	var denied int
	for _, e := range evs {
		if e.(map[string]any)["action"] == "authz.deny" {
			denied++
		}
	}
	if denied < 2 {
		t.Fatalf("denials must be audited, got %d", denied)
	}
}

// T-014: a project A user guesses project B's resource IDs. Query, download,
// search and the event stream reveal nothing, and a guessed ID that does not
// exist answers exactly the same as one that does.
func TestT014GuessedResourceIDsDoNotLeak(t *testing.T) {
	hs := newHarness(t)
	owner := hs.bootstrap()
	projects := map[string]iam.Scope{"proj-a": {Kind: iam.ScopeProject, ID: "proj-a"}, "proj-b": {Kind: iam.ScopeProject, ID: "proj-b"}}
	lookup := func(c *gin.Context) (iam.Scope, bool, error) {
		sc, ok := projects[c.Param("id")]
		return sc, ok, nil
	}
	ok := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"project": c.Param("id"), "secret": "content of " + c.Param("id")})
	}
	// Resource routes the domain tickets will register the same way.
	g := hs.srv.Management()
	g.GET("/projects/:id", hs.srv.RequireScoped(iam.ActionProjectsRead, lookup), ok)
	g.GET("/projects/:id/download", hs.srv.RequireScoped(iam.ActionProjectsRead, lookup), ok)
	g.GET("/projects/:id/events", hs.srv.RequireScoped(iam.ActionProjectsRead, lookup), ok)
	g.POST("/projects/:id/deploy", hs.srv.RequireScoped(iam.ActionProjectsDeploy, lookup), ok)

	alice := hs.createUser(owner, "alice", iam.RoleProjectAdmin, projects["proj-a"], "10.0.0.3")
	viewer := hs.createUser(owner, "viewer", iam.RoleAuditor, projects["proj-a"], "10.0.0.4")

	if r := alice.do(http.MethodGet, "/management/v1/projects/proj-a", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("own project: %+v", r)
	}
	if r := alice.do(http.MethodPost, "/management/v1/projects/proj-a/deploy", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("own deploy: %+v", r)
	}
	strip := func(r resp) string { delete(r.Body, "traceId"); b, _ := json.Marshal(r.Body); return string(b) }
	for _, path := range []string{"/projects/%s", "/projects/%s/download", "/projects/%s/events"} {
		b := alice.do(http.MethodGet, "/management/v1"+strings.Replace(path, "%s", "proj-b", 1), nil, nil)
		ghost := alice.do(http.MethodGet, "/management/v1"+strings.Replace(path, "%s", "proj-zzz", 1), nil, nil)
		if b.Code != http.StatusNotFound || ghost.Code != http.StatusNotFound {
			t.Fatalf("%s: existing-but-foreign=%d missing=%d", path, b.Code, ghost.Code)
		}
		if strip(b) != strip(ghost) {
			t.Fatalf("%s: foreign and missing must be indistinguishable:\n%s\n%s", path, b.Raw, ghost.Raw)
		}
		if strings.Contains(b.Raw, "content of") {
			t.Fatalf("%s leaked content: %s", path, b.Raw)
		}
	}
	// Visible but not permitted is a 403 (the viewer knows A exists, may not deploy it).
	if r := viewer.do(http.MethodPost, "/management/v1/projects/proj-a/deploy", nil, nil); r.Code != http.StatusForbidden {
		t.Fatalf("viewer deploy: %+v", r)
	}
	if r := viewer.do(http.MethodPost, "/management/v1/projects/proj-b/deploy", nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("viewer deploy on B must be 404: %+v", r)
	}
	// Search: the audit ledger filtered to B is empty for A's auditor, the same as for a scope that does not exist.
	owner.do(http.MethodPost, "/management/v1/projects/proj-b/deploy", nil, nil)
	hs.svc.Record(context.Background(), iam.AuditEvent{Action: "deploy", Scope: projects["proj-b"], JobID: "job-b", Result: iam.ResultOK})
	forB := viewer.do(http.MethodGet, "/management/v1/audit-events?scopeKind=project&scopeId=proj-b", nil, nil)
	forGhost := viewer.do(http.MethodGet, "/management/v1/audit-events?scopeKind=project&scopeId=proj-zzz", nil, nil)
	byJob := viewer.do(http.MethodGet, "/management/v1/audit-events?jobId=job-b", nil, nil)
	for name, r := range map[string]resp{"scope B": forB, "ghost scope": forGhost, "job of B": byJob} {
		if r.Code != http.StatusOK || len(r.Body["events"].([]any)) != 0 {
			t.Fatalf("%s must be an empty page: %+v", name, r)
		}
	}
	if strip(forB) != strip(forGhost) {
		t.Fatalf("search answers differ:\n%s\n%s", forB.Raw, forGhost.Raw)
	}
	forA := viewer.do(http.MethodGet, "/management/v1/audit-events?scopeKind=project&scopeId=proj-a", nil, nil)
	if len(forA.Body["events"].([]any)) == 0 {
		t.Fatal("A's auditor must see A's events")
	}
	if r := alice.do(http.MethodGet, "/management/v1/audit-events", nil, nil); r.Code != http.StatusForbidden {
		t.Fatalf("project admin without audit:read: %+v", r)
	}
}

func TestLoginThrottleBoundedAndTrustedSource(t *testing.T) {
	hs := newHarness(t)
	hs.bootstrap() // owner logged in from 10.0.0.1 → trusted
	attacker := hs.client("203.0.113.9")
	var throttled resp
	for i := 0; i < 10; i++ {
		r := attacker.login("owner", "guess-password-00")
		if r.Code == http.StatusTooManyRequests {
			throttled = r
			break
		}
	}
	if throttled.Code != http.StatusTooManyRequests || throttled.Hdr.Get("Retry-After") == "" || throttled.Body["retryable"] != true {
		t.Fatalf("expected 429 with Retry-After: %+v", throttled)
	}
	// Unknown usernames throttle the same way (no existence leak).
	var ghost resp
	for i := 0; i < 10; i++ {
		if ghost = attacker.login("ghost", "guess-password-00"); ghost.Code == http.StatusTooManyRequests {
			break
		}
	}
	if ghost.Code != http.StatusTooManyRequests {
		t.Fatalf("unknown account must throttle identically: %+v", ghost)
	}
	// The real owner still gets in from the trusted source...
	if r := hs.client("10.0.0.1").login("owner", ownerPW); r.Code != http.StatusOK {
		t.Fatalf("trusted source blocked: %+v", r)
	}
	// ...and from a brand-new device once the bounded cool-down passes.
	newDevice := hs.client("198.51.100.7")
	if r := newDevice.login("owner", ownerPW); r.Code != http.StatusTooManyRequests {
		t.Fatalf("new device during cool-down: %+v", r)
	}
	hs.clock = hs.clock.Add(iam.DefaultLimiterPolicy.AccountMaxDelay + time.Second)
	if r := newDevice.login("owner", ownerPW); r.Code != http.StatusOK {
		t.Fatalf("cool-down must end: %+v", r)
	}
	// A source spraying many accounts is blocked on its own, even with the right password.
	spray := hs.client("203.0.113.77")
	for i := 0; i < iam.DefaultLimiterPolicy.SourceMaxFailures; i++ {
		spray.login("user"+string(rune('a'+i%26))+string(rune('a'+i/26)), "guess-password-00")
	}
	if r := spray.login("owner", ownerPW); r.Code != http.StatusTooManyRequests {
		t.Fatalf("spraying source must be blocked: %+v", r)
	}
}

func TestAuditLedgerLinksChangeAndMasksSecrets(t *testing.T) {
	hs := newHarness(t)
	owner := hs.bootstrap()
	r := owner.do(http.MethodPost, "/management/v1/users", map[string]string{"username": "dave", "password": "dave-password-123"}, map[string]string{TraceIDHdr: "trace-dave"})
	daveID := r.Body["user"].(map[string]any)["id"].(string)
	evs := owner.do(http.MethodGet, "/management/v1/audit-events?resourceId="+daveID, nil, nil).Body["events"].([]any)
	if len(evs) != 1 {
		t.Fatalf("expected one event for dave, got %d", len(evs))
	}
	ev := evs[0].(map[string]any)
	after := ev["after"].(map[string]any)
	if ev["action"] != "user.create" || ev["actorName"] != "owner" || ev["traceId"] != "trace-dave" || ev["requestId"] != nil || ev["sourceIp"] != "10.0.0.1" || after["password"] != "***" {
		t.Fatalf("event: %+v", ev)
	}
	// Pagination cursor and page cap.
	page := owner.do(http.MethodGet, "/management/v1/audit-events?limit=1", nil, nil)
	if len(page.Body["events"].([]any)) != 1 || page.Body["nextAfterSeq"].(float64) < 1 {
		t.Fatalf("page: %+v", page)
	}
	if r := owner.do(http.MethodGet, "/management/v1/audit-events?limit=99999", nil, nil); r.Body["limit"].(float64) != iam.MaxAuditPage {
		t.Fatalf("limit must be capped: %+v", r.Body["limit"])
	}
}

func TestOverviewListsIAMOperations(t *testing.T) {
	hs := newHarness(t)
	r := hs.client("10.0.0.1").do(http.MethodGet, "/management/v1", nil, nil)
	ops, _ := r.Body["operations"].([]any)
	var found bool
	for _, op := range ops {
		if op.(map[string]any)["path"] == "/management/v1/auth/login" {
			found = true
		}
	}
	if !found {
		t.Fatalf("overview must list IAM operations: %s", r.Raw)
	}
}

// TestErrorsCarryTraceIDNotRequestID: requestId is reserved for the
// operation's request identifier (spec 33, 36.1). The per-HTTP-call trace
// identifier travels as X-Trace-ID and traceId.
func TestErrorsCarryTraceIDNotRequestID(t *testing.T) {
	hs := newHarness(t)
	c := hs.client("10.0.0.7")
	r := c.do(http.MethodGet, "/management/v1/users", nil, map[string]string{TraceIDHdr: "trace-42"})
	if r.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %+v", r)
	}
	if r.Body["traceId"] != "trace-42" || r.Hdr.Get(TraceIDHdr) != "trace-42" {
		t.Fatalf("trace id must be echoed in body and header: %s %v", r.Raw, r.Hdr)
	}
	if _, ok := r.Body["requestId"]; ok {
		t.Fatalf("HTTP trace id must not be reported as requestId: %s", r.Raw)
	}
	r = c.do(http.MethodGet, "/management/v1/users", nil, nil)
	if id, _ := r.Body["traceId"].(string); id == "" || r.Hdr.Get(TraceIDHdr) != id {
		t.Fatalf("a trace id is generated when the client sends none: %s %v", r.Raw, r.Hdr)
	}
}

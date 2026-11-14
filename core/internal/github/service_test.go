package github

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

const origin = "https://panel.example.test"

func asAuthErr(t *testing.T, err error, reason string) {
	t.Helper()
	var ae *AuthorizationError
	if !errors.As(err, &ae) || ae.Reason != reason {
		t.Fatalf("want authorization refused (%s), got %v", reason, err)
	}
}

// --- PAT (GIT-002, GIT-003, GIT-004, GH-T02～T04) -------------------------

func TestPATIsVerifiedSavedOnceAndNeverShown(t *testing.T) {
	e := newEnv(t, "")
	const tok = "github_pat_11AAAA_secretvalue"
	exp := e.clock.Add(30 * 24 * time.Hour)
	e.gh.users[tok] = User{ID: 101, Login: "octo", Type: "User"}
	e.gh.facts[tok] = TokenFacts{ExpiresAt: &exp}
	e.gh.canList[tok] = true

	c, err := e.svc.CreatePAT(ctx, PATInput{Token: "  " + tok + "\n"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	if c.Account.ID != 101 || c.Account.Login != "octo" || c.Name != "octo" || c.Kind != KindPAT || c.TokenType != "fine-grained" {
		t.Fatalf("identity must come from GitHub: %+v", c)
	}
	if c.AccessExpiresAt == nil || !c.AccessExpiresAt.Equal(exp) || c.Capabilities.RepoRead != Yes || c.Capabilities.Webhook != Unknown || !c.Capabilities.Polling {
		t.Fatalf("expiry and verified capabilities: %+v", c)
	}
	// Two later consumers (two deployment projects) reuse it without
	// pasting it again.
	for i := 0; i < 2; i++ {
		cr, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice)
		if err != nil || cr.Value != tok {
			t.Fatalf("credential %d: %v", i, err)
		}
	}
	list, _ := e.svc.ListConnections(ctx, e.alice)
	e.mustNotContain([]any{c, list}, tok)
	if got := e.aud.actions(); got[len(got)-1] != "github.connection.create" {
		t.Fatalf("audit: %v", got)
	}
}

func TestPATRefusedByGitHubSavesNothing(t *testing.T) {
	e := newEnv(t, "")
	_, err := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_wrong"}, e.alice)
	if !errors.Is(err, ErrRejected) || strings.Contains(err.Error(), "ghp_wrong") {
		t.Fatalf("want refused without echoing the token: %v", err)
	}
	if n := e.countConnections(); n != 0 {
		t.Fatalf("nothing saved, got %d", n)
	}
	var secretsLeft int
	e.st.DB.QueryRow(`SELECT count(*) FROM secrets WHERE deleted_at IS NULL`).Scan(&secretsLeft)
	if secretsLeft != 0 {
		t.Fatalf("no secret stored for a refused token: %d", secretsLeft)
	}
	expired := e.clock.Add(-time.Hour)
	e.gh.users["ghp_old"], e.gh.facts["ghp_old"] = User{ID: 1, Login: "a"}, TokenFacts{ExpiresAt: &expired}
	if _, err := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_old"}, e.alice); !errors.Is(err, ErrValidation) {
		t.Fatalf("expired token: %v", err)
	}
}

func TestClassicPATCapabilitiesFollowScopes(t *testing.T) {
	e := newEnv(t, "")
	e.gh.users["ghp_c"] = User{ID: 7, Login: "c"}
	e.gh.facts["ghp_c"] = TokenFacts{Scopes: []string{"repo", "admin:repo_hook"}}
	e.gh.canList["ghp_c"] = true
	c, err := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_c"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	k := c.Capabilities
	if c.TokenType != "classic" || k.RepoRead != Yes || k.Webhook != Yes || k.ContentsWrite != Yes || k.OrgAdmin != No {
		t.Fatalf("classic scopes: %+v %+v", c, k)
	}
}

func TestTwoAccountsWithSimilarNamesStaySeparate(t *testing.T) {
	e := newEnv(t, "")
	e.gh.users["ghp_a"], e.gh.users["ghp_b"] = User{ID: 501, Login: "dev-team"}, User{ID: 502, Login: "dev-tearn"}
	a, _ := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_a", Name: "work"}, e.alice)
	b, _ := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_b", Name: "work 2"}, e.alice)
	// Rename on HazelminX touches one connection only.
	n := "personal"
	if _, err := e.svc.UpdateConnection(ctx, a.ID, a.Version, Patch{Name: &n}, e.alice); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateConnection(ctx, a.ID, a.Version, Patch{Name: &n}, e.alice); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("stale version: %v", err)
	}
	// The account is renamed on GitHub: same id, new login.
	e.gh.users["ghp_a"] = User{ID: 501, Login: "dev-team-renamed"}
	got, err := e.svc.Check(ctx, a.ID, e.alice)
	if err != nil || got.Account.ID != 501 || got.Account.Login != "dev-team-renamed" || got.State != StateActive {
		t.Fatalf("rename keeps the id: %+v %v", got, err)
	}
	other, _ := e.svc.GetConnection(ctx, b.ID, e.alice)
	if other.Name != "work 2" || other.Account.ID != 502 || other.Account.Login != "dev-tearn" {
		t.Fatalf("the other account changed: %+v", other)
	}
	ca, _ := e.svc.Credential(ctx, a.ID, Scope{}, e.alice)
	cb, _ := e.svc.Credential(ctx, b.ID, Scope{}, e.alice)
	if ca.Value != "ghp_a" || cb.Value != "ghp_b" {
		t.Fatal("secrets mixed between accounts")
	}
	// The token now answers as someone else: the connection stops.
	e.gh.users["ghp_b"] = User{ID: 999, Login: "dev-tearn"}
	if got, _ := e.svc.Check(ctx, b.ID, e.alice); got.State != StateNeedsReauth {
		t.Fatalf("identity change must need authorization: %+v", got)
	}
}

func TestOneAccountManyPATsRotateIndependently(t *testing.T) {
	e := newEnv(t, "")
	var ids []string
	for _, tok := range []string{"ghp_personal", "ghp_org1", "ghp_org2"} {
		e.gh.users[tok] = User{ID: 42, Login: "same"}
		c, err := e.svc.CreatePAT(ctx, PATInput{Token: tok, Name: "pat " + itoa(int64(len(ids)))}, e.alice)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	e.gh.users["ghp_org1_new"] = User{ID: 42, Login: "same"}
	if _, err := e.svc.RotatePAT(ctx, ids[1], "ghp_org1_new", e.alice); err != nil {
		t.Fatal(err)
	}
	want := []string{"ghp_personal", "ghp_org1_new", "ghp_org2"}
	for i, id := range ids {
		cr, err := e.svc.Credential(ctx, id, Scope{}, e.alice)
		if err != nil || cr.Value != want[i] {
			t.Fatalf("connection %d: %q %v", i, cr.Value, err)
		}
	}
	// A token of another account is refused; the stored one stays.
	e.gh.users["ghp_intruder"] = User{ID: 43, Login: "same"}
	_, err := e.svc.RotatePAT(ctx, ids[2], "ghp_intruder", e.alice)
	asAuthErr(t, err, "wrong-account")
	if cr, _ := e.svc.Credential(ctx, ids[2], Scope{}, e.alice); cr.Value != "ghp_org2" {
		t.Fatal("refused rotation changed the token")
	}
	e.mustNotContain(nil, "ghp_org1_new", "ghp_personal", "ghp_intruder")
}

func TestPATExpiryNeedsReauthorizationNotRenewal(t *testing.T) {
	e := newEnv(t, "")
	exp := e.clock.Add(time.Hour)
	e.gh.users["ghp_x"], e.gh.facts["ghp_x"] = User{ID: 1, Login: "x"}, TokenFacts{ExpiresAt: &exp}
	c, _ := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_x"}, e.alice)
	e.advance(2 * time.Hour)
	before := e.gh.count("RefreshUserToken")
	if _, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); !errors.Is(err, ErrNeedsReauth) {
		t.Fatalf("expired PAT: %v", err)
	}
	got, _ := e.svc.GetConnection(ctx, c.ID, e.alice)
	if got.State != StateNeedsReauth || e.gh.count("RefreshUserToken") != before {
		t.Fatalf("expired PAT is marked, not renewed: %+v", got)
	}
	// Rotating in a new token of the same account brings it back.
	e.gh.users["ghp_y"] = User{ID: 1, Login: "x"}
	if got, err := e.svc.RotatePAT(ctx, c.ID, "ghp_y", e.alice); err != nil || got.State != StateActive || got.AccessExpiresAt != nil {
		t.Fatalf("rotation: %+v %v", got, err)
	}
}

func TestConnectionsBelongToTheirCreator(t *testing.T) {
	e := newEnv(t, "")
	e.gh.users["ghp_a"] = User{ID: 1, Login: "a"}
	c, _ := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_a"}, e.alice)
	for name, err := range map[string]error{
		"get":        func() error { _, err := e.svc.GetConnection(ctx, c.ID, e.bob); return err }(),
		"credential": func() error { _, err := e.svc.Credential(ctx, c.ID, Scope{}, e.bob); return err }(),
		"disable":    func() error { _, err := e.svc.SetEnabled(ctx, c.ID, false, e.bob); return err }(),
		"rotate":     func() error { _, err := e.svc.RotatePAT(ctx, c.ID, "ghp_a", e.bob); return err }(),
		"delete":     e.svc.DeleteConnection(ctx, c.ID, e.bob),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s by another account: %v", name, err)
		}
	}
	if l, _ := e.svc.ListConnections(ctx, e.bob); len(l) != 0 {
		t.Fatal("bob sees alice's connection")
	}
}

func TestDisableEnableAndDelete(t *testing.T) {
	e := newEnv(t, "")
	e.gh.users["ghp_a"] = User{ID: 1, Login: "a"}
	c, _ := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_a"}, e.alice)
	if got, _ := e.svc.SetEnabled(ctx, c.ID, false, e.alice); got.State != StateDisabled {
		t.Fatal("disable")
	}
	if _, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled connection gives no credential: %v", err)
	}
	if got, _ := e.svc.SetEnabled(ctx, c.ID, true, e.alice); got.State != StateActive {
		t.Fatal("enable")
	}
	if err := e.svc.DeleteConnection(ctx, c.ID, e.alice); err != nil {
		t.Fatal(err)
	}
	var live int
	e.st.DB.QueryRow(`SELECT count(*) FROM secrets WHERE deleted_at IS NULL`).Scan(&live)
	if live != 0 || e.countConnections() != 0 {
		t.Fatalf("delete leaves the row or the secret: secrets=%d", live)
	}
}

func TestAuditFailureLeavesNoConnection(t *testing.T) {
	e := newEnv(t, "")
	e.gh.users["ghp_a"] = User{ID: 1, Login: "a"}
	e.aud.fail = true
	if _, err := e.svc.CreatePAT(ctx, PATInput{Token: "ghp_a"}, e.alice); err == nil {
		t.Fatal("creation must fail when its audit row cannot be written")
	}
	var live int
	e.st.DB.QueryRow(`SELECT count(*) FROM secrets WHERE deleted_at IS NULL`).Scan(&live)
	if e.countConnections() != 0 || live != 0 {
		t.Fatalf("rolled back: connections=%d secrets=%d", e.countConnections(), live)
	}
}

// --- Apps (GIT-013, GIT-014, GH-T12, GH-T13) --------------------------------

func TestManualAppIsCheckedWithGitHub(t *testing.T) {
	e := newEnv(t, origin)
	k, pemText := rsaKey(t)
	e.gh.apps[77] = AppInfo{ID: 77, Slug: "mine", ClientID: "Iv1.c77", Name: "Mine", Permissions: map[string]string{"contents": "read", "metadata": "read"}}
	e.gh.appKeys[77] = &k.PublicKey
	_, otherPEM := rsaKey(t)
	cases := []ManualApp{
		{AppID: 77, ClientID: "Iv1.c77", ClientSecret: "s", PrivateKey: "not pem"},
		{AppID: 77, ClientID: "Iv1.c77", ClientSecret: "s", PrivateKey: otherPEM},
		{AppID: 77, ClientID: "Iv1.wrong", ClientSecret: "s", PrivateKey: pemText},
		{AppID: 0, ClientID: "Iv1.c77", ClientSecret: "s", PrivateKey: pemText},
	}
	for i, in := range cases {
		if _, err := e.svc.CreateManualApp(ctx, in, e.alice); err == nil {
			t.Fatalf("case %d must be refused", i)
		}
	}
	a, err := e.svc.CreateManualApp(ctx, ManualApp{AppID: 77, ClientID: "Iv1.c77", ClientSecret: "cs-very-secret", PrivateKey: pemText}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	if a.Slug != "mine" || a.Source != "manual" || a.InstallURL != "https://github.com/apps/mine/installations/new" || a.HasWebhookSecret {
		t.Fatalf("app: %+v", a)
	}
	if _, err := e.svc.CreateManualApp(ctx, ManualApp{AppID: 77, ClientID: "Iv1.c77", ClientSecret: "s", PrivateKey: pemText}, e.alice); !errors.Is(err, ErrValidation) {
		t.Fatalf("registered twice: %v", err)
	}
	apps, _ := e.svc.ListApps(ctx)
	e.mustNotContain(apps, "cs-very-secret", "PRIVATE KEY", strings.Split(pemText, "\n")[1])
}

func TestManifestAsksForReadingOnlyUnlessMoreIsChosen(t *testing.T) {
	e := newEnv(t, "")
	if _, err := e.svc.StartManifest(ctx, ManifestInput{Name: "HX"}, e.alice); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("no public origin: %v", err)
	}
	e = newEnv(t, origin)
	st, err := e.svc.StartManifest(ctx, ManifestInput{Name: "HX deploy"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	m := string(st.Manifest)
	for _, want := range []string{`"default_permissions":{"contents":"read","metadata":"read"}`, `"redirect_url":"https://panel.example.test/sources/github/callback"`,
		`"active":false`, `"default_events":[]`, `"public":false`} {
		if !strings.Contains(m, want) {
			t.Fatalf("manifest lacks %s: %s", want, m)
		}
	}
	if st.Method != "POST" || !strings.HasPrefix(st.URL, "https://github.com/settings/apps/new?state=") {
		t.Fatalf("start: %+v", st)
	}
	st2, err := e.svc.StartManifest(ctx, ManifestInput{Name: "HX", Organization: "acme", Capabilities: []string{"webhook", "status-write"}, WebhookURL: "https://hooks.example.test/gh"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(st2.Manifest), `"deployments":"write"`) || !strings.Contains(string(st2.Manifest), `"default_events":["push"]`) ||
		!strings.HasPrefix(st2.URL, "https://github.com/organizations/acme/settings/apps/new?") {
		t.Fatalf("capabilities: %s %s", st2.Manifest, st2.URL)
	}
	if _, err := e.svc.StartManifest(ctx, ManifestInput{Name: "HX", Capabilities: []string{"webhook"}, WebhookURL: "http://user:pw@x"}, e.alice); !errors.Is(err, ErrValidation) {
		t.Fatalf("webhook url: %v", err)
	}
	// The state is stored only as its hash.
	var stored int
	e.st.DB.QueryRow(`SELECT count(*) FROM github_auth_requests WHERE state_hash=?`, st.State).Scan(&stored)
	if stored != 0 {
		t.Fatal("state stored in clear")
	}
}

func TestManifestCompletionCreatesTheApp(t *testing.T) {
	e := newEnv(t, origin)
	k, pemText := rsaKey(t)
	_ = k
	e.gh.manifests["mcode"] = ManifestApp{AppInfo: AppInfo{ID: 31, Slug: "hx-deploy", ClientID: "Iv1.m", Name: "HX deploy",
		Permissions: map[string]string{"contents": "read", "metadata": "read"}}, ClientSecret: "mcs", WebhookSecret: "mwh", PEM: pemText}
	st, _ := e.svc.StartManifest(ctx, ManifestInput{Name: "HX deploy"}, e.alice)
	// Someone without App rights in the same session cannot finish it.
	noAdmin := e.alice
	noAdmin.CanManageApps = false
	if _, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "mcode"}, noAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manifest completion without App rights: %v", err)
	}
	st, _ = e.svc.StartManifest(ctx, ManifestInput{Name: "HX deploy"}, e.alice)
	got, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "mcode"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	if got.App == nil || got.App.AppID != 31 || got.App.Source != "manifest" || !got.App.HasWebhookSecret {
		t.Fatalf("app: %+v", got.App)
	}
	e.mustNotContain(got, "mcs", "mwh", strings.Split(pemText, "\n")[1])
	if _, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "mcode"}, e.alice); err == nil {
		t.Fatal("replayed manifest callback accepted")
	}
}

func TestAppCapabilitiesWithReadOnlyPermissions(t *testing.T) {
	e := newEnv(t, origin)
	app := e.addApp(5, map[string]string{"contents": "read", "metadata": "read"}, nil)
	e.gh.codes["c1"] = codeGrant{tok: UserToken{AccessToken: "ghu_1"}}
	e.gh.users["ghu_1"] = User{ID: 11, Login: "reader"}
	st, _ := e.svc.StartAuthorization(ctx, AuthorizeInput{AppID: app.ID}, e.alice)
	e.gh.codes["c1"] = codeGrant{tok: UserToken{AccessToken: "ghu_1"}, verifier: e.svc.pkceVerifier(st.ID)}
	got, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c1"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	k := got.Connections[0].Capabilities
	if k.RepoRead != Yes || k.Webhook != No || k.StatusWrite != No || k.ContentsWrite != No || k.OrgAdmin != No || !k.Polling {
		t.Fatalf("read-only App: %+v", k)
	}
}

func TestDeleteAppRefusedWhileUsed(t *testing.T) {
	e := newEnv(t, origin)
	app := e.addApp(5, map[string]string{"contents": "read"}, nil)
	st, _ := e.svc.StartAuthorization(ctx, AuthorizeInput{AppID: app.ID}, e.alice)
	e.gh.codes["c"] = codeGrant{tok: UserToken{AccessToken: "ghu_a"}, verifier: e.svc.pkceVerifier(st.ID)}
	e.gh.users["ghu_a"] = User{ID: 1, Login: "a"}
	done, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	var ref *ReferencedError
	if err := e.svc.DeleteApp(ctx, app.ID, e.alice); !errors.As(err, &ref) || ref.Connections != 1 {
		t.Fatalf("app in use: %v", err)
	}
	e.svc.DeleteConnection(ctx, done.Connections[0].ID, e.alice)
	if err := e.svc.DeleteApp(ctx, app.ID, e.alice); err != nil {
		t.Fatal(err)
	}
}

// --- Browser authorization (GIT-001, GIT-015, GH-T01, GH-T14) ---------------

func startAuth(t *testing.T, e *env, app App, in AuthorizeInput, actor Actor, code string, tok UserToken) Started {
	t.Helper()
	in.AppID = app.ID
	st, err := e.svc.StartAuthorization(ctx, in, actor)
	if err != nil {
		t.Fatal(err)
	}
	v := ""
	if st.Purpose == PurposeAuthorize {
		v = e.svc.pkceVerifier(st.ID)
	}
	e.gh.codes[code] = codeGrant{tok: tok, verifier: v}
	return st
}

func TestAuthorizeShowsTheRealAccountAndUsesPKCE(t *testing.T) {
	e := newEnv(t, origin)
	app := e.addApp(5, map[string]string{"contents": "read", "metadata": "read"}, nil)
	e.gh.users["ghu_real"] = User{ID: 3001, Login: "real-login", Type: "User"}
	st := startAuth(t, e, app, AuthorizeInput{Name: "I typed this"}, e.alice, "code1", UserToken{AccessToken: "ghu_real", ExpiresAt: e.clock.Add(8 * time.Hour),
		RefreshToken: "ghr_real", RefreshExpiresAt: e.clock.Add(180 * 24 * time.Hour)})
	u, _ := url.Parse(st.URL)
	q := u.Query()
	if u.Host != "github.com" || q.Get("redirect_uri") != origin+CallbackPath || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") != pkceChallenge(e.svc.pkceVerifier(st.ID)) || q.Get("state") != st.State || q.Get("client_id") != app.ClientID {
		t.Fatalf("authorize url: %s", st.URL)
	}
	got, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "code1"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	c := got.Connections[0]
	if len(got.Connections) != 1 || c.Account.ID != 3001 || c.Account.Login != "real-login" || c.Name != "I typed this" || c.Kind != KindAppUser ||
		c.Permissions["contents"] != "read" || c.AccessExpiresAt == nil || c.RefreshExpiresAt == nil {
		t.Fatalf("connection: %+v", c)
	}
	if e.gh.lastVerifier == "" {
		t.Fatal("the code exchange must send the PKCE verifier")
	}
	e.mustNotContain(got, "ghu_real", "ghr_real", "code1", e.svc.pkceVerifier(st.ID))
	var users int
	e.st.DB.QueryRow(`SELECT count(*) FROM users`).Scan(&users)
	if users != 2 {
		t.Fatal("connecting GitHub must not create HazelminX accounts")
	}
}

func TestMaliciousCallbacksAreRefusedAndChangeNothing(t *testing.T) {
	e := newEnv(t, origin)
	app := e.addApp(5, map[string]string{"contents": "read"}, nil)
	e.gh.users["ghu_a"] = User{ID: 1, Login: "a"}
	tok := UserToken{AccessToken: "ghu_a"}

	// Replay: the second use of a state is refused.
	st := startAuth(t, e, app, AuthorizeInput{}, e.alice, "c-replay", tok)
	if _, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c-replay"}, e.alice); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c-replay"}, e.alice)
	asAuthErr(t, err, "used")
	base := e.countConnections()

	// Another browser session of the same account.
	st = startAuth(t, e, app, AuthorizeInput{}, e.alice, "c-session", tok)
	other := e.alice
	other.SessionID = "ses_other"
	_, err = e.svc.Complete(ctx, Callback{State: st.State, Code: "c-session"}, other)
	asAuthErr(t, err, "session-mismatch")
	// Another HazelminX account with the victim's state: as if unknown,
	// and the victim can still finish.
	bob := e.bob
	_, err = e.svc.Complete(ctx, Callback{State: st.State, Code: "c-session"}, bob)
	asAuthErr(t, err, "unknown")
	if _, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c-session"}, e.alice); err != nil {
		t.Fatalf("the owner's request must survive others' attempts: %v", err)
	}
	base++

	// Forged state.
	_, err = e.svc.Complete(ctx, Callback{State: "forged", Code: "x"}, e.alice)
	asAuthErr(t, err, "unknown")

	// Expired.
	st = startAuth(t, e, app, AuthorizeInput{}, e.alice, "c-late", tok)
	e.advance(RequestTTL + time.Second)
	_, err = e.svc.Complete(ctx, Callback{State: st.State, Code: "c-late"}, e.alice)
	asAuthErr(t, err, "expired")

	// An installation id slipped into a plain authorization.
	st = startAuth(t, e, app, AuthorizeInput{}, e.alice, "c-inst", tok)
	_, err = e.svc.Complete(ctx, Callback{State: st.State, Code: "c-inst", InstallationID: 999}, e.alice)
	asAuthErr(t, err, "installation-mismatch")

	// A code from another flow (wrong PKCE verifier).
	st = startAuth(t, e, app, AuthorizeInput{}, e.alice, "c-pkce", tok)
	e.gh.codes["c-pkce"] = codeGrant{tok: tok, verifier: "someone-elses"}
	if _, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c-pkce"}, e.alice); !errors.Is(err, ErrRejected) {
		t.Fatalf("foreign code: %v", err)
	}

	// Cancelled on GitHub, and cancelled here.
	st = startAuth(t, e, app, AuthorizeInput{}, e.alice, "c-deny", tok)
	_, err = e.svc.Complete(ctx, Callback{State: st.State, Error: "access_denied"}, e.alice)
	asAuthErr(t, err, "cancelled-by-github")
	st = startAuth(t, e, app, AuthorizeInput{}, e.alice, "c-cancel", tok)
	if err := e.svc.Cancel(ctx, st.State, e.alice); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Complete(ctx, Callback{State: st.State, Code: "c-cancel"}, e.alice)
	asAuthErr(t, err, "used")

	if n := e.countConnections(); n != base {
		t.Fatalf("refused callbacks created connections: %d → %d", base, n)
	}
	// Browser flows refuse API-token callers (no session to bind).
	if _, err := e.svc.StartAuthorization(ctx, AuthorizeInput{AppID: app.ID}, Actor{UserID: "usr_alice"}); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("token caller: %v", err)
	}
}

func TestConcurrentCompletionsOnlyOneWins(t *testing.T) {
	e := newEnv(t, origin)
	app := e.addApp(5, map[string]string{"contents": "read"}, nil)
	e.gh.users["ghu_a"] = User{ID: 1, Login: "a"}
	st := startAuth(t, e, app, AuthorizeInput{}, e.alice, "c", UserToken{AccessToken: "ghu_a"})
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c"}, e.alice); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 || e.countConnections() != 1 || e.gh.count("ExchangeCode") != 1 {
		t.Fatalf("winners=%d connections=%d exchanges=%d", ok, e.countConnections(), e.gh.count("ExchangeCode"))
	}
}

func TestInstallationIsVerifiedBeforeBinding(t *testing.T) {
	e := newEnv(t, origin)
	app := e.addApp(5, map[string]string{"contents": "read", "metadata": "read"}, []string{"push"})
	other := e.addApp(6, map[string]string{"contents": "write"}, nil)
	e.gh.installs[700] = Installation{ID: 700, AppID: 5, Account: Account{ID: 8000, Login: "acme", Type: "Organization"}, RepositorySelection: "selected",
		Permissions: map[string]string{"contents": "read", "metadata": "read"}, Events: []string{"push"}}
	e.gh.installs[701] = Installation{ID: 701, AppID: 5, Account: Account{ID: 8001, Login: "victim-org"}}
	e.gh.installs[800] = Installation{ID: 800, AppID: 6, Account: Account{ID: 8000, Login: "acme"}}
	e.gh.users["ghu_i"] = User{ID: 21, Login: "installer"}
	e.gh.userInstalls["ghu_i"] = []int64{700, 800}
	tok := UserToken{AccessToken: "ghu_i"}
	before := e.countConnections()

	for _, forged := range []int64{701 /* not the user's */, 800 /* another App's */, 12345 /* does not exist */} {
		st := startAuth(t, e, app, AuthorizeInput{Mode: PurposeInstall}, e.alice, "ci", tok)
		if !strings.HasPrefix(st.URL, app.InstallURL+"?state=") {
			t.Fatalf("install url: %s", st.URL)
		}
		_, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "ci", InstallationID: forged, SetupAction: "install"}, e.alice)
		asAuthErr(t, err, "installation-mismatch")
	}
	if e.countConnections() != before {
		t.Fatal("forged installation ids created connections")
	}
	st := startAuth(t, e, app, AuthorizeInput{Mode: PurposeInstall}, e.alice, "ci", tok)
	got, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "ci", InstallationID: 700, SetupAction: "install"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Connections) != 2 {
		t.Fatalf("user + installation connections: %+v", got.Connections)
	}
	inst := got.Connections[1]
	if inst.Kind != KindAppInstallation || inst.InstallationID != 700 || inst.Account.ID != 8000 || inst.RepositorySelection != "selected" || inst.Capabilities.Webhook != Yes {
		t.Fatalf("installation connection: %+v", inst)
	}
	// Installing again refreshes, it does not duplicate.
	st = startAuth(t, e, app, AuthorizeInput{Mode: PurposeInstall}, e.alice, "ci", tok)
	again, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "ci", InstallationID: 700}, e.alice)
	if err != nil || again.Connections[1].ID != inst.ID {
		t.Fatalf("second install: %v", err)
	}
	_ = other
}

func TestReauthorizeKeepsTheAccountOrRefuses(t *testing.T) {
	e := newEnv(t, origin)
	app := e.addApp(5, map[string]string{"contents": "read"}, nil)
	e.gh.users["ghu_1"] = User{ID: 1, Login: "me"}
	st := startAuth(t, e, app, AuthorizeInput{}, e.alice, "c1", UserToken{AccessToken: "ghu_1", RefreshToken: "ghr_1", ExpiresAt: e.clock.Add(time.Hour)})
	done, _ := e.svc.Complete(ctx, Callback{State: st.State, Code: "c1"}, e.alice)
	conn := done.Connections[0]

	// Signed in to the wrong GitHub account: refused, nothing replaced.
	e.gh.users["ghu_wrong"] = User{ID: 2, Login: "me-work"}
	st = startAuth(t, e, app, AuthorizeInput{ConnectionID: conn.ID}, e.alice, "c2", UserToken{AccessToken: "ghu_wrong", RefreshToken: "ghr_wrong"})
	_, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c2"}, e.alice)
	asAuthErr(t, err, "wrong-account")
	if cr, _ := e.svc.Credential(ctx, conn.ID, Scope{}, e.alice); cr.Value != "ghu_1" {
		t.Fatal("wrong account replaced the token")
	}
	if e.countConnections() != 1 {
		t.Fatal("wrong account added a connection")
	}
	// The right account replaces the tokens of the same connection.
	e.gh.users["ghu_new"] = User{ID: 1, Login: "me-renamed"}
	st = startAuth(t, e, app, AuthorizeInput{ConnectionID: conn.ID}, e.alice, "c3", UserToken{AccessToken: "ghu_new", RefreshToken: "ghr_new", ExpiresAt: e.clock.Add(8 * time.Hour)})
	got, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c3"}, e.alice)
	if err != nil || got.Connections[0].ID != conn.ID || got.Connections[0].Account.Login != "me-renamed" {
		t.Fatalf("reauthorize: %+v %v", got, err)
	}
	if cr, _ := e.svc.Credential(ctx, conn.ID, Scope{}, e.alice); cr.Value != "ghu_new" {
		t.Fatal("token not replaced")
	}
}

// --- Tokens (GIT-016, GH-T15) -----------------------------------------------

func userConn(t *testing.T, e *env, tok UserToken) Connection {
	t.Helper()
	app := e.addApp(5, map[string]string{"contents": "read"}, nil)
	e.gh.users[tok.AccessToken] = User{ID: 1, Login: "me"}
	st := startAuth(t, e, app, AuthorizeInput{}, e.alice, "c", tok)
	done, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "c"}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	return done.Connections[0]
}

func TestUserTokenIsRefreshedByItsRealExpiry(t *testing.T) {
	e := newEnv(t, origin)
	c := userConn(t, e, UserToken{AccessToken: "ghu_1", ExpiresAt: e.clock.Add(time.Hour), RefreshToken: "ghr_1", RefreshExpiresAt: e.clock.Add(48 * time.Hour)})
	if cr, _ := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); cr.Value != "ghu_1" || e.gh.count("RefreshUserToken") != 0 {
		t.Fatal("a fresh token is used as is")
	}
	// A new token of a different length and format, with a short life.
	long := "ghu_" + strings.Repeat("Z", 300)
	wantExp := e.clock.Add(58 * time.Minute)
	e.gh.refresh["ghr_1"] = UserToken{AccessToken: long, ExpiresAt: wantExp, RefreshToken: "ghr_2", RefreshExpiresAt: e.clock.Add(96 * time.Hour)}
	e.advance(57 * time.Minute)
	cr, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice)
	if err != nil || cr.Value != long {
		t.Fatalf("refresh: %v", err)
	}
	got, _ := e.svc.GetConnection(ctx, c.ID, e.alice)
	if got.AccessExpiresAt == nil || !got.AccessExpiresAt.Equal(wantExp) {
		t.Fatalf("expiry from GitHub's answer: %v", got.AccessExpiresAt)
	}
	// The used refresh token is gone; the new one is stored.
	e.gh.refresh["ghr_2"] = UserToken{AccessToken: "ghu_3", ExpiresAt: e.clock.Add(10 * time.Hour)}
	e.advance(time.Hour)
	if cr, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); err != nil || cr.Value != "ghu_3" {
		t.Fatalf("second refresh: %v", err)
	}
	e.mustNotContain(got, long, "ghr_2")
}

func TestConcurrentCallersRefreshOnce(t *testing.T) {
	e := newEnv(t, origin)
	c := userConn(t, e, UserToken{AccessToken: "ghu_1", ExpiresAt: e.clock.Add(time.Minute), RefreshToken: "ghr_1"})
	e.gh.refresh["ghr_1"] = UserToken{AccessToken: "ghu_2", ExpiresAt: e.clock.Add(8 * time.Hour), RefreshToken: "ghr_2"}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cr, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice)
			if err == nil && cr.Value != "ghu_2" {
				err = errors.New("stale token " + cr.Value)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := e.gh.count("RefreshUserToken"); n != 1 {
		t.Fatalf("single-use refresh token used %d times", n)
	}
}

func TestRefusedRefreshNeedsReauthAndOutageBacksOff(t *testing.T) {
	e := newEnv(t, origin)
	c := userConn(t, e, UserToken{AccessToken: "ghu_1", ExpiresAt: e.clock.Add(time.Minute), RefreshToken: "ghr_1"})
	e.gh.down = true
	if _, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("outage: %v", err)
	}
	if _, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); !errors.Is(err, ErrRefreshBackoff) {
		t.Fatalf("immediate retry must wait: %v", err)
	}
	if n := e.gh.count("RefreshUserToken"); n != 1 {
		t.Fatalf("retries during backoff reached GitHub: %d", n)
	}
	e.gh.down = false
	e.advance(31 * time.Second)
	// GitHub now refuses the refresh token: authorize again.
	if _, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); !errors.Is(err, ErrNeedsReauth) {
		t.Fatalf("refused refresh: %v", err)
	}
	got, _ := e.svc.GetConnection(ctx, c.ID, e.alice)
	if got.State != StateNeedsReauth || !strings.Contains(got.StateReason, "bad_refresh_token") {
		t.Fatalf("state: %+v", got)
	}
	// Expired refresh token: no call at all.
	e2 := newEnv(t, origin)
	c2 := userConn(t, e2, UserToken{AccessToken: "ghu_1", ExpiresAt: e2.clock.Add(time.Minute), RefreshToken: "ghr_1", RefreshExpiresAt: e2.clock.Add(time.Minute)})
	e2.advance(2 * time.Minute)
	if _, err := e2.svc.Credential(ctx, c2.ID, Scope{}, e2.alice); !errors.Is(err, ErrNeedsReauth) || e2.gh.count("RefreshUserToken") != 0 {
		t.Fatalf("expired refresh token: %v", err)
	}
}

func installationConn(t *testing.T, e *env) Connection {
	t.Helper()
	app := e.addApp(5, map[string]string{"contents": "read"}, nil)
	e.gh.installs[700] = Installation{ID: 700, AppID: 5, Account: Account{ID: 8000, Login: "acme"}, Permissions: map[string]string{"contents": "read"}}
	e.gh.users["ghu_i"] = User{ID: 21, Login: "installer"}
	e.gh.userInstalls["ghu_i"] = []int64{700}
	st := startAuth(t, e, app, AuthorizeInput{Mode: PurposeInstall}, e.alice, "ci", UserToken{AccessToken: "ghu_i"})
	got, err := e.svc.Complete(ctx, Callback{State: st.State, Code: "ci", InstallationID: 700}, e.alice)
	if err != nil {
		t.Fatal(err)
	}
	return got.Connections[1]
}

func TestInstallationTokensFollowReturnedExpiryAndScope(t *testing.T) {
	e := newEnv(t, origin)
	c := installationConn(t, e)
	e.gh.instTokens = []Token{
		{Value: "ghs_short", ExpiresAt: e.clock.Add(time.Hour)},
		{Value: "v2." + strings.Repeat("q", 520), ExpiresAt: e.clock.Add(20 * time.Minute)}, // new format and length
		{Value: "ghs_scoped", ExpiresAt: e.clock.Add(time.Hour)},
	}
	cr, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice)
	if err != nil || cr.Value != "ghs_short" || !cr.ExpiresAt.Equal(e.clock.Add(time.Hour)) {
		t.Fatalf("first token: %+v %v", cr, err)
	}
	e.advance(30 * time.Minute)
	if cr, _ := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); cr.Value != "ghs_short" {
		t.Fatal("a token with life left is reused")
	}
	e.advance(26 * time.Minute) // 4 minutes left: renewed
	if cr, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); err != nil || !strings.HasPrefix(cr.Value, "v2.") {
		t.Fatalf("renewal accepts any format: %v", err)
	}
	cr, err = e.svc.Credential(ctx, c.ID, Scope{RepositoryIDs: []int64{9, 3}, Permissions: map[string]string{"contents": "read"}}, e.alice)
	if err != nil || cr.Value != "ghs_scoped" || len(e.gh.lastScope.RepositoryIDs) != 2 || e.gh.lastScope.Permissions["contents"] != "read" {
		t.Fatalf("narrowed token: %+v %+v %v", cr, e.gh.lastScope, err)
	}
	e.mustNotContain(c, "ghs_short", "ghs_scoped")
}

func TestRemovedInstallationNeedsReauth(t *testing.T) {
	e := newEnv(t, origin)
	c := installationConn(t, e)
	delete(e.gh.installs, 700)
	if _, err := e.svc.Credential(ctx, c.ID, Scope{}, e.alice); !errors.Is(err, ErrNeedsReauth) {
		t.Fatalf("removed installation: %v", err)
	}
}

// --- Reachability (GIT-031) -------------------------------------------------

func TestReachabilityChecksAreSeparate(t *testing.T) {
	e := newEnv(t, "")
	app := e.addApp(5, map[string]string{"contents": "read"}, nil)
	r, err := e.svc.CheckReachability(ctx, app.ID, "10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	if r.BrowserCallback.Status != "not-configured" || r.Webhook.Status != "not-configured" || !r.PollingAvailable {
		t.Fatalf("intranet without origin: %+v", r)
	}

	e = newEnv(t, "http://panel.lan:8080")
	app = e.addApp(5, map[string]string{"contents": "read"}, []string{"push"})
	e.gh.hook = HookConfig{URL: "https://hooks.example.test/gh"}
	r, _ = e.svc.CheckReachability(ctx, app.ID, "panel.lan:8080")
	if r.BrowserCallback.Status != "configured" || r.BrowserCallback.CallbackURL != "http://panel.lan:8080"+CallbackPath || !r.BrowserCallback.RequestHostMatches {
		t.Fatalf("callback: %+v", r.BrowserCallback)
	}
	if r.Webhook.Status != "no-deliveries" {
		t.Fatalf("webhook without deliveries: %+v", r.Webhook)
	}
	e.gh.delivery = &HookDelivery{StatusCode: 502, DeliveredAt: e.clock}
	if r, _ = e.svc.CheckReachability(ctx, app.ID, "x"); r.Webhook.Status != "failing" || r.BrowserCallback.RequestHostMatches {
		t.Fatalf("failing: %+v", r)
	}
	e.gh.delivery = &HookDelivery{StatusCode: 202, DeliveredAt: e.clock}
	if r, _ = e.svc.CheckReachability(ctx, app.ID, "x"); r.Webhook.Status != "delivered" {
		t.Fatalf("delivered: %+v", r.Webhook)
	}
	e.gh.down = true
	if r, err = e.svc.CheckReachability(ctx, app.ID, "x"); err != nil || r.Webhook.Status != "unknown" || r.BrowserCallback.Status != "configured" {
		t.Fatalf("GitHub down: %+v %v", r, err)
	}
}

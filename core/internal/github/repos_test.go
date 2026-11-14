package github

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func patConn(t *testing.T, e *env, tok string, accountID int64, login string, actor Actor) Connection {
	t.Helper()
	e.gh.users[tok] = User{ID: accountID, Login: login, Type: "User"}
	e.gh.canList[tok] = true
	c, err := e.svc.CreatePAT(ctx, PATInput{Token: tok}, actor)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func repo(id int64, owner, name string, private bool) Repository {
	vis := "public"
	if private {
		vis = "private"
	}
	return Repository{ID: id, FullName: owner + "/" + name, Name: name, Owner: Account{ID: id * 10, Login: owner}, Private: private,
		Visibility: vis, DefaultBranch: "main"}
}

// manyRepos makes n repositories of owner with ids from first.
func manyRepos(first int64, n int, owner string) []Repository {
	out := make([]Repository, n)
	for i := range out {
		out[i] = repo(first+int64(i), owner, fmt.Sprintf("svc-%03d", i), i%2 == 0)
	}
	return out
}

func syncUntilDone(t *testing.T, e *env, connID string, actor Actor) SyncStatus {
	t.Helper()
	for i := 0; i < 20; i++ {
		st, err := e.svc.SyncRepositories(ctx, connID, actor)
		if err != nil {
			t.Fatal(err)
		}
		if st.State != InventorySyncing {
			return st
		}
	}
	t.Fatal("sync did not finish")
	return SyncStatus{}
}

// GH-T05: more than two pages, with own, collaborator, organization,
// public and private repositories; later pages are searchable; nothing
// outside the authorization appears.
func TestInventorySpansPagesAndOnlyShowsWhatTheConnectionReaches(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_alice", 101, "octo", e.alice)
	e.gh.repos().orgs["ghp_alice"] = []Account{{ID: 7, Login: "acme", Type: "Organization"}}
	own := manyRepos(1000, 150, "octo")
	collab := repo(5000, "friend", "shared-tool", true) // collaborator repository
	archived := repo(5001, "octo", "old-site", false)
	archived.Archived = true
	e.gh.addRepos(SourceUser, append(own, collab, archived)...)
	e.gh.addRepos("org:acme", manyRepos(2000, 130, "acme")...)
	// A repository of an organization this account does not belong to.
	e.gh.addRepos("org:other", repo(9999, "other", "secret", true))

	st := syncUntilDone(t, e, c.ID, e.alice)
	if !st.Complete || st.State != InventoryComplete || st.Repositories != 282 || len(st.Sources) != 2 {
		t.Fatalf("complete inventory: %+v", st)
	}
	all, err := e.svc.ListRepositories(ctx, e.alice, RepoFilter{})
	if err != nil || len(all) != 282 {
		t.Fatalf("all repositories: %d %v", len(all), err)
	}
	// acme/svc-125 is on page 2 of the organization; octo/svc-149 on page 2 of the user.
	for _, q := range []string{"acme/svc-125", "OCTO/SVC-149", "friend/shared"} {
		got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{Query: q})
		if len(got) != 1 || got[0].ConnectionID != c.ID {
			t.Fatalf("search %q: %+v", q, got)
		}
	}
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{Query: "other/secret"}); len(got) != 0 {
		t.Fatalf("an organization the account is not in must not appear: %+v", got)
	}
	yes, no := true, false
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{Archived: &yes}); len(got) != 1 || got[0].ID != 5001 || !got[0].Archived {
		t.Fatalf("archived filter: %+v", got)
	}
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{Visibility: "private", Archived: &no, Owner: "friend"}); len(got) != 1 || !got[0].Private {
		t.Fatalf("visibility filter: %+v", got)
	}
	// Another account sees nothing of it.
	if got, _ := e.svc.ListRepositories(ctx, e.bob, RepoFilter{}); len(got) != 0 {
		t.Fatalf("bob sees alice's inventory: %d", len(got))
	}
	if _, err := e.svc.SyncRepositories(ctx, c.ID, e.bob); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob syncs alice's connection: %v", err)
	}
	if _, err := e.svc.RepositorySync(ctx, c.ID, e.bob); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob reads alice's sync: %v", err)
	}
	if _, err := e.svc.ListRefs(ctx, c.ID, 1000, "branches", 1, e.bob); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob browses alice's repository: %v", err)
	}
	e.mustNotContain([]any{st, all}, "ghp_alice")
}

func TestSyncStepReadsABoundedNumberOfPages(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	e.gh.repos().pageSize = 1
	e.gh.addRepos(SourceUser, manyRepos(1, 25, "octo")...)
	st, err := e.svc.SyncRepositories(ctx, c.ID, e.alice)
	if err != nil || st.State != InventorySyncing || st.Complete || st.Repositories != SyncPageBudget || st.Sources[0].NextPage != SyncPageBudget+1 {
		t.Fatalf("first step: %+v %v", st, err)
	}
	st = syncUntilDone(t, e, c.ID, e.alice)
	if !st.Complete || st.Repositories != 25 {
		t.Fatalf("finished: %+v", st)
	}
}

// GH-T06: a rate limit on page 2 shows the partial result, waits for the
// time GitHub gave, resumes where it stopped and never changes identity.
func TestRateLimitOnSecondPageShowsPartialResultAndWaits(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	patConn(t, e, "ghp_other", 102, "octo2", e.alice) // must never be used instead
	e.gh.addRepos(SourceUser, manyRepos(1, 250, "octo")...)
	r := e.gh.repos()
	r.pageErr["user#2"] = &APIError{Kind: ErrUnavailable, Status: 403, Reason: "rate limited", RetryAfter: 2 * time.Minute}

	st, err := e.svc.SyncRepositories(ctx, c.ID, e.alice)
	if err != nil || st.State != InventoryRateLimited || st.Complete || st.Repositories != 100 || st.RetryAt == nil ||
		!st.RetryAt.Equal(e.clock.Add(2*time.Minute)) || st.Sources[0].NextPage != 2 {
		t.Fatalf("rate limited step: %+v %v", st, err)
	}
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{}); len(got) != 100 {
		t.Fatalf("first page stays visible: %d", len(got))
	}
	calls := e.gh.count("ListRepositories")
	e.advance(time.Minute)
	if st, err = e.svc.SyncRepositories(ctx, c.ID, e.alice); err != nil || st.State != InventoryRateLimited || e.gh.count("ListRepositories") != calls {
		t.Fatalf("no call before the wait ends: %+v %v", st, err)
	}
	var rl *RateLimitedError
	if _, err := e.svc.ListRefs(ctx, c.ID, 1, "branches", 1, e.alice); !errors.As(err, &rl) {
		t.Fatalf("browsing also waits: %v", err)
	}
	delete(r.pageErr, "user#2")
	e.advance(61 * time.Second)
	st = syncUntilDone(t, e, c.ID, e.alice)
	if !st.Complete || st.Repositories != 250 {
		t.Fatalf("resumed: %+v", st)
	}
	if !strings.HasPrefix(r.lastPages[len(r.lastPages)-2], "user#2 ") {
		t.Fatalf("resumed from page 2: %v", r.lastPages)
	}
	for _, tok := range r.lastTokens {
		if tok != "ghp_a" {
			t.Fatalf("another identity was used: %q", tok)
		}
	}
}

// GitHub says the budget is spent on a successful answer: stop before it
// refuses, until the reset time.
func TestSyncStopsWhenRemainingRateIsZero(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	e.gh.addRepos(SourceUser, manyRepos(1, 250, "octo")...)
	e.gh.repos().rate["user#1"] = 0
	st, err := e.svc.SyncRepositories(ctx, c.ID, e.alice)
	if err != nil || st.State != InventoryRateLimited || st.Repositories != 100 || st.RetryAt == nil || !st.RetryAt.Equal(e.clock.Add(20*time.Minute)) {
		t.Fatalf("stop at zero remaining: %+v %v", st, err)
	}
	if n := e.gh.count("ListRepositories"); n != 1 {
		t.Fatalf("calls: %d", n)
	}
}

// One organization failing keeps the others and is never "complete".
func TestOneOrganizationFailingDoesNotHideOthersOrComplete(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	r := e.gh.repos()
	r.orgs["ghp_a"] = []Account{{ID: 7, Login: "acme"}, {ID: 8, Login: "sso-org"}}
	e.gh.addRepos(SourceUser, repo(1, "octo", "site", false))
	e.gh.addRepos("org:acme", repo(2, "acme", "api", true))
	e.gh.addRepos("org:sso-org", repo(3, "sso-org", "x", true))
	r.pageErr["org:sso-org#1"] = refused(403, "Resource protected by organization SAML enforcement.")

	st := syncUntilDone(t, e, c.ID, e.alice)
	if st.Complete || st.State != InventoryPartial || st.Repositories != 2 {
		t.Fatalf("partial: %+v", st)
	}
	var failed SourceStatus
	for _, s := range st.Sources {
		if s.Key == "org:sso-org" {
			failed = s
		} else if s.State != SyncComplete {
			t.Fatalf("other sources complete: %+v", s)
		}
	}
	if failed.State != SyncFailed || !strings.Contains(failed.Error, "SAML") {
		t.Fatalf("failed organization: %+v", failed)
	}
	// Organizations cannot be listed at all: the user's own repositories
	// are still synced, the result is partial.
	r.orgsErr = refused(403, "Resource not accessible by personal access token")
	st = syncUntilDone(t, e, c.ID, e.alice)
	if st.Complete || st.State != InventoryPartial {
		t.Fatalf("organization listing failed: %+v", st)
	}
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{Query: "octo/site"}); len(got) != 1 {
		t.Fatal("own repositories still listed")
	}
}

// ETags: a new round sends the stored ETag; 304 keeps the rows and costs
// no rate limit; a change on GitHub is picked up; a shorter list drops the
// pages that are gone; leaving an organization drops its repositories.
func TestNextRoundReusesETagsAndFollowsChanges(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	r := e.gh.repos()
	r.orgs["ghp_a"] = []Account{{ID: 7, Login: "acme"}}
	e.gh.addRepos(SourceUser, manyRepos(1, 150, "octo")...)
	e.gh.addRepos("org:acme", repo(900, "acme", "api", true))
	syncUntilDone(t, e, c.ID, e.alice)
	full := e.gh.count("ListRepositories:200")

	st := syncUntilDone(t, e, c.ID, e.alice)
	if !st.Complete || st.Repositories != 151 || e.gh.count("ListRepositories:200") != full {
		t.Fatalf("unchanged round must be all 304: %+v, 200s %d→%d", st, full, e.gh.count("ListRepositories:200"))
	}
	for _, p := range r.lastPages[len(r.lastPages)-3:] {
		if !strings.Contains(p, `W/"`) {
			t.Fatalf("ETag sent: %v", r.lastPages)
		}
	}
	e.gh.mu.Lock()
	r.sources[SourceUser] = r.sources[SourceUser][:90] // page 2 is gone
	r.sources[SourceUser][0].Archived = true
	r.orgs["ghp_a"] = nil // left acme
	e.gh.mu.Unlock()
	st = syncUntilDone(t, e, c.ID, e.alice)
	if !st.Complete || st.Repositories != 90 || len(st.Sources) != 1 {
		t.Fatalf("changes followed: %+v", st)
	}
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{RepoID: 1}); len(got) != 1 || !got[0].Archived {
		t.Fatalf("archived change: %+v", got)
	}
	// Removing the connection removes its inventory.
	if err := e.svc.DeleteConnection(ctx, c.ID, c.Version, e.alice); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = e.st.DB.QueryRow(`SELECT COUNT(*) FROM github_repositories`).Scan(&n)
	if n != 0 {
		t.Fatalf("rows left after delete: %d", n)
	}
}

// GH-T07: the same repository through an App installation and a PAT is
// shown once per connection; browsing uses the chosen one, and when that
// one stops working the other is never used instead.
func TestSameRepositoryThroughTwoConnectionsStaysBound(t *testing.T) {
	e := newEnv(t, origin)
	inst := installationConn(t, e) // installation 700, maker reaches repositories 3 and 9
	pat := patConn(t, e, "ghp_a", 21, "installer", e.alice)
	shared := repo(3, "acme", "shop", true)
	e.gh.addRepos(SourceInstallation, shared, repo(4, "acme", "hidden", true), repo(9, "acme", "docs", false))
	e.gh.addRepos(SourceUser, shared)
	r := e.gh.repos()
	r.commits["acme/shop"] = map[string]Commit{"main": {SHA: "aaa1", Message: "init"}}
	for i := 0; i < 4; i++ {
		e.gh.instTokens = append(e.gh.instTokens, Token{Value: fmt.Sprintf("ghs_%d", i), ExpiresAt: e.clock.Add(time.Hour)})
	}
	syncUntilDone(t, e, inst.ID, e.alice)
	syncUntilDone(t, e, pat.ID, e.alice)

	got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{Query: "acme/"})
	byConn := map[string][]int64{}
	for _, it := range got {
		byConn[it.ConnectionID] = append(byConn[it.ConnectionID], it.ID)
	}
	if fmt.Sprint(byConn[inst.ID]) != "[9 3]" && fmt.Sprint(byConn[inst.ID]) != "[3 9]" || fmt.Sprint(byConn[pat.ID]) != "[3]" {
		t.Fatalf("one entry per connection, installation limited to its maker's repositories: %v", byConn)
	}
	if _, err := e.svc.ResolveRef(ctx, inst.ID, 4, "main", e.alice); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a repository outside the connection: %v", err)
	}
	// Installation browsing uses a token narrowed to the repository.
	if res, err := e.svc.ResolveRef(ctx, inst.ID, 3, "main", e.alice); err != nil || res.Commit.SHA != "aaa1" {
		t.Fatalf("resolve through installation: %+v %v", res, err)
	}
	if fmt.Sprint(e.gh.lastScope.RepositoryIDs) != "[3]" || e.gh.lastScope.Permissions["contents"] != "read" {
		t.Fatalf("narrowed token: %+v", e.gh.lastScope)
	}
	// The PAT is revoked on GitHub: its calls fail, nothing switches to
	// the installation.
	r.refused["ghp_a"] = true
	before := len(r.lastTokens)
	if _, err := e.svc.ResolveRef(ctx, pat.ID, 3, "main", e.alice); !errors.Is(err, ErrRejected) {
		t.Fatalf("revoked PAT: %v", err)
	}
	for _, tok := range r.lastTokens[before:] {
		if tok != "ghp_a" {
			t.Fatalf("switched identity to %q", tok)
		}
	}
	// An expired PAT needs authorization again; still no switch.
	if _, err := e.st.DB.Exec(`UPDATE github_connections SET access_expires_at=? WHERE id=?`, e.clock.Add(-time.Hour).Format(time.RFC3339), pat.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ResolveRef(ctx, pat.ID, 3, "main", e.alice); !errors.Is(err, ErrNeedsReauth) {
		t.Fatalf("expired PAT: %v", err)
	}
	e.mustNotContain(got, "ghp_a", "ghs_0", "ghs_1")
}

// GH-T08: the branch moves after selection (the resolved SHA stays what was
// read), the default branch changes, the repository is empty, has no
// Compose file, or has several.
func TestRefsCommitsAndComposeDetection(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	shop, empty, plain := repo(1, "octo", "shop", true), repo(2, "octo", "fresh", true), repo(3, "octo", "lib", false)
	e.gh.addRepos(SourceUser, shop, empty, plain)
	syncUntilDone(t, e, c.ID, e.alice)
	r := e.gh.repos()
	e.gh.mu.Lock()
	r.refs["octo/shop"] = map[string][]Ref{"branches": {{Name: "main", SHA: "s1", Protected: true}, {Name: "dev", SHA: "d1"}}, "tags": {{Name: "v1.0", SHA: "s0"}}}
	r.commits["octo/shop"] = map[string]Commit{"main": {SHA: "s1", Message: "add api"}, "release": {SHA: "r1", Message: "release"}, "v1.0": {SHA: "s0"}}
	r.history["octo/shop"] = []Commit{{SHA: "s1", Message: "add api"}, {SHA: "s0", Message: "init"}}
	r.trees["s1"] = files("s1", "README.md", "compose.yaml", "compose.override.yml", "docker-compose.yml",
		"services/api/docker-compose.yaml", "services/api/main.go", "infra/compose.yml.bak")
	r.trees["r1"] = files("r1", "README.md")
	r.dirs["s1:"] = []Entry{{Name: "compose.yaml", Path: "compose.yaml", Type: "file"}, {Name: "services", Path: "services", Type: "dir"}}
	r.empty["octo/fresh"] = true
	e.gh.mu.Unlock()

	br, err := e.svc.ListRefs(ctx, c.ID, 1, "branches", 1, e.alice)
	if err != nil || len(br.Items) != 2 || br.Next || br.Items[0].SHA != "s1" || !br.Items[0].Protected {
		t.Fatalf("branches: %+v %v", br, err)
	}
	if tg, err := e.svc.ListRefs(ctx, c.ID, 1, "tags", 1, e.alice); err != nil || tg.Items[0].Name != "v1.0" {
		t.Fatalf("tags: %+v %v", tg, err)
	}
	if _, err := e.svc.ListRefs(ctx, c.ID, 1, "heads", 1, e.alice); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown kind: %v", err)
	}
	if cm, err := e.svc.ListCommits(ctx, c.ID, 1, "main", 1, e.alice); err != nil || len(cm.Items) != 2 {
		t.Fatalf("commits: %+v %v", cm, err)
	}
	if _, err := e.svc.ListCommits(ctx, c.ID, 1, "nope", 1, e.alice); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown ref: %v", err)
	}

	scan, err := e.svc.FindComposeFiles(ctx, c.ID, 1, "main", e.alice)
	if err != nil || scan.State != ComposeSeveral || scan.Commit.SHA != "s1" || len(scan.Projects) != 2 {
		t.Fatalf("several: %+v %v", scan, err)
	}
	root, api := scan.Projects[0], scan.Projects[1]
	if root.Directory != "." || fmt.Sprint(root.Files) != "[compose.yaml compose.override.yml]" || fmt.Sprint(root.Ignored) != "[docker-compose.yml]" {
		t.Fatalf("root project order: %+v", root)
	}
	if api.Directory != "services/api" || fmt.Sprint(api.Files) != "[services/api/docker-compose.yaml]" {
		t.Fatalf("subdirectory project: %+v", api)
	}
	// The branch moves after the selection: the scan answered s1; a new
	// resolve answers the new commit, the earlier result is unchanged.
	e.gh.mu.Lock()
	r.commits["octo/shop"]["main"] = Commit{SHA: "s2", Message: "moved"}
	e.gh.mu.Unlock()
	if res, _ := e.svc.ResolveRef(ctx, c.ID, 1, "main", e.alice); res.Commit.SHA != "s2" || scan.Commit.SHA != "s1" {
		t.Fatalf("moved branch: %+v", res)
	}
	// The default branch changed on GitHub: an empty ref follows it.
	e.gh.mu.Lock()
	moved := r.byID[1]
	moved.DefaultBranch = "release"
	r.byID[1] = moved
	e.gh.mu.Unlock()
	none, err := e.svc.FindComposeFiles(ctx, c.ID, 1, "", e.alice)
	if err != nil || none.State != ComposeNone || none.Ref != "release" || none.DefaultBranch != "release" || none.Commit.SHA != "r1" {
		t.Fatalf("no compose on the new default branch: %+v %v", none, err)
	}
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{RepoID: 1}); got[0].DefaultBranch != "release" {
		t.Fatalf("inventory follows the default branch: %+v", got)
	}
	// Empty repository.
	es, err := e.svc.FindComposeFiles(ctx, c.ID, 2, "", e.alice)
	if err != nil || es.State != ComposeEmptyRepository || len(es.Projects) != 0 {
		t.Fatalf("empty: %+v %v", es, err)
	}
	if _, err := e.svc.ResolveRef(ctx, c.ID, 2, "main", e.alice); !errors.Is(err, ErrEmptyRepository) {
		t.Fatalf("resolve empty: %v", err)
	}
	if b, err := e.svc.ListRefs(ctx, c.ID, 2, "branches", 1, e.alice); err != nil || len(b.Items) != 0 {
		t.Fatalf("branches of an empty repository: %+v %v", b, err)
	}
	// Directory browsing at a fixed commit.
	e.gh.mu.Lock()
	r.commits["octo/shop"]["s1"] = Commit{SHA: "s1"}
	e.gh.mu.Unlock()
	dir, err := e.svc.ListDirectory(ctx, c.ID, 1, "s1", "/", e.alice)
	if err != nil || dir.Path != "" || dir.Entries[0].Type != "dir" {
		t.Fatalf("directory: %+v %v", dir, err)
	}
	for _, p := range []string{"../etc", "a/../../b", "a//b", "a\\b"} {
		if _, err := e.svc.ListDirectory(ctx, c.ID, 1, "s1", p, e.alice); !errors.Is(err, ErrValidation) {
			t.Fatalf("path %q: %v", p, err)
		}
	}
	if _, err := e.svc.ListDirectory(ctx, c.ID, 1, "s1", "missing", e.alice); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing directory: %v", err)
	}
}

// A renamed repository (301 on the old name) is read again by its id.
func TestRenamedRepositoryIsFollowedByID(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	e.gh.addRepos(SourceUser, repo(1, "octo", "old", false))
	syncUntilDone(t, e, c.ID, e.alice)
	r := e.gh.repos()
	e.gh.mu.Lock()
	r.moved["octo/old"] = true
	r.byID[1] = repo(1, "acme", "new", false)
	r.refs["acme/new"] = map[string][]Ref{"branches": {{Name: "main", SHA: "x"}}}
	e.gh.mu.Unlock()
	br, err := e.svc.ListRefs(ctx, c.ID, 1, "branches", 1, e.alice)
	if err != nil || len(br.Items) != 1 {
		t.Fatalf("followed rename: %+v %v", br, err)
	}
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{RepoID: 1}); got[0].FullName != "acme/new" {
		t.Fatalf("inventory renamed: %+v", got)
	}
}

func TestComposeProjectsOrder(t *testing.T) {
	got := composeProjects(files("x", "b/compose.yml", "a/docker-compose.yml", "a/docker-compose.override.yaml", "compose.yaml", "a/compose.override.yaml").Entries)
	if fmt.Sprint(got) != "[{. [compose.yaml] []} {a [a/docker-compose.yml a/compose.override.yaml] []} {b [b/compose.yml] []}]" {
		t.Fatalf("%v", got)
	}
}

func TestConcurrentSyncStepsDoNotDuplicate(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	e.gh.repos().pageSize = 10
	e.gh.addRepos(SourceUser, manyRepos(1, 95, "octo")...)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.SyncRepositories(ctx, c.ID, e.alice); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	st := syncUntilDone(t, e, c.ID, e.alice)
	if !st.Complete || st.Repositories != 95 {
		t.Fatalf("%+v", st)
	}
	if got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{}); len(got) != 95 {
		t.Fatalf("duplicates: %d", len(got))
	}
}

// Spec 15.9: rows read under an earlier authorization are marked stale,
// never complete, and the next step re-reads them; repositories an
// installation no longer covers disappear at once; a rotated PAT starts
// from nothing.
func TestInventoryFollowsTheCurrentAuthorization(t *testing.T) {
	e := newEnv(t, origin)
	inst := installationConn(t, e) // covers 3 and 9
	e.gh.addRepos(SourceInstallation, repo(3, "acme", "shop", true), repo(9, "acme", "docs", false))
	for i := 0; i < 3; i++ {
		e.gh.instTokens = append(e.gh.instTokens, Token{Value: fmt.Sprintf("ghs_%d", i), ExpiresAt: e.clock.Add(time.Hour)})
	}
	syncUntilDone(t, e, inst.ID, e.alice)
	// The maker loses access to repository 9.
	if _, err := e.st.DB.Exec(`UPDATE github_connections SET repository_ids='[3]' WHERE id=?`, inst.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{ConnectionID: inst.ID})
	if len(got) != 1 || got[0].ID != 3 || !got[0].Stale || got[0].SeenAt.IsZero() {
		t.Fatalf("current coverage applied before answering: %+v", got)
	}
	if st, _ := e.svc.RepositorySync(ctx, inst.ID, e.alice); st.Complete || st.State != InventoryStale || !st.Sources[0].Stale {
		t.Fatalf("stale is not complete: %+v", st)
	}
	if st := syncUntilDone(t, e, inst.ID, e.alice); !st.Complete || st.Repositories != 1 {
		t.Fatalf("re-read: %+v", st)
	}

	pat := patConn(t, e, "ghp_old", 21, "installer", e.alice)
	e.gh.addRepos(SourceUser, repo(50, "installer", "a", false))
	syncUntilDone(t, e, pat.ID, e.alice)
	e.gh.users["ghp_new"] = User{ID: 21, Login: "installer"}
	e.gh.canList["ghp_new"] = true
	if _, err := e.svc.RotatePAT(ctx, pat.ID, 0, "ghp_new", e.alice); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.svc.RepositorySync(ctx, pat.ID, e.alice); st.State != InventoryNever || st.Repositories != 0 {
		t.Fatalf("rotation drops the inventory: %+v", st)
	}
}

// Review fixes (2026-11-14).

// An App user token renews every few hours; that alone must not make the
// inventory stale.
func TestTokenRenewalKeepsTheGeneration(t *testing.T) {
	a := Connection{Kind: KindAppUser, Account: Account{ID: 1}}
	b := a
	exp := time.Date(2026, 11, 14, 16, 0, 0, 0, time.UTC)
	b.AccessExpiresAt = &exp
	if generationOf(a) != generationOf(b) {
		t.Fatal("expiry changes the generation")
	}
	b.Scopes = []string{"repo"}
	if generationOf(a) == generationOf(b) {
		t.Fatal("scopes must change the generation")
	}
}

// Rows beyond the first step of a new round stay marked stale until they
// are read again.
func TestRowsNotReadAgainStayStale(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	e.gh.repos().pageSize = 1
	e.gh.addRepos(SourceUser, manyRepos(1, 15, "octo")...)
	syncUntilDone(t, e, c.ID, e.alice)
	if _, err := e.st.DB.Exec(`UPDATE github_connections SET scopes='["public_repo"]' WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.SyncRepositories(ctx, c.ID, e.alice)
	if err != nil || st.Complete || st.State != InventoryStale || !st.Sources[0].Stale {
		t.Fatalf("mid round: %+v %v", st, err)
	}
	got, _ := e.svc.ListRepositories(ctx, e.alice, RepoFilter{})
	stale := 0
	for _, it := range got {
		if it.Stale {
			stale++
		}
	}
	if len(got) != 15 || stale != 5 {
		t.Fatalf("rows not yet read again: %d of %d stale", stale, len(got))
	}
	if st := syncUntilDone(t, e, c.ID, e.alice); !st.Complete {
		t.Fatalf("finished: %+v", st)
	}
}

// After the wait the status says the sync can go on; a shorter wait never
// shortens a longer one.
func TestRateLimitWaitEndsAndIsNeverShortened(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	e.gh.addRepos(SourceUser, manyRepos(1, 150, "octo")...)
	e.gh.repos().pageErr["user#2"] = &APIError{Kind: ErrUnavailable, Status: 429, Reason: "rate limited", RetryAfter: 10 * time.Minute}
	if st, _ := e.svc.SyncRepositories(ctx, c.ID, e.alice); st.State != InventoryRateLimited {
		t.Fatalf("%+v", st)
	}
	e.svc.noteRateLimit(ctx, c.ID, e.clock.Add(time.Minute))
	e.advance(2 * time.Minute)
	if st, _ := e.svc.RepositorySync(ctx, c.ID, e.alice); st.State != InventoryRateLimited || !st.RetryAt.Equal(e.clock.Add(8*time.Minute)) {
		t.Fatalf("shortened: %+v", st)
	}
	e.advance(9 * time.Minute)
	if st, _ := e.svc.RepositorySync(ctx, c.ID, e.alice); st.State != InventorySyncing {
		t.Fatalf("after the wait: %+v", st)
	}
}

// A ref is put into a GitHub URL path: dot segments and other non-ref
// characters are refused before GitHub is asked.
func TestRefsCannotLeaveTheRepository(t *testing.T) {
	e := newEnv(t, "")
	c := patConn(t, e, "ghp_a", 101, "octo", e.alice)
	e.gh.addRepos(SourceUser, repo(1, "octo", "shop", true))
	syncUntilDone(t, e, c.ID, e.alice)
	before := e.gh.count("GetCommit") + e.gh.count("ListCommits")
	for _, ref := range []string{"../../../victim/private/commits/main", "a/./b", "a//b", "/main", "main/", "a\\b", "a b", "x\x00", "a..b/../c", "@{-1}", ".hidden"} {
		if _, err := e.svc.ResolveRef(ctx, c.ID, 1, ref, e.alice); !errors.Is(err, ErrValidation) {
			t.Fatalf("resolve %q: %v", ref, err)
		}
		if _, err := e.svc.FindComposeFiles(ctx, c.ID, 1, ref, e.alice); !errors.Is(err, ErrValidation) {
			t.Fatalf("compose %q: %v", ref, err)
		}
		if _, err := e.svc.ListCommits(ctx, c.ID, 1, ref, 1, e.alice); !errors.Is(err, ErrValidation) {
			t.Fatalf("commits %q: %v", ref, err)
		}
	}
	if after := e.gh.count("GetCommit") + e.gh.count("ListCommits"); after != before {
		t.Fatalf("GitHub was asked: %d", after-before)
	}
	for _, ok := range []string{"main", "feature/x-1", "v1.0.0", "4d81a2c"} {
		if err := checkRef(ok); err != nil {
			t.Fatalf("%q refused: %v", ok, err)
		}
	}
}

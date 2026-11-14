package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/github"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
)

// Repository calls of apiFakeGitHub: every token sees the same user
// repositories, 100 per page; octo/shop has branches, commits and Compose
// files; octo/fresh is empty.
func (f *apiFakeGitHub) apiRepos() []github.Repository {
	out := make([]github.Repository, 0, 120)
	for i := 0; i < 120; i++ {
		out = append(out, github.Repository{ID: int64(10 + i), FullName: fmt.Sprintf("octo/svc-%03d", i), Name: fmt.Sprintf("svc-%03d", i),
			Owner: github.Account{ID: 77, Login: "octo"}, Visibility: "public", DefaultBranch: "main"})
	}
	return append(out,
		github.Repository{ID: 1, FullName: "octo/shop", Name: "shop", Owner: github.Account{ID: 77, Login: "octo"}, Private: true, Visibility: "private", DefaultBranch: "main"},
		github.Repository{ID: 2, FullName: "octo/fresh", Name: "fresh", Owner: github.Account{ID: 77, Login: "octo"}, Visibility: "public"})
}

func (f *apiFakeGitHub) ListUserOrgs(context.Context, string) ([]github.Account, error) {
	return nil, nil
}

func (f *apiFakeGitHub) ListRepositories(_ context.Context, _ string, src github.RepoSource, page int, _ string) (github.RepoPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return github.RepoPage{}, &github.APIError{Kind: github.ErrUnavailable, Status: 429, Reason: "rate limited", RetryAfter: 90 * time.Second}
	}
	all := f.apiRepos()
	from, to := min((page-1)*100, len(all)), min(page*100, len(all))
	return github.RepoPage{Repositories: all[from:to], Next: to < len(all), RateRemaining: -1}, nil
}

func (f *apiFakeGitHub) GetRepositoryByID(_ context.Context, _ string, id int64) (github.Repository, error) {
	for _, r := range f.apiRepos() {
		if r.ID == id {
			return r, nil
		}
	}
	return github.Repository{}, &github.APIError{Kind: github.ErrRejected, Status: 404}
}

func emptyOr(fullName string) error {
	if fullName == "octo/fresh" {
		return &github.APIError{Kind: github.ErrRejected, Status: 409, Reason: "Git Repository is empty."}
	}
	return nil
}

func (f *apiFakeGitHub) ListRefs(_ context.Context, _, fullName, kind string, page int) ([]github.Ref, bool, error) {
	if kind == "tags" {
		return []github.Ref{{Name: "v1", SHA: "c0"}}, false, nil
	}
	if page == 1 {
		return []github.Ref{{Name: "main", SHA: "c1"}}, true, nil
	}
	return []github.Ref{{Name: "dev", SHA: "c2"}}, false, nil
}

func (f *apiFakeGitHub) ListCommits(_ context.Context, _, fullName, _ string, _ int) ([]github.Commit, bool, error) {
	if err := emptyOr(fullName); err != nil {
		return nil, false, err
	}
	return []github.Commit{{SHA: "c1", Message: "add"}}, false, nil
}

func (f *apiFakeGitHub) GetCommit(_ context.Context, _, fullName, ref string) (github.Commit, error) {
	if err := emptyOr(fullName); err != nil {
		return github.Commit{}, err
	}
	if ref != "main" && ref != "c1" {
		return github.Commit{}, &github.APIError{Kind: github.ErrRejected, Status: 422}
	}
	return github.Commit{SHA: "c1", Message: "add"}, nil
}

func (f *apiFakeGitHub) GetTree(context.Context, string, string, string) (github.Tree, error) {
	return github.Tree{SHA: "c1", Entries: []github.Entry{{Name: "compose.yaml", Path: "compose.yaml", Type: "file"}, {Name: "compose.yaml", Path: "web/compose.yaml", Type: "file"}}}, nil
}

func (f *apiFakeGitHub) ListDirectory(context.Context, string, string, string, string) ([]github.Entry, error) {
	return []github.Entry{{Name: "compose.yaml", Path: "compose.yaml", Type: "file"}, {Name: "web", Path: "web", Type: "dir"}}, nil
}

func TestGitHubRepositoryPickerAPI(t *testing.T) {
	g := newGitHubHarness(t, "")
	const tok = "ghp_repo_secret_value"
	g.gh.users[tok] = github.User{ID: 77, Login: "octo", Type: "User"}
	id := g.owner.do(http.MethodPost, "/management/v1/github/connections", map[string]string{"token": tok}, nil).Body["connection"].(map[string]any)["id"].(string)
	base := "/management/v1/github/connections/" + id + "/repositories"

	if r := g.owner.do(http.MethodGet, base+"/sync", nil, nil); r.Code != 200 || r.Body["sync"].(map[string]any)["state"] != "never" {
		t.Fatalf("before sync: %+v", r)
	}
	r := g.owner.do(http.MethodPost, base+"/sync", nil, nil)
	sync := r.Body["sync"].(map[string]any)
	if r.Code != 200 || sync["state"] != "complete" || sync["complete"] != true || sync["repositories"].(float64) != 122 {
		t.Fatalf("sync: %+v", r)
	}
	mustNotLeak(t, r, tok)

	// The merged list pages with the contract's cursors and finds items of
	// GitHub's second page.
	list := g.owner.do(http.MethodGet, "/management/v1/github/repositories?limit=50", nil, nil)
	if items := list.Body["items"].([]any); len(items) != 50 || list.Body["page"].(map[string]any)["nextCursor"] == "" {
		t.Fatalf("list: %+v", list.Body["page"])
	}
	found := g.owner.do(http.MethodGet, "/management/v1/github/repositories?q=svc-119&connectionId="+id, nil, nil)
	items := found.Body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["connectionId"] != id || items[0].(map[string]any)["fullName"] != "octo/svc-119" {
		t.Fatalf("search: %+v", found)
	}
	if r := g.owner.do(http.MethodGet, "/management/v1/github/repositories?visibility=secret", nil, nil); r.Code != 400 {
		t.Fatalf("bad filter: %+v", r)
	}
	mustNotLeak(t, list, tok)

	// Branches page through GitHub's pages.
	br := g.owner.do(http.MethodGet, base+"/1/branches", nil, nil)
	if br.Code != 200 || br.Body["page"].(map[string]any)["nextCursor"] != "2" {
		t.Fatalf("branches: %+v", br)
	}
	if br := g.owner.do(http.MethodGet, base+"/1/branches?cursor=2", nil, nil); br.Body["items"].([]any)[0].(map[string]any)["name"] != "dev" || br.Body["page"].(map[string]any)["nextCursor"] != nil {
		t.Fatalf("branches page 2: %+v", br)
	}
	if r := g.owner.do(http.MethodGet, base+"/1/branches?cursor=x", nil, nil); r.Code != 400 {
		t.Fatalf("bad cursor: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, base+"/1/tags", nil, nil); r.Code != 200 {
		t.Fatalf("tags: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, base+"/1/commits?ref=main", nil, nil); r.Code != 200 || len(r.Body["items"].([]any)) != 1 {
		t.Fatalf("commits: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, base+"/1/resolve?ref=main", nil, nil); r.Code != 200 || r.Body["resolved"].(map[string]any)["commit"].(map[string]any)["sha"] != "c1" {
		t.Fatalf("resolve: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, base+"/1/resolve?ref=nope", nil, nil); r.Code != 400 || r.Body["code"] != CodeValidationFailed {
		t.Fatalf("unknown ref: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, base+"/1/contents?ref=main&path=..", nil, nil); r.Code != 400 {
		t.Fatalf("path outside: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, base+"/1/contents?ref=main", nil, nil); r.Code != 200 || r.Body["directory"].(map[string]any)["entries"].([]any)[0].(map[string]any)["type"] != "dir" {
		t.Fatalf("contents: %+v", r)
	}
	cf := g.owner.do(http.MethodGet, base+"/1/compose-files?ref=main", nil, nil)
	if cf.Code != 200 || cf.Body["compose"].(map[string]any)["state"] != "several" || len(cf.Body["compose"].(map[string]any)["projects"].([]any)) != 2 {
		t.Fatalf("compose: %+v", cf)
	}
	if r := g.owner.do(http.MethodGet, base+"/2/compose-files", nil, nil); r.Code != 200 || r.Body["compose"].(map[string]any)["state"] != "empty-repository" {
		t.Fatalf("empty compose: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, base+"/2/commits", nil, nil); r.Code != http.StatusConflict || r.Body["code"] != CodeGitHubRepoEmpty {
		t.Fatalf("empty commits: %+v", r)
	}
	if r := g.owner.do(http.MethodGet, base+"/1", nil, nil); r.Code != 200 || r.Body["repository"].(map[string]any)["connectionId"] != id {
		t.Fatalf("repository: %+v", r)
	}
	for _, p := range []string{"/999/branches", "/abc/branches", "/0"} {
		if r := g.owner.do(http.MethodGet, base+p, nil, nil); r.Code != http.StatusNotFound {
			t.Fatalf("%s: %+v", p, r)
		}
	}

	// Another account reaches nothing of it.
	dev := g.createUser(g.owner, "dev", iam.RoleProjectAdmin, iam.Scope{Kind: iam.ScopeProject, ID: "prj_1"}, "10.0.0.21")
	for _, p := range []string{base + "/sync", base + "/1/branches", base + "/1/compose-files"} {
		if r := dev.do(http.MethodGet, p, nil, nil); r.Code != http.StatusNotFound {
			t.Fatalf("dev %s: %+v", p, r)
		}
	}
	if r := dev.do(http.MethodPost, base+"/sync", nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("dev sync: %+v", r)
	}
	if r := dev.do(http.MethodGet, "/management/v1/github/repositories", nil, nil); len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("dev list: %+v", r)
	}
	auditor := g.createUser(g.owner, "auditor", iam.RoleAuditor, iam.Global, "10.0.0.22")
	if r := auditor.do(http.MethodGet, "/management/v1/github/repositories", nil, nil); r.Code != http.StatusForbidden {
		t.Fatalf("auditor: %+v", r)
	}

	// Rate limited: the sync answers rate-limited with retryAt; browsing
	// answers GITHUB_UNAVAILABLE with the wait and does not ask GitHub.
	g.gh.mu.Lock()
	g.gh.down = true
	g.gh.mu.Unlock()
	r = g.owner.do(http.MethodPost, base+"/sync", nil, nil)
	if r.Code != 200 || r.Body["sync"].(map[string]any)["state"] != "rate-limited" || r.Body["sync"].(map[string]any)["retryAt"] == nil {
		t.Fatalf("rate limited sync: %+v", r)
	}
	r = g.owner.do(http.MethodGet, base+"/1/branches", nil, nil)
	if r.Code != http.StatusBadGateway || r.Body["code"] != CodeGitHubUnavailable || r.Body["retryAfterSeconds"].(float64) < 60 {
		t.Fatalf("browse while rate limited: %+v", r)
	}
}

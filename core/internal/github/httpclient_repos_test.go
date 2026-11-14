package github

import (
	"errors"
	"net/http"
	"testing"
)

func TestHTTPClientRepositoryPagesETagsAndLimits(t *testing.T) {
	c := localGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/orgs/acme/repos":
			if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("type") != "all" {
				t.Errorf("query: %s", r.URL.RawQuery)
			}
			if r.Header.Get("If-None-Match") == `W/"e2"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `W/"e2"`)
			w.Header().Set("Link", `<https://api.github.com/orgs/acme/repos?page=3>; rel="next", <https://api.github.com/orgs/acme/repos?page=5>; rel="last"`)
			w.Header().Set("X-RateLimit-Remaining", "17")
			w.Header().Set("X-RateLimit-Reset", "1795000000")
			w.Write([]byte(`[{"id":9,"name":"api","full_name":"acme/api","owner":{"id":7,"login":"acme","type":"Organization"},"private":true,"visibility":"internal","archived":true,"default_branch":"trunk","size":0,"permissions":{"push":true}}]`))
		case "/installation/repositories":
			w.Write([]byte(`{"total_count":1,"repositories":[{"id":3,"full_name":"acme/shop","private":false}]}`))
		case "/repos/acme/we ird/commits/feature/x":
			w.Write([]byte(`{"sha":"abc","commit":{"message":"first line\n\nbody","author":{"name":"Octo","date":"2026-11-01T10:00:00+08:00"}}}`))
		case "/repos/acme/empty/git/trees/main":
			w.WriteHeader(409)
			w.Write([]byte(`{"message":"Git Repository is empty."}`))
		case "/repos/acme/shop/git/trees/abc":
			w.Write([]byte(`{"sha":"abc","truncated":true,"tree":[{"path":"a/compose.yaml","type":"blob","mode":"100644","size":12},{"path":"a","type":"tree"},{"path":"l","type":"blob","mode":"120000"},{"path":"m","type":"commit"}]}`))
		case "/repos/acme/shop/branches":
			w.Header().Set("Link", `<x?page=1>; rel="prev"`)
			w.Write([]byte(`[{"name":"main","commit":{"sha":"abc"},"protected":true}]`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.EscapedPath())
			w.WriteHeader(404)
		}
	})
	p, err := c.ListRepositories(ctx, "tok", RepoSource{Kind: SourceOrg, Login: "acme"}, 2, "")
	if err != nil || !p.Next || p.ETag != `W/"e2"` || p.RateRemaining != 17 || p.RateReset.Unix() != 1795000000 || len(p.Repositories) != 1 {
		t.Fatalf("page: %+v %v", p, err)
	}
	if r := p.Repositories[0]; r.Visibility != "internal" || !r.Archived || r.DefaultBranch != "trunk" || !r.Push || r.Owner.Login != "acme" {
		t.Fatalf("repository: %+v", r)
	}
	if p, err := c.ListRepositories(ctx, "tok", RepoSource{Kind: SourceOrg, Login: "acme"}, 2, `W/"e2"`); err != nil || !p.NotModified || len(p.Repositories) != 0 {
		t.Fatalf("304: %+v %v", p, err)
	}
	if p, err := c.ListRepositories(ctx, "tok", RepoSource{Kind: SourceInstallation}, 1, ""); err != nil || p.Next || len(p.Repositories) != 1 || p.Repositories[0].Visibility != "public" {
		t.Fatalf("installation: %+v %v", p, err)
	}
	if cm, err := c.GetCommit(ctx, "tok", "acme/we ird", "feature/x"); err != nil || cm.SHA != "abc" || cm.Message != "first line" || cm.Date.Hour() != 2 {
		t.Fatalf("commit: %+v %v", cm, err)
	}
	if _, err := c.GetTree(ctx, "tok", "acme/empty", "main"); !errors.Is(err, ErrRejected) || !isStatus(err, 409) {
		t.Fatalf("empty: %v", err)
	}
	tr, err := c.GetTree(ctx, "tok", "acme/shop", "abc")
	if err != nil || !tr.Truncated || len(tr.Entries) != 4 || tr.Entries[0].Name != "compose.yaml" || tr.Entries[1].Type != "dir" || tr.Entries[2].Type != "symlink" || tr.Entries[3].Type != "submodule" {
		t.Fatalf("tree: %+v %v", tr, err)
	}
	refs, next, err := c.ListRefs(ctx, "tok", "acme/shop", "branches", 1)
	if err != nil || next || len(refs) != 1 || !refs[0].Protected {
		t.Fatalf("refs: %+v %v", refs, err)
	}
	if _, err := c.ListRepositories(ctx, "bad", RepoSource{Kind: SourceUser}, 1, ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("refused: %v", err)
	}
}

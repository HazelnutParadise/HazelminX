package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/composedeploy"
	"github.com/HazelnutParadise/hazelminx/core/internal/github"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource/gittest"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitunits"
)

const gitToken = "glpat-TOPSECRET-0123456789"

const gitCompose = `services:
  web:
    image: nginx:1.27
    volumes:
      - ./conf/app.conf:/etc/app.conf:ro
      - ./data:/var/lib/app
`

// fakeGitHub hands out access to a local repository the way 047.1/048.1
// would for a connection.
type fakeGitHub struct {
	url, token, commit string
	unusable           atomic.Bool
}

func (f *fakeGitHub) FetchAccess(_ context.Context, connID string, repoID int64, ref string, _ github.Actor) (github.FetchAccess, error) {
	if connID != "ghc_1" || repoID != 42 {
		return github.FetchAccess{}, github.ErrNotFound
	}
	return github.FetchAccess{URL: f.url, Host: "github.com", Token: f.token, RepoID: 42, FullName: "acme/shop", Ref: ref, Commit: f.commit}, nil
}

func (f *fakeGitHub) SourceUsable(context.Context, string, int64) error {
	if f.unusable.Load() {
		return fmt.Errorf("%w: the installation no longer includes the repository", github.ErrSourceUnusable)
	}
	return nil
}

type gitHarness struct {
	*composeDeployHarness
	git   *gitunits.Service
	fake  *fakeGitHub
	gbase string
}

func newGitHarness(t *testing.T) *gitHarness {
	h := newComposeDeployHarness(t, "")
	fake := &fakeGitHub{}
	g := &gitunits.Service{DB: h.st.DB, Units: h.units, GitHub: fake, Vault: h.vault, Fetcher: gitsource.Fetcher{AllowLocal: true}}
	h.d.Sources = g
	h.srv.MountGitUnits(g, nil)
	h.h = h.srv.Handler()
	return &gitHarness{composeDeployHarness: h, git: g, fake: fake, gbase: h.ubase + "/git"}
}

func (h *gitHarness) body(src map[string]any) map[string]any {
	b := map[string]any{"name": "web", "ref": "main", "mainFile": "compose.yaml",
		"target": map[string]any{"nodeId": h.node, "engineEnvironmentId": "env:/run/docker.sock"}}
	for k, v := range src {
		b[k] = v
	}
	return b
}

func (h *gitHarness) execute(t *testing.T) (string, map[string]any) {
	t.Helper()
	pl := h.plan(t)
	if pl["state"] == "awaiting_approval" {
		pl = h.approve(t, pl)
	}
	r := h.owner.do(http.MethodPost, "/management/v1/change-plans/"+pl["id"].(string)+"/execute",
		map[string]any{"digest": pl["digest"], "requestId": "git-" + pl["id"].(string)}, nil)
	if r.Code != 202 {
		t.Fatalf("execute: %d %s", r.Code, r.Raw)
	}
	id := r.Body["jobId"].(string)
	return id, h.waitJob(t, id)
}

func stagedContents(c []composedeploy.StagedFile) string {
	var b strings.Builder
	for _, f := range c {
		b.WriteString(f.Content + "\n")
	}
	return b.String()
}

// dump returns every text column of every table, to look for a value.
func dump(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	var b strings.Builder
	for _, tb := range tables {
		rs, err := db.Query(`SELECT * FROM "` + tb + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rs.Columns()
		for rs.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			rs.Scan(ptrs...)
			for _, v := range vals {
				switch x := v.(type) {
				case string:
					b.WriteString(x + "\n")
				case []byte:
					b.Write(x)
					b.WriteString("\n")
				}
			}
		}
		rs.Close()
	}
	return b.String()
}

func TestGitUnitDeploysTheCommitCoreFixedThroughThePlan(t *testing.T) {
	h := newGitHarness(t)
	repo := gittest.NewRepo(t, "shop")
	first := repo.Commit("main", "one", map[string]string{"compose.yaml": gitCompose, "conf/app.conf": "v1\n", "README.md": "not needed\n"})
	srv := repo.ServeHTTP(gitToken)
	src := map[string]any{"url": srv.URL(repo), "credential": map[string]any{"kind": "token", "token": gitToken}}

	r := h.owner.do(http.MethodPost, h.gbase+"/check", h.body(src), nil)
	if r.Code != 200 || obj(r.Body["source"])["commit"] != first {
		t.Fatalf("check: %d %s", r.Code, r.Raw)
	}
	if r := h.owner.do(http.MethodGet, h.ubase, nil, nil); r.Code != 404 {
		t.Fatalf("check saved a unit: %d", r.Code)
	}
	r = h.owner.do(http.MethodPost, h.gbase, h.body(src), nil)
	if r.Code != 201 {
		t.Fatalf("import: %d %s", r.Code, r.Raw)
	}
	unit, source := obj(r.Body["unit"]), obj(r.Body["source"])
	if unit["source"] != "git" || source["commit"] != first || source["ref"] != "main" || source["auth"] != "token" {
		t.Fatalf("imported: %s", r.Raw)
	}
	if files := fmt.Sprint(source["files"]); files != "[compose.yaml conf/app.conf]" {
		t.Fatalf("files taken: %s", files)
	}
	// The branch moves on; the unit keeps the commit it was imported at.
	second := repo.Commit("main", "two", map[string]string{"conf/app.conf": "v2\n"})
	jobID, j := h.execute(t)
	if j["state"] != "succeeded" {
		t.Fatalf("job: %v", j)
	}
	calls := h.agent.calls()
	if got := stagedContents(calls[len(calls)-1].Params["files"].([]composedeploy.StagedFile)); !strings.Contains(got, "v1\n") || strings.Contains(got, "v2") || strings.Contains(got, "not needed") {
		t.Fatalf("staged: %q", got)
	}
	r = h.owner.do(http.MethodGet, h.ubase+"/deployments", nil, nil)
	dep := obj(r.Body["deployments"].([]any)[0])
	if dep["sourceCommit"] != first || dep["sourceRef"] != "main" || dep["revision"] != jobID || !strings.HasSuffix(dep["sourceRepo"].(string), "/shop.git") {
		t.Fatalf("deployment: %s", r.Raw)
	}

	// Moving to the new commit keeps the stored token for the same URL and
	// voids nothing silently: the unit version changes.
	cur := h.owner.do(http.MethodGet, h.ubase, nil, nil)
	src["credential"] = map[string]any{"unchanged": true}
	r = h.owner.do(http.MethodPut, h.gbase, h.body(src), etag(cur))
	if r.Code != 200 || obj(r.Body["source"])["commit"] != second {
		t.Fatalf("move: %d %s", r.Code, r.Raw)
	}
	_, j = h.execute(t)
	calls = h.agent.calls()
	if j["state"] != "succeeded" || !strings.Contains(stagedContents(calls[len(calls)-1].Params["files"].([]composedeploy.StagedFile)), "v2\n") {
		t.Fatalf("second deployment: %v", j)
	}

	// The token is revoked on the server: a new plan shows the source check
	// failing, so nothing deploys with a credential that no longer works.
	srv.SetToken("rotated")
	pl := h.plan(t)
	if raw, _ := json.Marshal(pl); !strings.Contains(string(raw), "can no longer be read with the stored credential") {
		t.Fatalf("plan after revocation: %s", raw)
	}
	srv.SetToken(gitToken)

	// The stored token is kept only for the same URL.
	cur = h.owner.do(http.MethodGet, h.ubase, nil, nil)
	other := map[string]any{"url": "https://example.com/other.git", "credential": map[string]any{"unchanged": true}}
	if r := h.owner.do(http.MethodPut, h.gbase, h.body(other), etag(cur)); r.Code != 400 {
		t.Fatalf("unchanged credential for another URL: %d %s", r.Code, r.Raw)
	}
	// Files of a Git unit are not edited directly.
	body := unitBody(nil)
	if r := h.owner.do(http.MethodPut, h.ubase, body, etag(cur)); r.Code != 400 {
		t.Fatalf("direct edit of a Git unit: %d %s", r.Code, r.Raw)
	}

	// The token is nowhere but the secret store's sealed value.
	raw, _ := json.Marshal(j)
	all := dump(t, h.st.DB) + string(raw) + fmt.Sprint(calls)
	if strings.Contains(all, gitToken) {
		t.Fatal("the token is stored or sent in clear")
	}
	uris, _ := srv.Requests()
	if strings.Contains(strings.Join(uris, " "), gitToken) {
		t.Fatal("the token went into a URL")
	}
	// Deleting the unit removes its credential.
	before := countSecrets(t, h.st.DB)
	cur = h.owner.do(http.MethodGet, h.ubase, nil, nil)
	if r := h.owner.do(http.MethodDelete, h.ubase, nil, etag(cur)); r.Code != 204 {
		t.Fatalf("delete: %d %s", r.Code, r.Raw)
	}
	if after := countSecrets(t, h.st.DB); after != before-1 {
		t.Fatalf("secrets %d → %d", before, after)
	}
}

func countSecrets(t *testing.T, db *sql.DB) int {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM secrets WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		if err := db.QueryRow(`SELECT COUNT(*) FROM secrets`).Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	return n
}

func TestGitUnitRefusesContentItCannotDeployCompletely(t *testing.T) {
	h := newGitHarness(t)
	repo := gittest.NewRepo(t, "bad")
	srv := repo.ServeHTTP("")
	cases := map[string]struct {
		files map[string]string
		code  string
	}{
		"symlinked mount": {map[string]string{"compose.yaml": gitCompose, "conf/app.conf": "->/etc/passwd"}, "symlink"},
		"linked dir":      {map[string]string{"compose.yaml": gitCompose, "conf": "->/etc"}, "symlink"},
		"lfs":             {map[string]string{"compose.yaml": gitCompose, "conf/app.conf": "version https://git-lfs.github.com/spec/v1\noid sha256:00\nsize 1\n"}, "lfs"},
		"build":           {map[string]string{"compose.yaml": "services:\n  web:\n    build: ./app\n", "app/Dockerfile": "FROM scratch\n"}, "build"},
		"main is a link":  {map[string]string{"real.yaml": gitCompose, "compose.yaml": "->real.yaml"}, "symlink"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Each case starts from an empty tree.
			sha := repo.Commit(strings.ReplaceAll(name, " ", "-"), name, c.files)
			b := h.body(map[string]any{"url": srv.URL(repo)})
			b["ref"] = sha
			r := h.owner.do(http.MethodPost, h.gbase+"/check", b, nil)
			if r.Code != 422 || r.Body["code"] != CodeGitSourceRefused || !strings.Contains(string(r.Raw), `"code":"`+c.code+`"`) {
				t.Fatalf("%d %s", r.Code, r.Raw)
			}
		})
	}
	base := repo.Commit("sub", "base", map[string]string{"compose.yaml": gitCompose, "conf/app.conf": "x\n"})
	sha := repo.Gitlink("sub", "data/vendor", base)
	b := h.body(map[string]any{"url": srv.URL(repo)})
	b["ref"] = sha
	if r := h.owner.do(http.MethodPost, h.gbase+"/check", b, nil); r.Code != 422 || !strings.Contains(string(r.Raw), `"code":"submodule"`) {
		t.Fatalf("submodule under a mounted dir: %d %s", r.Code, r.Raw)
	}
	if r := h.owner.do(http.MethodGet, h.ubase, nil, nil); r.Code != 404 {
		t.Fatal("a refused source saved a unit")
	}
}

func TestGitHubUnitIsRecheckedBeforeDeploying(t *testing.T) {
	h := newGitHarness(t)
	repo := gittest.NewRepo(t, "gh")
	sha := repo.Commit("main", "one", map[string]string{"compose.yaml": gitCompose, "conf/app.conf": "v1\n"})
	srv := repo.ServeHTTP("ghs_installation")
	h.fake.url, h.fake.token, h.fake.commit = srv.URL(repo), "ghs_installation", sha

	if r := h.owner.do(http.MethodPost, h.gbase, h.body(map[string]any{"github": map[string]any{"connectionId": "ghc_1", "repoId": 7}}), nil); r.Code != 404 {
		t.Fatalf("a repository the connection does not list: %d %s", r.Code, r.Raw)
	}
	gh := map[string]any{"github": map[string]any{"connectionId": "ghc_1", "repoId": 42}}
	withCred := h.body(gh)
	withCred["credential"] = map[string]any{"kind": "token", "token": "x"}
	if r := h.owner.do(http.MethodPost, h.gbase, withCred, nil); r.Code != 400 {
		t.Fatalf("a credential with a GitHub repository: %d", r.Code)
	}
	r := h.owner.do(http.MethodPost, h.gbase, h.body(gh), nil)
	if r.Code != 201 || obj(r.Body["source"])["repoName"] != "acme/shop" || obj(r.Body["source"])["auth"] != "connection" {
		t.Fatalf("import: %d %s", r.Code, r.Raw)
	}
	pl := h.plan(t)
	raw, _ := json.Marshal(pl)
	if !strings.Contains(string(raw), `"commit":"`+sha+`"`) || !strings.Contains(string(raw), `"git-source"`) {
		t.Fatalf("the plan does not name the source: %s", raw)
	}
	// The installation loses the repository after the plan was approved:
	// the deployment stops before anything is sent to the node.
	if pl["state"] == "awaiting_approval" {
		pl = h.approve(t, pl)
	}
	h.fake.unusable.Store(true)
	before := len(h.agent.calls())
	r = h.owner.do(http.MethodPost, "/management/v1/change-plans/"+pl["id"].(string)+"/execute",
		map[string]any{"digest": pl["digest"], "requestId": "gh-1"}, nil)
	if r.Code == 202 {
		j := h.waitJob(t, r.Body["jobId"].(string))
		jr, _ := json.Marshal(j)
		if j["state"] == "succeeded" || !strings.Contains(string(jr), "GIT_SOURCE_CHANGED") {
			t.Fatalf("job: %s", jr)
		}
	} else if r.Code != 409 || r.Body["code"] != CodeChangePlanChanged {
		// The plan no longer matches: its source check failed.
		t.Fatalf("execute: %d %s", r.Code, r.Raw)
	}
	if len(h.agent.calls()) != before {
		t.Fatal("a step reached the node after the source became unusable")
	}
	pl = h.plan(t)
	raw, _ = json.Marshal(pl)
	if !strings.Contains(string(raw), "no longer includes the repository") {
		t.Fatalf("a new plan does not show the failed source check: %s", raw)
	}
	// The installation token never lands in the database.
	if strings.Contains(dump(t, h.st.DB), "ghs_installation") {
		t.Fatal("installation token stored")
	}
}

func TestGitUnitOverSSHNeedsThePinnedHostKey(t *testing.T) {
	h := newGitHarness(t)
	repo := gittest.NewRepo(t, "ssh")
	sha := repo.Commit("main", "one", map[string]string{"compose.yaml": gitCompose, "conf/app.conf": "v1\n"})
	s := repo.ServeSSH()
	_, otherHost, _ := gittest.NewKey(t)
	cred := map[string]any{"kind": "ssh-key", "privateKey": string(s.ClientKey), "hostKey": otherHost}
	b := h.body(map[string]any{"url": s.URL(repo), "credential": cred})
	if r := h.owner.do(http.MethodPost, h.gbase, b, nil); r.Code != 422 || r.Body["code"] != CodeGitAuthFailed {
		t.Fatalf("another host key: %d %s", r.Code, r.Raw)
	}
	cred["hostKey"] = s.HostKey
	r := h.owner.do(http.MethodPost, h.gbase, b, nil)
	if r.Code != 201 || obj(r.Body["source"])["commit"] != sha || obj(r.Body["source"])["auth"] != "ssh-key" || obj(r.Body["source"])["hostKey"] != s.HostKey {
		t.Fatalf("import over ssh: %d %s", r.Code, r.Raw)
	}
	if _, j := h.execute(t); j["state"] != "succeeded" {
		t.Fatalf("deploy: %v", j)
	}
	if strings.Contains(dump(t, h.st.DB), "PRIVATE KEY") {
		t.Fatal("the private key is stored in clear")
	}
}

// A directory mounted from the repository is taken whole even when it holds
// the Compose files themselves (they were all the first analysis saw).
func TestGitUnitTakesWholeMountedDirectories(t *testing.T) {
	h := newGitHarness(t)
	repo := gittest.NewRepo(t, "dirs")
	srv := repo.ServeHTTP("")
	root := repo.Commit("root", "root", map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx:1.27\n    volumes: [\"./:/usr/share/nginx/html:ro\"]\n",
		"index.html":   "<h1>hi</h1>\n", "css/site.css": "body{}\n"})
	b := h.body(map[string]any{"url": srv.URL(repo)})
	b["ref"] = root
	r := h.owner.do(http.MethodPost, h.gbase+"/check", b, nil)
	if r.Code != 200 || fmt.Sprint(obj(r.Body["source"])["files"]) != "[compose.yaml css/site.css index.html]" {
		t.Fatalf("root mount: %d %s", r.Code, r.Raw)
	}
	// GH-T09: a URL with no credential deploys like the others.
	if r := h.owner.do(http.MethodPost, h.gbase, b, nil); r.Code != 201 || obj(r.Body["source"])["auth"] != "none" {
		t.Fatalf("import without a credential: %d %s", r.Code, r.Raw)
	}
	if _, j := h.execute(t); j["state"] != "succeeded" {
		t.Fatalf("deploy without a credential: %v", j)
	}
	cur := h.owner.do(http.MethodGet, h.ubase, nil, nil)
	if r := h.owner.do(http.MethodDelete, h.ubase, nil, etag(cur)); r.Code != 204 {
		t.Fatalf("delete: %d", r.Code)
	}
	extra := repo.Commit("extra", "extra", map[string]string{
		"compose.yaml":    "services:\n  web:\n    image: nginx:1.27\n    volumes: [\"./deploy:/etc/deploy:ro\"]\n",
		"deploy/prod.yml": "services: {}\n", "deploy/app.conf": "x=1\n"})
	b = h.body(map[string]any{"url": srv.URL(repo), "extraFiles": []string{"deploy/prod.yml"}})
	b["ref"] = extra
	r = h.owner.do(http.MethodPost, h.gbase+"/check", b, nil)
	if r.Code != 200 || fmt.Sprint(obj(r.Body["source"])["files"]) != "[compose.yaml deploy/app.conf deploy/prod.yml]" {
		t.Fatalf("directory holding an extra file: %d %s", r.Code, r.Raw)
	}
	legacy := repo.Commit("legacy", "legacy", map[string]string{"compose.yaml": gitCompose,
		"conf/app.conf": "version https://hawser.github.com/spec/v1\noid sha256:00\nsize 1\n"})
	b = h.body(map[string]any{"url": srv.URL(repo)})
	b["ref"] = legacy
	if r := h.owner.do(http.MethodPost, h.gbase+"/check", b, nil); r.Code != 422 || !strings.Contains(string(r.Raw), `"code":"lfs"`) {
		t.Fatalf("legacy LFS pointer: %d %s", r.Code, r.Raw)
	}
}

// A hand-made tree with an entry Git does not allow (".git" inside data/)
// is refused as content, not reported as an unreachable server, and never
// becomes unit files.
func TestGitUnitRefusesATreeWithPathsGitDoesNotAllow(t *testing.T) {
	h := newGitHarness(t)
	repo := gittest.NewRepo(t, "evil")
	repo.Commit("main", "one", map[string]string{"compose.yaml": gitCompose, "conf/app.conf": "v1\n"})
	bare := filepath.Join(repo.Root, "evil.git")
	run := func(stdin string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = bare
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	blob := run("pwned\n", "hash-object", "-w", "--stdin")
	compose := run(gitCompose, "hash-object", "-w", "--stdin")
	conf := run("v1\n", "hash-object", "-w", "--stdin")
	inner := run("100644 blob "+blob+"\tescape.txt\n", "mktree")
	// data/ holds an entry named ".." (a tree) and ".git" (a tree).
	data := run("040000 tree "+inner+"\t.git\n100644 blob "+blob+"\tok.txt\n", "mktree")
	confT := run("100644 blob "+conf+"\tapp.conf\n", "mktree")
	root := run("100644 blob "+compose+"\tcompose.yaml\n040000 tree "+confT+"\tconf\n040000 tree "+data+"\tdata\n", "mktree")
	commit := run("", "-c", "user.name=t", "-c", "user.email=t@x", "commit-tree", root, "-m", "evil")
	run("", "update-ref", "refs/heads/evil", commit)
	srv := repo.ServeHTTP("")

	b := h.body(map[string]any{"url": srv.URL(repo)})
	b["ref"] = commit
	for _, path := range []string{h.gbase + "/check", h.gbase} {
		r := h.owner.do(http.MethodPost, path, b, nil)
		if r.Code != 422 || r.Body["code"] != CodeGitSourceRefused || r.Body["retryable"] == true {
			t.Fatalf("%s: %d %s", path, r.Code, r.Raw)
		}
	}
	if r := h.owner.do(http.MethodGet, h.ubase, nil, nil); r.Code != 404 {
		t.Fatal("a refused tree saved a unit")
	}
}

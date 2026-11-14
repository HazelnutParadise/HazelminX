package composedeploy

// End to end from a real Git repository to containers (ticket 049.1): a
// repository served over HTTP with a token by git http-backend → Core
// resolves and fetches one commit (gitunits) → 044.1 unit in SQLite →
// compose.deploy step through the real agent (uid 65534) and executor
// (root) → dockerd. Same opt-in as TestInteropDeploysThroughTheRealAgentExecutorAndDocker.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource/gittest"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitunits"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

type gitVault struct{ *memSecrets }

func (g gitVault) Delete(_ context.Context, id, _ string) error { delete(g.vals, id); return nil }

type nopAudit struct{}

func (nopAudit) RecordTx(context.Context, *sql.Tx, iam.AuditEvent) error { return nil }

type oneNode struct{ id, fp string }

func (n oneNode) Node(_ context.Context, id string) (string, string, error) {
	if id != n.id {
		return "", "", fmt.Errorf("no node %s", id)
	}
	return n.fp, "interop", nil
}

// seed inserts a row with placeholders for every required column.
func seed(t *testing.T, db *sql.DB, table string, fixed map[string]any) {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	var vals []any
	for rows.Next() {
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		rows.Scan(&name, &typ, &notnull, &dflt, &pk)
		if v, ok := fixed[name]; ok {
			cols, vals = append(cols, name), append(vals, v)
		} else if (notnull == 1 || pk == 1) && !dflt.Valid {
			var v any = name + "-x"
			if strings.Contains(strings.ToUpper(typ), "INT") {
				v = 1
			}
			cols, vals = append(cols, name), append(vals, v)
		}
	}
	rows.Close()
	if _, err := db.Exec(fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, table, strings.Join(cols, ","),
		strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")), vals...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

const gitInteropToken = "glpat-interop-do-not-leak-77"

func TestInteropDeploysAGitCommitThroughTheRealAgentExecutorAndDocker(t *testing.T) {
	agentBin, privBin := os.Getenv("HAZELMINX_AGENT_BIN"), os.Getenv("HAZELMINX_PRIVILEGED_BIN")
	if agentBin == "" || privBin == "" {
		t.Skip("HAZELMINX_AGENT_BIN and HAZELMINX_PRIVILEGED_BIN not set")
	}
	if os.Getenv("HAZELMINX_DISPOSABLE_HOST") != "1" || os.Geteuid() != 0 {
		t.Skip("deploys containers on the host: needs root and HAZELMINX_DISPOSABLE_HOST=1")
	}
	if _, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skip("no Docker engine on /var/run/docker.sock")
	}
	ctx := context.Background()
	x := setup(t, agentBin, privBin)
	const composeProject = "hzxgit-prod-web"
	root := "/var/lib/hazelminx/compose/" + composeProject
	t.Cleanup(func() {
		out, _ := exec.Command("docker", "compose", "-p", composeProject, "down", "-v").CombinedOutput()
		t.Logf("cleanup: %s", out)
		_ = os.RemoveAll(root)
	})

	// The repository: a compose file, the config it mounts, and a file
	// nothing references, which must not be taken.
	repo := gittest.NewRepo(t, "shop")
	compose := "services:\n  web:\n    image: busybox:1.37\n" +
		"    command: [\"sh\", \"-c\", \"cat /conf/app.conf > /data/seen && sleep 3600\"]\n" +
		"    volumes:\n      - ./conf/app.conf:/conf/app.conf:ro\n      - ./data:/data\n"
	first := repo.Commit("main", "one", map[string]string{"compose.yaml": compose, "conf/app.conf": "commit=1\n", "notes/unused.txt": "x\n"})
	srv := repo.ServeHTTP(gitInteropToken)

	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	seed(t, st.DB, "users", map[string]any{"id": "usr_interop"})
	seed(t, st.DB, "nodes", map[string]any{"id": "node_interop", "state": "active"})
	vault := gitVault{&memSecrets{vals: map[string][]byte{}}}
	units := projects.New(projects.Options{DB: st.DB, Vault: vault, Reveal: vault, Audit: nopAudit{}})
	actor := projects.Actor{UserID: "usr_interop", Username: "interop"}
	p, err := units.CreateProject(ctx, actor, projects.ProjectInput{Name: "hzxgit"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := units.CreateEnvironment(ctx, actor, p.ID, projects.EnvironmentInput{Name: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	git := &gitunits.Service{DB: st.DB, Units: units, Vault: vault, Fetcher: gitsource.Fetcher{AllowLocal: true}}
	d := &Deployer{Units: units, Store: NewStore(st.DB, nil), Grants: x.d.Grants, Steps: x.runner,
		Nodes: oneNode{id: "node_interop", fp: x.fp}, Sources: git}
	in := gitunits.Input{Name: "web", URL: srv.URL(repo), Credential: &gitunits.Credential{Kind: "token", Token: gitInteropToken},
		Ref: "main", MainFile: "compose.yaml", Target: &projects.Target{NodeID: "node_interop", EngineEnvironmentID: "env:/var/run/docker.sock"}}

	deploy := func(n int) *Deployment {
		t.Helper()
		u, err := units.GetUnit(ctx, p.ID, env.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		params, _ := json.Marshal(Params{ProjectID: p.ID, EnvironmentID: env.ID, UnitID: u.ID, UnitVersion: u.Version})
		job := &jobs.Job{ID: fmt.Sprintf("job_gitinterop%d", n), Kind: KindDeploy, Node: x.fp, Params: params, Requester: "usr_interop",
			Resources: []jobs.ResourceRef{{Kind: ResourceKind, ID: composeProject, Node: x.fp}}}
		rep := &logRep{}
		if err := d.deploy(ctx, job, &jobs.Step{Index: 1, Name: ActionDeploy}, rep); err != nil {
			t.Fatalf("deploy %d: %v\nstep logs:\n%s\n%s", n, err, rep.String(), x.logs.String())
		}
		if strings.Contains(rep.String(), gitInteropToken) {
			t.Fatal("the token reached the step log")
		}
		dep, err := d.Store.ByJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		return dep
	}

	// 1. Import at main = first, then main moves on before deploying.
	res, err := git.Import(ctx, actor, p.ID, env.ID, 0, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Source.Commit != first || strings.Join(res.Source.Files, ",") != "compose.yaml,conf/app.conf" || !res.Analysis.Valid {
		t.Fatalf("import: %+v", res.Source)
	}
	second := repo.Commit("main", "two", map[string]string{"conf/app.conf": "commit=2\n"})
	dep := deploy(1)
	if dep.State != StateSucceeded || dep.SourceCommit != first || dep.SourceRef != "main" {
		t.Fatalf("deployment 1: %+v", dep)
	}
	waitFile(t, root+"/data/data/seen", "commit=1\n")
	if _, err := os.Stat(root + "/revisions/job_gitinterop1/notes"); err == nil {
		t.Fatal("a file nothing references was deployed")
	}

	// 2. Move the unit to the commit main names now and deploy again.
	u, _ := units.GetUnit(ctx, p.ID, env.ID, false)
	in.Credential = &gitunits.Credential{Unchanged: true}
	if res, err = git.Import(ctx, actor, p.ID, env.ID, u.Version, in); err != nil || res.Source.Commit != second {
		t.Fatalf("move: %v %+v", err, res.Source)
	}
	dep = deploy(2)
	if dep.SourceCommit != second {
		t.Fatalf("deployment 2: %+v", dep)
	}
	waitFile(t, root+"/data/data/seen", "commit=2\n")

	// 3. The token is nowhere on the host: not in the revision
	// directories, the ledger, the agent's state or the executor's log.
	var leaked []string
	for _, dir := range []string{root, filepath.Dir(x.ledger)} {
		filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
			if err == nil && fi.Mode().IsRegular() {
				if b, _ := os.ReadFile(p); strings.Contains(string(b), gitInteropToken) {
					leaked = append(leaked, p)
				}
			}
			return nil
		})
	}
	if len(leaked) > 0 || strings.Contains(x.logs.String(), gitInteropToken) {
		t.Fatalf("the token reached the host: %v", leaked)
	}
	uris, _ := srv.Requests()
	if strings.Contains(strings.Join(uris, " "), gitInteropToken) {
		t.Fatal("the token went into a URL")
	}
}

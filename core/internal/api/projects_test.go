package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/engines"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

type projectsHarness struct {
	*harness
	st    *store.Store
	reg   *nodereg.Registry
	es    *engines.Store
	vault *secrets.Service
	owner *client
}

// newProjectsHarness puts IAM, nodes, engine reports, secrets and projects
// in one database, as Core does: units refer to nodes.
func newProjectsHarness(t *testing.T) *projectsHarness {
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
	srv.MountIAM(svc, IAMOptions{})
	pki, err := nodes.LoadOrCreatePKI(context.Background(), &pkiSecrets{vals: map[string][]byte{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := nodereg.New(nodereg.Options{DB: st.DB, PKI: pki, IssuerKeyPEM: []byte("pub"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	srv.RegisterNodes(reg)
	es := engines.NewStore(st.DB, nil)
	srv.MountProjects(projects.New(projects.Options{DB: st.DB, Vault: vault, Audit: svc, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}), es)
	hs.h, hs.svc, hs.srv = srv.Handler(), svc, srv
	return &projectsHarness{harness: hs, st: st, reg: reg, es: es, vault: vault, owner: hs.bootstrap()}
}

func (ph *projectsHarness) audit(t *testing.T, resourceID string) []string {
	t.Helper()
	rows, err := ph.st.DB.Query(`SELECT action || ':' || result FROM audit_events WHERE resource_id = ? ORDER BY seq`, resourceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func etag(r resp) map[string]string { return map[string]string{"If-Match": r.Hdr.Get("ETag")} }

const webCompose = `services:
  web:
    build: ./web
    ports: ["127.0.0.1:8080:80"]
    volumes: [./nginx.conf:/etc/nginx/nginx.conf:ro, data:/data]
    secrets: [db_pw]
    x-team: storefront
  debug:
    image: busybox:1.37
    profiles: [debug]
volumes:
  data: {}
secrets:
  db_pw:
    file: ./db_pw.txt
`

func unitBody(target map[string]any) map[string]any {
	b := map[string]any{
		"name": "web", "source": "upload", "mainFile": "compose.yaml",
		"files": []map[string]any{
			{"path": "compose.yaml", "content": webCompose},
			{"path": "web/Dockerfile", "content": "FROM nginx:1.27\n"},
			{"path": "nginx.conf", "content": "events {}\n"},
			{"path": "db_pw.txt", "content": "s3cr3t-value", "secret": true},
		},
	}
	if target != nil {
		b["target"] = target
	}
	return b
}

func TestProjectsEnvironmentsAndComposeUnitsOverTheAPI(t *testing.T) {
	ph := newProjectsHarness(t)
	owner := ph.owner

	r := owner.do(http.MethodPost, "/management/v1/projects", map[string]any{"name": "shop", "displayName": "Shop"}, nil)
	if r.Code != 201 || r.Hdr.Get("ETag") != `"v1"` {
		t.Fatalf("create project: %d %s", r.Code, r.Raw)
	}
	pid := obj(r.Body["project"])["id"].(string)
	if r := owner.do(http.MethodPost, "/management/v1/projects", map[string]any{"name": "shop"}, nil); r.Code != 409 || r.Body["code"] != CodeAlreadyExists {
		t.Fatalf("duplicate name: %d %s", r.Code, r.Raw)
	}
	for _, bad := range []string{"Shop", "1shop", "shop-", "a-very-long-project-name-over-30", "shop_x"} {
		if r := owner.do(http.MethodPost, "/management/v1/projects", map[string]any{"name": bad}, nil); r.Code != 400 {
			t.Fatalf("name %q accepted: %d", bad, r.Code)
		}
	}

	pbase := "/management/v1/projects/" + pid
	r = owner.do(http.MethodPost, pbase+"/environments", map[string]any{"name": "production"}, nil)
	if r.Code != 201 {
		t.Fatalf("create env: %d %s", r.Code, r.Raw)
	}
	eid := obj(r.Body["environment"])["id"].(string)
	ubase := pbase + "/environments/" + eid + "/compose"

	// The draft check saves nothing.
	r = owner.do(http.MethodPost, ubase+"/check", unitBody(nil), nil)
	if r.Code != 200 || obj(r.Body["analysis"])["valid"] != true {
		t.Fatalf("check: %d %s", r.Code, r.Raw)
	}
	if r := owner.do(http.MethodGet, ubase, nil, nil); r.Code != 404 {
		t.Fatalf("check must not save: %d", r.Code)
	}

	// Import.
	r = owner.do(http.MethodPost, ubase, unitBody(nil), nil)
	if r.Code != 201 {
		t.Fatalf("create unit: %d %s", r.Code, r.Raw)
	}
	unit := obj(r.Body["unit"])
	if unit["composeProject"] != "shop-production-web" {
		t.Fatalf("compose project: %v", unit["composeProject"])
	}
	if strings.Contains(r.Raw, "s3cr3t-value") {
		t.Fatal("a secret file's content came back")
	}
	for _, f := range unit["files"].([]any) {
		fm := obj(f)
		if fm["path"] == "compose.yaml" && fm["content"] != webCompose {
			t.Fatalf("compose.yaml not kept byte for byte: %q", fm["content"])
		}
		if fm["path"] == "db_pw.txt" && (fm["secret"] != true || fm["content"] != nil || fm["size"] != float64(12)) {
			t.Fatalf("secret file: %v", fm)
		}
	}
	var stored []byte
	if err := ph.st.DB.QueryRow(`SELECT content FROM compose_unit_files WHERE path='db_pw.txt'`).Scan(&stored); err != nil || stored != nil {
		t.Fatalf("secret content in the projects table: %q %v", stored, err)
	}
	var vaultRows int
	ph.st.DB.QueryRow(`SELECT count(*) FROM secrets WHERE name LIKE 'projects/%' AND deleted_at IS NULL`).Scan(&vaultRows)
	if vaultRows != 1 {
		t.Fatalf("secret store rows: %d", vaultRows)
	}
	an := obj(r.Body["analysis"])
	kept := false
	for _, f := range an["findings"].([]any) {
		if obj(f)["severity"] == projects.SeverityKept && strings.Contains(obj(f)["attribute"].(string), "x-team") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("services.web.x-team must be listed as kept: %v", an["findings"])
	}
	uid := unit["id"].(string)
	for _, f := range unit["files"].([]any) {
		if fm := obj(f); fm["path"] == "db_pw.txt" && fm["sha256"] != "" {
			t.Fatalf("a secret file must carry no digest: %v", fm)
		}
	}

	// A secret file's content can be replaced in one write.
	rot := unitBody(nil)
	rot["files"].([]map[string]any)[3]["content"] = "new-value"
	if r := owner.do(http.MethodPut, ubase, rot, etag(owner.do(http.MethodGet, ubase, nil, nil))); r.Code != 200 {
		t.Fatalf("rotate a secret file: %d %s", r.Code, r.Raw)
	}
	var live int
	ph.st.DB.QueryRow(`SELECT count(*) FROM secrets WHERE name LIKE 'projects/%' AND deleted_at IS NULL`).Scan(&live)
	if live != 1 {
		t.Fatalf("after rotation %d live secrets, want 1", live)
	}

	// Replace keeping the secret, enabling a profile.
	cur := owner.do(http.MethodGet, ubase, nil, nil)
	body := unitBody(nil)
	body["profiles"] = []string{"debug"}
	body["files"].([]map[string]any)[3] = map[string]any{"path": "db_pw.txt", "secret": true, "unchanged": true}
	if r := owner.do(http.MethodPut, ubase, body, nil); r.Code != 428 {
		t.Fatalf("replace without If-Match: %d", r.Code)
	}
	r = owner.do(http.MethodPut, ubase, body, etag(cur))
	if r.Code != 200 || obj(r.Body["unit"])["version"] != float64(3) {
		t.Fatalf("replace: %d %s", r.Code, r.Raw)
	}
	if r := owner.do(http.MethodPut, ubase, body, etag(cur)); r.Code != 412 {
		t.Fatalf("stale If-Match: %d %s", r.Code, r.Raw)
	}
	ph.st.DB.QueryRow(`SELECT count(*) FROM secrets WHERE name LIKE 'projects/%' AND deleted_at IS NULL`).Scan(&vaultRows)
	if vaultRows != 1 {
		t.Fatalf("an unchanged secret must not be stored again: %d", vaultRows)
	}

	// Preview without a target: not deployable, says why.
	r = owner.do(http.MethodGet, ubase+"/preview", nil, nil)
	pv := obj(r.Body["preview"])
	if r.Code != 200 || pv["deployable"] != false || obj(pv["target"])["state"] != projects.TargetNotSet {
		t.Fatalf("preview without target: %d %s", r.Code, r.Raw)
	}

	// A target node with an engine report: deployable.
	node := approveNode(t, owner, func(code string) { claimAs(t, ph.reg, code, "tw-01", "") }, "tw-01")
	fx, err := os.ReadFile("../../../engineering/contracts/fixtures/messages/engines_report.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := protocol.Decode(fx)
	if err != nil {
		t.Fatal(err)
	}
	rep := m.(*protocol.EnginesReport)
	rep.CollectedAt = time.Now().UTC().Truncate(time.Second)
	if err := ph.es.RecordReport(context.Background(), node, rep); err != nil {
		t.Fatal(err)
	}
	cur = owner.do(http.MethodGet, ubase, nil, nil)
	body["target"] = map[string]any{"nodeId": node, "engineEnvironmentId": "env:/run/docker.sock"}
	if r := owner.do(http.MethodPut, ubase, body, etag(cur)); r.Code != 200 {
		t.Fatalf("set target: %d %s", r.Code, r.Raw)
	}
	r = owner.do(http.MethodGet, ubase+"/preview", nil, nil)
	pv = obj(r.Body["preview"])
	if pv["deployable"] != true || obj(pv["target"])["state"] != projects.TargetReady || pv["needsApproval"] != false {
		t.Fatalf("preview with docker target: %s", r.Raw)
	}

	// A Podman environment that does not answer blocks with the reason.
	cur = owner.do(http.MethodGet, ubase, nil, nil)
	body["target"] = map[string]any{"nodeId": node, "engineEnvironmentId": "env:/run/user/1001/podman/podman.sock"}
	owner.do(http.MethodPut, ubase, body, etag(cur))
	r = owner.do(http.MethodGet, ubase+"/preview", nil, nil)
	pv = obj(r.Body["preview"])
	if pv["deployable"] != false || obj(pv["target"])["state"] != projects.TargetNotRunning {
		t.Fatalf("not responding podman: %s", r.Raw)
	}
	// An unknown node is refused like an invisible one.
	cur = owner.do(http.MethodGet, ubase, nil, nil)
	body["target"] = map[string]any{"nodeId": "nope", "engineEnvironmentId": "env:x"}
	if r := owner.do(http.MethodPut, ubase, body, etag(cur)); r.Code != 400 {
		t.Fatalf("unknown node: %d %s", r.Code, r.Raw)
	}

	// Permissions: a project admin of another project sees nothing; one of
	// this project reads and writes it but cannot target a node it cannot see.
	other := ph.createUser(owner, "other", iam.RoleProjectAdmin, iam.Scope{Kind: iam.ScopeProject, ID: "prj_other"}, "10.0.0.7")
	if r := other.do(http.MethodGet, pbase, nil, nil); r.Code != 404 {
		t.Fatalf("other project admin: %d", r.Code)
	}
	if r := other.do(http.MethodGet, "/management/v1/projects", nil, nil); r.Code != 200 || len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("other project admin list: %d %s", r.Code, r.Raw)
	}
	if r := other.do(http.MethodPost, "/management/v1/projects", map[string]any{"name": "mine"}, nil); r.Code != 403 {
		t.Fatalf("project admin creating a project: %d", r.Code)
	}
	admin := ph.createUser(owner, "shopadmin", iam.RoleProjectAdmin, iam.Scope{Kind: iam.ScopeProject, ID: pid}, "10.0.0.8")
	if r := admin.do(http.MethodGet, ubase+"/preview", nil, nil); r.Code != 200 || obj(obj(r.Body["preview"])["target"])["state"] != projects.TargetNotVisible || strings.Contains(r.Raw, "engineVersion") {
		t.Fatalf("project admin preview must not describe a node it cannot see: %d %s", r.Code, r.Raw)
	}
	cur = admin.do(http.MethodGet, ubase, nil, nil)
	body["target"] = map[string]any{"nodeId": node, "engineEnvironmentId": "env:/run/docker.sock"}
	if r := admin.do(http.MethodPut, ubase, body, etag(cur)); r.Code != 400 {
		t.Fatalf("project admin targeting an invisible node: %d %s", r.Code, r.Raw)
	}
	auditor := ph.createUser(owner, "aud", iam.RoleAuditor, iam.Global, "10.0.0.9")
	if r := auditor.do(http.MethodGet, ubase, nil, nil); r.Code != 200 {
		t.Fatalf("auditor read: %d", r.Code)
	}
	if r := auditor.do(http.MethodPost, ubase+"/check", unitBody(nil), nil); r.Code != 403 {
		t.Fatalf("auditor check: %d", r.Code)
	}

	// Deleting in order; the audit trail has every change.
	if r := owner.do(http.MethodDelete, pbase+"/environments/"+eid, nil, etag(owner.do(http.MethodGet, pbase+"/environments/"+eid, nil, nil))); r.Code != 409 || r.Body["code"] != CodeResourceReferenced {
		t.Fatalf("delete env with unit: %d %s", r.Code, r.Raw)
	}
	if r := owner.do(http.MethodDelete, ubase, nil, etag(owner.do(http.MethodGet, ubase, nil, nil))); r.Code != 204 {
		t.Fatalf("delete unit: %d %s", r.Code, r.Raw)
	}
	ph.st.DB.QueryRow(`SELECT count(*) FROM secrets WHERE name LIKE 'projects/%' AND deleted_at IS NULL`).Scan(&vaultRows)
	if vaultRows != 0 {
		t.Fatalf("secret files left after delete: %d", vaultRows)
	}
	if got := strings.Join(ph.audit(t, uid), " "); got != "compose-unit.create:ok compose-unit.update:ok compose-unit.update:ok compose-unit.update:ok compose-unit.update:ok compose-unit.delete:ok" {
		t.Fatalf("unit audit: %s", got)
	}
	if r := owner.do(http.MethodDelete, pbase+"/environments/"+eid, nil, etag(owner.do(http.MethodGet, pbase+"/environments/"+eid, nil, nil))); r.Code != 204 {
		t.Fatalf("delete env: %d %s", r.Code, r.Raw)
	}
	if r := owner.do(http.MethodDelete, pbase, nil, etag(owner.do(http.MethodGet, pbase, nil, nil))); r.Code != 204 {
		t.Fatalf("delete project: %d %s", r.Code, r.Raw)
	}
}

func TestComposeUnitInputLimits(t *testing.T) {
	ph := newProjectsHarness(t)
	owner := ph.owner
	pid := obj(owner.do(http.MethodPost, "/management/v1/projects", map[string]any{"name": "shop"}, nil).Body["project"])["id"].(string)
	eid := obj(owner.do(http.MethodPost, "/management/v1/projects/"+pid+"/environments", map[string]any{"name": "staging"}, nil).Body["environment"])["id"].(string)
	ubase := "/management/v1/projects/" + pid + "/environments/" + eid + "/compose"
	cases := map[string]func(b map[string]any){
		"absolute path":   func(b map[string]any) { b["files"].([]map[string]any)[1]["path"] = "/etc/passwd" },
		"dot-dot path":    func(b map[string]any) { b["files"].([]map[string]any)[1]["path"] = "web/../../x" },
		"main not a file": func(b map[string]any) { b["mainFile"] = "missing.yaml" },
		"main is secret":  func(b map[string]any) { b["mainFile"] = "db_pw.txt" },
		"unknown source":  func(b map[string]any) { b["source"] = "git" },
		"bad profile":     func(b map[string]any) { b["profiles"] = []string{"-x"} },
		"too big": func(b map[string]any) {
			b["files"].([]map[string]any)[2]["content"] = strings.Repeat("a", projects.MaxFileBytes+1)
		},
		"unchanged without stored": func(b map[string]any) {
			b["files"].([]map[string]any)[3] = map[string]any{"path": "db_pw.txt", "secret": true, "unchanged": true}
		},
	}
	for name, mutate := range cases {
		b := unitBody(nil)
		mutate(b)
		if r := owner.do(http.MethodPost, ubase, b, nil); r.Code != 400 && r.Code != 413 {
			t.Fatalf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	var n int
	ph.st.DB.QueryRow(`SELECT count(*) FROM secrets WHERE name LIKE 'projects/%' AND deleted_at IS NULL`).Scan(&n)
	if n != 0 {
		t.Fatalf("refused imports left %d secrets", n)
	}
	// Invalid Compose is saved (the editor keeps work in progress) and the
	// answer says why it cannot deploy.
	b := unitBody(nil)
	b["files"].([]map[string]any)[0]["content"] = "services:\n  web:\n    image: nginx\n    restartt: always\n"
	r := owner.do(http.MethodPost, ubase, b, nil)
	if r.Code != 201 || obj(r.Body["analysis"])["valid"] != false {
		t.Fatalf("invalid compose: %d %s", r.Code, r.Raw)
	}
}

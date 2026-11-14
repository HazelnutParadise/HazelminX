package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/composedeploy"
	"github.com/HazelnutParadise/hazelminx/core/internal/grants"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/plans"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// fakeAgent answers compose steps the way the agent and executor would,
// and keeps what it was sent.
type fakeAgent struct {
	mu        sync.Mutex
	sent      []nodes.Action
	nodes     []string
	checkFail bool
	deployErr *jobs.StepError
}

func (f *fakeAgent) Execute(_ context.Context, job *jobs.Job, _ *jobs.Step, rep jobs.Reporter, a nodes.Action) (json.RawMessage, error) {
	f.mu.Lock()
	f.sent = append(f.sent, a)
	f.nodes = append(f.nodes, job.Node)
	f.mu.Unlock()
	if a.Name == composedeploy.ActionCheck {
		rep.Log("checking s3cr3t-value")
		if f.checkFail {
			return json.RawMessage(`{"ready":false,"checks":[{"severity":"error","code":"PORT_IN_USE","service":"web","detail":"host port tcp/8080 is already listened on at 0.0.0.0"}]}`), nil
		}
		return json.RawMessage(`{"ready":true,"checks":[{"severity":"warning","code":"DISK_LOW","detail":"4 GiB free"}]}`), nil
	}
	if f.deployErr != nil {
		return nil, f.deployErr
	}
	rep.Log("Container shop-production-web-web-1  Started")
	return json.RawMessage(`{"revision":"` + job.ID + `","containers":[{"name":"shop-production-web-web-1","state":"running"}]}`), nil
}

func (f *fakeAgent) Reconcile(context.Context, *jobs.Job, *jobs.Step) (jobs.StepReport, error) {
	return jobs.StepReport{}, nil
}

func (f *fakeAgent) calls() []nodes.Action {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]nodes.Action{}, f.sent...)
}

type composeDeployHarness struct {
	*projectsHarness
	engine *jobs.Engine
	agent  *fakeAgent
	pub    ed25519.PublicKey
	pid    string
	ubase  string
	node   string
	fp     string
}

func newComposeDeployHarness(t *testing.T, compose string) *composeDeployHarness {
	t.Helper()
	ph := newProjectsHarness(t)
	ctx := context.Background()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	agent := &fakeAgent{}
	e := jobs.New(jobs.NewSQLStore(ph.st.DB), jobs.Options{PollInterval: 10 * time.Millisecond})
	vault := ph.vault
	svc := projects.New(projects.Options{DB: ph.st.DB, Vault: vault, Reveal: vault, Audit: ph.svc})
	d := &composedeploy.Deployer{Units: svc, Views: ph.es, Store: composedeploy.NewStore(ph.st.DB, nil),
		Grants: grants.New(key, nil), Steps: agent, Nodes: composedeploy.RegistryNodes{Get: ph.reg.GetNode}}
	for _, h := range []jobs.Handler{composedeploy.DeployHandler{D: d}, composedeploy.CheckHandler{D: d}} {
		if err := e.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	pl := plans.New(plans.Options{DB: ph.st.DB, Audit: ph.svc, Jobs: e})
	if err := pl.Register(composedeploy.PlanKind{D: d}); err != nil {
		t.Fatal(err)
	}
	ph.srv.RegisterJobs(e)
	ph.srv.MountChangePlans(pl)
	ph.srv.MountComposeDeploy(svc, d, e)
	ph.h = ph.srv.Handler()
	e.Start(ctx)
	t.Cleanup(e.Stop)

	owner := ph.owner
	r := owner.do(http.MethodPost, "/management/v1/projects", map[string]any{"name": "shop"}, nil)
	pid := obj(r.Body["project"])["id"].(string)
	r = owner.do(http.MethodPost, "/management/v1/projects/"+pid+"/environments", map[string]any{"name": "production"}, nil)
	eid := obj(r.Body["environment"])["id"].(string)
	ubase := "/management/v1/projects/" + pid + "/environments/" + eid + "/compose"
	node := approveNode(t, owner, func(code string) { claimAs(t, ph.reg, code, "tw-01", "") }, "tw-01")
	fx, err := os.ReadFile("../../../engineering/contracts/fixtures/messages/engines_report.json")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := protocol.Decode(fx)
	rep := m.(*protocol.EnginesReport)
	rep.CollectedAt = time.Now().UTC().Truncate(time.Second)
	if err := ph.es.RecordReport(ctx, node, rep); err != nil {
		t.Fatal(err)
	}
	body := unitBody(map[string]any{"nodeId": node, "engineEnvironmentId": "env:/run/docker.sock"})
	body["files"].([]map[string]any)[0]["content"] = compose
	if r := owner.do(http.MethodPost, ubase, body, nil); r.Code != 201 {
		t.Fatalf("create unit: %d %s", r.Code, r.Raw)
	}
	n, _ := ph.reg.GetNode(ctx, node)
	return &composeDeployHarness{projectsHarness: ph, engine: e, agent: agent, pub: pub, pid: pid, ubase: ubase, node: node, fp: n.Fingerprint}
}

func (h *composeDeployHarness) plan(t *testing.T) map[string]any {
	t.Helper()
	params := map[string]any{"projectId": h.pid, "environmentId": strings.Split(h.ubase, "/")[6]}
	r := h.owner.do(http.MethodPost, "/management/v1/change-plans", map[string]any{"kind": composedeploy.KindDeploy, "params": params}, nil)
	if r.Code != 201 {
		t.Fatalf("create plan: %d %s", r.Code, r.Raw)
	}
	return obj(r.Body["plan"])
}

func (h *composeDeployHarness) approve(t *testing.T, pl map[string]any) map[string]any {
	t.Helper()
	r := h.owner.do(http.MethodPost, "/management/v1/change-plans/"+pl["id"].(string)+"/approve", map[string]any{"digest": pl["digest"]}, nil)
	if r.Code != 200 {
		t.Fatalf("approve: %d %s", r.Code, r.Raw)
	}
	return obj(r.Body["plan"])
}

func (h *composeDeployHarness) waitJob(t *testing.T, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r := h.owner.do(http.MethodGet, "/management/v1/jobs/"+id, nil, nil)
		j := obj(r.Body["job"])
		switch j["state"] {
		case "succeeded", "failed", "cancelled", "rolled_back", "unknown":
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s still %v: %s", id, j["state"], r.Raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestComposeDeployRunsThePlanThroughTheAgentWithBoundGrants(t *testing.T) {
	h := newComposeDeployHarness(t, webCompose)
	pl := h.plan(t)
	// The disabled profile is a warning: the plan waits for an approval,
	// but not an owner's.
	if pl["state"] != "awaiting_approval" || obj(pl["approval"])["ownerRequired"] != false {
		t.Fatalf("plan: %v", pl)
	}
	pl = h.approve(t, pl)
	// Secret files are marked, never shown, in the plan.
	raw, _ := json.Marshal(pl)
	if strings.Contains(string(raw), "s3cr3t-value") {
		t.Fatal("the plan shows a secret value")
	}
	r := h.owner.do(http.MethodPost, "/management/v1/change-plans/"+pl["id"].(string)+"/execute",
		map[string]any{"digest": pl["digest"], "requestId": "deploy-1"}, nil)
	if r.Code != 202 {
		t.Fatalf("execute: %d %s", r.Code, r.Raw)
	}
	jobID := r.Body["jobId"].(string)
	j := h.waitJob(t, jobID)
	if j["state"] != "succeeded" {
		t.Fatalf("job: %v", j)
	}
	calls := h.agent.calls()
	if len(calls) != 2 || calls[0].Name != composedeploy.ActionCheck || calls[1].Name != composedeploy.ActionDeploy {
		t.Fatalf("steps sent: %+v", calls)
	}
	for i, c := range calls {
		if h.agent.nodes[i] != h.fp {
			t.Fatal("a step went to another node")
		}
		m := c.Params["manifest"].(composedeploy.Manifest)
		g := c.Params["grant"].(protocol.ExecutorGrant)
		exec := map[int]string{0: "compose_check", 1: "compose_deploy"}[i]
		if g.Binding != m.Binding(exec) || !ed25519.Verify(h.pub, grants.Canonical(g), mustHex(t, g.Signature)) {
			t.Fatalf("step %d grant is not bound to its manifest", i)
		}
		if m.Revision != jobID || m.Project != "shop-production-web" || m.Environment != "env:/run/docker.sock" {
			t.Fatalf("manifest: %+v", m)
		}
		// The secret file goes to the host, as its content.
		var found bool
		for _, f := range c.Params["files"].([]composedeploy.StagedFile) {
			found = found || f.Content == "s3cr3t-value"
		}
		if !found {
			t.Fatal("the secret file was not staged")
		}
	}
	// Logs carry the agent's output with secret values removed.
	logs, _ := json.Marshal(j["steps"])
	if strings.Contains(string(logs), "s3cr3t-value") || !strings.Contains(string(logs), "[secret]") || !strings.Contains(string(logs), "DISK_LOW") {
		t.Fatalf("logs: %s", logs)
	}
	r = h.owner.do(http.MethodGet, h.ubase+"/deployments", nil, nil)
	deps := r.Body["deployments"].([]any)
	if r.Code != 200 || len(deps) != 1 || obj(deps[0])["state"] != "succeeded" || obj(deps[0])["revision"] != jobID {
		t.Fatalf("deployments: %d %s", r.Code, r.Raw)
	}
	// NOD-008: maintenance lists the deployed Compose project.
	r = h.owner.do(http.MethodPut, "/management/v1/nodes/"+h.node+"/maintenance", map[string]any{"enabled": true, "reason": "kernel"}, nil)
	svcs, _ := r.Body["affectedServices"].([]any)
	if r.Code != 200 || len(svcs) != 1 || obj(svcs[0])["composeProject"] != "shop-production-web" {
		t.Fatalf("maintenance: %d %s", r.Code, r.Raw)
	}
}

func TestComposeDeployWithHighRiskSettingsWaitsForApproval(t *testing.T) {
	h := newComposeDeployHarness(t, strings.Replace(webCompose, "x-team: storefront", "privileged: true", 1))
	pl := h.plan(t)
	if pl["state"] != "awaiting_approval" || obj(pl["approval"])["ownerRequired"] != true {
		t.Fatalf("plan: %v", pl)
	}
	r := h.owner.do(http.MethodPost, "/management/v1/change-plans/"+pl["id"].(string)+"/execute",
		map[string]any{"digest": pl["digest"], "requestId": "deploy-x"}, nil)
	if r.Code == 202 {
		t.Fatal("an unapproved high-risk deployment ran")
	}
	if len(h.agent.calls()) != 0 {
		t.Fatal("something was sent to the agent")
	}
}

func TestComposeHostCheckFailureChangesNothing(t *testing.T) {
	h := newComposeDeployHarness(t, webCompose)
	h.agent.checkFail = true
	pl := h.approve(t, h.plan(t))
	r := h.owner.do(http.MethodPost, "/management/v1/change-plans/"+pl["id"].(string)+"/execute",
		map[string]any{"digest": pl["digest"], "requestId": "deploy-2"}, nil)
	j := h.waitJob(t, r.Body["jobId"].(string))
	if j["state"] != "failed" || j["errorCode"] != "HOST_CHECK_FAILED" {
		t.Fatalf("job: %v", j)
	}
	if calls := h.agent.calls(); len(calls) != 1 {
		t.Fatalf("deploy was attempted after a failed check: %+v", calls)
	}
	r = h.owner.do(http.MethodGet, h.ubase+"/deployments", nil, nil)
	if len(r.Body["deployments"].([]any)) != 0 {
		t.Fatalf("a failed check recorded a deployment: %s", r.Raw)
	}
}

func TestComposeDeployFailureIsRecorded(t *testing.T) {
	h := newComposeDeployHarness(t, webCompose)
	h.agent.deployErr = &jobs.StepError{Code: "EXECUTOR_ACTION_FAILED", Summary: "docker compose up exited with 1; s3cr3t-value"}
	pl := h.approve(t, h.plan(t))
	r := h.owner.do(http.MethodPost, "/management/v1/change-plans/"+pl["id"].(string)+"/execute",
		map[string]any{"digest": pl["digest"], "requestId": "deploy-3"}, nil)
	h.waitJob(t, r.Body["jobId"].(string))
	r = h.owner.do(http.MethodGet, h.ubase+"/deployments", nil, nil)
	d := obj(r.Body["deployments"].([]any)[0])
	if d["state"] != "failed" || d["errorCode"] != "EXECUTOR_ACTION_FAILED" || strings.Contains(r.Raw, "s3cr3t-value") {
		t.Fatalf("failed deployment: %s", r.Raw)
	}
}

func TestComposeCheckJobsOnlyThroughTheirEndpoint(t *testing.T) {
	h := newComposeDeployHarness(t, webCompose)
	// POST /jobs cannot choose a unit and a node for compose.check.
	r := h.owner.do(http.MethodPost, "/management/v1/jobs", map[string]any{"requestId": "c-1", "kind": composedeploy.KindCheck,
		"node": "elsewhere", "params": map[string]any{"projectId": h.pid, "environmentId": "x", "unitId": "u", "unitVersion": 1}}, nil)
	if r.Code != 400 || !strings.Contains(r.Raw, "host-checks") {
		t.Fatalf("POST /jobs compose.check: %d %s", r.Code, r.Raw)
	}
	// Nor run a deployment outside a plan.
	r = h.owner.do(http.MethodPost, "/management/v1/jobs", map[string]any{"requestId": "c-2", "kind": composedeploy.KindDeploy}, nil)
	if r.Code == 202 {
		t.Fatal("compose.deploy ran without a plan")
	}
	r = h.owner.do(http.MethodPost, h.ubase+"/host-checks", map[string]any{"requestId": "c-3"}, nil)
	if r.Code != 202 {
		t.Fatalf("host check: %d %s", r.Code, r.Raw)
	}
	if j := h.waitJob(t, r.Body["jobId"].(string)); j["state"] != "succeeded" {
		t.Fatalf("host check job: %v", j)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestComposeDeployPlanIsVoidWhenTheUnitChanges(t *testing.T) {
	h := newComposeDeployHarness(t, webCompose)
	pl := h.approve(t, h.plan(t))
	cur := h.owner.do(http.MethodGet, h.ubase, nil, nil)
	body := unitBody(map[string]any{"nodeId": h.node, "engineEnvironmentId": "env:/run/docker.sock"})
	body["files"].([]map[string]any)[2]["content"] = "events { worker_connections 64; }\n"
	if r := h.owner.do(http.MethodPut, h.ubase, body, etag(cur)); r.Code != 200 {
		t.Fatalf("change unit: %d %s", r.Code, r.Raw)
	}
	r := h.owner.do(http.MethodPost, "/management/v1/change-plans/"+pl["id"].(string)+"/execute",
		map[string]any{"digest": pl["digest"], "requestId": "deploy-4"}, nil)
	if r.Code != 409 || len(h.agent.calls()) != 0 {
		t.Fatalf("execute after the unit changed: %d %s", r.Code, r.Raw)
	}
}

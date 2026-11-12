package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/plans"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// settingDoc is a versioned setting owned by a project.
type settingDoc struct {
	name, project, node, nodeName, env string
	version                            int64
	values                             map[string]string
}

// settingsKind plans "set keys of one setting". mount=/ is high risk.
type settingsKind struct {
	mu   sync.Mutex
	docs map[string]*settingDoc
}

func (k *settingsKind) Name() string       { return "setting.update" }
func (k *settingsKind) Action() iam.Action { return iam.ActionProjectsWrite }
func (k *settingsKind) JobKind() string    { return "setting.apply" }

func (k *settingsKind) write(id, key, val string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.docs[id].values[key] = val
	k.docs[id].version++
}

func (k *settingsKind) Draft(_ context.Context, raw json.RawMessage) (*plans.Draft, error) {
	var p struct {
		Setting string            `json:"setting"`
		Set     map[string]string `json:"set"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.Setting == "" {
		return nil, fmt.Errorf("%w: setting is required", plans.ErrInvalid)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	d, ok := k.docs[p.Setting]
	if !ok {
		return nil, fmt.Errorf("%w: setting %s", plans.ErrNotFound, p.Setting)
	}
	key := "setting:" + p.Setting
	out := &plans.Draft{Summary: "update " + d.name + " on " + d.nodeName, RequireApproval: true,
		Targets: []plans.Target{{Key: key, Kind: "setting", ID: p.Setting, Name: d.name, Node: d.node, NodeName: d.nodeName, Environment: d.env,
			Scope: iam.Scope{Kind: iam.ScopeProject, ID: d.project}, Version: d.version}},
		Affected: []plans.Affected{{Kind: "service", ID: "shop-api", Name: "shop-api", Effect: "recreated", Relation: "direct"}},
		Checks:   []plans.Check{{ID: "disk", Status: plans.CheckPass, Title: "enough space"}}}
	for _, kk := range []string{"image", "mount"} {
		v, set := p.Set[kk]
		if !set {
			continue
		}
		out.Changes = append(out.Changes, plans.Change{Target: key, Path: kk, Before: d.values[kk], After: v, Op: "change"})
		if kk == "mount" && v == "/" {
			out.Checks = append(out.Checks, plans.Check{ID: "root-mount", Status: plans.CheckHighRisk, Title: "mounts the host root", Target: key})
		}
	}
	return out, nil
}

func (k *settingsKind) Job(p *plans.Plan) (jobs.SubmitRequest, error) {
	return jobs.SubmitRequest{Kind: k.JobKind(), Params: p.Params, Resources: []jobs.ResourceRef{{Kind: "setting", ID: p.Targets[0].ID}}}, nil
}

// applyHandler finishes at once.
type applyHandler struct{}

func (applyHandler) Kind() string { return "setting.apply" }
func (applyHandler) Plan(context.Context, *jobs.Job) ([]jobs.StepSpec, error) {
	return []jobs.StepSpec{{Name: "write", Retryable: true}}, nil
}
func (applyHandler) Recheck(context.Context, *jobs.Job, *jobs.Step) error { return nil }
func (applyHandler) Execute(context.Context, *jobs.Job, *jobs.Step, jobs.Reporter) error {
	return nil
}
func (applyHandler) Rollback(context.Context, *jobs.Job, *jobs.Step, jobs.Reporter) error {
	return nil
}
func (applyHandler) Reconcile(_ context.Context, _ *jobs.Job, s *jobs.Step) (jobs.StepReport, error) {
	return jobs.StepReport{Index: s.Index, Completed: true}, nil
}
func (applyHandler) Verify(context.Context, *jobs.Job) (jobs.CompletionReport, error) {
	return jobs.CompletionReport{Checks: []jobs.Check{{Name: "applied", Required: true, Passed: true}}}, nil
}

type plansHarness struct {
	*harness
	kind   *settingsKind
	engine *jobs.Engine
	owner  *client
	clock  struct {
		sync.Mutex
		t time.Time
	}
}

func (ph *plansHarness) now() time.Time { ph.clock.Lock(); defer ph.clock.Unlock(); return ph.clock.t }
func (ph *plansHarness) advance(d time.Duration) {
	ph.clock.Lock()
	defer ph.clock.Unlock()
	ph.clock.t = ph.clock.t.Add(d)
}

func newPlansHarness(t *testing.T) *plansHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := jobs.New(jobs.NewSQLStore(st.DB), jobs.Options{PollInterval: 10 * time.Millisecond})
	if err := e.Register(applyHandler{}); err != nil {
		t.Fatal(err)
	}
	e.Start(ctx)
	t.Cleanup(func() { cancel(); e.Stop(); st.Close() })
	hs := &harness{t: t, clock: time.Now().UTC().Truncate(time.Second), totp: map[string]*totpDevice{}}
	ph := &plansHarness{harness: hs, engine: e}
	ph.clock.t = hs.clock
	svc := newIAMService(t, st, hs)
	s := New(st, nil)
	s.MountIAM(svc, IAMOptions{})
	s.RegisterJobs(e)
	ps := plans.New(plans.Options{DB: st.DB, Audit: svc, Jobs: e, Now: ph.now})
	ph.kind = &settingsKind{docs: map[string]*settingDoc{
		"shop-env": {name: "shop production settings", project: "shop", node: "node_tw01", nodeName: "hazel-tw-01", env: "production", version: 7, values: map[string]string{"image": "shop-api:a3f9c1e"}},
	}}
	if err := ps.Register(ph.kind); err != nil {
		t.Fatal(err)
	}
	s.MountChangePlans(ps)
	hs.h, hs.svc, hs.srv = s.Handler(), svc, s
	ph.owner = hs.bootstrap()
	return ph
}

func (ph *plansHarness) create(c *client, set map[string]string) resp {
	ph.t.Helper()
	return c.do(http.MethodPost, "/management/v1/change-plans", map[string]any{"kind": "setting.update", "params": map[string]any{"setting": "shop-env", "set": set}}, nil)
}

func planOf(r resp) map[string]any {
	p, _ := r.Body["plan"].(map[string]any)
	return p
}

func TestChangePlanAPIFlow(t *testing.T) {
	ph := newPlansHarness(t)
	admin := ph.createUser(ph.owner, "pat", iam.RoleProjectAdmin, iam.Scope{Kind: iam.ScopeProject, ID: "shop"}, "10.0.0.2")

	r := ph.create(admin, map[string]string{"image": "shop-api:9b3e7f1"})
	if r.Code != http.StatusCreated {
		t.Fatalf("create: %+v", r)
	}
	p := planOf(r)
	id, digest := p["id"].(string), p["digest"].(string)
	tg := p["targets"].([]any)[0].(map[string]any)
	if p["state"] != "awaiting_approval" || tg["nodeName"] != "hazel-tw-01" || tg["environment"] != "production" || tg["version"] != float64(7) {
		t.Fatalf("plan %+v", p)
	}
	if ch := p["changes"].([]any)[0].(map[string]any); ch["before"] != "shop-api:a3f9c1e" || ch["after"] != "shop-api:9b3e7f1" {
		t.Fatalf("diff %+v", ch)
	}
	if r.Hdr.Get("Location") != "/management/v1/change-plans/"+id {
		t.Fatalf("location %q", r.Hdr.Get("Location"))
	}

	r = ph.owner.do(http.MethodGet, "/management/v1/change-plans?state=awaiting_approval", nil, nil)
	if items := r.Body["items"].([]any); r.Code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["id"] != id {
		t.Fatalf("waiting list: %+v", r)
	}

	r = ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+id+"/approve", map[string]any{"digest": digest}, nil)
	if r.Code != http.StatusOK || planOf(r)["state"] != "approved" {
		t.Fatalf("approve: %+v", r)
	}

	r = admin.do(http.MethodPost, "/management/v1/change-plans/"+id+"/execute", map[string]any{"digest": digest, "requestId": "req-1"}, nil)
	if r.Code != http.StatusAccepted || r.Body["deduplicated"] != false {
		t.Fatalf("execute: %+v", r)
	}
	job := r.Body["job"].(map[string]any)
	if job["changePlanId"] != id || job["changePlanDigest"] != digest || job["approver"] == "" || job["requester"] == "" {
		t.Fatalf("job %+v", job)
	}
	again := admin.do(http.MethodPost, "/management/v1/change-plans/"+id+"/execute", map[string]any{"digest": digest, "requestId": "req-1"}, nil)
	if again.Code != http.StatusAccepted || again.Body["jobId"] != r.Body["jobId"] || again.Body["deduplicated"] != true {
		t.Fatalf("replay: %+v", again)
	}
	got := admin.do(http.MethodGet, "/management/v1/change-plans/"+id, nil, nil)
	if pl := planOf(got); pl["state"] != "executed" || pl["execution"].(map[string]any)["jobId"] != r.Body["jobId"] {
		t.Fatalf("plan after execute: %+v", got)
	}
}

// T-030 over HTTP: the first person's change lands first; the second
// person's approved plan answers the conflict with the difference and does
// not run.
func TestChangePlanAPIConflictKeepsFirstChange(t *testing.T) {
	ph := newPlansHarness(t)
	r := ph.create(ph.owner, map[string]string{"image": "second"})
	p := planOf(r)
	id, digest := p["id"].(string), p["digest"].(string)
	if r := ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+id+"/approve", map[string]any{"digest": digest}, nil); r.Code != http.StatusOK {
		t.Fatalf("approve: %+v", r)
	}
	ph.kind.write("shop-env", "image", "first")

	r = ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+id+"/execute", map[string]any{"digest": digest, "requestId": "req-2"}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeChangePlanChanged {
		t.Fatalf("execute after a concurrent change: %+v", r)
	}
	det := r.Body["details"].(map[string]any)
	mv := det["targets"].([]any)[0].(map[string]any)
	cur := det["current"].([]any)[0].(map[string]any)
	if mv["plannedVersion"] != float64(7) || mv["currentVersion"] != float64(8) || mv["name"] != "shop production settings" || cur["before"] != "first" || det["invalidated"] != true {
		t.Fatalf("details %+v", det)
	}
	if n, _ := ph.engine.Count(context.Background(), jobs.ListOptions{}); n != 0 {
		t.Fatalf("%d jobs created", n)
	}
	if pl := planOf(ph.owner.do(http.MethodGet, "/management/v1/change-plans/"+id, nil, nil)); pl["state"] != "invalidated" {
		t.Fatalf("plan state %v", pl["state"])
	}
}

func TestChangePlanAPIPermissions(t *testing.T) {
	ph := newPlansHarness(t)
	wiki := ph.createUser(ph.owner, "wikiadm", iam.RoleProjectAdmin, iam.Scope{Kind: iam.ScopeProject, ID: "wiki"}, "10.0.0.3")
	aud := ph.createUser(ph.owner, "aud", iam.RoleAuditor, iam.Global, "10.0.0.4")
	plat := ph.createUser(ph.owner, "plat", iam.RolePlatformAdmin, iam.Global, "10.0.0.5")

	p := planOf(ph.create(ph.owner, map[string]string{"mount": "/"}))
	id, digest := p["id"].(string), p["digest"].(string)
	if p["risk"] != "high" || p["approval"].(map[string]any)["ownerRequired"] != true {
		t.Fatalf("plan %+v", p)
	}
	// Someone who cannot see the project gets the answer of a missing plan.
	hidden := wiki.do(http.MethodGet, "/management/v1/change-plans/"+id, nil, nil)
	missing := wiki.do(http.MethodGet, "/management/v1/change-plans/plan_missing", nil, nil)
	if hidden.Code != http.StatusNotFound || missing.Code != http.StatusNotFound || hidden.Body["message"] != missing.Body["message"] {
		t.Fatalf("hidden %+v missing %+v", hidden, missing)
	}
	if r := wiki.do(http.MethodPost, "/management/v1/change-plans/"+id+"/approve", map[string]any{"digest": digest}, nil); r.Code != http.StatusNotFound {
		t.Fatalf("approve hidden: %+v", r)
	}
	if r := wiki.do(http.MethodGet, "/management/v1/change-plans", nil, nil); len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("list leaks: %+v", r)
	}
	if r := aud.do(http.MethodPost, "/management/v1/change-plans/"+id+"/approve", map[string]any{"digest": digest}, nil); r.Code != http.StatusForbidden {
		t.Fatalf("auditor approve: %+v", r)
	}
	r := plat.do(http.MethodPost, "/management/v1/change-plans/"+id+"/approve", map[string]any{"digest": digest}, nil)
	if r.Code != http.StatusForbidden || r.Body["details"].(map[string]any)["reason"] != "owner-required" {
		t.Fatalf("platform admin approves a high-risk plan: %+v", r)
	}
	if r := ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+id+"/approve", map[string]any{"digest": "sha256:other"}, nil); r.Code != http.StatusConflict || r.Body["code"] != CodeChangePlanChanged {
		t.Fatalf("approve another digest: %+v", r)
	}
	if r := ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+id+"/approve", map[string]any{"digest": digest}, nil); r.Code != http.StatusOK {
		t.Fatalf("owner approve: %+v", r)
	}
}

func TestChangePlanAPIExpiry(t *testing.T) {
	ph := newPlansHarness(t)
	p := planOf(ph.create(ph.owner, map[string]string{"image": "x"}))
	ph.advance(plans.DefaultTTL + time.Second)
	r := ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+p["id"].(string)+"/approve", map[string]any{"digest": p["digest"]}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeChangePlanExpired {
		t.Fatalf("approve expired: %+v", r)
	}
	if r := ph.owner.do(http.MethodGet, "/management/v1/change-plans?state=awaiting_approval", nil, nil); len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("expired plan still waiting: %+v", r)
	}
}

func TestJobsRefuseKindsThatNeedAPlan(t *testing.T) {
	ph := newPlansHarness(t)
	r := ph.owner.do(http.MethodPost, "/management/v1/jobs", map[string]any{"requestId": "direct", "kind": "setting.apply", "params": map[string]any{"setting": "shop-env"}}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeChangePlanRequired {
		t.Fatalf("direct submission of a plan kind: %+v", r)
	}
	// A body cannot claim an approval either.
	r = ph.owner.do(http.MethodPost, "/management/v1/jobs", map[string]any{"requestId": "direct2", "kind": "setting.apply", "approver": "usr_x", "changePlanId": "plan_x"}, nil)
	if r.Code != http.StatusBadRequest && r.Code != http.StatusConflict {
		t.Fatalf("forged plan fields: %+v", r)
	}
	if n, _ := ph.engine.Count(context.Background(), jobs.ListOptions{}); n != 0 {
		t.Fatalf("%d jobs created", n)
	}
}

func TestChangePlanExecuteRespectsPlatformDegradation(t *testing.T) {
	ph := newPlansHarness(t)
	p := planOf(ph.create(ph.owner, map[string]string{"image": "x"}))
	id, digest := p["id"].(string), p["digest"].(string)
	ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+id+"/approve", map[string]any{"digest": digest}, nil)
	ph.srv.SetPlatformState(func() *PlatformDegradation { return &PlatformDegradation{Mode: PlatformDBReadOnly} })
	r := ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+id+"/execute", map[string]any{"digest": digest, "requestId": "req-d"}, nil)
	if r.Code != http.StatusServiceUnavailable || r.Body["code"] != CodePlatformDegraded {
		t.Fatalf("execute while degraded: %+v", r)
	}
	if pl := planOf(ph.owner.do(http.MethodGet, "/management/v1/change-plans/"+id, nil, nil)); pl["state"] != "approved" {
		t.Fatalf("plan consumed while refused: %v", pl["state"])
	}
	ph.srv.SetPlatformState(nil)
	if r := ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+id+"/execute", map[string]any{"digest": digest, "requestId": "req-d"}, nil); r.Code != http.StatusAccepted {
		t.Fatalf("execute after recovery: %+v", r)
	}
}

func TestChangePlanAPIRejectAndWithdraw(t *testing.T) {
	ph := newPlansHarness(t)
	a := planOf(ph.create(ph.owner, map[string]string{"image": "x"}))
	if r := ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+a["id"].(string)+"/reject", map[string]any{}, nil); r.Code != http.StatusBadRequest {
		t.Fatalf("reject without reason: %+v", r)
	}
	r := ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+a["id"].(string)+"/reject", map[string]any{"reason": "outside the window"}, nil)
	if r.Code != http.StatusOK || planOf(r)["state"] != "rejected" {
		t.Fatalf("reject: %+v", r)
	}
	r = ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+a["id"].(string)+"/approve", map[string]any{"digest": a["digest"]}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodeChangePlanState {
		t.Fatalf("approve rejected: %+v", r)
	}
	b := planOf(ph.create(ph.owner, map[string]string{"image": "y"}))
	r = ph.owner.do(http.MethodPost, "/management/v1/change-plans/"+b["id"].(string)+"/withdraw", map[string]any{"reason": "typo"}, nil)
	if r.Code != http.StatusOK || planOf(r)["state"] != "withdrawn" {
		t.Fatalf("withdraw: %+v", r)
	}
}

// T-030 on a direct versioned write: the second save of the same version
// gets the current content and the conflicting fields, and does not
// overwrite the first.
func TestUserUpdateConflictShowsCurrentContent(t *testing.T) {
	ph := newPlansHarness(t)
	ph.createUser(ph.owner, "ava", iam.RoleAuditor, iam.Global, "10.0.0.6")
	var id, etag string
	for _, u := range ph.owner.do(http.MethodGet, "/management/v1/users", nil, nil).Body["items"].([]any) {
		if m := u.(map[string]any); m["username"] == "ava" {
			id = m["id"].(string)
		}
	}
	etag = ph.owner.do(http.MethodGet, "/management/v1/users/"+id, nil, nil).Hdr.Get("ETag")
	first := ph.owner.do(http.MethodPatch, "/management/v1/users/"+id, map[string]any{"displayName": "Ava C."}, map[string]string{"If-Match": etag})
	if first.Code != http.StatusOK {
		t.Fatalf("first save: %+v", first)
	}
	second := ph.owner.do(http.MethodPatch, "/management/v1/users/"+id, map[string]any{"displayName": "Ava Chen"}, map[string]string{"If-Match": etag})
	if second.Code != http.StatusPreconditionFailed || second.Body["code"] != CodeVersionMismatch {
		t.Fatalf("second save: %+v", second)
	}
	det := second.Body["details"].(map[string]any)
	if det["current"].(map[string]any)["displayName"] != "Ava C." || det["yours"].(map[string]any)["displayName"] != "Ava Chen" {
		t.Fatalf("details %+v", det)
	}
	if f := det["conflictingFields"].([]any); len(f) != 1 || f[0] != "displayName" {
		t.Fatalf("conflicting fields %v", f)
	}
	if u := ph.owner.do(http.MethodGet, "/management/v1/users/"+id, nil, nil).Body["user"].(map[string]any); u["displayName"] != "Ava C." {
		t.Fatalf("first save overwritten: %v", u["displayName"])
	}
}

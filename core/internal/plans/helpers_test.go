package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// configDoc is a versioned configuration owned by a project, the kind of
// resource a plan protects (34.2).
type configDoc struct {
	ID, Name, Node, NodeName, Env, Project string
	Version                                int64
	Values                                 map[string]string
	Secrets                                map[string]string
}

// configs is an in-memory store of configDocs with version-checked writes.
type configs struct {
	mu   sync.Mutex
	docs map[string]*configDoc
}

func newConfigs() *configs { return &configs{docs: map[string]*configDoc{}} }

func (c *configs) put(d configDoc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d.Values == nil {
		d.Values = map[string]string{}
	}
	if d.Secrets == nil {
		d.Secrets = map[string]string{}
	}
	c.docs[d.ID] = &d
}

// write is what another person saving the config does.
func (c *configs) write(id, key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.docs[id]
	d.Values[key] = value
	d.Version++
}

func (c *configs) get(id string) (configDoc, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.docs[id]
	if !ok {
		return configDoc{}, false
	}
	cp := *d
	cp.Values = map[string]string{}
	for k, v := range d.Values {
		cp.Values[k] = v
	}
	cp.Secrets = map[string]string{}
	for k, v := range d.Secrets {
		cp.Secrets[k] = v
	}
	return cp, true
}

type configParams struct {
	Configs []string          `json:"configs"`
	Set     map[string]string `json:"set"`
	Secrets map[string]string `json:"secrets,omitempty"`
}

// configKind plans "set these keys in these configs". mount=/ is high risk,
// tag=latest is a warning, fail=yes fails a precheck. Secrets are given as
// references ("ref:<name>") and resolved by the kind, as real kinds do: a
// plan's params never carry a secret value.
type configKind struct {
	store   *configs
	require bool
}

func (k *configKind) Name() string       { return "config.update" }
func (k *configKind) Action() iam.Action { return iam.ActionProjectsWrite }
func (k *configKind) JobKind() string    { return "config.apply" }

func (k *configKind) Draft(_ context.Context, raw json.RawMessage) (*Draft, error) {
	var p configParams
	if err := json.Unmarshal(raw, &p); err != nil || len(p.Configs) == 0 {
		return nil, fmt.Errorf("%w: configs is required", ErrInvalid)
	}
	d := &Draft{RequireApproval: k.require}
	for _, id := range p.Configs {
		doc, ok := k.store.get(id)
		if !ok {
			return nil, fmt.Errorf("%w: config %s", ErrNotFound, id)
		}
		key := "config:" + id
		d.Targets = append(d.Targets, Target{Key: key, Kind: "config", ID: id, Name: doc.Name, Node: doc.Node, NodeName: doc.NodeName,
			Environment: doc.Env, Scope: iam.Scope{Kind: iam.ScopeProject, ID: doc.Project}, Version: doc.Version})
		keys := make([]string, 0, len(p.Set))
		for kk := range p.Set {
			keys = append(keys, kk)
		}
		sort.Strings(keys)
		for _, kk := range keys {
			v := p.Set[kk]
			ch := Change{Target: key, Path: kk, After: v, Op: "add"}
			if old, ok := doc.Values[kk]; ok {
				ch.Before, ch.Op = old, "change"
			}
			d.Changes = append(d.Changes, ch)
			switch {
			case kk == "mount" && v == "/":
				d.Checks = append(d.Checks, Check{ID: "host-root-mount", Status: CheckHighRisk, Title: "mounts the host root file system", Target: key})
			case kk == "tag" && v == "latest":
				d.Checks = append(d.Checks, Check{ID: "mutable-tag", Status: CheckWarning, Title: "image tag latest", Target: key})
			case kk == "fail":
				d.Checks = append(d.Checks, Check{ID: "port-free", Status: CheckFailed, Title: "port 8080 is taken", Target: key})
			}
		}
		for kk, v := range p.Secrets {
			d.Changes = append(d.Changes, Change{Target: key, Path: kk, Before: doc.Secrets[kk], After: "resolved-" + v, Secret: true, Op: "change"})
		}
		d.Affected = append(d.Affected, Affected{Kind: "service", ID: id + "-api", Name: doc.Name + "-api", Effect: "recreated", Relation: "direct"})
	}
	d.Checks = append(d.Checks, Check{ID: "disk-space", Status: CheckPass, Title: "enough disk space"})
	d.Summary = "update " + p.Configs[0]
	return d, nil
}

func (k *configKind) Job(p *Plan) (jobs.SubmitRequest, error) {
	versions := map[string]int64{}
	for _, t := range p.Targets {
		versions[t.ID] = t.Version
	}
	params, err := json.Marshal(map[string]any{"plan": json.RawMessage(p.Params), "expectedVersions": versions})
	if err != nil {
		return jobs.SubmitRequest{}, err
	}
	return jobs.SubmitRequest{Kind: k.JobKind(), Params: params}, nil
}

// fakeJobs records submissions and replays by request id like the engine.
type fakeJobs struct {
	mu   sync.Mutex
	byID map[string]*jobs.Job
	reqs []jobs.SubmitRequest
}

func (f *fakeJobs) Submit(_ context.Context, req jobs.SubmitRequest) (*jobs.Job, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byID == nil {
		f.byID = map[string]*jobs.Job{}
	}
	if j, ok := f.byID[req.RequestID]; ok {
		return j, true, nil
	}
	f.reqs = append(f.reqs, req)
	j := &jobs.Job{ID: fmt.Sprintf("job_%d", len(f.reqs)), RequestID: req.RequestID, Kind: req.Kind, Approver: req.Approver,
		ChangePlanID: req.ChangePlanID, ChangePlanDigest: req.ChangePlanDigest}
	f.byID[req.RequestID] = j
	return j, false, nil
}

func (f *fakeJobs) submitted() []jobs.SubmitRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]jobs.SubmitRequest(nil), f.reqs...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	t     *testing.T
	db    *sql.DB
	svc   *Service
	iam   *iam.Service
	cfg   *configs
	jobs  *fakeJobs
	clock *clock
	kind  *configKind

	owner, admin, projA, projB, auditor *iam.Principal
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clk := &clock{t: time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)}
	idsvc, err := iam.New(st.DB, iam.Config{Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: st.DB, iam: idsvc, cfg: newConfigs(), jobs: &fakeJobs{}, clock: clk}
	e.svc = New(Options{DB: st.DB, Audit: idsvc, Jobs: e.jobs, Now: clk.now})
	e.kind = &configKind{store: e.cfg, require: true}
	if err := e.svc.Register(e.kind); err != nil {
		t.Fatal(err)
	}
	e.owner = e.user("owner", iam.RoleOwner, iam.Global)
	e.admin = e.user("admin", iam.RolePlatformAdmin, iam.Global)
	e.projA = e.user("pa", iam.RoleProjectAdmin, iam.Scope{Kind: iam.ScopeProject, ID: "shop"})
	e.projB = e.user("pb", iam.RoleProjectAdmin, iam.Scope{Kind: iam.ScopeProject, ID: "wiki"})
	e.auditor = e.user("aud", iam.RoleAuditor, iam.Global)
	e.cfg.put(configDoc{ID: "shop-compose", Name: "shop compose.yaml", Node: "node_tw01", NodeName: "hazel-tw-01", Env: "production", Project: "shop",
		Version: 7, Values: map[string]string{"image": "shop-api:a3f9c1e"}, Secrets: map[string]string{"STRIPE_KEY": "sk_old"}})
	e.cfg.put(configDoc{ID: "wiki-compose", Name: "wiki compose.yaml", Node: "node_jp02", NodeName: "hazel-jp-02", Env: "production", Project: "wiki", Version: 3})
	return e
}

// user inserts an account with one role assignment and returns its principal.
func (e *env) user(name string, role iam.Role, scope iam.Scope) *iam.Principal {
	e.t.Helper()
	now := e.clock.now().Format(time.RFC3339Nano)
	id := "usr_" + name
	if _, err := e.db.Exec(`INSERT INTO users(id,username,display_name,password_hash,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		id, name, name, "x", iam.StatusActive, now, now); err != nil {
		e.t.Fatal(err)
	}
	return &iam.Principal{Kind: "session", User: iam.User{ID: id, Username: name, Status: iam.StatusActive},
		Assignments: []iam.Assignment{{ID: "as_" + name, UserID: id, Role: role, Scope: scope}}}
}

func (e *env) params(v configParams) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

var src = iam.Source{IP: "203.0.113.5", TraceID: "trace-1"}

func (e *env) create(p *iam.Principal, v configParams) *Plan {
	e.t.Helper()
	pl, err := e.svc.Create(context.Background(), p, "config.update", e.params(v), src)
	if err != nil {
		e.t.Fatalf("create: %v", err)
	}
	return pl
}

// audit returns the audit actions written for a plan, oldest first.
func (e *env) audit(planID string) []string {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT action || ':' || result FROM audit_events WHERE resource_id = ? ORDER BY seq`, planID)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

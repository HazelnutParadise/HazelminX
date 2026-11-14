package composedeploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/plans"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Kinds: the plan kind and its job kind share the name; compose.check is a
// read-only job on its own (CMP-007: the host checks before anything
// changes).
const (
	KindDeploy = "compose.deploy"
	KindCheck  = "compose.check"
	// Agent step actions (agent/crates/agent/src/compose.rs).
	ActionCheck  = "compose.check"
	ActionDeploy = "compose.deploy"
	// Executor actions and permissions.
	execCheck    = "compose_check"
	execDeploy   = "compose_deploy"
	PermCheck    = "host.compose.check"
	PermDeploy   = "host.compose.deploy"
	ResourceKind = "compose-project"
	// GrantLifetime: the grant is checked when the executor receives the
	// action, so it only has to outlive the step's delivery.
	GrantLifetime = 10 * time.Minute
	// MaxStepParamBytes keeps a step under the channel's 1 MiB message.
	MaxStepParamBytes = 900 << 10
)

// RootfulEnvironments are the engine environments a unit can deploy to;
// the executor accepts the same list (decision 0035).
var RootfulEnvironments = []string{"env:/var/run/docker.sock", "env:/run/docker.sock", "env:/run/podman/podman.sock"}

// Grants issues executor grants bound to one action.
type Grants interface {
	IssueBound(subject string, perms []string, lifetime time.Duration, binding string) (protocol.ExecutorGrant, error)
}

// Steps runs agent steps (nodes.StepRunner).
type Steps interface {
	Execute(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter, a nodes.Action) (json.RawMessage, error)
	Reconcile(ctx context.Context, job *jobs.Job, step *jobs.Step) (jobs.StepReport, error)
}

// Nodes names nodes.
type Nodes interface {
	// Node returns the fingerprint and name of a node id.
	Node(ctx context.Context, id string) (fingerprint, name string, err error)
}

// Deployer is the compose.deploy plan kind and the compose.deploy and
// compose.check job handlers.
type Deployer struct {
	Units  *projects.Service
	Views  projects.EngineViews
	Store  *Store
	Grants Grants
	Steps  Steps
	Nodes  Nodes
}

// Params of the plan and of both jobs.
type Params struct {
	ProjectID     string `json:"projectId"`
	EnvironmentID string `json:"environmentId"`
	// UnitID and UnitVersion are filled by the plan (and by the check
	// endpoint): the job deploys exactly this version.
	UnitID      string `json:"unitId,omitempty"`
	UnitVersion int64  `json:"unitVersion,omitempty"`
}

func parseParams(raw json.RawMessage) (Params, error) {
	var p Params
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("%w: %v", plans.ErrInvalid, err)
	}
	if p.ProjectID == "" || p.EnvironmentID == "" {
		return p, fmt.Errorf("%w: projectId and environmentId are required", plans.ErrInvalid)
	}
	return p, nil
}

// ---- plan kind ------------------------------------------------------------

// PlanKind adapts the Deployer to plans.Kind.
type PlanKind struct{ D *Deployer }

func (PlanKind) Name() string       { return KindDeploy }
func (PlanKind) Action() iam.Action { return iam.ActionProjectsDeploy }
func (PlanKind) JobKind() string    { return KindDeploy }

// Draft is the deployment of the unit as it is now: its files, the target,
// 044.1's findings as checks. High-risk settings need the owner's approval.
func (k PlanKind) Draft(ctx context.Context, raw json.RawMessage) (*plans.Draft, error) {
	p, err := parseParams(raw)
	if err != nil {
		return nil, err
	}
	u, err := k.D.Units.GetUnit(ctx, p.ProjectID, p.EnvironmentID, false)
	if errors.Is(err, projects.ErrNotFound) {
		return nil, fmt.Errorf("%w: no such Compose unit", plans.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	env, err := k.D.Units.GetEnvironment(ctx, p.ProjectID, p.EnvironmentID)
	if err != nil {
		return nil, err
	}
	pv, err := k.D.Units.PreviewUnit(ctx, u, k.D.Views, true)
	if err != nil {
		return nil, err
	}
	t := plans.Target{Key: "compose-unit:" + u.ID, Kind: "compose-unit", ID: u.ID, Name: u.ComposeProject,
		Environment: env.Name, Scope: projects.Scope(p.ProjectID), Version: u.Version}
	var checks []plans.Check
	if u.Target != nil {
		fp, name, err := k.D.Nodes.Node(ctx, u.Target.NodeID)
		if err != nil {
			checks = append(checks, plans.Check{ID: "target-node", Status: plans.CheckFailed, Title: "target node", Detail: "the target node is not registered"})
		} else {
			t.Node, t.NodeName = fp, name
		}
		if !slices.Contains(RootfulEnvironments, u.Target.EngineEnvironmentID) {
			checks = append(checks, plans.Check{ID: "target-rootful", Status: plans.CheckFailed, Title: "engine environment",
				Detail: "only the root-owned Docker or Podman engine of a host can be deployed to; this environment belongs to a user"})
		}
	}
	for _, b := range pv.Blockers {
		checks = append(checks, plans.Check{ID: b.Code, Status: plans.CheckFailed, Title: b.Code, Detail: b.Message, Target: t.Key, Evidence: evidence(b)})
	}
	for _, f := range pv.Analysis.Findings {
		switch f.Severity {
		case projects.SeverityHighRisk:
			checks = append(checks, plans.Check{ID: f.Code + ":" + f.Attribute, Status: plans.CheckHighRisk, Title: f.Code, Detail: f.Message, Target: t.Key, Evidence: evidence(f)})
		case projects.SeverityWarning:
			checks = append(checks, plans.Check{ID: f.Code + ":" + f.Attribute, Status: plans.CheckWarning, Title: f.Code, Detail: f.Message, Target: t.Key, Evidence: evidence(f)})
		}
	}
	if pv.Deployable {
		checks = append(checks, plans.Check{ID: "target-ready", Status: plans.CheckPass, Title: "target engine",
			Detail: "the target engine runs and has Docker Compose; images, disk space, host ports and bind mount sources are checked on the host before anything changes"})
	}
	var changes []plans.Change
	for _, f := range u.Files {
		changes = append(changes, plans.Change{Target: t.Key, Path: "files/" + f.Path, After: map[string]any{"size": f.Size, "sha256": f.SHA256}, Secret: f.Secret, Op: "add"})
	}
	changes = append(changes, plans.Change{Target: t.Key, Path: "compose", Op: "change",
		After: map[string]any{"mainFile": u.MainFile, "extraFiles": u.ExtraFiles, "profiles": u.Profiles, "workingDir": u.WorkingDir}})
	var affected []plans.Affected
	for _, s := range pv.Analysis.Services {
		affected = append(affected, plans.Affected{Kind: "compose-service", ID: u.ComposeProject + "/" + s.Name, Name: s.Name,
			Effect: "created or recreated by docker compose up", Relation: "direct"})
	}
	where := "no target"
	if t.NodeName != "" {
		where = t.NodeName
	}
	return &plans.Draft{Summary: fmt.Sprintf("deploy %s on %s · %s", u.ComposeProject, where, env.Name),
		Targets: []plans.Target{t}, Changes: changes, Affected: affected, Checks: checks, RequireApproval: pv.NeedsApproval}, nil
}

func evidence(f projects.Finding) []string {
	var e []string
	if f.File != "" {
		e = append(e, "file "+f.File)
	}
	if f.Attribute != "" {
		e = append(e, f.Attribute)
	}
	return e
}

// Job carries the plan out against the unit version it was computed from.
func (k PlanKind) Job(pl *plans.Plan) (jobs.SubmitRequest, error) {
	p, err := parseParams(pl.Params)
	if err != nil {
		return jobs.SubmitRequest{}, err
	}
	if len(pl.Targets) != 1 || pl.Targets[0].Node == "" {
		return jobs.SubmitRequest{}, fmt.Errorf("%w: the unit has no target node", plans.ErrPrecheck)
	}
	t := pl.Targets[0]
	p.UnitID, p.UnitVersion = t.ID, t.Version
	return jobRequest(KindDeploy, p, t.Name, t.Node)
}

func jobRequest(kind string, p Params, composeProject, fingerprint string) (jobs.SubmitRequest, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return jobs.SubmitRequest{}, err
	}
	return jobs.SubmitRequest{Kind: kind, Params: b, Node: fingerprint,
		Resources: []jobs.ResourceRef{{Kind: ResourceKind, ID: composeProject, Node: fingerprint}}}, nil
}

// CheckRequest is the compose.check job for a unit as it is now.
func (d *Deployer) CheckRequest(ctx context.Context, projectID, envID string) (jobs.SubmitRequest, error) {
	u, err := d.Units.GetUnit(ctx, projectID, envID, false)
	if err != nil {
		return jobs.SubmitRequest{}, err
	}
	if u.Target == nil {
		return jobs.SubmitRequest{}, fmt.Errorf("%w: choose the node and engine environment to deploy to first", plans.ErrPrecheck)
	}
	if !slices.Contains(RootfulEnvironments, u.Target.EngineEnvironmentID) {
		return jobs.SubmitRequest{}, fmt.Errorf("%w: only the root-owned Docker or Podman engine of a host can be deployed to", plans.ErrPrecheck)
	}
	fp, _, err := d.Nodes.Node(ctx, u.Target.NodeID)
	if err != nil {
		return jobs.SubmitRequest{}, fmt.Errorf("%w: the target node is not registered", plans.ErrPrecheck)
	}
	return jobRequest(KindCheck, Params{ProjectID: projectID, EnvironmentID: envID, UnitID: u.ID, UnitVersion: u.Version}, u.ComposeProject, fp)
}

// ---- job handlers -----------------------------------------------------------

// CheckHandler is the compose.check job.
type CheckHandler struct{ D *Deployer }

// DeployHandler is the compose.deploy job.
type DeployHandler struct{ D *Deployer }

func (CheckHandler) Kind() string  { return KindCheck }
func (DeployHandler) Kind() string { return KindDeploy }

func planSteps(job *jobs.Job, deploy bool) ([]jobs.StepSpec, error) {
	p, err := parseParams(job.Params)
	if err != nil || p.UnitID == "" || p.UnitVersion == 0 {
		return nil, &jobs.StepError{Code: "INVALID_PARAMS", Summary: "a Compose job needs projectId, environmentId, unitId and unitVersion"}
	}
	if job.Node == "" {
		return nil, &jobs.StepError{Code: "NODE_REQUIRED", Summary: "a Compose job runs on the unit's target node"}
	}
	steps := []jobs.StepSpec{{Name: "check the host (images, disk space, ports, bind mount sources)", Retryable: true, Cancellable: true}}
	if deploy {
		steps = append(steps, jobs.StepSpec{Name: "write the revision and run docker compose up", Retryable: true,
			RollbackMethod: "none: the previous revision's files stay on the host; returning to it is ticket 046"})
	}
	return steps, nil
}

func (CheckHandler) Plan(_ context.Context, job *jobs.Job) ([]jobs.StepSpec, error) {
	return planSteps(job, false)
}

func (DeployHandler) Plan(_ context.Context, job *jobs.Job) ([]jobs.StepSpec, error) {
	return planSteps(job, true)
}

func (CheckHandler) Recheck(context.Context, *jobs.Job, *jobs.Step) error  { return nil }
func (DeployHandler) Recheck(context.Context, *jobs.Job, *jobs.Step) error { return nil }

func (h CheckHandler) Execute(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter) error {
	_, err := h.D.check(ctx, job, step, rep)
	return err
}

func (h DeployHandler) Execute(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter) error {
	if step.Index == 0 {
		_, err := h.D.check(ctx, job, step, rep)
		return err
	}
	return h.D.deploy(ctx, job, step, rep)
}

func (CheckHandler) Rollback(context.Context, *jobs.Job, *jobs.Step, jobs.Reporter) error { return nil }

// Rollback leaves the host as the failed step left it: the ledger has the
// containers' state before and after, the previous revision's directory is
// kept, and the deployment row says it failed.
func (DeployHandler) Rollback(context.Context, *jobs.Job, *jobs.Step, jobs.Reporter) error {
	return nil
}

func (h CheckHandler) Reconcile(ctx context.Context, job *jobs.Job, step *jobs.Step) (jobs.StepReport, error) {
	return h.D.Steps.Reconcile(ctx, job, step)
}

func (h DeployHandler) Reconcile(ctx context.Context, job *jobs.Job, step *jobs.Step) (jobs.StepReport, error) {
	return h.D.Steps.Reconcile(ctx, job, step)
}

func (CheckHandler) Verify(context.Context, *jobs.Job) (jobs.CompletionReport, error) {
	return jobs.CompletionReport{Checks: []jobs.Check{{Name: "host-checks-passed", Required: true, Passed: true,
		Detail: "every host check passed (a failed check fails the step)"}}}, nil
}

// Verify: the deployment row says succeeded and every container the
// executor read afterwards is running.
func (h DeployHandler) Verify(ctx context.Context, job *jobs.Job) (jobs.CompletionReport, error) {
	d, err := h.D.Store.ByJob(ctx, job.ID)
	if err != nil {
		return jobs.CompletionReport{}, err
	}
	// The deploy step's outcome came from the agent's record (the result
	// was lost with the connection): the row still says running.
	if d.State == StateRunning && len(job.Steps) > 1 && job.Steps[1].Status == jobs.StepDone {
		if err := h.D.Store.Finish(ctx, job.ID, StateSucceeded, json.RawMessage(`{"reconciled":true}`), "", ""); err != nil {
			return jobs.CompletionReport{}, err
		}
		if d, err = h.D.Store.ByJob(ctx, job.ID); err != nil {
			return jobs.CompletionReport{}, err
		}
	}
	recorded := jobs.Check{Name: "deployment-recorded", Required: true, Passed: d.State == StateSucceeded, Detail: "deployment " + d.ID + " is " + d.State}
	var res struct {
		Containers []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"containers"`
	}
	_ = json.Unmarshal(d.Result, &res)
	var notRunning []string
	for _, c := range res.Containers {
		if c.State != "running" {
			notRunning = append(notRunning, c.Name+" "+c.State)
		}
	}
	running := jobs.Check{Name: "containers-running", Required: false, Passed: len(notRunning) == 0 && len(res.Containers) > 0,
		Detail: fmt.Sprintf("%d containers; not running: %s", len(res.Containers), strings.Join(notRunning, ", "))}
	return jobs.CompletionReport{Checks: []jobs.Check{recorded, running}}, nil
}

// ---- steps --------------------------------------------------------------------

type prepared struct {
	unit     *projects.Unit
	manifest Manifest
	staged   []StagedFile
	redact   *redactor
}

func (d *Deployer) prepare(ctx context.Context, job *jobs.Job) (*prepared, error) {
	p, _ := parseParams(job.Params)
	u, files, err := d.Units.DeployContent(ctx, p.UnitID, p.UnitVersion)
	switch {
	case errors.Is(err, projects.ErrVersionMismatch):
		return nil, &jobs.StepError{Code: "UNIT_CHANGED", NothingChanged: true,
			Summary: fmt.Sprintf("the unit is at version %d, not %d this job was made for; make a new deployment plan", u.Version, p.UnitVersion)}
	case errors.Is(err, projects.ErrNotFound):
		return nil, &jobs.StepError{Code: "UNIT_GONE", NothingChanged: true, Summary: "the Compose unit no longer exists"}
	case err != nil:
		return nil, err
	}
	if u.Target == nil {
		return nil, &jobs.StepError{Code: "TARGET_NOT_SET", NothingChanged: true, Summary: "the unit has no target engine"}
	}
	// The unit's files, secrets among them, only go to the unit's own
	// target node.
	if fp, _, err := d.Nodes.Node(ctx, u.Target.NodeID); err != nil || fp != job.Node {
		return nil, &jobs.StepError{Code: "TARGET_MOVED", NothingChanged: true,
			Summary: "the unit's target node is not the node this job runs on; make a new deployment"}
	}
	r := newRedactor()
	for _, f := range u.Files {
		if f.Secret {
			r.add(files[f.Path])
		}
	}
	m, staged := NewManifest(u.ComposeProject, job.ID, u.Target.EngineEnvironmentID, u.MainFile, u.ExtraFiles, u.Profiles, u.WorkingDir, files)
	// High-risk settings pass the executor only in a deployment whose plan
	// an owner approved: the plan service requires the owner for any
	// high-risk check, and the plan is bound to this unit version.
	if job.Kind == KindDeploy && job.ChangePlanID != "" && job.Approver != "" {
		an, err := d.Units.AnalyzeUnit(ctx, u)
		if err != nil {
			return nil, err
		}
		m.HostRiskApproved = slices.ContainsFunc(an.Findings, func(f projects.Finding) bool { return f.Severity == projects.SeverityHighRisk })
	}
	return &prepared{unit: u, manifest: m, staged: staged, redact: r}, nil
}

func (d *Deployer) run(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter, pr *prepared, deploy bool) (json.RawMessage, error) {
	action, exec, perm := ActionCheck, execCheck, PermCheck
	if deploy {
		action, exec, perm = ActionDeploy, execDeploy, PermDeploy
	}
	subject := job.Requester
	if subject == "" {
		subject = "system:compose"
	}
	g, err := d.Grants.IssueBound(subject, []string{perm}, GrantLifetime, pr.manifest.Binding(exec))
	if err != nil {
		return nil, &jobs.StepError{Code: "GRANT_FAILED", Summary: "cannot issue the executor grant: " + err.Error(), NothingChanged: true}
	}
	params := map[string]any{"grant": g, "manifest": pr.manifest, "files": pr.staged}
	if b, _ := json.Marshal(params); len(b) > MaxStepParamBytes {
		return nil, &jobs.StepError{Code: "UNIT_TOO_LARGE_TO_SEND", NothingChanged: true,
			Summary: fmt.Sprintf("the unit's files encode to %d bytes; one step carries at most %d; sending them in parts is not available yet", len(b), MaxStepParamBytes)}
	}
	res, err := d.Steps.Execute(ctx, job, step, redacting{rep, pr.redact}, nodes.Action{Name: action, Version: 1, Params: params})
	var se *jobs.StepError
	if errors.As(err, &se) {
		se.Summary = pr.redact.clean(se.Summary)
		for i, e := range se.Evidence {
			se.Evidence[i] = pr.redact.clean(e)
		}
	}
	return res, err
}

// HostCheck is one host check the executor made.
type HostCheck struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Service  string `json:"service,omitempty"`
	Detail   string `json:"detail"`
}

func (d *Deployer) check(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter) (json.RawMessage, error) {
	pr, err := d.prepare(ctx, job)
	if err != nil {
		return nil, err
	}
	res, err := d.run(ctx, job, step, rep, pr, false)
	if err != nil {
		return nil, err
	}
	var out struct {
		Ready  bool        `json:"ready"`
		Checks []HostCheck `json:"checks"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, &jobs.StepError{Code: "AGENT_RESULT_INVALID", Summary: "the host check result is not readable"}
	}
	var failed []string
	for _, c := range out.Checks {
		line := fmt.Sprintf("host check %s %s", c.Severity, c.Code)
		if c.Service != "" {
			line += " service " + c.Service
		}
		line = pr.redact.clean(line + ": " + c.Detail)
		rep.Log(line)
		if c.Severity == "error" {
			failed = append(failed, line)
		}
	}
	if !out.Ready {
		return nil, &jobs.StepError{Code: "HOST_CHECK_FAILED", NothingChanged: true, Evidence: failed,
			Summary: fmt.Sprintf("%d host checks failed; nothing was changed", len(failed))}
	}
	rep.Log(fmt.Sprintf("host checks passed (%d findings)", len(out.Checks)))
	return res, nil
}

func (d *Deployer) deploy(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter) error {
	pr, err := d.prepare(ctx, job)
	if err != nil {
		return err
	}
	u := pr.unit
	if _, err := d.Store.Begin(ctx, Deployment{ID: "dep_" + strings.TrimPrefix(job.ID, "job_"), UnitID: u.ID, UnitVersion: u.Version,
		ComposeProject: u.ComposeProject, Revision: pr.manifest.Revision, ManifestDigest: pr.manifest.Digest(), NodeID: u.Target.NodeID,
		EngineEnvironmentID: u.Target.EngineEnvironmentID, JobID: job.ID, PlanID: job.ChangePlanID, CreatedBy: job.Requester}); err != nil {
		return err
	}
	res, err := d.run(ctx, job, step, rep, pr, true)
	if err != nil {
		var se *jobs.StepError
		if errors.As(err, &se) {
			if ferr := d.Store.Finish(ctx, job.ID, StateFailed, nil, se.Code, se.Summary); ferr != nil {
				return ferr
			}
		}
		// A lost connection is reconciled by the engine; the row stays
		// running until the outcome is known.
		return err
	}
	rep.Log("deployed revision " + pr.manifest.Revision)
	return d.Store.Finish(ctx, job.ID, StateSucceeded, json.RawMessage(pr.redact.clean(string(res))), "", "")
}

// ---- secrets in output ------------------------------------------------------------

// redactor replaces secret file content in log lines and error texts. It
// is defence in depth: the executor never prints file content, but build
// output is the user's own and may echo anything.
type redactor struct{ values []string }

func newRedactor() *redactor { return &redactor{} }

// add registers each line of a secret file (and its value after '=' for
// env-style lines) of at least 6 characters.
func (r *redactor) add(content []byte) {
	for _, l := range strings.Split(string(content), "\n") {
		l = strings.TrimSpace(l)
		if len(l) >= 6 {
			r.values = append(r.values, l)
		}
		if _, v, ok := strings.Cut(l, "="); ok {
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if len(v) >= 6 {
				r.values = append(r.values, v)
			}
		}
	}
	slices.SortFunc(r.values, func(a, b string) int { return len(b) - len(a) })
}

func (r *redactor) clean(s string) string {
	for _, v := range r.values {
		s = strings.ReplaceAll(s, v, "[secret]")
	}
	return s
}

type redacting struct {
	jobs.Reporter
	r *redactor
}

func (x redacting) Log(line string)     { x.Reporter.Log(x.r.clean(line)) }
func (x redacting) Evidence(ref string) { x.Reporter.Evidence(x.r.clean(ref)) }

// RegistryNodes adapts the node registry's GetNode.
type RegistryNodes struct {
	Get func(ctx context.Context, id string) (nodereg.Node, error)
}

// Node returns the fingerprint and name of node id.
func (r RegistryNodes) Node(ctx context.Context, id string) (string, string, error) {
	n, err := r.Get(ctx, id)
	if err != nil {
		return "", "", err
	}
	return n.Fingerprint, n.Name, nil
}

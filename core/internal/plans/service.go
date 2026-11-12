package plans

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
)

// Auditor writes audit rows. RecordTx writes inside the caller's
// transaction, so a plan's state and its audit row commit together.
// *iam.Service implements it.
type Auditor interface {
	RecordTx(ctx context.Context, tx *sql.Tx, ev iam.AuditEvent) error
	RecordDenied(ctx context.Context, p *iam.Principal, a iam.Action, on iam.Scope, src iam.Source)
}

// Submitter creates jobs; *jobs.Engine implements it. Preview validates a
// request the way Submit would without recording anything.
type Submitter interface {
	Preview(ctx context.Context, req jobs.SubmitRequest) (*jobs.Preview, error)
	Submit(ctx context.Context, req jobs.SubmitRequest) (*jobs.Job, bool, error)
}

// Options configures a Service.
type Options struct {
	DB    *sql.DB
	Audit Auditor
	Jobs  Submitter
	// Now is the clock; UTC is applied. Defaults to time.Now.
	Now func() time.Time
}

// Service is the change-plan engine. Every method takes the calling
// principal and decides permissions itself, so no route can forget a check.
type Service struct {
	o     Options
	gate  func(ctx context.Context, req jobs.SubmitRequest) error
	mu    sync.RWMutex
	kinds map[string]Kind
	// byJob maps a job kind to the plan kind that owns it.
	byJob map[string]string
}

// New returns a Service.
func New(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{o: o, kinds: map[string]Kind{}, byJob: map[string]string{}}
}

// writeTimeout bounds a state change once it was decided. The write does
// not inherit the caller's cancellation: a client that goes away after the
// decision must not leave a half-applied change without its audit row.
const writeTimeout = 10 * time.Second

func (s *Service) now() time.Time { return s.o.Now().UTC() }

// SetGate installs the write gate an execution passes before it is claimed
// (platform degraded, a pending confirmation on the same resources…). It is
// set once while the server is assembled.
func (s *Service) SetGate(g func(ctx context.Context, req jobs.SubmitRequest) error) { s.gate = g }

// Register adds a plan kind. Names and job kinds must be unique.
func (s *Service) Register(k Kind) error {
	if k == nil || k.Name() == "" || k.JobKind() == "" || k.Action() == "" {
		return errors.New("plans: a kind needs a name, a job kind and an action")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.kinds[k.Name()]; dup {
		return fmt.Errorf("plans: kind %q already registered", k.Name())
	}
	if owner, dup := s.byJob[k.JobKind()]; dup {
		return fmt.Errorf("plans: job kind %q already belongs to plan kind %q", k.JobKind(), owner)
	}
	s.kinds[k.Name()] = k
	s.byJob[k.JobKind()] = k.Name()
	return nil
}

// Kinds lists the registered plan kinds, sorted.
func (s *Service) Kinds() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.kinds))
	for n := range s.kinds {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// RequiresPlan reports whether jobs of jobKind may only come from a plan.
func (s *Service) RequiresPlan(jobKind string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.byJob[jobKind]
	return ok
}

func (s *Service) kind(name string) (Kind, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.kinds[name]
	return k, ok
}

// ListOptions narrows List and Count. States filter by the effective state:
// a plan past its deadline counts as expired even before it is written so.
type ListOptions struct {
	States      []State
	Kind        string
	RequestedBy string
	Page        paging.Query
}

// ExecuteResult is what Execute returns.
type ExecuteResult struct {
	Plan *Plan
	Job  *jobs.Job
	// Replayed: the plan had already been executed with this request id and
	// the existing job is returned.
	Replayed bool
}

// ---- create ----------------------------------------------------------------

// Create computes a plan of kind for params and records it. The caller needs
// the kind's action on every target; a target it cannot see answers
// ErrNotFound. A plan with no warning or high-risk check whose kind does not
// demand approval is approved on creation (autoApproved).
func (s *Service) Create(ctx context.Context, p *iam.Principal, kind string, params json.RawMessage, src iam.Source) (*Plan, error) {
	k, ok := s.kind(kind)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	if !json.Valid(params) {
		return nil, fmt.Errorf("%w: params must be JSON", ErrInvalid)
	}
	d, err := k.Draft(ctx, params)
	if err != nil {
		return nil, err
	}
	if err := validDraft(d); err != nil {
		return nil, err
	}
	pl := &Plan{Kind: kind, Params: params}
	pl.setContent(d, k)
	if err := s.authorize(ctx, p, pl, k.Action(), src); err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	pl.salt = salt
	if pl.Digest, err = digest(kind, params, d, pl.Approval, pl.Risk, salt); err != nil {
		return nil, err
	}
	now := s.now()
	ttl := d.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	ttl = min(ttl, MaxTTL)
	pl.ID = newID()
	pl.RequestedBy = actor(p)
	pl.CreatedAt, pl.UpdatedAt, pl.ExpiresAt = now, now, now.Add(ttl)
	pl.State = StateAwaitingApproval
	if !pl.Approval.Required && pl.Executable {
		exp := pl.ExpiresAt
		pl.State = StateApproved
		pl.Approval.AutoApproved = true
		pl.Approval.Digest = pl.Digest
		pl.Approval.ApprovedAt = &now
		pl.Approval.ExpiresAt = &exp
	}
	err = s.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		doc, err := json.Marshal(pl)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO change_plans(id,kind,state,digest,secret_salt,requested_by,created_at,expires_at,updated_at,approved_at,approved_digest,auto_approved,doc)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, pl.ID, pl.Kind, string(pl.State), pl.Digest, salt, pl.RequestedBy.ID, stamp(now), stamp(pl.ExpiresAt), stamp(now),
			nullTime(pl.Approval.ApprovedAt), nullStr(pl.Approval.Digest), boolInt(pl.Approval.AutoApproved), string(doc)); err != nil {
			return err
		}
		for _, sc := range pl.scopes() {
			if _, err := tx.ExecContext(ctx, `INSERT INTO change_plan_scopes(plan_id,scope_kind,scope_id) VALUES(?,?,?)`, pl.ID, string(sc.Kind), sc.ID); err != nil {
				return err
			}
		}
		return s.audit(ctx, tx, p, pl, "change-plans.create", src, nil, map[string]any{
			"state": pl.State, "digest": pl.Digest, "risk": pl.Risk, "expiresAt": pl.ExpiresAt, "summary": pl.Summary, "autoApproved": pl.Approval.AutoApproved})
	})
	if err != nil {
		return nil, err
	}
	return pl, nil
}

// setContent fills the content fields of pl from a draft.
func (pl *Plan) setContent(d *Draft, k Kind) {
	pl.Summary = d.Summary
	pl.Targets = slices.Clone(d.Targets)
	pl.Changes = maskSecrets(d.Changes)
	pl.Affected = slices.Clone(d.Affected)
	pl.Checks = slices.Clone(d.Checks)
	pl.Risk, pl.Executable = riskOf(d.Checks)
	pl.Approval = Approval{Action: k.Action(), Required: d.RequireApproval || pl.Risk != RiskLow, OwnerRequired: pl.Risk == RiskHigh}
}

func validDraft(d *Draft) error {
	if d == nil || len(d.Targets) == 0 {
		return fmt.Errorf("%w: a plan needs at least one target", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, t := range d.Targets {
		if t.Key == "" || seen[t.Key] {
			return fmt.Errorf("plans: kind returned an empty or repeated target key %q", t.Key)
		}
		seen[t.Key] = true
		switch t.Scope.Kind {
		case iam.ScopeGlobal, iam.ScopeNode, iam.ScopeProject, iam.ScopeDatabase:
		default:
			return fmt.Errorf("plans: target %q has no valid scope", t.Key)
		}
	}
	for _, c := range d.Checks {
		switch c.Status {
		case CheckPass, CheckWarning, CheckHighRisk, CheckFailed:
		default:
			return fmt.Errorf("plans: check %q has unknown status %q", c.ID, c.Status)
		}
	}
	return nil
}

func riskOf(checks []Check) (Risk, bool) {
	risk, executable := RiskLow, true
	for _, c := range checks {
		switch c.Status {
		case CheckHighRisk:
			risk = RiskHigh
		case CheckWarning:
			if risk == RiskLow {
				risk = RiskWarning
			}
		case CheckFailed:
			executable = false
		}
	}
	return risk, executable
}

// maskSecrets drops the values of secret changes; the digest still covers
// them.
func maskSecrets(in []Change) []Change {
	out := slices.Clone(in)
	for i := range out {
		if out[i].Secret {
			out[i].Before, out[i].After = nil, nil
		}
	}
	return out
}

func (pl *Plan) scopes() []iam.Scope {
	var out []iam.Scope
	for _, t := range pl.Targets {
		if !slices.Contains(out, t.Scope) {
			out = append(out, t.Scope)
		}
	}
	return out
}

// ---- read ------------------------------------------------------------------

// Get returns a plan the caller can see in full; otherwise ErrNotFound.
func (s *Service) Get(ctx context.Context, p *iam.Principal, id string) (*Plan, error) {
	pl, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.visible(ctx, p, pl, src0); err != nil {
		return nil, err
	}
	return pl, nil
}

var src0 = iam.Source{}

// List pages the plans the caller can see in full.
func (s *Service) List(ctx context.Context, p *iam.Principal, o ListOptions) ([]*Plan, error) {
	where, args := s.filter(p, o)
	order := "created_at DESC, id DESC"
	limit := 200
	if len(o.Page.Sort) > 0 {
		ob, w, a, err := o.Page.SQL(SortColumns)
		if err != nil {
			return nil, err
		}
		order, limit = ob, o.Page.Limit
		if w != "" {
			where = append(where, w)
			args = append(args, a...)
		}
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	q := `SELECT ` + planColumns + ` FROM change_plans`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY " + order + " LIMIT ?"
	args = append(args, limit)
	rows, err := s.o.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Plan
	for rows.Next() {
		pl, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pl)
	}
	return out, rows.Err()
}

// Count counts what List would return without paging.
func (s *Service) Count(ctx context.Context, p *iam.Principal, o ListOptions) (int, error) {
	where, args := s.filter(p, o)
	q := `SELECT COUNT(*) FROM change_plans`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	var n int
	err := s.o.DB.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// effectiveState is the state column with plans past their deadline counted
// as expired. Its one argument is the current time.
const effectiveState = `(CASE WHEN state IN ('awaiting_approval', 'approved') AND expires_at <= ? THEN 'expired' ELSE state END)`

// SortColumns maps list sort fields to columns.
var SortColumns = map[string]paging.Column{
	"createdAt": {Expr: "created_at"},
	"expiresAt": {Expr: "expires_at"},
	"kind":      {Expr: "kind"},
	"id":        {Expr: "id"},
}

func (s *Service) filter(p *iam.Principal, o ListOptions) ([]string, []any) {
	var where []string
	var args []any
	if len(o.States) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(o.States)), ",")
		where = append(where, effectiveState+" IN ("+ph+")")
		args = append(args, stamp(s.now()))
		for _, st := range o.States {
			args = append(args, string(st))
		}
	}
	if o.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, o.Kind)
	}
	if o.RequestedBy != "" {
		where = append(where, "requested_by = ?")
		args = append(args, o.RequestedBy)
	}
	if w, a, all := visibilityFilter(p); !all {
		where = append(where, w)
		args = append(args, a...)
	}
	return where, args
}

// noSuchID is never a scope id; asking iam.Visible about it tells whether a
// scope kind is visible as a whole (a global assignment).
const noSuchID = "\x00any"

// visibilityFilter is the SQL form of "every target scope is visible to p":
// no scope row of the plan falls outside what p can see.
func visibilityFilter(p *iam.Principal) (where string, args []any, all bool) {
	kinds := []iam.ScopeKind{iam.ScopeGlobal, iam.ScopeNode, iam.ScopeProject, iam.ScopeDatabase}
	var ors []string
	all = true
	for _, k := range kinds {
		probe := iam.Scope{Kind: k, ID: noSuchID}
		if k == iam.ScopeGlobal {
			probe = iam.Global
		}
		if p != nil && iam.Visible(p, probe) {
			ors = append(ors, "cs.scope_kind = ?")
			args = append(args, string(k))
			continue
		}
		all = false
		if p == nil {
			continue
		}
		var ids []string
		for _, as := range p.Assignments {
			if as.Scope.Kind == k && !slices.Contains(ids, as.Scope.ID) && iam.Visible(p, as.Scope) {
				ids = append(ids, as.Scope.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		ors = append(ors, "(cs.scope_kind = ? AND cs.scope_id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+"))")
		args = append(args, string(k))
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if all {
		return "", nil, true
	}
	cond := "0"
	if len(ors) > 0 {
		cond = strings.Join(ors, " OR ")
	}
	return "NOT EXISTS (SELECT 1 FROM change_plan_scopes cs WHERE cs.plan_id = change_plans.id AND NOT (" + cond + "))", args, false
}

// ---- decisions -------------------------------------------------------------

// Approve approves the plan the caller reviewed, named by its digest. The
// plan is computed again first: a different content or target version
// invalidates it and returns a *ChangedError with the difference. High-risk
// plans need an owner.
func (s *Service) Approve(ctx context.Context, p *iam.Principal, id, sentDigest string, src iam.Source) (*Plan, error) {
	if strings.TrimSpace(sentDigest) == "" {
		return nil, fmt.Errorf("%w: digest is required: approve the digest of the plan you reviewed", ErrInvalid)
	}
	pl, k, err := s.decidable(ctx, p, id, src, StateAwaitingApproval)
	if err != nil {
		return nil, err
	}
	if !pl.Executable {
		return nil, fmt.Errorf("%w: fix the failed checks and create a new plan", ErrPrecheck)
	}
	if pl.Approval.OwnerRequired && !isOwner(p) {
		s.denied(ctx, p, k.Action(), pl, src)
		return nil, &ForbiddenError{Reason: "owner-required", Action: k.Action(), Scope: iam.Global}
	}
	if sentDigest != pl.Digest {
		return nil, &ChangedError{PlanDigest: pl.Digest, SentDigest: sentDigest, Targets: []TargetMove{}, Planned: pl.Changes, Current: []Change{}}
	}
	if err := s.recheck(ctx, p, pl, k, src); err != nil {
		return nil, err
	}
	now := s.now()
	before := map[string]any{"state": pl.State, "digest": pl.Digest}
	exp := pl.ExpiresAt
	by := actor(p)
	pl.State, pl.UpdatedAt = StateApproved, now
	pl.Approval.ApprovedBy, pl.Approval.ApprovedAt, pl.Approval.Digest, pl.Approval.ExpiresAt = &by, &now, pl.Digest, &exp
	err = s.transitionLive(ctx, pl, StateAwaitingApproval, `approved_by = ?, approved_at = ?, approved_digest = ?`, []any{by.ID, stamp(now), pl.Digest},
		func(ctx context.Context, tx *sql.Tx) error {
			return s.audit(ctx, tx, p, pl, "change-plans.approve", src, before, map[string]any{"state": pl.State, "digest": pl.Digest, "expiresAt": pl.ExpiresAt})
		})
	if errors.Is(err, ErrState) {
		if cur, lerr := s.load(ctx, id); lerr == nil && cur.State == StateExpired {
			return nil, s.liveIn(ctx, p, cur, src, StateAwaitingApproval)
		}
	}
	if err != nil {
		return nil, err
	}
	return pl, nil
}

// Reject ends a plan that is waiting or approved but not run. It needs the
// kind's action on every target and a reason.
func (s *Service) Reject(ctx context.Context, p *iam.Principal, id, reason string, src iam.Source) (*Plan, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrInvalid)
	}
	pl, _, err := s.decidable(ctx, p, id, src, StateAwaitingApproval, StateApproved)
	if err != nil {
		return nil, err
	}
	return s.end(ctx, p, pl, StateRejected, "change-plans.reject", reason, src)
}

// Withdraw ends the caller's own plan that is waiting or approved but not
// run.
func (s *Service) Withdraw(ctx context.Context, p *iam.Principal, id, reason string, src iam.Source) (*Plan, error) {
	pl, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.visible(ctx, p, pl, src); err != nil {
		return nil, err
	}
	if p.User.ID != pl.RequestedBy.ID {
		s.denied(ctx, p, "change-plans:withdraw", pl, src)
		return nil, &ForbiddenError{Reason: "not-requester"}
	}
	if err := s.liveIn(ctx, p, pl, src, StateAwaitingApproval, StateApproved); err != nil {
		return nil, err
	}
	return s.end(ctx, p, pl, StateWithdrawn, "change-plans.withdraw", reason, src)
}

func (s *Service) end(ctx context.Context, p *iam.Principal, pl *Plan, to State, action, reason string, src iam.Source) (*Plan, error) {
	now := s.now()
	from := pl.State
	by := actor(p)
	pl.State, pl.UpdatedAt = to, now
	pl.Decision = &Decision{By: &by, At: now, Reason: reason}
	err := s.transition(ctx, pl, from, `decided_by = ?, decided_at = ?, decision_reason = ?`, []any{by.ID, stamp(now), reason},
		func(ctx context.Context, tx *sql.Tx) error {
			return s.audit(ctx, tx, p, pl, action, src, map[string]any{"state": from}, map[string]any{"state": to, "reason": reason})
		})
	if err != nil {
		return nil, err
	}
	return pl, nil
}

// Execute runs an approved plan as a job. requestID deduplicates: calling
// again with the same id returns the same job; another id is refused. The
// plan is computed again first, so a target changed since approval stops the
// run and invalidates the approval (T-030).
func (s *Service) Execute(ctx context.Context, p *iam.Principal, id, sentDigest, requestID string, src iam.Source) (*ExecuteResult, error) {
	if strings.TrimSpace(requestID) == "" || strings.TrimSpace(sentDigest) == "" {
		return nil, fmt.Errorf("%w: requestId and digest are required", ErrInvalid)
	}
	pl, k, err := s.loadFor(ctx, p, id, src)
	if err != nil {
		return nil, err
	}
	if pl.State == StateExecuted {
		return s.replay(ctx, pl, k, requestID)
	}
	if err := s.liveIn(ctx, p, pl, src, StateApproved); err != nil {
		return nil, err
	}
	if sentDigest != pl.Digest || pl.Approval.Digest != pl.Digest {
		return nil, &ChangedError{PlanDigest: pl.Digest, SentDigest: sentDigest, Targets: []TargetMove{}, Planned: pl.Changes, Current: []Change{}}
	}
	if err := s.recheck(ctx, p, pl, k, src); err != nil {
		return nil, err
	}
	now := s.now()
	by := actor(p)
	pl.Execution = &Execution{RequestID: requestID, By: by, At: now}
	req, err := s.jobRequest(pl, k)
	if err != nil {
		return nil, err
	}
	if s.gate != nil {
		if err := s.gate(ctx, req); err != nil {
			return nil, err
		}
	}
	// The job is checked before the plan is claimed: a request id taken by
	// another job, or a job kind without a handler, refuses here and leaves
	// the plan approved.
	pv, err := s.o.Jobs.Preview(ctx, req)
	switch {
	case errors.Is(err, jobs.ErrConflict), err == nil && pv.Replay != nil && pv.Replay.ChangePlanID != pl.ID:
		return nil, fmt.Errorf("%w: request id %q is used by another job", ErrRequestIDTaken, requestID)
	case errors.Is(err, jobs.ErrUnknownKind), errors.Is(err, jobs.ErrInvalidRequest):
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	case err != nil:
		return nil, err
	}
	pl.State, pl.UpdatedAt = StateExecuted, now
	err = s.transitionLive(ctx, pl, StateApproved, `request_id = ?, executed_by = ?, executed_at = ?`, []any{requestID, by.ID, stamp(now)},
		func(ctx context.Context, tx *sql.Tx) error {
			return s.audit(ctx, tx, p, pl, "change-plans.execute", src, map[string]any{"state": StateApproved},
				map[string]any{"state": pl.State, "digest": pl.Digest, "requestId": requestID})
		})
	if isUnique(err) {
		return nil, fmt.Errorf("%w: request id %q is used by another plan", ErrRequestIDTaken, requestID)
	}
	if errors.Is(err, ErrState) {
		// Another call claimed it first; with the same request id that is a
		// retry of this one and answers the same job. A plan that reached
		// its deadline meanwhile answers expired.
		cur, lerr := s.load(ctx, id)
		switch {
		case lerr == nil && cur.State == StateExecuted:
			return s.replay(ctx, cur, k, requestID)
		case lerr == nil && cur.State == StateExpired:
			return nil, s.liveIn(ctx, p, cur, src, StateApproved)
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	res, created, err := s.submitJob(ctx, pl, req, false)
	if err != nil && !created {
		// No job exists, so the approval is still unused.
		if rerr := s.unclaim(ctx, p, pl, requestID, err, src); rerr != nil {
			return nil, errors.Join(err, rerr)
		}
		return nil, err
	}
	return res, err
}

// replay answers an execute of an executed plan: the same request id gets
// its job again, another one is refused.
func (s *Service) replay(ctx context.Context, pl *Plan, k Kind, requestID string) (*ExecuteResult, error) {
	if pl.Execution == nil || pl.Execution.RequestID != requestID {
		return nil, ErrRequestIDTaken
	}
	req, err := s.jobRequest(pl, k)
	if err != nil {
		return nil, err
	}
	return s.submit(ctx, pl, req, true)
}

// unclaim puts a plan whose job could not be created back to approved, with
// an audit row saying the execution failed, so it can be sent again.
func (s *Service) unclaim(ctx context.Context, p *iam.Principal, pl *Plan, requestID string, cause error, src iam.Source) error {
	pl.State, pl.UpdatedAt, pl.Execution = StateApproved, s.now(), nil
	return s.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		doc, err := json.Marshal(pl)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE change_plans SET state = 'approved', updated_at = ?, doc = ?, request_id = NULL, executed_by = NULL, executed_at = NULL
			WHERE id = ? AND state = 'executed' AND request_id = ? AND job_id IS NULL`, stamp(pl.UpdatedAt), string(doc), pl.ID, requestID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("%w: the plan changed state meanwhile", ErrState)
		}
		ev := map[string]any{"requestId": requestID, "error": cause.Error()}
		return s.auditResult(ctx, tx, p, pl, "change-plans.execute", iam.ResultFailed, src, map[string]any{"state": StateExecuted}, ev)
	})
}

// jobRequest is the job an executed plan runs as.
func (s *Service) jobRequest(pl *Plan, k Kind) (jobs.SubmitRequest, error) {
	req, err := k.Job(pl)
	if err != nil {
		return jobs.SubmitRequest{}, err
	}
	req.RequestID = pl.Execution.RequestID
	req.Requester = pl.Execution.By.ID
	switch {
	case pl.Approval.ApprovedBy != nil:
		req.Approver = pl.Approval.ApprovedBy.ID
	case pl.Approval.AutoApproved:
		req.Approver = AutoApprover
	}
	req.ChangePlanID, req.ChangePlanDigest = pl.ID, pl.Digest
	req.RequiredPermissions = []string{string(k.Action())}
	if req.Kind == "" {
		req.Kind = k.JobKind()
	}
	if req.Kind != k.JobKind() {
		return jobs.SubmitRequest{}, fmt.Errorf("plans: kind %s built a %s job, not its own %s", k.Name(), req.Kind, k.JobKind())
	}
	return req, nil
}

// submit creates (or finds again) the job of an executed plan.
func (s *Service) submit(ctx context.Context, pl *Plan, req jobs.SubmitRequest, replay bool) (*ExecuteResult, error) {
	res, created, err := s.submitJob(ctx, pl, req, replay)
	if created {
		return res, nil
	}
	return nil, err
}

// submitJob creates the job and records its id on the plan. created reports
// whether the job exists; once it does, the plan stays executed even when
// recording the id failed: the job will run, and replaying the same
// request id records it later.
func (s *Service) submitJob(ctx context.Context, pl *Plan, req jobs.SubmitRequest, replay bool) (res *ExecuteResult, created bool, err error) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	job, dedup, err := s.o.Jobs.Submit(wctx, req)
	if err != nil {
		return nil, false, err
	}
	res = &ExecuteResult{Plan: pl, Job: job, Replayed: replay || dedup}
	if pl.Execution.JobID != job.ID {
		pl.Execution.JobID = job.ID
		doc, err := json.Marshal(pl)
		if err == nil {
			_, err = s.o.DB.ExecContext(wctx, `UPDATE change_plans SET job_id = ?, doc = ? WHERE id = ?`, job.ID, string(doc), pl.ID)
		}
		if err != nil {
			slog.Error("change plan: job created but its id could not be recorded; replaying the request id records it", "plan", pl.ID, "job", job.ID, "error", err)
		}
	}
	return res, true, nil
}

// decidable loads a plan for a decision: visible, the kind's action on
// every target, and live in one of states.
func (s *Service) decidable(ctx context.Context, p *iam.Principal, id string, src iam.Source, states ...State) (*Plan, Kind, error) {
	pl, k, err := s.loadFor(ctx, p, id, src)
	if err != nil {
		return nil, nil, err
	}
	if err := s.liveIn(ctx, p, pl, src, states...); err != nil {
		return nil, nil, err
	}
	return pl, k, nil
}

// loadFor loads a plan the caller can see (404 first, so a missing kind
// never tells a stranger the plan exists) and holds its kind's action on
// every target.
func (s *Service) loadFor(ctx context.Context, p *iam.Principal, id string, src iam.Source) (*Plan, Kind, error) {
	pl, err := s.load(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if err := s.visible(ctx, p, pl, src); err != nil {
		return nil, nil, err
	}
	k, ok := s.kind(pl.Kind)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %q is no longer registered", ErrUnknownKind, pl.Kind)
	}
	if err := s.authorize(ctx, p, pl, k.Action(), src); err != nil {
		return nil, nil, err
	}
	return pl, k, nil
}

// liveIn checks the plan is in one of states and before its deadline. A plan
// found past its deadline is written as expired (with its audit row).
func (s *Service) liveIn(ctx context.Context, p *iam.Principal, pl *Plan, src iam.Source, states ...State) error {
	if pl.State == StateExpired && pl.storedState != StateExpired {
		from := pl.storedState
		pl.UpdatedAt = s.now()
		err := s.transition(ctx, pl, from, "", nil, func(ctx context.Context, tx *sql.Tx) error {
			return s.audit(ctx, tx, p, pl, "change-plans.expire", src, map[string]any{"state": from}, map[string]any{"state": StateExpired, "expiresAt": pl.ExpiresAt})
		})
		if err != nil && !errors.Is(err, ErrState) {
			return err
		}
		return fmt.Errorf("%w at %s; create a new plan", ErrExpired, pl.ExpiresAt.Format(time.RFC3339))
	}
	if pl.State == StateExpired {
		return fmt.Errorf("%w at %s; create a new plan", ErrExpired, pl.ExpiresAt.Format(time.RFC3339))
	}
	if !slices.Contains(states, pl.State) {
		return fmt.Errorf("%w: the plan is %s", ErrState, pl.State)
	}
	return nil
}

// recheck computes the plan again from its params. When the digest differs
// or a target is gone, the plan is invalidated and the difference returned.
func (s *Service) recheck(ctx context.Context, p *iam.Principal, pl *Plan, k Kind, src iam.Source) error {
	d, err := k.Draft(ctx, pl.Params)
	ce := &ChangedError{PlanDigest: pl.Digest, Targets: []TargetMove{}, Planned: pl.Changes, Current: []Change{}, Invalidated: true}
	switch {
	case errors.Is(err, ErrNotFound):
		for _, t := range pl.Targets {
			ce.Targets = append(ce.Targets, TargetMove{Key: t.Key, Name: t.Name, PlannedVersion: t.Version})
		}
	case err != nil:
		return err
	default:
		if err := validDraft(d); err != nil {
			return err
		}
		probe := &Plan{}
		probe.setContent(d, k)
		cur, err := digest(pl.Kind, pl.Params, d, probe.Approval, probe.Risk, pl.salt)
		if err != nil {
			return err
		}
		if cur == pl.Digest {
			return nil
		}
		ce.CurrentDigest, ce.Current, ce.Targets = cur, probe.Changes, moves(pl.Targets, d.Targets)
	}
	from := pl.State
	pl.State, pl.UpdatedAt = StateInvalidated, s.now()
	pl.Invalidation = ce
	pl.Approval.ApprovedBy, pl.Approval.ApprovedAt, pl.Approval.Digest, pl.Approval.ExpiresAt, pl.Approval.AutoApproved = nil, nil, "", nil, false
	now := s.now()
	err = s.transition(ctx, pl, from, `decided_at = ?, decision_reason = ?`, []any{stamp(now), "content or target version changed"},
		func(ctx context.Context, tx *sql.Tx) error {
			return s.audit(ctx, tx, p, pl, "change-plans.invalidate", src, map[string]any{"state": from, "digest": pl.Digest},
				map[string]any{"state": pl.State, "currentDigest": ce.CurrentDigest, "targets": ce.Targets})
		})
	if err != nil {
		return err
	}
	return ce
}

// moves lists targets whose version differs or that appeared or vanished.
func moves(planned, current []Target) []TargetMove {
	out := []TargetMove{}
	cur := map[string]Target{}
	for _, t := range current {
		cur[t.Key] = t
	}
	for _, t := range planned {
		c, ok := cur[t.Key]
		switch {
		case !ok:
			out = append(out, TargetMove{Key: t.Key, Name: t.Name, PlannedVersion: t.Version})
		case c.Version != t.Version:
			out = append(out, TargetMove{Key: t.Key, Name: t.Name, PlannedVersion: t.Version, CurrentVersion: c.Version})
		}
		delete(cur, t.Key)
	}
	for _, t := range current {
		if _, extra := cur[t.Key]; extra {
			out = append(out, TargetMove{Key: t.Key, Name: t.Name, CurrentVersion: t.Version})
		}
	}
	return out
}

// ---- permissions -----------------------------------------------------------

// visible answers ErrNotFound unless the caller can see every target. A
// refusal on an existing plan is audited.
func (s *Service) visible(ctx context.Context, p *iam.Principal, pl *Plan, src iam.Source) error {
	for _, sc := range pl.scopes() {
		if !iam.Visible(p, sc) {
			s.recordDenied(ctx, p, "change-plans:read", sc, src)
			return ErrNotFound
		}
	}
	return nil
}

// authorize requires visibility of every target, then action on each.
func (s *Service) authorize(ctx context.Context, p *iam.Principal, pl *Plan, a iam.Action, src iam.Source) error {
	if err := s.visible(ctx, p, pl, src); err != nil {
		return err
	}
	for _, sc := range pl.scopes() {
		if !iam.Authorize(p, a, sc) {
			s.recordDenied(ctx, p, a, sc, src)
			return &ForbiddenError{Reason: "missing-permission", Action: a, Scope: sc}
		}
	}
	return nil
}

func (s *Service) denied(ctx context.Context, p *iam.Principal, a iam.Action, pl *Plan, src iam.Source) {
	sc := iam.Global
	if sco := pl.scopes(); len(sco) > 0 {
		sc = sco[0]
	}
	s.recordDenied(ctx, p, a, sc, src)
}

func (s *Service) recordDenied(ctx context.Context, p *iam.Principal, a iam.Action, sc iam.Scope, src iam.Source) {
	if s.o.Audit == nil {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	s.o.Audit.RecordDenied(wctx, p, a, sc, src)
}

// isOwner: an owner assignment on the global scope.
func isOwner(p *iam.Principal) bool {
	if p == nil || p.User.Status != iam.StatusActive {
		return false
	}
	for _, as := range p.Assignments {
		if as.Role == iam.RoleOwner && as.Scope.Kind == iam.ScopeGlobal {
			return true
		}
	}
	return false
}

// ---- storage ---------------------------------------------------------------

const planColumns = `id, state, expires_at, secret_salt, doc`

type scanner interface{ Scan(dest ...any) error }

func (s *Service) load(ctx context.Context, id string) (*Plan, error) {
	pl, err := s.scan(s.o.DB.QueryRowContext(ctx, `SELECT `+planColumns+` FROM change_plans WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return pl, err
}

func (s *Service) scan(r scanner) (*Plan, error) {
	var id, state, expires, doc string
	var salt []byte
	if err := r.Scan(&id, &state, &expires, &salt, &doc); err != nil {
		return nil, err
	}
	pl := &Plan{}
	if err := json.Unmarshal([]byte(doc), pl); err != nil {
		return nil, fmt.Errorf("plans: plan %s is unreadable: %w", id, err)
	}
	pl.salt = salt
	// The column is what conditional writes compare against; it decides.
	if exp, err := time.Parse(time.RFC3339Nano, expires); err == nil {
		pl.ExpiresAt = exp
	}
	pl.State = State(state)
	pl.storedState = pl.State
	if (pl.State == StateAwaitingApproval || pl.State == StateApproved) && !s.now().Before(pl.ExpiresAt) {
		pl.State = StateExpired
	}
	return pl, nil
}

// transition writes pl (its State and document) if the stored state is
// still from, together with extra columns and the audit row in one
// transaction. A lost race answers ErrState.
func (s *Service) transition(ctx context.Context, pl *Plan, from State, extra string, extraArgs []any, also func(context.Context, *sql.Tx) error) error {
	return s.transitionIf(ctx, pl, from, false, extra, extraArgs, also)
}

// transitionLive is transition that also requires the plan to be before its
// deadline when the row is written, so an approval or execution racing the
// deadline cannot land after it.
func (s *Service) transitionLive(ctx context.Context, pl *Plan, from State, extra string, extraArgs []any, also func(context.Context, *sql.Tx) error) error {
	return s.transitionIf(ctx, pl, from, true, extra, extraArgs, also)
}

func (s *Service) transitionIf(ctx context.Context, pl *Plan, from State, live bool, extra string, extraArgs []any, also func(context.Context, *sql.Tx) error) error {
	return s.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		doc, err := json.Marshal(pl)
		if err != nil {
			return err
		}
		set := `state = ?, updated_at = ?, doc = ?`
		if extra != "" {
			set += ", " + extra
		}
		args := append([]any{string(pl.State), stamp(pl.UpdatedAt), string(doc)}, extraArgs...)
		args = append(args, pl.ID, string(from), pl.Digest)
		where := `id = ? AND state = ? AND digest = ?`
		if live {
			where += ` AND expires_at > ?`
			args = append(args, stamp(s.now()))
		}
		res, err := tx.ExecContext(ctx, `UPDATE change_plans SET `+set+` WHERE `+where, args...)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return fmt.Errorf("%w: the plan changed state meanwhile", ErrState)
		}
		if err := also(ctx, tx); err != nil {
			return err
		}
		pl.storedState = pl.State
		return nil
	})
}

// write runs f in a transaction that outlives the caller's cancellation.
// Nothing inside f may use s.o.DB: the database has a single connection.
func (s *Service) write(ctx context.Context, f func(context.Context, *sql.Tx) error) error {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	tx, err := s.o.DB.BeginTx(wctx, nil)
	if err != nil {
		return err
	}
	if err := f(wctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Service) audit(ctx context.Context, tx *sql.Tx, p *iam.Principal, pl *Plan, action string, src iam.Source, before, after map[string]any) error {
	return s.auditResult(ctx, tx, p, pl, action, iam.ResultOK, src, before, after)
}

func (s *Service) auditResult(ctx context.Context, tx *sql.Tx, p *iam.Principal, pl *Plan, action, result string, src iam.Source, before, after map[string]any) error {
	if s.o.Audit == nil {
		return errors.New("plans: no audit ledger configured")
	}
	sc := iam.Global
	if sco := pl.scopes(); len(sco) == 1 {
		sc = sco[0]
	}
	ev := iam.AuditEvent{Action: action, Scope: sc, ResourceKind: "change-plan", ResourceID: pl.ID, Before: before, After: after,
		Message: pl.Summary, Result: result, TraceID: src.TraceID, SourceIP: src.IP}
	if p != nil {
		ev.ActorID, ev.ActorName = p.User.ID, p.User.Username
	}
	return s.o.Audit.RecordTx(ctx, tx, ev)
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: change_plans.request_id")
}

func actor(p *iam.Principal) Actor {
	if p == nil {
		return Actor{}
	}
	name := p.User.DisplayName
	if name == "" {
		name = p.User.Username
	}
	return Actor{ID: p.User.ID, Name: name}
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "plan_" + hex.EncodeToString(b[:])
}

// stamp is fixed width so text comparison in SQL orders times correctly.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") }

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return stamp(*t)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

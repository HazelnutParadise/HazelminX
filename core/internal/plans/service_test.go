package plans

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
)

var bg = context.Background()

func TestCreateDescribesTargetsChangesAndApproval(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "shop-api:9b3e7f1", "tag": "latest"}})

	if p.State != StateAwaitingApproval || p.Risk != RiskWarning || !p.Executable {
		t.Fatalf("state %s risk %s executable %v", p.State, p.Risk, p.Executable)
	}
	if !strings.HasPrefix(p.Digest, "sha256:") {
		t.Fatalf("digest %q", p.Digest)
	}
	tg := p.Targets[0]
	if tg.Name != "shop compose.yaml" || tg.NodeName != "hazel-tw-01" || tg.Environment != "production" || tg.Version != 7 {
		t.Fatalf("target lacks the names a person checks: %+v", tg)
	}
	if len(p.Changes) != 2 || p.Changes[0].Path != "image" || p.Changes[0].Before != "shop-api:a3f9c1e" || p.Changes[0].After != "shop-api:9b3e7f1" {
		t.Fatalf("changes %+v", p.Changes)
	}
	if len(p.Affected) != 1 || p.Affected[0].Name != "shop compose.yaml-api" {
		t.Fatalf("affected %+v", p.Affected)
	}
	if !p.Approval.Required || p.Approval.OwnerRequired || p.Approval.Action != iam.ActionProjectsWrite {
		t.Fatalf("approval %+v", p.Approval)
	}
	if want := e.clock.now().Add(DefaultTTL); !p.ExpiresAt.Equal(want) {
		t.Fatalf("expires %v, want %v", p.ExpiresAt, want)
	}
	if p.RequestedBy.ID != e.projA.User.ID {
		t.Fatalf("requested by %+v", p.RequestedBy)
	}
	got, err := e.svc.Get(bg, e.projA, p.ID)
	if err != nil || got.Digest != p.Digest || got.Targets[0].NodeName != "hazel-tw-01" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if a := e.audit(p.ID); !slices.Equal(a, []string{"change-plans.create:ok"}) {
		t.Fatalf("audit %v", a)
	}
}

func TestDigestIsStableAndCoversContent(t *testing.T) {
	e := newEnv(t)
	a := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	b := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	if a.Digest != b.Digest {
		t.Fatalf("same content, different digests %s %s", a.Digest, b.Digest)
	}
	c := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "y"}})
	if c.Digest == a.Digest {
		t.Fatal("different content, same digest")
	}
	e.cfg.write("shop-compose", "other", "1")
	d := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	if d.Digest == a.Digest {
		t.Fatal("a new target version must change the digest")
	}
}

func TestApproveBindsDigestAndDeadline(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	got, err := e.svc.Approve(bg, e.admin, p.ID, p.Digest, src)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateApproved || got.Approval.ApprovedBy == nil || got.Approval.ApprovedBy.ID != e.admin.User.ID ||
		got.Approval.Digest != p.Digest || got.Approval.ExpiresAt == nil || !got.Approval.ExpiresAt.Equal(p.ExpiresAt) {
		t.Fatalf("approval %+v state %s", got.Approval, got.State)
	}
	if a := e.audit(p.ID); !slices.Equal(a, []string{"change-plans.create:ok", "change-plans.approve:ok"}) {
		t.Fatalf("audit %v", a)
	}
	// Approving twice is a state error, not a second approval.
	if _, err := e.svc.Approve(bg, e.owner, p.ID, p.Digest, src); !errors.Is(err, ErrState) {
		t.Fatalf("second approval: %v", err)
	}
}

func TestApproveWithAnotherDigestIsRefused(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	_, err := e.svc.Approve(bg, e.admin, p.ID, "sha256:0000", src)
	var ce *ChangedError
	if !errors.As(err, &ce) || ce.SentDigest != "sha256:0000" || ce.PlanDigest != p.Digest || ce.Invalidated {
		t.Fatalf("got %v %+v", err, ce)
	}
	got, _ := e.svc.Get(bg, e.admin, p.ID)
	if got.State != StateAwaitingApproval {
		t.Fatalf("a wrong digest must not change the plan: %s", got.State)
	}
	if _, err := e.svc.Approve(bg, e.admin, p.ID, "", src); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing digest: %v", err)
	}
}

// IAM-005 / ENG-005: the content changed before approval, so the plan that
// was reviewed is not what would run.
func TestTargetChangedBeforeApprovalInvalidatesPlan(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	e.cfg.write("shop-compose", "image", "shop-api:hotfix")

	_, err := e.svc.Approve(bg, e.admin, p.ID, p.Digest, src)
	var ce *ChangedError
	if !errors.As(err, &ce) || !ce.Invalidated {
		t.Fatalf("got %v", err)
	}
	if len(ce.Targets) != 1 || ce.Targets[0].PlannedVersion != 7 || ce.Targets[0].CurrentVersion != 8 || ce.Targets[0].Name != "shop compose.yaml" {
		t.Fatalf("targets %+v", ce.Targets)
	}
	if ce.Planned[0].Before != "shop-api:a3f9c1e" || ce.Current[0].Before != "shop-api:hotfix" {
		t.Fatalf("diff planned %+v current %+v", ce.Planned, ce.Current)
	}
	got, _ := e.svc.Get(bg, e.admin, p.ID)
	if got.State != StateInvalidated || got.Invalidation == nil || got.Approval.ApprovedBy != nil {
		t.Fatalf("plan %s %+v", got.State, got.Invalidation)
	}
	if a := e.audit(p.ID); !slices.Contains(a, "change-plans.invalidate:ok") {
		t.Fatalf("audit %v", a)
	}
	if _, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-1", src); !errors.Is(err, ErrState) {
		t.Fatalf("execute an invalidated plan: %v", err)
	}
	if n := len(e.jobs.submitted()); n != 0 {
		t.Fatalf("%d jobs submitted", n)
	}
}

// T-030: two people change the same config version. The second one's
// approved plan does not overwrite the first; it gets the conflict and diff.
func TestTargetChangedAfterApprovalBlocksExecution(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	if _, err := e.svc.Approve(bg, e.admin, p.ID, p.Digest, src); err != nil {
		t.Fatal(err)
	}
	e.cfg.write("shop-compose", "image", "first-person")

	_, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-1", src)
	var ce *ChangedError
	if !errors.As(err, &ce) || !ce.Invalidated || ce.Targets[0].CurrentVersion != 8 || ce.Current[0].Before != "first-person" {
		t.Fatalf("got %v %+v", err, ce)
	}
	if n := len(e.jobs.submitted()); n != 0 {
		t.Fatalf("the second change ran anyway: %d jobs", n)
	}
	if d, _ := e.cfg.get("shop-compose"); d.Values["image"] != "first-person" {
		t.Fatalf("first person's change lost: %v", d.Values)
	}
	got, _ := e.svc.Get(bg, e.admin, p.ID)
	if got.State != StateInvalidated {
		t.Fatalf("approval still usable: %s", got.State)
	}
}

func TestApproverNeedsPermissionOnEveryTarget(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.owner, configParams{Configs: []string{"shop-compose", "wiki-compose"}, Set: map[string]string{"image": "x"}})
	// The auditor sees both targets but may not change them.
	_, err := e.svc.Approve(bg, e.auditor, p.ID, p.Digest, src)
	var fe *ForbiddenError
	if !errors.As(err, &fe) || fe.Reason != "missing-permission" {
		t.Fatalf("auditor: %v", err)
	}
	got, _ := e.svc.Get(bg, e.owner, p.ID)
	if got.State != StateAwaitingApproval {
		t.Fatalf("state %s", got.State)
	}
	var denied int
	e.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE actor_id = ? AND result = 'denied'`, e.auditor.User.ID).Scan(&denied)
	if denied == 0 {
		t.Fatal("refused approval was not audited")
	}
	// Rejecting needs the same permission.
	if _, err := e.svc.Reject(bg, e.auditor, p.ID, "no", src); !errors.Is(err, ErrForbidden) {
		t.Fatalf("auditor reject: %v", err)
	}
}

// A plan touching a project the caller cannot see is indistinguishable from
// a plan that does not exist.
func TestPlanWithInvisibleTargetIsNotFound(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.owner, configParams{Configs: []string{"shop-compose", "wiki-compose"}, Set: map[string]string{"image": "x"}})
	for name, call := range map[string]func() error{
		"get":      func() error { _, err := e.svc.Get(bg, e.projA, p.ID); return err },
		"approve":  func() error { _, err := e.svc.Approve(bg, e.projA, p.ID, p.Digest, src); return err },
		"reject":   func() error { _, err := e.svc.Reject(bg, e.projA, p.ID, "x", src); return err },
		"withdraw": func() error { _, err := e.svc.Withdraw(bg, e.projA, p.ID, "x", src); return err },
		"execute":  func() error { _, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "r", src); return err },
		"missing":  func() error { _, err := e.svc.Get(bg, e.projA, "plan_nope"); return err },
	} {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Creating a plan on a project the caller cannot see is also 404.
	if _, err := e.svc.Create(bg, e.projA, "config.update", e.params(configParams{Configs: []string{"wiki-compose"}, Set: map[string]string{"a": "b"}}), src); !errors.Is(err, ErrNotFound) {
		t.Fatalf("create on invisible target: %v", err)
	}
	// The project admin of shop alone may approve a shop-only plan.
	q := e.create(e.owner, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	if _, err := e.svc.Approve(bg, e.projA, q.ID, q.Digest, src); err != nil {
		t.Fatalf("scoped approver: %v", err)
	}
}

func TestHighRiskNeedsOwner(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"mount": "/"}})
	if p.Risk != RiskHigh || !p.Approval.OwnerRequired {
		t.Fatalf("risk %s approval %+v", p.Risk, p.Approval)
	}
	var fe *ForbiddenError
	if _, err := e.svc.Approve(bg, e.admin, p.ID, p.Digest, src); !errors.As(err, &fe) || fe.Reason != "owner-required" {
		t.Fatalf("platform admin approved a high-risk plan: %v", err)
	}
	if _, err := e.svc.Approve(bg, e.projA, p.ID, p.Digest, src); !errors.As(err, &fe) || fe.Reason != "owner-required" {
		t.Fatalf("requester approved own high-risk plan: %v", err)
	}
	if _, err := e.svc.Approve(bg, e.owner, p.ID, p.Digest, src); err != nil {
		t.Fatalf("owner: %v", err)
	}
}

func TestExpiredPlanCannotBeApprovedOrRun(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	q := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "y"}})
	if _, err := e.svc.Approve(bg, e.admin, q.ID, q.Digest, src); err != nil {
		t.Fatal(err)
	}
	e.clock.add(DefaultTTL)
	if _, err := e.svc.Approve(bg, e.admin, p.ID, p.Digest, src); !errors.Is(err, ErrExpired) {
		t.Fatalf("approve expired: %v", err)
	}
	if _, err := e.svc.Execute(bg, e.projA, q.ID, q.Digest, "req-q", src); !errors.Is(err, ErrExpired) {
		t.Fatalf("execute after the approval's deadline: %v", err)
	}
	for _, id := range []string{p.ID, q.ID} {
		got, _ := e.svc.Get(bg, e.projA, id)
		if got.State != StateExpired {
			t.Fatalf("%s: state %s", id, got.State)
		}
	}
	waiting, err := e.svc.List(bg, e.projA, ListOptions{States: []State{StateAwaitingApproval, StateApproved}})
	if err != nil || len(waiting) != 0 {
		t.Fatalf("expired plans still listed as waiting: %v %d", err, len(waiting))
	}
	if n := len(e.jobs.submitted()); n != 0 {
		t.Fatalf("%d jobs", n)
	}
}

func TestExecuteSubmitsJobOnceWithPlanFields(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	if _, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-1", src); !errors.Is(err, ErrState) {
		t.Fatalf("execute before approval: %v", err)
	}
	if _, err := e.svc.Approve(bg, e.admin, p.ID, p.Digest, src); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "", src); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing request id: %v", err)
	}
	if _, err := e.svc.Execute(bg, e.auditor, p.ID, p.Digest, "req-1", src); !errors.Is(err, ErrForbidden) {
		t.Fatalf("auditor executed: %v", err)
	}
	res, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-1", src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Job == nil || res.Replayed || res.Plan.State != StateExecuted || res.Plan.Execution.JobID != res.Job.ID {
		t.Fatalf("result %+v", res)
	}
	reqs := e.jobs.submitted()
	if len(reqs) != 1 {
		t.Fatalf("%d submissions", len(reqs))
	}
	r := reqs[0]
	if r.Kind != "config.apply" || r.RequestID != "req-1" || r.Requester != e.projA.User.ID || r.Approver != e.admin.User.ID ||
		r.ChangePlanID != p.ID || r.ChangePlanDigest != p.Digest || !strings.Contains(string(r.Params), `"expectedVersions":{"shop-compose":7}`) {
		t.Fatalf("submission %+v params %s", r, r.Params)
	}
	if !slices.Contains(r.RequiredPermissions, string(iam.ActionProjectsWrite)) {
		t.Fatalf("permissions %v", r.RequiredPermissions)
	}
	// Sending execute again with the same request id answers the same job.
	again, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-1", src)
	if err != nil || !again.Replayed || again.Job.ID != res.Job.ID || len(e.jobs.submitted()) != 1 {
		t.Fatalf("replay: %v %+v", err, again)
	}
	if _, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-2", src); !errors.Is(err, ErrRequestIDTaken) {
		t.Fatalf("second request id: %v", err)
	}
	if a := e.audit(p.ID); !slices.Contains(a, "change-plans.execute:ok") {
		t.Fatalf("audit %v", a)
	}
}

func TestConcurrentApprovalsOnlyOneWins(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			who := e.admin
			if i%2 == 0 {
				who = e.owner
			}
			_, errs[i] = e.svc.Approve(bg, who, p.ID, p.Digest, src)
		}()
	}
	wg.Wait()
	var ok int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrState):
			t.Errorf("unexpected: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d approvals succeeded", ok)
	}
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE resource_id = ? AND action = 'change-plans.approve' AND result = 'ok'`, p.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("%d approval audit rows", n)
	}
}

// The audit row and the state change commit together: when the audit row
// cannot be written, the approval did not happen.
func TestAuditInSameTransaction(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	if _, err := e.db.Exec(`CREATE TRIGGER no_audit BEFORE INSERT ON audit_events WHEN NEW.action LIKE 'change-plans.%' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(bg, e.admin, p.ID, p.Digest, src); err == nil {
		t.Fatal("approved without an audit row")
	}
	got, _ := e.svc.Get(bg, e.admin, p.ID)
	if got.State != StateAwaitingApproval {
		t.Fatalf("state %s", got.State)
	}
	// A request cancelled after the decision still commits it with its row:
	// the write does not use the caller's context.
	if _, err := e.db.Exec(`DROP TRIGGER no_audit`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := e.svc.Approve(ctx, e.admin, p.ID, p.Digest, src); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
	got, _ = e.svc.Get(bg, e.admin, p.ID)
	audited := slices.Contains(e.audit(p.ID), "change-plans.approve:ok")
	if (got.State == StateApproved) != audited {
		t.Fatalf("state %s but audited=%v", got.State, audited)
	}
}

func TestLowRiskPlanIsAutoApproved(t *testing.T) {
	e := newEnv(t)
	e.kind.require = false
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"replicas": "2"}})
	if p.Risk != RiskLow || p.Approval.Required || !p.Approval.AutoApproved || p.State != StateApproved || p.Approval.Digest != p.Digest {
		t.Fatalf("plan %s risk %s approval %+v", p.State, p.Risk, p.Approval)
	}
	res, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-low", src)
	if err != nil || res.Job == nil {
		t.Fatalf("execute: %v", err)
	}
	if r := e.jobs.submitted()[0]; r.Approver != AutoApprover {
		t.Fatalf("auto approval approver: %q", r.Approver)
	}
	// A kind that always wants approval does not auto-approve.
	e.kind.require = true
	q := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"replicas": "3"}})
	if q.State != StateAwaitingApproval || !q.Approval.Required {
		t.Fatalf("required approval: %s %+v", q.State, q.Approval)
	}
}

func TestFailedPrecheckBlocksApproval(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"fail": "yes"}})
	if p.Executable || p.State != StateAwaitingApproval {
		t.Fatalf("executable %v state %s", p.Executable, p.State)
	}
	if _, err := e.svc.Approve(bg, e.owner, p.ID, p.Digest, src); !errors.Is(err, ErrPrecheck) {
		t.Fatalf("approve: %v", err)
	}
}

func TestWithdrawOnlyByRequesterAndReject(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	var fe *ForbiddenError
	if _, err := e.svc.Withdraw(bg, e.admin, p.ID, "not mine", src); !errors.As(err, &fe) || fe.Reason != "not-requester" {
		t.Fatalf("withdraw by another: %v", err)
	}
	got, err := e.svc.Withdraw(bg, e.projA, p.ID, "typo", src)
	if err != nil || got.State != StateWithdrawn || got.Decision == nil || got.Decision.Reason != "typo" {
		t.Fatalf("withdraw: %v %+v", err, got)
	}
	q := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "y"}})
	if _, err := e.svc.Reject(bg, e.admin, q.ID, "", src); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reject without reason: %v", err)
	}
	got, err = e.svc.Reject(bg, e.admin, q.ID, "wrong window", src)
	if err != nil || got.State != StateRejected || got.Decision.By.ID != e.admin.User.ID {
		t.Fatalf("reject: %v %+v", err, got)
	}
	if _, err := e.svc.Approve(bg, e.owner, q.ID, q.Digest, src); !errors.Is(err, ErrState) {
		t.Fatalf("approve rejected: %v", err)
	}
	if a := e.audit(q.ID); !slices.Equal(a, []string{"change-plans.create:ok", "change-plans.reject:ok"}) {
		t.Fatalf("audit %v", a)
	}
}

func TestSecretChangesAreMaskedButDigested(t *testing.T) {
	e := newEnv(t)
	a := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}, Secrets: map[string]string{"STRIPE_KEY": "ref:new1"}})
	b := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}, Secrets: map[string]string{"STRIPE_KEY": "ref:new2"}})
	var sec *Change
	for i := range a.Changes {
		if a.Changes[i].Secret {
			sec = &a.Changes[i]
		}
	}
	if sec == nil || sec.Before != nil || sec.After != nil || sec.Path != "STRIPE_KEY" {
		t.Fatalf("secret change %+v", sec)
	}
	var doc string
	e.db.QueryRow(`SELECT doc FROM change_plans WHERE id = ?`, a.ID).Scan(&doc)
	if strings.Contains(doc, "resolved-") || strings.Contains(doc, "sk_old") {
		t.Fatal("secret value stored in the plan")
	}
	if a.Digest == b.Digest {
		t.Fatal("changing only a secret must change the digest")
	}
	// The secret value changing after approval invalidates it too.
	if _, err := e.svc.Approve(bg, e.admin, a.ID, a.Digest, src); err != nil {
		t.Fatal(err)
	}
	e.cfg.mu.Lock()
	e.cfg.docs["shop-compose"].Secrets["STRIPE_KEY"] = "sk_rotated"
	e.cfg.mu.Unlock()
	if _, err := e.svc.Execute(bg, e.projA, a.ID, a.Digest, "req-s", src); !errors.Is(err, ErrChanged) {
		t.Fatalf("secret rotated underneath: %v", err)
	}
}

func TestListShowsOnlyFullyVisiblePlans(t *testing.T) {
	e := newEnv(t)
	shop := e.create(e.owner, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	both := e.create(e.owner, configParams{Configs: []string{"shop-compose", "wiki-compose"}, Set: map[string]string{"image": "x"}})
	wiki := e.create(e.owner, configParams{Configs: []string{"wiki-compose"}, Set: map[string]string{"image": "x"}})
	ids := func(p *iam.Principal, o ListOptions) []string {
		t.Helper()
		ps, err := e.svc.List(bg, p, o)
		if err != nil {
			t.Fatal(err)
		}
		n, err := e.svc.Count(bg, p, o)
		if err != nil || n != len(ps) {
			t.Fatalf("count %d %v for %d rows", n, err, len(ps))
		}
		var out []string
		for _, x := range ps {
			out = append(out, x.ID)
		}
		slices.Sort(out)
		return out
	}
	sorted := func(s ...string) []string { slices.Sort(s); return s }
	if got := ids(e.projA, ListOptions{}); !slices.Equal(got, []string{shop.ID}) {
		t.Fatalf("project admin of shop sees %v", got)
	}
	if got := ids(e.projB, ListOptions{}); !slices.Equal(got, []string{wiki.ID}) {
		t.Fatalf("project admin of wiki sees %v", got)
	}
	if got := ids(e.auditor, ListOptions{States: []State{StateAwaitingApproval}}); !slices.Equal(got, sorted(shop.ID, both.ID, wiki.ID)) {
		t.Fatalf("auditor sees %v", got)
	}
	if got := ids(e.owner, ListOptions{RequestedBy: e.projA.User.ID}); len(got) != 0 {
		t.Fatalf("requester filter %v", got)
	}
	_ = time.Second
}

func TestUnknownKindAndBadParams(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Create(bg, e.owner, "nope", []byte(`{}`), src); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := e.svc.Create(bg, e.owner, "config.update", []byte(`{}`), src); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad params: %v", err)
	}
	if _, err := e.svc.Create(bg, e.owner, "config.update", e.params(configParams{Configs: []string{"gone"}}), src); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing target: %v", err)
	}
	if _, err := e.svc.Create(bg, e.auditor, "config.update", e.params(configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"a": "b"}}), src); !errors.Is(err, ErrForbidden) {
		t.Fatalf("auditor creates: %v", err)
	}
	if !e.svc.RequiresPlan("config.apply") || e.svc.RequiresPlan("list.query") {
		t.Fatal("RequiresPlan")
	}
}

// A request id already used by another job is refused before the plan is
// marked executed, so the plan stays approved and can run with another id.
func TestExecuteRefusesTakenRequestIDBeforeClaiming(t *testing.T) {
	e := newEnv(t)
	other := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "o"}})
	e.svc.Approve(bg, e.admin, other.ID, other.Digest, src)
	if _, err := e.svc.Execute(bg, e.projA, other.ID, other.Digest, "req-shared", src); err != nil {
		t.Fatal(err)
	}
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	e.svc.Approve(bg, e.admin, p.ID, p.Digest, src)
	if _, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-shared", src); !errors.Is(err, ErrRequestIDTaken) {
		t.Fatalf("taken request id: %v", err)
	}
	if got, _ := e.svc.Get(bg, e.projA, p.ID); got.State != StateApproved {
		t.Fatalf("plan consumed by a refused execution: %s", got.State)
	}
	if res, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-own", src); err != nil || res.Job == nil {
		t.Fatalf("execute with a free id: %v", err)
	}
}

// When creating the job fails after the plan was claimed, the claim is
// undone with an audit row and the same request id can be sent again.
func TestExecuteRestoresApprovalWhenSubmitFails(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	e.svc.Approve(bg, e.admin, p.ID, p.Digest, src)
	e.jobs.failNext = errors.New("disk full")
	if _, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-1", src); err == nil {
		t.Fatal("submit failure was not reported")
	}
	got, _ := e.svc.Get(bg, e.projA, p.ID)
	if got.State != StateApproved || got.Execution != nil {
		t.Fatalf("plan after a failed submit: %s %+v", got.State, got.Execution)
	}
	if a := e.audit(p.ID); !slices.Contains(a, "change-plans.execute:failed") {
		t.Fatalf("audit %v", a)
	}
	if res, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-1", src); err != nil || res.Job == nil {
		t.Fatalf("retry: %v", err)
	}
}

// Two executes with the same request id at once both answer the one job.
func TestConcurrentExecuteWithSameRequestIDReplays(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	e.svc.Approve(bg, e.admin, p.ID, p.Digest, src)
	var wg sync.WaitGroup
	res := make([]*ExecuteResult, 6)
	errs := make([]error, 6)
	for i := range res {
		wg.Add(1)
		go func() { defer wg.Done(); res[i], errs[i] = e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-same", src) }()
	}
	wg.Wait()
	for i := range res {
		if errs[i] != nil || res[i].Job == nil || res[i].Job.ID != res[0].Job.ID {
			t.Fatalf("execute %d: %v", i, errs[i])
		}
	}
	if n := len(e.jobs.submitted()); n != 1 {
		t.Fatalf("%d jobs", n)
	}
}

// A plan whose kind is no longer registered still answers 404 to someone
// who cannot see it, not an error that reveals it exists.
func TestUnregisteredKindDoesNotRevealHiddenPlan(t *testing.T) {
	e := newEnv(t)
	p := e.create(e.owner, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"image": "x"}})
	bare := New(Options{DB: e.db, Audit: e.iam, Jobs: e.jobs, Now: e.clock.now})
	for name, call := range map[string]func() error{
		"approve": func() error { _, err := bare.Approve(bg, e.projB, p.ID, p.Digest, src); return err },
		"reject":  func() error { _, err := bare.Reject(bg, e.projB, p.ID, "x", src); return err },
		"execute": func() error { _, err := bare.Execute(bg, e.projB, p.ID, p.Digest, "r", src); return err },
	} {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAutoApprovedJobSaysSo(t *testing.T) {
	e := newEnv(t)
	e.kind.require = false
	p := e.create(e.projA, configParams{Configs: []string{"shop-compose"}, Set: map[string]string{"replicas": "2"}})
	if _, err := e.svc.Execute(bg, e.projA, p.ID, p.Digest, "req-auto", src); err != nil {
		t.Fatal(err)
	}
	if r := e.jobs.submitted()[0]; r.Approver != AutoApprover {
		t.Fatalf("approver %q", r.Approver)
	}
}

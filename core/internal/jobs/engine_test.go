package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// fakeHandler is the in-process executor the test seam in ENG.md asks for:
// every hook can inject a disconnect, a disk-full error or a rollback failure.
type fakeHandler struct {
	kind      string
	steps     []StepSpec
	recheck   func(step *Step) error
	execute   func(ctx context.Context, job *Job, step *Step, rep Reporter) error
	rollback  func(step *Step) error
	reconcile func(step *Step) (StepReport, error)
	verify    func(job *Job) (CompletionReport, error)

	mu         sync.Mutex
	executed   []int
	rolledBack []int
}

func (f *fakeHandler) Kind() string { return f.kind }
func (f *fakeHandler) Plan(context.Context, *Job) ([]StepSpec, error) {
	return f.steps, nil
}
func (f *fakeHandler) Recheck(_ context.Context, _ *Job, s *Step) error {
	if f.recheck != nil {
		return f.recheck(s)
	}
	return nil
}
func (f *fakeHandler) Execute(ctx context.Context, j *Job, s *Step, rep Reporter) error {
	f.mu.Lock()
	f.executed = append(f.executed, s.Index)
	f.mu.Unlock()
	if f.execute != nil {
		return f.execute(ctx, j, s, rep)
	}
	rep.Evidence("evidence://" + s.Name)
	return nil
}
func (f *fakeHandler) Rollback(_ context.Context, _ *Job, s *Step, _ Reporter) error {
	f.mu.Lock()
	f.rolledBack = append(f.rolledBack, s.Index)
	f.mu.Unlock()
	if f.rollback != nil {
		return f.rollback(s)
	}
	return nil
}
func (f *fakeHandler) Reconcile(_ context.Context, _ *Job, s *Step) (StepReport, error) {
	if f.reconcile != nil {
		return f.reconcile(s)
	}
	return StepReport{Index: s.Index}, nil
}
func (f *fakeHandler) Verify(_ context.Context, j *Job) (CompletionReport, error) {
	if f.verify != nil {
		return f.verify(j)
	}
	return CompletionReport{Checks: []Check{{Name: "default", Required: true, Passed: true}}}, nil
}
func (f *fakeHandler) executedSteps() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.executed...)
}

// waitOn blocks until ch is closed or ctx ends, so a failing test never
// leaves a driver goroutine stuck in Stop().
func waitOn(ctx context.Context, ch chan struct{}) {
	select {
	case <-ch:
	case <-ctx.Done():
	}
}

func simpleSteps(n int) []StepSpec {
	var out []StepSpec
	for i := 0; i < n; i++ {
		out = append(out, StepSpec{Name: fmt.Sprintf("step-%d", i), RollbackMethod: "undo", Cancellable: true})
	}
	return out
}

type testEnv struct {
	t      *testing.T
	ctx    context.Context
	cancel context.CancelFunc
	st     *store.Store
	engine *Engine
	path   string
}

func newEnv(t *testing.T, opts Options, handlers ...Handler) *testEnv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "core.db")
	return openEnv(t, path, opts, handlers...)
}

func openEnv(t *testing.T, path string, opts Options, handlers ...Handler) *testEnv {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 10 * time.Millisecond
	}
	e := New(NewSQLStore(st.DB), opts)
	for _, h := range handlers {
		if err := e.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	e.Start(ctx)
	env := &testEnv{t: t, ctx: ctx, cancel: cancel, st: st, engine: e, path: path}
	t.Cleanup(env.close)
	return env
}

func (env *testEnv) close() {
	env.cancel()
	env.engine.Stop()
	env.st.Close()
}

func (env *testEnv) submit(req SubmitRequest) *Job {
	env.t.Helper()
	j, _, err := env.engine.Submit(env.ctx, req)
	if err != nil {
		env.t.Fatalf("submit: %v", err)
	}
	return j
}

func (env *testEnv) waitState(id string, states ...State) *Job {
	env.t.Helper()
	ctx, cancel := context.WithTimeout(env.ctx, 5*time.Second)
	defer cancel()
	j, err := env.engine.Wait(ctx, id, func(j *Job) bool {
		for _, s := range states {
			if j.State == s {
				return true
			}
		}
		return false
	})
	if err != nil {
		cur, _ := env.engine.Get(env.ctx, id)
		env.t.Fatalf("waiting for %v: %v (current %+v)", states, err, cur)
	}
	return j
}

func (env *testEnv) waitFinished(id string) *Job {
	env.t.Helper()
	return env.waitState(id, StateSucceeded, StateFailed, StateRolledBack, StateCancelled, StateUnknown, StateNeedsAttention)
}

// ---- ENG-001 / T-027: the job outlives the request and its record is re-readable.

func TestJobSurvivesCallerAndIsReReadable(t *testing.T) {
	release := make(chan struct{})
	h := &fakeHandler{kind: "install", steps: simpleSteps(2), execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
		rep.Log("installing " + s.Name)
		rep.Progress(50 * (s.Index + 1))
		rep.Evidence("evidence://" + s.Name)
		if s.Index == 0 {
			waitOn(ctx, release)
		}
		return nil
	}}
	env := newEnv(t, Options{}, h)
	reqCtx, cancelReq := context.WithCancel(env.ctx)
	j, dedup, err := env.engine.Submit(reqCtx, SubmitRequest{RequestID: "r1", Kind: "install", Params: json.RawMessage(`{"pkg":"nginx"}`)})
	if err != nil || dedup {
		t.Fatalf("submit: %v dedup=%v", err, dedup)
	}
	cancelReq() // the browser closed the page
	wctx, wcancel := context.WithTimeout(env.ctx, 5*time.Second)
	defer wcancel()
	running, err := env.engine.Wait(wctx, j.ID, func(j *Job) bool {
		return j.State == StateRunning && j.Steps[0].Status == StepRunning && j.Progress == 50
	})
	if err != nil {
		t.Fatalf("step 0 never ran: %v (%+v)", err, running)
	}
	if running.Steps[0].StartedAt == nil || running.LastHeartbeatAt == nil {
		t.Fatalf("running record must show step start and heartbeat: %+v", running)
	}
	close(release)
	done := env.waitFinished(j.ID)
	if done.State != StateSucceeded {
		t.Fatalf("state %s reason %q", done.State, done.StateReason)
	}
	if done.Progress != 100 || len(done.Steps[1].Evidence) == 0 || done.Completion == nil {
		t.Fatalf("progress/evidence/completion missing: %+v", done)
	}
	// A reconnecting UI resumes from the seq it last saw.
	all, err := env.engine.Events(env.ctx, j.ID, 0, 100)
	if err != nil || len(all) < 4 {
		t.Fatalf("events: %v %d", err, len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].Seq != all[i-1].Seq+1 {
			t.Fatalf("event seqs must be contiguous: %d then %d", all[i-1].Seq, all[i].Seq)
		}
	}
	tail, _ := env.engine.Events(env.ctx, j.ID, all[1].Seq, 100)
	if len(tail) != len(all)-2 || tail[0].Seq != all[2].Seq {
		t.Fatalf("resume after seq %d returned %d events", all[1].Seq, len(tail))
	}
	// After a Core restart the record is still there, untouched.
	env.close()
	env2 := openEnv(t, env.path, Options{}, h)
	again, err := env2.engine.Get(env2.ctx, j.ID)
	if err != nil || again.State != StateSucceeded || len(again.Steps) != 2 {
		t.Fatalf("after restart: %v %+v", err, again)
	}
}

// ---- ENG-002 / T-025 / T-026: idempotency key.

func TestDuplicateRequestReturnsSameJobAndChangedContentConflicts(t *testing.T) {
	block := make(chan struct{})
	h := &fakeHandler{kind: "install", steps: simpleSteps(1), execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error { waitOn(ctx, block); return nil }}
	env := newEnv(t, Options{}, h)
	first := env.submit(SubmitRequest{RequestID: "req-A", Kind: "install", Params: json.RawMessage(`{"target":"disk-1","n": 1}`)})
	// Same content, different key order and whitespace: still the same request.
	again, dedup, err := env.engine.Submit(env.ctx, SubmitRequest{RequestID: "req-A", Kind: "install", Params: json.RawMessage(`{ "n":1, "target":"disk-1" }`)})
	if err != nil || !dedup || again.ID != first.ID {
		t.Fatalf("replay: err=%v dedup=%v id=%s want %s", err, dedup, again.ID, first.ID)
	}
	// Same key, another disk target: conflict, the original is not reused.
	_, _, err = env.engine.Submit(env.ctx, SubmitRequest{RequestID: "req-A", Kind: "install", Params: json.RawMessage(`{"target":"disk-2","n":1}`)})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	list, _ := env.engine.List(env.ctx, ListOptions{})
	if len(list) != 1 {
		t.Fatalf("exactly one job must exist, got %d", len(list))
	}
	close(block)
	env.waitFinished(first.ID)
}

func TestSubmitRejectsUnknownKindAndMissingRequestID(t *testing.T) {
	env := newEnv(t, Options{}, &fakeHandler{kind: "install", steps: simpleSteps(1)})
	if _, _, err := env.engine.Submit(env.ctx, SubmitRequest{RequestID: "x", Kind: "nope"}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("want ErrUnknownKind, got %v", err)
	}
	if _, _, err := env.engine.Submit(env.ctx, SubmitRequest{Kind: "install"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

// ---- ENG-004: conflicting resources are serialised.

func TestConflictingJobsAreSerialised(t *testing.T) {
	release := make(chan struct{})
	format := &fakeHandler{kind: "disk.format", steps: []StepSpec{{Name: "mkfs", Irreversible: true}}, execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error { waitOn(ctx, release); return nil }}
	move := &fakeHandler{kind: "data.move", steps: simpleSteps(1)}
	env := newEnv(t, Options{}, format, move)
	disk := ResourceRef{Kind: "disk", Device: "ata-WDC-123"}
	f := env.submit(SubmitRequest{RequestID: "f", Kind: "disk.format", Resources: []ResourceRef{disk}})
	env.waitState(f.ID, StateRunning)
	m := env.submit(SubmitRequest{RequestID: "m", Kind: "data.move", Resources: []ResourceRef{{Kind: "path", Device: "ata-WDC-123", Mount: "/data", Path: "/data/mysql"}}})
	queued := env.waitState(m.ID, StateQueued)
	time.Sleep(50 * time.Millisecond)
	queued, _ = env.engine.Get(env.ctx, m.ID)
	if queued.State != StateQueued || !strings.Contains(queued.StateReason, f.ID) {
		t.Fatalf("move must wait on the format job: %s %q", queued.State, queued.StateReason)
	}
	if len(move.executedSteps()) != 0 {
		t.Fatal("move must not have started")
	}
	// An unrelated database job runs meanwhile.
	other := env.submit(SubmitRequest{RequestID: "o", Kind: "data.move", Resources: []ResourceRef{{Kind: "database", ID: "db-9"}}})
	if env.waitFinished(other.ID).State != StateSucceeded {
		t.Fatal("unrelated job must not be blocked")
	}
	close(release)
	env.waitFinished(f.ID)
	if env.waitFinished(m.ID).State != StateSucceeded {
		t.Fatal("move must run once the format finished")
	}
}

// ---- ENG-006 / T-031: re-check before executing.

func TestRecheckBlocksWhenEnvironmentChanged(t *testing.T) {
	h := &fakeHandler{kind: "web.enable", steps: []StepSpec{{Name: "bind-port", RollbackMethod: "unbind"}},
		recheck: func(s *Step) error {
			return &StepError{Code: "PORT_IN_USE", Summary: "port 8080 is now used by pid 4242 (nginx), which was free at precheck",
				Evidence: []string{"ss://tcp/8080"}, SuggestedActions: []string{"choose-another-port", "stop-the-other-service-yourself"}}
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "p", Kind: "web.enable", Resources: []ResourceRef{{Kind: "port", ID: "tcp/8080"}}})
	done := env.waitFinished(j.ID)
	if done.State != StateFailed || done.ErrorCode != "PORT_IN_USE" {
		t.Fatalf("state %s code %s", done.State, done.ErrorCode)
	}
	if len(h.executedSteps()) != 0 {
		t.Fatal("step must not execute after a blocked re-check")
	}
	if done.Diagnostics == nil || !strings.Contains(done.Diagnostics.ErrorSummary, "pid 4242") {
		t.Fatalf("diagnostics must carry the re-check finding: %+v", done.Diagnostics)
	}
	for _, a := range done.Diagnostics.SuggestedActions {
		if strings.Contains(a, "kill") {
			t.Fatalf("must not suggest killing the other process: %v", done.Diagnostics.SuggestedActions)
		}
	}
	if done.Steps[0].Status != StepFailed || done.Steps[0].Recheck == "" {
		t.Fatalf("step must record the re-check outcome: %+v", done.Steps[0])
	}
}

// ---- ENG-007 / T-029: cancellation semantics.

func TestCancelStopsCancellableStep(t *testing.T) {
	started := make(chan struct{})
	h := &fakeHandler{kind: "backup", steps: simpleSteps(3), execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
		if s.Index == 1 {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "c", Kind: "backup"})
	<-started
	res, err := env.engine.Cancel(env.ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateCancelRequested && res.State != StateCancelled {
		t.Fatalf("state %s", res.State)
	}
	done := env.waitFinished(j.ID)
	if done.State != StateCancelled {
		t.Fatalf("state %s reason %q", done.State, done.StateReason)
	}
	if done.Steps[0].Status != StepDone || done.Steps[1].Status == StepDone || done.Steps[2].Status != StepPending {
		t.Fatalf("steps after cancel: %+v", done.Steps)
	}
	if got := h.executedSteps(); len(got) != 2 {
		t.Fatalf("step 2 must not run after cancel: %v", got)
	}
	if done.Diagnostics == nil || len(done.Diagnostics.Residue) == 0 {
		t.Fatalf("cancelled job must list completed/unreverted parts: %+v", done.Diagnostics)
	}
	// A finished job cannot be cancelled.
	if _, err := env.engine.Cancel(env.ctx, j.ID); !errors.Is(err, ErrNotCancellable) {
		t.Fatalf("want ErrNotCancellable, got %v", err)
	}
}

func TestCancelWaitsForIrreversibleStep(t *testing.T) {
	inStep := make(chan struct{})
	release := make(chan struct{})
	h := &fakeHandler{kind: "disk.format", steps: []StepSpec{
		{Name: "unmount", RollbackMethod: "mount", Cancellable: true},
		{Name: "mkfs", Irreversible: true},
		{Name: "mount", RollbackMethod: "unmount", Cancellable: true},
	}, execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
		if s.Index == 1 {
			close(inStep)
			waitOn(ctx, release)
			if ctx.Err() != nil {
				t.Error("irreversible step must not get a cancelled context")
			}
		}
		return nil
	}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "fmt", Kind: "disk.format", Resources: []ResourceRef{{Kind: "disk", Device: "d1"}}})
	<-inStep
	res, err := env.engine.Cancel(env.ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateCancelRequested || res.Cancellable || res.Steps[1].WaitReason == "" {
		t.Fatalf("cancel of an irreversible step must wait with a reason: state=%s cancellable=%v reason=%q", res.State, res.Cancellable, res.Steps[1].WaitReason)
	}
	// Still waiting: the state must not pretend to be stopped.
	time.Sleep(30 * time.Millisecond)
	cur, _ := env.engine.Get(env.ctx, j.ID)
	if cur.State != StateCancelRequested {
		t.Fatalf("state %s while step still running", cur.State)
	}
	close(release)
	done := env.waitFinished(j.ID)
	if done.State != StateCancelled {
		t.Fatalf("state %s", done.State)
	}
	if done.Steps[1].Status != StepDone || done.Steps[2].Status != StepPending {
		t.Fatalf("mkfs finished, mount never started: %+v", done.Steps)
	}
	if done.Diagnostics == nil || len(done.Diagnostics.Residue) == 0 {
		t.Fatal("cancelled job must say what was done and not reverted")
	}
}

func TestCancelDuringLastStepEndsCancelledAndReleasesLocks(t *testing.T) {
	inStep := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "only", Irreversible: true}}, execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
		if j.RequestID == "a" {
			once.Do(func() { close(inStep) })
			waitOn(ctx, release)
		}
		return nil
	}}
	env := newEnv(t, Options{}, h)
	r := ResourceRef{Kind: "database", ID: "db"}
	j := env.submit(SubmitRequest{RequestID: "a", Kind: "k", Resources: []ResourceRef{r}})
	<-inStep
	if _, err := env.engine.Cancel(env.ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	done := env.waitFinished(j.ID)
	if done.State != StateCancelled || done.Steps[0].Status != StepDone || done.Completion != nil {
		t.Fatalf("state %s steps %+v completion %v", done.State, done.Steps, done.Completion)
	}
	if !strings.Contains(done.StateReason, "not verified") {
		t.Fatalf("must say completion was not verified: %q", done.StateReason)
	}
	next := env.submit(SubmitRequest{RequestID: "b", Kind: "k", Resources: []ResourceRef{r}})
	if env.waitFinished(next.ID).State != StateSucceeded {
		t.Fatal("lock must be released after the cancelled job")
	}
}

func TestReconcileAfterRollbackDisconnectResumesRollback(t *testing.T) {
	var mu sync.Mutex
	rbCalls := 0
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "a", RollbackMethod: "undo-a"}, {Name: "b", RollbackMethod: "undo-b"}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			if s.Index == 1 {
				return &StepError{Code: "DISK_FULL", Summary: "no space"}
			}
			return nil
		},
		rollback: func(s *Step) error {
			mu.Lock()
			defer mu.Unlock()
			rbCalls++
			if rbCalls == 1 {
				return ErrDisconnected
			}
			return nil
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "r", Kind: "k"})
	un := env.waitState(j.ID, StateUnknown)
	if un.PriorState != StateRollingBack {
		t.Fatalf("prior state %s", un.PriorState)
	}
	rec, err := env.engine.Reconcile(env.ctx, j.ID, nil)
	if err != nil || rec.State != StateRollingBack {
		t.Fatalf("err=%v state=%s", err, rec.State)
	}
	done := env.waitFinished(j.ID)
	if done.State != StateRolledBack || done.Steps[0].Status != StepRolledBack || done.Steps[1].Status != StepRolledBack {
		t.Fatalf("state %s steps %+v", done.State, done.Steps)
	}
	if len(h.executedSteps()) != 2 {
		t.Fatal("reconcile must not re-run steps")
	}
}

func TestReconcileNeedsAttentionReleasesLocksAndBudget(t *testing.T) {
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "s", Irreversible: true}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error { return ErrDisconnected }}
	env := newEnv(t, Options{Budgets: map[string]Budget{"k": {MaxConcurrent: 1}}}, h)
	r := ResourceRef{Kind: "database", ID: "db"}
	j := env.submit(SubmitRequest{RequestID: "a", Kind: "k", Resources: []ResourceRef{r}})
	env.waitState(j.ID, StateUnknown)
	if _, err := env.engine.Reconcile(env.ctx, j.ID, []StepReport{{Index: 0, Started: true}}); err != nil {
		t.Fatal(err)
	}
	h.execute = nil
	next := env.submit(SubmitRequest{RequestID: "b", Kind: "k", Resources: []ResourceRef{r}})
	if env.waitFinished(next.ID).State != StateSucceeded {
		t.Fatal("lock and budget must be released once the job needs attention")
	}
}

func TestCancelQueuedJobIsImmediate(t *testing.T) {
	block := make(chan struct{})
	h := &fakeHandler{kind: "k", steps: simpleSteps(1), execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error { waitOn(ctx, block); return nil }}
	env := newEnv(t, Options{}, h)
	r := ResourceRef{Kind: "database", ID: "db"}
	a := env.submit(SubmitRequest{RequestID: "a", Kind: "k", Resources: []ResourceRef{r}})
	env.waitState(a.ID, StateRunning)
	b := env.submit(SubmitRequest{RequestID: "b", Kind: "k", Resources: []ResourceRef{r}})
	env.waitState(b.ID, StateQueued)
	res, err := env.engine.Cancel(env.ctx, b.ID)
	if err != nil || res.State != StateCancelled {
		t.Fatalf("queued cancel: %v %s", err, res.State)
	}
	close(block)
	env.waitFinished(a.ID)
	if len(h.executedSteps()) != 1 {
		t.Fatal("cancelled queued job must never execute")
	}
}

// ---- ENG-008 / T-028 / T-033: failed vs rolled back vs unknown vs needs attention.

func TestInjectedDisconnectIsUnknownNotFailed(t *testing.T) {
	h := &fakeHandler{kind: "db.upgrade", steps: []StepSpec{{Name: "dump", RollbackMethod: "rm"}, {Name: "upgrade", Irreversible: true}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			if s.Index == 1 {
				return ErrDisconnected
			}
			return nil
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "u", Kind: "db.upgrade", Resources: []ResourceRef{{Kind: "database", ID: "db-1"}}})
	done := env.waitFinished(j.ID)
	if done.State != StateUnknown || done.Steps[1].Status != StepUnknown {
		t.Fatalf("state %s step %s", done.State, done.Steps[1].Status)
	}
	if done.Diagnostics == nil || done.Diagnostics.Cause != "" {
		t.Fatalf("no evidence → no invented root cause: %+v", done.Diagnostics)
	}
	// The lock is still held: a restore on the same database must wait.
	restore := &fakeHandler{kind: "db.restore", steps: simpleSteps(1)}
	if err := env.engine.Register(restore); err != nil {
		t.Fatal(err)
	}
	r := env.submit(SubmitRequest{RequestID: "r", Kind: "db.restore", Resources: []ResourceRef{{Kind: "database", ID: "db-1"}}})
	env.waitState(r.ID, StateQueued)
	time.Sleep(30 * time.Millisecond)
	if cur, _ := env.engine.Get(env.ctx, r.ID); cur.State != StateQueued {
		t.Fatalf("restore must wait while upgrade is unknown, got %s", cur.State)
	}
	// Reconnect: the executor's record says the step completed → continue, no re-run.
	rec, err := env.engine.Reconcile(env.ctx, j.ID, []StepReport{{Index: 1, Started: true, Completed: true, Evidence: []string{"agent://log/upgrade"}}})
	if err != nil {
		t.Fatal(err)
	}
	if rec.State == StateUnknown {
		t.Fatalf("reconcile must leave unknown: %s", rec.State)
	}
	final := env.waitFinished(j.ID)
	if final.State != StateSucceeded {
		t.Fatalf("state %s reason %q", final.State, final.StateReason)
	}
	if got := h.executedSteps(); len(got) != 2 {
		t.Fatalf("irreversible step must not run twice: %v", got)
	}
	if env.waitFinished(r.ID).State != StateSucceeded {
		t.Fatal("restore must run once the upgrade settled")
	}
}

func TestReconcileStartedButUnfinishedIrreversibleStepNeedsAttention(t *testing.T) {
	h := &fakeHandler{kind: "db.upgrade", steps: []StepSpec{{Name: "upgrade", Irreversible: true}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error { return ErrDisconnected }}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "u", Kind: "db.upgrade"})
	env.waitState(j.ID, StateUnknown)
	rec, err := env.engine.Reconcile(env.ctx, j.ID, []StepReport{{Index: 0, Started: true, Completed: false}})
	if err != nil || rec.State != StateNeedsAttention {
		t.Fatalf("err=%v state=%s", err, rec.State)
	}
	if len(h.executedSteps()) != 1 {
		t.Fatal("must not re-run a half-done irreversible step")
	}
}

func TestReconcileNeverStartedStepReruns(t *testing.T) {
	var calls int
	var mu sync.Mutex
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "s", Irreversible: true}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			mu.Lock()
			calls++
			c := calls
			mu.Unlock()
			if c == 1 {
				return ErrDisconnected
			}
			return nil
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "u", Kind: "k"})
	env.waitState(j.ID, StateUnknown)
	if _, err := env.engine.Reconcile(env.ctx, j.ID, []StepReport{{Index: 0, Started: false}}); err != nil {
		t.Fatal(err)
	}
	if env.waitFinished(j.ID).State != StateSucceeded {
		t.Fatal("a step that never began may be run")
	}
}

func TestInjectedDiskFullRollsBack(t *testing.T) {
	h := &fakeHandler{kind: "backup", steps: []StepSpec{{Name: "snapshot", RollbackMethod: "drop-snapshot"}, {Name: "copy", RollbackMethod: "rm-partial"}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			if s.Index == 1 {
				rep.Log("cp: write error: No space left on device")
				return &StepError{Code: "DISK_FULL", Summary: "no space left on /backup", Cause: "destination /backup is full", Evidence: []string{"df://backup"}, SuggestedActions: []string{"free-space", "choose-another-destination"}}
			}
			return nil
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "b", Kind: "backup"})
	done := env.waitFinished(j.ID)
	if done.State != StateRolledBack || done.ErrorCode != "DISK_FULL" {
		t.Fatalf("state %s code %s", done.State, done.ErrorCode)
	}
	if done.Steps[0].Status != StepRolledBack || done.Steps[1].Status != StepRolledBack {
		t.Fatalf("both steps must be rolled back: %+v", done.Steps)
	}
	d := done.Diagnostics
	if d == nil || d.Cause != "destination /backup is full" || len(d.Logs) == 0 || len(d.SuggestedActions) == 0 || len(d.Evidence) == 0 {
		t.Fatalf("diagnostics incomplete: %+v", d)
	}
	if done.Completion != nil {
		t.Fatal("a rolled-back job has no completion report")
	}
}

func TestKnownFailureWithoutRollbackMethodStaysFailed(t *testing.T) {
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "write"}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			return &StepError{Code: "DISK_FULL", Summary: "no space"}
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "b", Kind: "k"})
	done := env.waitFinished(j.ID)
	if done.State != StateFailed {
		t.Fatalf("state %s", done.State)
	}
	if done.Diagnostics.Cause != "" {
		t.Fatal("no evidence → no cause")
	}
}

// A step the executor refused without running (NothingChanged) is not an
// executed step: the job fails as "nothing was changed", not as "changes
// remain and cannot be rolled back".
func TestRefusedStepIsNotCountedAsExecuted(t *testing.T) {
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "write"}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			return &StepError{Code: "JOURNAL_UNAVAILABLE", Summary: "agent refused", Retryable: true, NothingChanged: true}
		}}
	env := newEnv(t, Options{}, h)
	done := env.waitFinished(env.submit(SubmitRequest{RequestID: "refused", Kind: "k"}).ID)
	if done.State != StateFailed || !done.Reversible || len(done.Diagnostics.Residue) != 0 || !strings.Contains(done.StateReason, "nothing was changed") {
		t.Fatalf("state %s reversible %v residue %v reason %q", done.State, done.Reversible, done.Diagnostics.Residue, done.StateReason)
	}
	// Without the flag the same failure is treated as a step that ran.
	h2 := &fakeHandler{kind: "k2", steps: []StepSpec{{Name: "write"}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			return &StepError{Code: "DISK_FULL", Summary: "no space"}
		}}
	env2 := newEnv(t, Options{}, h2)
	done2 := env2.waitFinished(env2.submit(SubmitRequest{RequestID: "ran", Kind: "k2"}).ID)
	if len(done2.Diagnostics.Residue) == 0 {
		t.Fatalf("a step that ran and failed without rollback must leave residue: %+v", done2.Diagnostics)
	}
}

// A retryable step whose first attempt was interrupted may have changed
// something. If the retry after reconciliation is refused without running,
// the step still counts as executed and is rolled back.
func TestRefusedRetryAfterInterruptedAttemptStillRollsBack(t *testing.T) {
	var calls int
	var mu sync.Mutex
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "write", RollbackMethod: "undo", Retryable: true}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				return ErrDisconnected // first attempt: outcome lost
			}
			return &StepError{Code: "NODE_OFFLINE", Summary: "nothing was sent", Retryable: true, NothingChanged: true}
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "retry-refused", Kind: "k"})
	env.waitState(j.ID, StateUnknown)
	if _, err := env.engine.Reconcile(env.ctx, j.ID, []StepReport{{Index: 0, Started: true}}); err != nil {
		t.Fatal(err)
	}
	done := env.waitFinished(j.ID)
	if done.State != StateRolledBack || len(h.rolledBack) != 1 || done.Steps[0].Attempts != 2 {
		t.Fatalf("state %s (%s) rolledBack %v attempts %d", done.State, done.StateReason, h.rolledBack, done.Steps[0].Attempts)
	}
}

func TestRollbackFailureNeedsAttention(t *testing.T) {
	h := &fakeHandler{kind: "restore", steps: []StepSpec{{Name: "stop-db", RollbackMethod: "start-db"}, {Name: "restore-files", RollbackMethod: "restore-previous"}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			if s.Index == 1 {
				return &StepError{Code: "RESTORE_FAILED", Summary: "archive truncated"}
			}
			return nil
		},
		rollback: func(s *Step) error {
			if s.Index == 1 {
				return errors.New("previous data directory is gone")
			}
			return nil
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "r", Kind: "restore"})
	done := env.waitFinished(j.ID)
	if done.State != StateNeedsAttention {
		t.Fatalf("state %s", done.State)
	}
	if done.Steps[1].Status != StepRollbackFailed || done.Steps[0].Status != StepDone {
		t.Fatalf("rollback stops at the first failure, leaving earlier steps as they are: %+v", done.Steps)
	}
	d := done.Diagnostics
	if d == nil || len(d.Residue) == 0 || !strings.Contains(strings.Join(d.Residue, " "), "restore-files") {
		t.Fatalf("needs_attention must list residue: %+v", d)
	}
	if !strings.Contains(d.ErrorSummary, "archive truncated") || !strings.Contains(strings.Join(d.Logs, "\n")+d.ErrorSummary+strings.Join(d.Residue, " "), "previous data directory is gone") {
		t.Fatalf("both the original error and the rollback error must be shown: %+v", d)
	}
}

// ---- ENG-003: restart recovery.

func TestRecoverAfterRestartDoesNotResetOrRerun(t *testing.T) {
	inStep := make(chan struct{})
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "a", RollbackMethod: "x"}, {Name: "b", Irreversible: true}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			if s.Index == 1 {
				close(inStep)
				<-ctx.Done() // the process "dies" here
				return ctx.Err()
			}
			return nil
		}}
	env := newEnv(t, Options{}, h)
	j := env.submit(SubmitRequest{RequestID: "k", Kind: "k", Resources: []ResourceRef{{Kind: "database", ID: "d"}}})
	<-inStep
	env.close()

	h2 := &fakeHandler{kind: "k", steps: h.steps}
	env2 := openEnv(t, env.path, Options{}, h2)
	rec, err := env2.engine.Get(env2.ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != StateUnknown {
		t.Fatalf("after restart a running job is unknown, got %s (%s)", rec.State, rec.StateReason)
	}
	if rec.Steps[0].Status != StepDone || rec.Steps[1].Status != StepUnknown {
		t.Fatalf("steps must keep their record: %+v", rec.Steps)
	}
	time.Sleep(50 * time.Millisecond)
	if len(h2.executedSteps()) != 0 {
		t.Fatal("recovery must not re-run anything")
	}
	// Locks are rebuilt: a conflicting job waits.
	other := env2.submit(SubmitRequest{RequestID: "o", Kind: "k", Resources: []ResourceRef{{Kind: "database", ID: "d"}}})
	env2.waitState(other.ID, StateQueued)
	// Cancel it once it is seen waiting, so the only steps that could run
	// below are the reconciled job's.
	if _, err := env2.engine.Cancel(env2.ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	// Reconcile says step b completed: continue to verification.
	if _, err := env2.engine.Reconcile(env2.ctx, j.ID, []StepReport{{Index: 1, Started: true, Completed: true}}); err != nil {
		t.Fatal(err)
	}
	if env2.waitFinished(j.ID).State != StateSucceeded {
		t.Fatal("reconciled job must finish")
	}
	if len(h2.executedSteps()) != 0 {
		t.Fatal("reconciled step must not be executed again")
	}
}

// ---- ENG-009: diagnostics are controlled.

func TestDiagnosticsNeverInventRootCause(t *testing.T) {
	h := &fakeHandler{kind: "k", steps: []StepSpec{{Name: "s"}},
		execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
			return &StepError{Code: "EXIT_1", Summary: "process exited with status 1", Cause: "guess: permissions"}
		}}
	env := newEnv(t, Options{}, h)
	done := env.waitFinished(env.submit(SubmitRequest{RequestID: "k", Kind: "k"}).ID)
	if done.Diagnostics.Cause != "" {
		t.Fatalf("a cause without evidence must be dropped, got %q", done.Diagnostics.Cause)
	}
	if done.Diagnostics.ErrorSummary == "" {
		t.Fatal("the raw error summary must still be shown")
	}
}

// ---- ENG-011: budgets.

func TestBudgetLimitsConcurrencyPerKind(t *testing.T) {
	release := make(chan struct{})
	h := &fakeHandler{kind: "backup", steps: simpleSteps(1), execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error { waitOn(ctx, release); return nil }}
	env := newEnv(t, Options{Budgets: map[string]Budget{"backup": {MaxConcurrent: 1}}}, h)
	a := env.submit(SubmitRequest{RequestID: "a", Kind: "backup"})
	env.waitState(a.ID, StateRunning)
	b := env.submit(SubmitRequest{RequestID: "b", Kind: "backup"})
	env.waitState(b.ID, StateQueued)
	time.Sleep(30 * time.Millisecond)
	cur, _ := env.engine.Get(env.ctx, b.ID)
	if cur.State != StateQueued || !strings.Contains(cur.StateReason, "budget") {
		t.Fatalf("second backup must wait on the budget: %s %q", cur.State, cur.StateReason)
	}
	close(release)
	env.waitFinished(a.ID)
	if env.waitFinished(b.ID).State != StateSucceeded {
		t.Fatal("second backup runs once the first finished")
	}
}

func TestBudgetKeepsSpaceReserve(t *testing.T) {
	h := &fakeHandler{kind: "extract", steps: simpleSteps(1)}
	env := newEnv(t, Options{
		DefaultBudget: Budget{MaxConcurrent: 4, ReserveBytes: 1 << 30},
		FreeBytes:     func() (int64, error) { return 1<<30 + 100, nil },
	}, h)
	j := env.submit(SubmitRequest{RequestID: "x", Kind: "extract", RequiredBytes: 200})
	done := env.waitFinished(j.ID)
	if done.State != StateFailed || done.ErrorCode != "BUDGET_SPACE_RESERVED" {
		t.Fatalf("state %s code %s", done.State, done.ErrorCode)
	}
	if len(h.executedSteps()) != 0 {
		t.Fatal("must not start")
	}
	ok := env.submit(SubmitRequest{RequestID: "y", Kind: "extract", RequiredBytes: 50})
	if env.waitFinished(ok.ID).State != StateSucceeded {
		t.Fatal("a job within the reserve runs")
	}
}

// ---- ENG-012: completion checks per kind.

func TestRequiredCheckFailurePreventsSuccess(t *testing.T) {
	h := &fakeHandler{kind: "service.install", steps: simpleSteps(1), verify: func(j *Job) (CompletionReport, error) {
		return CompletionReport{Checks: []Check{
			{Name: "package-installed", Required: true, Passed: true},
			{Name: "service-active", Required: true, Passed: false, Detail: "systemd reports failed"},
			{Name: "metrics-endpoint", Required: false, Passed: false},
		}}, nil
	}}
	env := newEnv(t, Options{}, h)
	done := env.waitFinished(env.submit(SubmitRequest{RequestID: "i", Kind: "service.install"}).ID)
	if done.State == StateSucceeded {
		t.Fatal("required check failed → must not be success")
	}
	if done.State != StateRolledBack || done.ErrorCode != "COMPLETION_CHECK_FAILED" {
		t.Fatalf("state %s code %s", done.State, done.ErrorCode)
	}
	if done.Completion == nil || len(done.Completion.Checks) != 3 {
		t.Fatalf("completion report must be kept: %+v", done.Completion)
	}
	if !strings.Contains(done.Diagnostics.ErrorSummary, "service-active") {
		t.Fatalf("diagnostics must name the failed check: %+v", done.Diagnostics)
	}
}

func TestOptionalCheckFailureStillSucceeds(t *testing.T) {
	h := &fakeHandler{kind: "k", steps: simpleSteps(1), verify: func(j *Job) (CompletionReport, error) {
		return CompletionReport{Checks: []Check{{Name: "must", Required: true, Passed: true}, {Name: "nice", Required: false, Passed: false}}}, nil
	}}
	env := newEnv(t, Options{}, h)
	if done := env.waitFinished(env.submit(SubmitRequest{RequestID: "k", Kind: "k"}).ID); done.State != StateSucceeded {
		t.Fatalf("state %s", done.State)
	}
}

func TestRegisterRejectsDuplicateAndNil(t *testing.T) {
	e := New(NewSQLStore(nil), Options{})
	if err := e.Register(nil); err == nil {
		t.Fatal("nil handler must be rejected")
	}
	if err := e.Register(&fakeHandler{kind: "k"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Register(&fakeHandler{kind: "k"}); err == nil {
		t.Fatal("duplicate kind must be rejected")
	}
}

// ---- T-034: log floods do not block heartbeat or cancel.

func TestLogFloodIsTruncatedAndCancelStillWorks(t *testing.T) {
	started := make(chan struct{})
	h := &fakeHandler{kind: "build", steps: []StepSpec{{Name: "build", Cancellable: true}}, execute: func(ctx context.Context, j *Job, s *Step, rep Reporter) error {
		for i := 0; i < 20000; i++ {
			rep.Log(fmt.Sprintf("line %d %s", i, strings.Repeat("x", 100)))
			if i == 100 {
				close(started)
			}
			if i%1000 == 0 {
				rep.Heartbeat()
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		return nil
	}}
	env := newEnv(t, Options{LogTail: 50, MaxLogEvents: 20}, h)
	j := env.submit(SubmitRequest{RequestID: "b", Kind: "build"})
	<-started
	if _, err := env.engine.Cancel(env.ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	done := env.waitFinished(j.ID)
	if done.State != StateCancelled {
		t.Fatalf("state %s", done.State)
	}
	st := done.Steps[0]
	if len(st.LogTail) > 50 || st.LogsDropped == 0 {
		t.Fatalf("tail must be bounded and loss explicit: tail=%d dropped=%d", len(st.LogTail), st.LogsDropped)
	}
	if done.LastHeartbeatAt == nil {
		t.Fatal("heartbeat must be recorded despite the flood")
	}
	evs, _ := env.engine.Events(env.ctx, j.ID, 0, 1000)
	truncated := false
	logEvents := 0
	for _, e := range evs {
		if e.Type == EventLogTruncated {
			truncated = true
		}
		if e.Type == EventLog {
			logEvents++
		}
	}
	if logEvents > 20 {
		t.Fatalf("log events must be capped at 20, got %d", logEvents)
	}
	if !truncated {
		t.Fatal("truncation must be announced as an event")
	}
}

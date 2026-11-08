package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Options tunes an Engine.
type Options struct {
	// Budgets per job kind; DefaultBudget applies to kinds not listed.
	Budgets       map[string]Budget
	DefaultBudget Budget
	// FreeBytes probes the free space of the data directory. nil disables the
	// space reserve check.
	FreeBytes func() (int64, error)
	// LogTail is how many log lines a step retains (default 200).
	LogTail int
	// MaxLogEvents caps the log lines stored as events per step (default
	// 200); the rest is counted as dropped.
	MaxLogEvents int
	PollInterval time.Duration
	Clock        func() time.Time
	Logger       *slog.Logger
}

// Engine owns the job state machine. One engine runs per Core process; it
// drives jobs from the store, never from memory alone, so a restart loses no
// record (ENG-001, ENG-003).
type Engine struct {
	store Store
	opts  Options

	mu       sync.Mutex
	handlers map[string]Handler
	locks    *lockTable
	running  map[string]int    // kind → running count (budget)
	admitted map[string]string // jobID → kind, for jobs counted in running
	inflight map[string]*flight
	// resumable marks running/verifying/rolling_back jobs that Reconcile
	// handed back to the loop; other jobs in those states are driven by the
	// goroutine that put them there.
	resumable map[string]bool

	kick chan struct{}
	stop chan struct{}
	wg   sync.WaitGroup
}

// flight is the in-memory side of a job being driven right now.
type flight struct {
	cancelRequested bool
	stepCancel      context.CancelFunc // cancels the current cancellable step
}

// New creates an engine over the store. Call Register for every kind, then
// Recover once, then Start.
func New(st Store, opts Options) *Engine {
	if opts.LogTail <= 0 {
		opts.LogTail = 200
	}
	if opts.MaxLogEvents <= 0 {
		opts.MaxLogEvents = 200
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.DefaultBudget.MaxConcurrent <= 0 {
		opts.DefaultBudget.MaxConcurrent = 4
	}
	return &Engine{
		store:     st,
		opts:      opts,
		handlers:  map[string]Handler{},
		locks:     newLockTable(),
		running:   map[string]int{},
		admitted:  map[string]string{},
		inflight:  map[string]*flight{},
		resumable: map[string]bool{},
		kick:      make(chan struct{}, 1),
		stop:      make(chan struct{}),
	}
}

// Register adds the handler for one kind. Every kind needs its own handler,
// hence its own Verify (ENG-012).
func (e *Engine) Register(h Handler) error {
	if h == nil || h.Kind() == "" {
		return errors.New("jobs: handler must not be nil and must name a kind")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, dup := e.handlers[h.Kind()]; dup {
		return fmt.Errorf("jobs: kind %q already registered", h.Kind())
	}
	e.handlers[h.Kind()] = h
	return nil
}

// Kinds lists the registered job kinds.
func (e *Engine) Kinds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.handlers))
	for k := range e.handlers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (e *Engine) now() time.Time { return e.opts.Clock().UTC() }

func (e *Engine) budgetFor(kind string) Budget {
	if b, ok := e.opts.Budgets[kind]; ok {
		if b.MaxConcurrent <= 0 {
			b.MaxConcurrent = e.opts.DefaultBudget.MaxConcurrent
		}
		return b
	}
	return e.opts.DefaultBudget
}

// requestHash is the content identity behind a request id: kind, canonical
// params, resources and node. Whitespace and key order do not matter.
func requestHash(req SubmitRequest) (string, error) {
	var params any
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return "", fmt.Errorf("%w: params must be JSON: %v", ErrInvalidRequest, err)
		}
	}
	canon, err := json.Marshal(struct {
		Kind      string        `json:"kind"`
		Params    any           `json:"params"`
		Resources []ResourceRef `json:"resources"`
		Node      string        `json:"node"`
		Required  int64         `json:"requiredBytes"`
	}{req.Kind, params, req.Resources, req.Node, req.RequiredBytes})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "job_" + hex.EncodeToString(b[:])
}

// Submit records a job and returns it. The same request id with the same
// content returns the existing job with deduplicated=true; the same id with
// different content returns ErrConflict (ENG-002).
func (e *Engine) Submit(ctx context.Context, req SubmitRequest) (job *Job, deduplicated bool, err error) {
	if strings.TrimSpace(req.RequestID) == "" {
		return nil, false, fmt.Errorf("%w: requestId is required", ErrInvalidRequest)
	}
	if req.Kind == "" {
		return nil, false, fmt.Errorf("%w: kind is required", ErrInvalidRequest)
	}
	e.mu.Lock()
	_, known := e.handlers[req.Kind]
	e.mu.Unlock()
	if !known {
		return nil, false, fmt.Errorf("%w: %q", ErrUnknownKind, req.Kind)
	}
	hash, err := requestHash(req)
	if err != nil {
		return nil, false, err
	}
	// The store's UNIQUE(request_id) is the backstop; the mutex keeps the
	// read-then-insert of two concurrent replays from racing.
	e.mu.Lock()
	defer e.mu.Unlock()
	existing, err := e.store.GetJobByRequestID(ctx, req.RequestID)
	switch {
	case err == nil:
		if existing.RequestHash == hash {
			return existing, true, nil
		}
		return nil, false, fmt.Errorf("%w: request %q", ErrConflict, req.RequestID)
	case !errors.Is(err, ErrNotFound):
		return nil, false, err
	}
	now := e.now()
	j := &Job{
		ID:            newID(),
		RequestID:     req.RequestID,
		RequestHash:   hash,
		Kind:          req.Kind,
		Params:        req.Params,
		Requester:     req.Requester,
		Node:          req.Node,
		Resources:     req.Resources,
		State:         StateCreated,
		StateReason:   "received; not yet changing anything",
		CreatedAt:     now,
		UpdatedAt:     now,
		Steps:         []Step{},
		Cancellable:   true,
		Reversible:    true,
		Budget:        e.budgetFor(req.Kind),
		RequiredBytes: req.RequiredBytes,
	}
	if err := e.store.CreateJob(ctx, j); err != nil {
		return nil, false, err
	}
	e.event(ctx, j, Event{Type: EventStateChanged, Message: string(StateCreated)})
	e.nudge()
	return j, false, nil
}

// Get returns the job record.
func (e *Engine) Get(ctx context.Context, id string) (*Job, error) { return e.store.GetJob(ctx, id) }

// List returns jobs, newest first.
func (e *Engine) List(ctx context.Context, opts ListOptions) ([]*Job, error) {
	return e.store.ListJobs(ctx, opts)
}

// Events returns the events after seq, so a reconnecting client resumes.
func (e *Engine) Events(ctx context.Context, id string, afterSeq int64, limit int) ([]Event, error) {
	if _, err := e.store.GetJob(ctx, id); err != nil {
		return nil, err
	}
	return e.store.ListEvents(ctx, id, afterSeq, limit)
}

// Cancel asks a job to stop (ENG-007). A job that has not started changing
// anything is cancelled at once. A running job becomes cancel_requested: a
// cancellable step is interrupted, an uninterruptible one finishes first and
// the step's WaitReason says so. "Requested" never means "stopped". A job
// whose steps are all done and is being verified cannot be cancelled: the
// result is whatever verification proves.
func (e *Engine) Cancel(ctx context.Context, id string) (*Job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, err := e.store.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	f := e.inflight[id]
	switch j.State {
	case StateCreated, StatePrechecking, StateAwaitingApproval:
		if f != nil {
			// The worker is planning; it checks the flag before changing
			// anything and ends the job as cancelled itself.
			f.cancelRequested = true
			j.CancelRequested = true
			j.StateReason = "cancel requested; stopping before execution"
			return j, e.save(ctx, j)
		}
		e.finishCancelled(ctx, j, "cancelled before anything was changed")
		return j, nil
	case StateQueued:
		// Nothing has been changed; cancel takes effect at once. A worker
		// that is in admission right now sees the flag (under e.mu) and
		// returns without taking locks.
		if f != nil {
			f.cancelRequested = true
		}
		e.finishCancelled(ctx, j, "cancelled while queued; nothing was changed")
		return j, nil
	case StateRunning:
		if f == nil {
			return nil, fmt.Errorf("%w: job is %s but no worker drives it; restart recovery pending", ErrNotCancellable, j.State)
		}
		f.cancelRequested = true
		e.applyCancelLocked(ctx, j, f)
		e.event(ctx, j, Event{Type: EventCancel, Message: j.StateReason})
		return j, e.save(ctx, j)
	case StateCancelRequested:
		return j, nil
	case StateVerifying:
		return nil, fmt.Errorf("%w: all steps are complete and verification is running; the result will be whatever it proves", ErrNotCancellable)
	default:
		return nil, fmt.Errorf("%w: job is %s", ErrNotCancellable, j.State)
	}
}

// applyCancelLocked moves a running job to cancel_requested on the given
// copy of the record. Both the API (on its fresh copy) and the driver (on
// its working copy) apply the same change, so whichever saves last leaves
// the same state. e.mu must be held.
func (e *Engine) applyCancelLocked(ctx context.Context, j *Job, f *flight) {
	j.CancelRequested = true
	if j.State != StateRunning {
		return
	}
	cur := e.currentStep(j)
	var reason string
	if cur != nil && cur.Cancellable {
		j.Cancellable = true
		reason = fmt.Sprintf("cancel requested; stopping step %q", cur.Name)
		if f.stepCancel != nil {
			f.stepCancel()
		}
	} else {
		j.Cancellable = false
		reason = "cancel requested"
		if cur != nil {
			wait := fmt.Sprintf("step %q cannot be interrupted safely", cur.Name)
			if cur.Irreversible {
				wait += " (it is irreversible)"
			}
			wait += "; the job stops once it completes. No forced kill."
			cur.WaitReason = wait
			reason += "; " + wait
		}
	}
	e.transition(ctx, j, StateCancelRequested, reason)
}

// syncCancel brings the driver's copy up to date with a cancel request made
// through the API and reports whether one is pending.
func (e *Engine) syncCancel(ctx context.Context, j *Job, f *flight) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !f.cancelRequested {
		return false
	}
	if !j.CancelRequested || (j.State == StateRunning) {
		e.applyCancelLocked(ctx, j, f)
	}
	return true
}

// RecoverReport says what Recover did at start.
type RecoverReport struct {
	MarkedUnknown []string
	LocksRebuilt  int
	Requeued      []string
}

// Recover is called once at start (ENG-003). Jobs that were mid-execution
// when the Core stopped become unknown: nothing is reset to success and
// nothing is re-run until Reconcile says what really happened. Locks of all
// active jobs are rebuilt so conflicting work keeps waiting.
func (e *Engine) Recover(ctx context.Context) (RecoverReport, error) {
	var rep RecoverReport
	active, err := e.store.ListJobs(ctx, ListOptions{States: []State{StatePrechecking, StateQueued, StateRunning, StateVerifying, StateRollingBack, StateCancelRequested, StateUnknown}})
	if err != nil {
		return rep, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, j := range active {
		switch j.State {
		case StateRunning, StateVerifying, StateRollingBack, StateCancelRequested:
			if cur := e.currentStep(j); cur != nil && cur.Status == StepRunning {
				cur.Status = StepUnknown
			}
			reason := fmt.Sprintf("hazelminx-core restarted while the job was %s; waiting for the executor's record before continuing", j.State)
			j.Cancellable = false
			if err := e.transition(ctx, j, StateUnknown, reason); err != nil {
				return rep, err
			}
			j.Diagnostics = mergeDiag(j.Diagnostics, &Diagnostics{ErrorSummary: reason, SuggestedActions: []string{"wait-for-reconnect", "inspect-node"}})
			if err := e.save(ctx, j); err != nil {
				return rep, err
			}
			rep.MarkedUnknown = append(rep.MarkedUnknown, j.ID)
		case StatePrechecking:
			// Precheck changes nothing; it simply runs again.
			rep.Requeued = append(rep.Requeued, j.ID)
		}
		if j.State.IsActive() && j.State != StateQueued {
			e.locks.tryAcquire(j.ID, j.Resources)
			e.admitLocked(j)
			rep.LocksRebuilt++
		}
	}
	return rep, nil
}

// Reconcile feeds the executor's persistent record for the steps of an
// unknown job (T-028). A completed step is accepted with its evidence; a
// step that never began may run; a step that began but did not finish is
// re-run only when it is retryable, rolled back when it has a method, and
// otherwise leaves the job in needs_attention. Nothing destructive is
// repeated on a guess.
func (e *Engine) Reconcile(ctx context.Context, id string, reports []StepReport) (*Job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, err := e.store.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if j.State != StateUnknown {
		return nil, fmt.Errorf("%w: job is %s, reconcile applies to unknown jobs", ErrInvalidRequest, j.State)
	}
	switch j.PriorState {
	case StateVerifying:
		// Verification was interrupted; it changes nothing, so verify again.
		return j, e.resumeFrom(ctx, j, StateVerifying, "executor record received; verifying again")
	case StateRollingBack:
		// Rollback was interrupted; continue undoing what is still applied.
		// Rollback methods must tolerate being asked twice.
		return j, e.resumeFrom(ctx, j, StateRollingBack, "executor record received; continuing rollback")
	}
	cur := e.currentStep(j)
	if cur == nil {
		return j, e.resumeFrom(ctx, j, StateVerifying, "all steps recorded; verifying")
	}
	var r *StepReport
	for i := range reports {
		if reports[i].Index == cur.Index {
			r = &reports[i]
		}
	}
	if r == nil {
		return nil, fmt.Errorf("%w: report for step %d is missing", ErrInvalidRequest, cur.Index)
	}
	now := e.now()
	switch {
	case r.Completed:
		cur.Status = StepDone
		cur.FinishedAt = &now
		cur.Evidence = append(cur.Evidence, r.Evidence...)
		j.Evidence = append(j.Evidence, r.Evidence...)
		j.CurrentStep = cur.Index + 1
		e.event(ctx, j, Event{Type: EventStepFinished, Step: cur.Index, Message: "confirmed complete by executor record"})
		if j.CurrentStep >= len(j.Steps) {
			return j, e.resumeFrom(ctx, j, StateVerifying, "all steps confirmed; verifying")
		}
		return j, e.resumeFrom(ctx, j, StateRunning, "step confirmed by executor record; continuing")
	case !r.Started:
		cur.Status = StepPending
		cur.StartedAt = nil
		return j, e.resumeFrom(ctx, j, StateRunning, "executor record shows the step never began; running it")
	case cur.Retryable:
		cur.Status = StepPending
		cur.StartedAt = nil
		return j, e.resumeFrom(ctx, j, StateRunning, "step is safe to repeat; running it again")
	case cur.RollbackMethod != "" && !cur.Irreversible:
		cur.Status = StepFailed
		j.ErrorCode = "STEP_INTERRUPTED"
		j.Diagnostics = mergeDiag(j.Diagnostics, &Diagnostics{ErrorSummary: fmt.Sprintf("step %q began but did not finish; rolling back", cur.Name)})
		return j, e.resumeFrom(ctx, j, StateRollingBack, "step interrupted; rolling back")
	default:
		cur.Status = StepUnknown
		residue := fmt.Sprintf("step %q (irreversible) began but has no completion record; it may be partially applied", cur.Name)
		j.Diagnostics = mergeDiag(j.Diagnostics, &Diagnostics{ErrorSummary: residue, Residue: []string{residue}, Evidence: r.Evidence,
			SuggestedActions: []string{"inspect-node", "decide-manually"}})
		if err := e.finish(ctx, j, StateNeedsAttention, residue); err != nil {
			return nil, err
		}
		e.releaseLocked(j)
		return j, nil
	}
}

// resumeFrom hands an unknown job back to the worker loop in the given
// state. e.mu must be held.
func (e *Engine) resumeFrom(ctx context.Context, j *Job, st State, reason string) error {
	if err := e.transition(ctx, j, st, reason); err != nil {
		return err
	}
	if err := e.save(ctx, j); err != nil {
		return err
	}
	e.resumable[j.ID] = true
	e.nudge()
	return nil
}

// Start launches the worker loop; it stops when ctx ends or Stop is called.
func (e *Engine) Start(ctx context.Context) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(e.opts.PollInterval)
		defer t.Stop()
		for {
			e.dispatch(ctx)
			select {
			case <-ctx.Done():
				return
			case <-e.stop:
				return
			case <-e.kick:
			case <-t.C:
			}
		}
	}()
}

// Stop ends the worker loop and waits for in-flight runs to return.
func (e *Engine) Stop() {
	select {
	case <-e.stop:
	default:
		close(e.stop)
	}
	e.wg.Wait()
}

func (e *Engine) nudge() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Wait blocks until cond holds for the job (polling the store) or ctx ends.
func (e *Engine) Wait(ctx context.Context, id string, cond func(*Job) bool) (*Job, error) {
	for {
		j, err := e.store.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}
		if cond(j) {
			return j, nil
		}
		select {
		case <-ctx.Done():
			return j, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// dispatch picks every job that is waiting for a worker and runs it.
func (e *Engine) dispatch(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	pending, err := e.store.ListJobs(ctx, ListOptions{States: []State{StateCreated, StatePrechecking, StateQueued, StateRunning, StateVerifying, StateRollingBack}, Ascending: true})
	if err != nil {
		if ctx.Err() == nil {
			e.opts.Logger.Error("jobs: list pending", "error", err)
		}
		return
	}
	for _, j := range pending { // oldest first
		e.mu.Lock()
		_, busy := e.inflight[j.ID]
		waitingForWorker := j.State == StateCreated || j.State == StatePrechecking || j.State == StateQueued || e.resumable[j.ID]
		if busy || !waitingForWorker {
			e.mu.Unlock()
			continue
		}
		delete(e.resumable, j.ID)
		f := &flight{cancelRequested: j.CancelRequested}
		e.inflight[j.ID] = f
		e.mu.Unlock()
		e.wg.Add(1)
		go func(id string, f *flight) {
			defer e.wg.Done()
			e.drive(ctx, id, f)
		}(j.ID, f)
	}
}

// drive moves one job as far as it can go right now and returns. A job that
// is blocked (lock, budget) stays queued and is picked up again later.
func (e *Engine) drive(ctx context.Context, id string, f *flight) {
	defer func() {
		e.mu.Lock()
		delete(e.inflight, id)
		e.mu.Unlock()
		e.nudge()
	}()
	j, err := e.store.GetJob(ctx, id)
	if err != nil {
		e.opts.Logger.Error("jobs: load", "job", id, "error", err)
		return
	}
	defer e.releaseAll(j)
	h := e.handlerFor(j.Kind)
	if h == nil {
		e.failPlanning(ctx, j, &StepError{Code: "JOB_KIND_UNKNOWN", Summary: fmt.Sprintf("no handler for kind %q in this build", j.Kind)})
		return
	}
	if j.State == StateCreated || j.State == StatePrechecking {
		if !e.precheck(ctx, j, h, f) {
			return
		}
	}
	if j.State == StateQueued {
		if !e.admit(ctx, j, f) {
			return
		}
	}
	if j.State == StateRunning {
		if !e.runSteps(ctx, j, h, f) {
			return
		}
	}
	if j.State == StateVerifying {
		e.verify(ctx, j, h)
	}
	if j.State == StateRollingBack {
		e.rollback(ctx, j, h)
	}
}

func (e *Engine) handlerFor(kind string) Handler {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.handlers[kind]
}

// precheck plans the steps and checks the space reserve (ENG-011).
func (e *Engine) precheck(ctx context.Context, j *Job, h Handler, f *flight) bool {
	if e.cancelled(f) {
		e.finishCancelled(ctx, j, "cancelled before precheck")
		return false
	}
	e.transition(ctx, j, StatePrechecking, "checking permissions, resources and plan")
	if err := e.save(ctx, j); err != nil {
		return false
	}
	specs, err := h.Plan(ctx, j)
	if err != nil {
		e.failPlanning(ctx, j, asStepError(err, "PRECHECK_FAILED"))
		return false
	}
	if len(specs) == 0 {
		e.failPlanning(ctx, j, &StepError{Code: "PRECHECK_FAILED", Summary: "plan has no steps"})
		return false
	}
	j.Steps = make([]Step, len(specs))
	for i, s := range specs {
		j.Steps[i] = Step{Index: i, Name: s.Name, Status: StepPending, InputRevision: s.InputRevision, Preconditions: s.Preconditions,
			RollbackMethod: s.RollbackMethod, Irreversible: s.Irreversible, Cancellable: s.Cancellable && !s.Irreversible, Retryable: s.Retryable}
	}
	j.PlanVersion++
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Name
	}
	j.PlanSummary = strings.Join(names, " → ")
	if j.RequiredBytes > 0 && e.opts.FreeBytes != nil {
		free, err := e.opts.FreeBytes()
		if err != nil {
			e.failPlanning(ctx, j, &StepError{Code: "BUDGET_SPACE_UNKNOWN", Summary: "could not measure free space: " + err.Error()})
			return false
		}
		if free-j.Budget.ReserveBytes < j.RequiredBytes {
			e.failPlanning(ctx, j, &StepError{Code: "BUDGET_SPACE_RESERVED",
				Summary:          fmt.Sprintf("job needs %d bytes but only %d are free above the %d-byte reserve kept for hazelminx-core", j.RequiredBytes, free-j.Budget.ReserveBytes, j.Budget.ReserveBytes),
				Evidence:         []string{"probe://free-space"},
				SuggestedActions: []string{"free-space", "choose-another-destination"}})
			return false
		}
	}
	if e.cancelled(f) {
		e.finishCancelled(ctx, j, "cancelled after precheck, before anything was changed")
		return false
	}
	e.transition(ctx, j, StateQueued, "waiting for resource locks and budget")
	return e.save(ctx, j) == nil
}

func (e *Engine) failPlanning(ctx context.Context, j *Job, se *StepError) {
	j.ErrorCode = se.Code
	j.Diagnostics = diagFrom(se, nil)
	j.Cancellable = false
	if j.State == StateCreated {
		e.transition(ctx, j, StatePrechecking, "")
	}
	e.finish(ctx, j, StateFailed, se.Summary)
}

// admit takes the resource locks and a budget slot, or leaves the job queued
// with the reason (ENG-004, ENG-011).
func (e *Engine) admit(ctx context.Context, j *Job, f *flight) bool {
	e.mu.Lock()
	if f.cancelRequested {
		e.mu.Unlock()
		if fresh, err := e.store.GetJob(ctx, j.ID); err == nil && fresh.State == StateCancelled {
			*j = *fresh // Cancel already ended it
			return false
		}
		e.finishCancelled(ctx, j, "cancelled while queued; nothing was changed")
		return false
	}
	b := e.budgetFor(j.Kind)
	if n := e.running[j.Kind]; n >= b.MaxConcurrent {
		e.mu.Unlock()
		reason := fmt.Sprintf("waiting for budget: %d %s job(s) already running (limit %d)", n, j.Kind, b.MaxConcurrent)
		if j.StateReason != reason {
			j.StateReason = reason
			e.save(ctx, j)
		}
		return false
	}
	if by, ok := e.locks.tryAcquire(j.ID, j.Resources); !ok {
		e.mu.Unlock()
		reason := fmt.Sprintf("waiting for resource lock held by job %s", by)
		if j.StateReason != reason {
			j.StateReason = reason
			e.save(ctx, j)
		}
		return false
	}
	e.admitLocked(j)
	e.mu.Unlock()
	now := e.now()
	j.StartedAt = &now
	j.LastHeartbeatAt = &now
	e.transition(ctx, j, StateRunning, "executing")
	return e.save(ctx, j) == nil
}

// admitLocked counts the job against its kind's budget. e.mu must be held.
func (e *Engine) admitLocked(j *Job) {
	if _, done := e.admitted[j.ID]; done {
		return
	}
	e.admitted[j.ID] = j.Kind
	e.running[j.Kind]++
}

// releaseAll gives back locks and budget once the job is no longer active.
func (e *Engine) releaseAll(j *Job) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.releaseLocked(j)
}

func (e *Engine) releaseLocked(j *Job) {
	if j.State.IsActive() {
		return
	}
	e.locks.release(j.ID)
	if kind, ok := e.admitted[j.ID]; ok {
		delete(e.admitted, j.ID)
		if e.running[kind] > 0 {
			e.running[kind]--
		}
	}
}

func (e *Engine) cancelled(f *flight) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return f.cancelRequested
}

// runSteps executes the remaining steps. It returns true when the job is
// ready for verification.
func (e *Engine) runSteps(ctx context.Context, j *Job, h Handler, f *flight) bool {
	for i := j.CurrentStep; i < len(j.Steps); i++ {
		step := &j.Steps[i]
		if step.Status == StepDone || step.Status == StepSkipped {
			continue
		}
		j.CurrentStep = i
		if e.syncCancel(ctx, j, f) {
			e.finishCancelled(ctx, j, fmt.Sprintf("stopped before step %q", step.Name))
			return false
		}
		// Re-check right before changing anything (ENG-006).
		if err := h.Recheck(ctx, j, step); err != nil {
			se := asStepError(err, "PRECHECK_CHANGED")
			if errors.Is(err, ErrRecheckBlocked) && se.Code == "PRECHECK_CHANGED" {
				se.Summary = "environment changed since precheck: " + err.Error()
			}
			step.Recheck = "blocked: " + se.Summary
			step.Status = StepFailed
			step.Error = se
			e.event(ctx, j, Event{Type: EventStepFinished, Step: i, Message: "re-check blocked: " + se.Summary})
			e.failAndMaybeRollback(ctx, j, h, se, nil)
			return false
		}
		step.Recheck = "ok"
		now := e.now()
		step.Status = StepRunning
		step.StartedAt = &now
		step.Attempts++
		j.Cancellable = step.Cancellable
		e.event(ctx, j, Event{Type: EventStepStarted, Step: i, Message: step.Name})
		if err := e.save(ctx, j); err != nil {
			return false
		}

		stepCtx, cancelStep := context.WithCancel(ctx)
		e.mu.Lock()
		if step.Cancellable {
			f.stepCancel = cancelStep
		} else {
			f.stepCancel = nil
		}
		if f.cancelRequested {
			// A cancel landed between the re-check and here: the record may
			// already say cancel_requested, so interrupt the step directly.
			e.applyCancelLocked(ctx, j, f)
			if step.Cancellable {
				cancelStep()
			}
		}
		e.mu.Unlock()
		rep := newReporter(e, j, i)
		err := h.Execute(stepCtx, j, step, rep)
		cancelStep()
		e.mu.Lock()
		f.stepCancel = nil
		e.mu.Unlock()
		rep.flushInto(step, j)
		cancelPending := e.syncCancel(ctx, j, f)

		fin := e.now()
		step.FinishedAt = &fin
		switch {
		case err == nil:
			step.Status = StepDone
			j.CurrentStep = i + 1
			e.event(ctx, j, Event{Type: EventStepFinished, Step: i, Message: step.Name + " done"})
			if cancelPending {
				reason := fmt.Sprintf("stopped after step %q completed", step.Name)
				if i+1 == len(j.Steps) {
					reason = fmt.Sprintf("every step had completed before the cancel could take effect (last: %q); completion was not verified", step.Name)
				}
				e.finishCancelled(ctx, j, reason)
				return false
			}
		case errors.Is(err, ErrDisconnected), ctx.Err() != nil && !cancelPending:
			// The executor went away, or this Core is shutting down while the
			// step may still be running elsewhere: the outcome is unknown.
			step.Status = StepUnknown
			step.FinishedAt = nil
			reason := fmt.Sprintf("executor disconnected during step %q before reporting a result; it may or may not have completed", step.Name)
			if ctx.Err() != nil {
				reason = fmt.Sprintf("hazelminx-core stopped during step %q before the result was reported; it may or may not have completed", step.Name)
			}
			j.Diagnostics = mergeDiag(j.Diagnostics, &Diagnostics{ErrorSummary: reason, Logs: step.LogTail, LogsTruncated: step.LogsDropped > 0, LogsDropped: step.LogsDropped,
				SuggestedActions: []string{"wait-for-reconnect", "inspect-node"}})
			j.Cancellable = false
			e.transition(ctx, j, StateUnknown, reason)
			e.event(ctx, j, Event{Type: EventStepFinished, Step: i, Message: "outcome unknown: " + reason})
			e.save(ctx, j)
			return false
		case errors.Is(err, context.Canceled) && cancelPending:
			step.Status = StepFailed
			step.Error = &StepError{Code: "CANCELLED", Summary: "stopped at user's request"}
			e.event(ctx, j, Event{Type: EventStepFinished, Step: i, Message: step.Name + " interrupted by cancel"})
			e.finishCancelled(ctx, j, fmt.Sprintf("stopped during step %q", step.Name))
			return false
		default:
			se := asStepError(err, "STEP_FAILED")
			step.Status = StepFailed
			step.Error = se
			e.event(ctx, j, Event{Type: EventStepFinished, Step: i, Message: step.Name + " failed: " + se.Summary})
			e.failAndMaybeRollback(ctx, j, h, se, step)
			return false
		}
		if err := e.save(ctx, j); err != nil {
			return false
		}
	}
	j.Cancellable = false
	e.transition(ctx, j, StateVerifying, "all steps done; proving the goal was reached")
	return e.save(ctx, j) == nil
}

// verify runs the kind's completion checks (ENG-012).
func (e *Engine) verify(ctx context.Context, j *Job, h Handler) {
	rep, err := h.Verify(ctx, j)
	if err != nil {
		if errors.Is(err, ErrDisconnected) {
			reason := "executor disconnected during verification"
			j.Diagnostics = mergeDiag(j.Diagnostics, &Diagnostics{ErrorSummary: reason})
			e.transition(ctx, j, StateUnknown, reason)
			e.save(ctx, j)
			return
		}
		e.failAndMaybeRollback(ctx, j, h, asStepError(err, "COMPLETION_CHECK_ERROR"), nil)
		return
	}
	j.Completion = &rep
	for _, c := range rep.Checks {
		e.event(ctx, j, Event{Type: EventCheck, Message: fmt.Sprintf("%s: passed=%v required=%v %s", c.Name, c.Passed, c.Required, c.Detail)})
		j.Evidence = append(j.Evidence, c.Evidence...)
	}
	if !rep.RequiredPassed() {
		var failed []string
		for _, c := range rep.Checks {
			if c.Required && !c.Passed {
				failed = append(failed, c.Name)
			}
		}
		se := &StepError{Code: "COMPLETION_CHECK_FAILED", Summary: "required completion check(s) did not pass: " + strings.Join(failed, ", ")}
		e.failAndMaybeRollback(ctx, j, h, se, nil)
		return
	}
	j.Progress = 100
	e.finish(ctx, j, StateSucceeded, "all required goals and checks met")
}

// failAndMaybeRollback records a known failure, then rolls back when every
// executed step has a rollback method; otherwise the job stays failed with
// its residue listed.
func (e *Engine) failAndMaybeRollback(ctx context.Context, j *Job, h Handler, se *StepError, failedStep *Step) {
	j.ErrorCode = se.Code
	var logs []string
	dropped := 0
	if failedStep != nil {
		logs, dropped = failedStep.LogTail, failedStep.LogsDropped
	}
	j.Diagnostics = mergeDiag(j.Diagnostics, diagFrom(se, logs))
	j.Diagnostics.LogsDropped = dropped
	j.Diagnostics.LogsTruncated = dropped > 0
	j.Cancellable = false
	executed := e.executedSteps(j)
	e.transition(ctx, j, StateFailed, se.Summary)
	if len(executed) == 0 {
		e.finish(ctx, j, StateFailed, se.Summary+"; nothing was changed")
		return
	}
	for _, s := range executed {
		if s.RollbackMethod == "" {
			j.Diagnostics.Residue = append(j.Diagnostics.Residue, fmt.Sprintf("step %q completed and has no rollback method", s.Name))
		}
	}
	if len(j.Diagnostics.Residue) > 0 {
		j.Reversible = false
		e.finish(ctx, j, StateFailed, se.Summary+"; some changes remain and cannot be rolled back automatically")
		return
	}
	e.transition(ctx, j, StateRollingBack, "undoing executed steps")
	if err := e.save(ctx, j); err != nil {
		return
	}
	e.rollback(ctx, j, h)
}

// rollback undoes executed steps in reverse order; the first failure leaves
// the job in needs_attention with the residue spelled out (T-033).
func (e *Engine) rollback(ctx context.Context, j *Job, h Handler) {
	executed := e.executedSteps(j)
	for i := len(executed) - 1; i >= 0; i-- {
		s := executed[i]
		if s.Status == StepRolledBack {
			continue
		}
		rep := newReporter(e, j, s.Index)
		err := h.Rollback(ctx, j, s, rep)
		rep.flushInto(s, j)
		if err != nil {
			if errors.Is(err, ErrDisconnected) {
				reason := fmt.Sprintf("executor disconnected while rolling back step %q", s.Name)
				j.Diagnostics = mergeDiag(j.Diagnostics, &Diagnostics{ErrorSummary: reason})
				e.transition(ctx, j, StateUnknown, reason)
				e.save(ctx, j)
				return
			}
			s.Status = StepRollbackFailed
			rbErr := asStepError(err, "ROLLBACK_FAILED")
			var residue []string
			residue = append(residue, fmt.Sprintf("step %q: rollback (%s) failed: %s", s.Name, s.RollbackMethod, rbErr.Summary))
			for k := 0; k < i; k++ {
				residue = append(residue, fmt.Sprintf("step %q: still applied, not rolled back", executed[k].Name))
			}
			j.Diagnostics = mergeDiag(j.Diagnostics, &Diagnostics{Residue: residue, Logs: s.LogTail, Evidence: rbErr.Evidence,
				SuggestedActions: append([]string{"inspect-node", "decide-manually"}, rbErr.SuggestedActions...)})
			j.Diagnostics.ErrorSummary = j.Diagnostics.ErrorSummary + "; rollback failed: " + rbErr.Summary
			e.event(ctx, j, Event{Type: EventStepFinished, Step: s.Index, Message: "rollback failed: " + rbErr.Summary})
			e.finish(ctx, j, StateNeedsAttention, "original operation failed and rollback failed; manual decision needed")
			return
		}
		s.Status = StepRolledBack
		e.event(ctx, j, Event{Type: EventStepFinished, Step: s.Index, Message: "rolled back via " + s.RollbackMethod})
		e.save(ctx, j)
	}
	e.finish(ctx, j, StateRolledBack, "the operation did not succeed; all changes were undone")
}

// executedSteps lists steps that ran (done, failed after starting, or rolled
// back) in order.
func (e *Engine) executedSteps(j *Job) []*Step {
	var out []*Step
	for i := range j.Steps {
		s := &j.Steps[i]
		if s.StartedAt != nil && (s.Status == StepDone || s.Status == StepFailed || s.Status == StepRolledBack || s.Status == StepRollbackFailed) {
			if s.Status == StepFailed && s.Recheck != "ok" {
				continue // blocked before doing anything
			}
			if s.Status == StepFailed && s.Error != nil && s.Error.NothingChanged && s.Attempts <= 1 {
				continue // refused without running, and never started before
			}
			out = append(out, s)
		}
	}
	return out
}

func (e *Engine) currentStep(j *Job) *Step {
	if j.CurrentStep < 0 || j.CurrentStep >= len(j.Steps) {
		return nil
	}
	return &j.Steps[j.CurrentStep]
}

// finishCancelled ends the job as cancelled and lists what was done and not
// reverted, so "cancelled" never hides partial work.
func (e *Engine) finishCancelled(ctx context.Context, j *Job, reason string) {
	var residue []string
	for i := range j.Steps {
		s := &j.Steps[i]
		if s.Status == StepDone {
			residue = append(residue, fmt.Sprintf("step %q completed and was not reverted", s.Name))
		}
	}
	if len(residue) == 0 {
		residue = []string{"nothing was changed"}
	}
	j.Diagnostics = mergeDiag(j.Diagnostics, &Diagnostics{ErrorSummary: reason, Residue: residue})
	j.Cancellable = false
	j.CancelRequested = true
	if j.State == StateRunning {
		e.transition(ctx, j, StateCancelRequested, reason)
	}
	e.finish(ctx, j, StateCancelled, reason)
}

// finish moves to a terminal state and stamps the end time.
func (e *Engine) finish(ctx context.Context, j *Job, st State, reason string) error {
	if err := e.transition(ctx, j, st, reason); err != nil {
		return err
	}
	now := e.now()
	j.FinishedAt = &now
	j.Cancellable = false
	return e.save(ctx, j)
}

// transition applies a state change; a change the spec table forbids is a
// programming error and is logged loudly rather than silently applied.
func (e *Engine) transition(ctx context.Context, j *Job, to State, reason string) error {
	if j.State == to {
		if reason != "" {
			j.StateReason = reason
		}
		return nil
	}
	if !CanTransition(j.State, to) {
		err := fmt.Errorf("jobs: forbidden transition %s → %s (job %s)", j.State, to, j.ID)
		e.opts.Logger.Error("jobs: forbidden transition", "job", j.ID, "from", j.State, "to", to)
		return err
	}
	from := j.State
	if to == StateUnknown {
		j.PriorState = from
	}
	j.State = to
	j.StateReason = reason
	e.event(ctx, j, Event{Type: EventStateChanged, Message: fmt.Sprintf("%s → %s: %s", from, to, reason)})
	return nil
}

func (e *Engine) save(ctx context.Context, j *Job) error {
	j.UpdatedAt = e.now()
	if err := e.store.UpdateJob(context.WithoutCancel(ctx), j); err != nil {
		e.opts.Logger.Error("jobs: save", "job", j.ID, "error", err)
		return err
	}
	return nil
}

func (e *Engine) event(ctx context.Context, j *Job, ev Event) {
	ev.JobID = j.ID
	ev.At = e.now()
	seq, err := e.store.AppendEvent(context.WithoutCancel(ctx), &ev)
	if err != nil {
		e.opts.Logger.Error("jobs: append event", "job", j.ID, "error", err)
		return
	}
	j.LastEventSeq = seq
}

// asStepError turns any error into a controlled diagnostic. A plain error
// becomes a summary with the given code and no root cause.
func asStepError(err error, code string) *StepError {
	var se *StepError
	if errors.As(err, &se) {
		cp := *se
		if cp.Code == "" {
			cp.Code = code
		}
		return &cp
	}
	return &StepError{Code: code, Summary: err.Error()}
}

// diagFrom builds the user-facing diagnostic (ENG-009). A claimed cause is
// kept only when evidence backs it.
func diagFrom(se *StepError, logs []string) *Diagnostics {
	d := &Diagnostics{ErrorSummary: se.Summary, Logs: logs, Evidence: se.Evidence, SuggestedActions: se.SuggestedActions}
	if len(se.Evidence) > 0 {
		d.Cause = se.Cause
	}
	return d
}

func mergeDiag(base, add *Diagnostics) *Diagnostics {
	if base == nil {
		return add
	}
	if add == nil {
		return base
	}
	if add.ErrorSummary != "" {
		base.ErrorSummary = add.ErrorSummary
	}
	if add.Cause != "" {
		base.Cause = add.Cause
	}
	if len(add.Logs) > 0 {
		base.Logs = add.Logs
		base.LogsTruncated = add.LogsTruncated
		base.LogsDropped = add.LogsDropped
	}
	base.Evidence = append(base.Evidence, add.Evidence...)
	base.SuggestedActions = append(base.SuggestedActions, add.SuggestedActions...)
	base.Residue = append(base.Residue, add.Residue...)
	for k, v := range add.SystemState {
		if base.SystemState == nil {
			base.SystemState = map[string]string{}
		}
		base.SystemState[k] = v
	}
	return base
}

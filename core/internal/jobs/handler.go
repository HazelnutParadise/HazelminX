package jobs

import "context"

// Handler is the per-kind behaviour of a job. Every kind must say how to plan
// its steps, re-check right before each step, execute, roll back, reconcile a
// lost outcome and verify real completion. There is no default Verify: a kind
// without its own completion checks cannot be registered (ENG-012).
//
// Handlers for host-changing kinds proxy to an Agent (tickets 004/007); the
// engine's tests use in-process fakes that inject disconnects and failures.
type Handler interface {
	// Kind is the job kind this handler owns.
	Kind() string
	// Plan is the precheck: validate params and resources, and return the
	// ordered steps. Returning a *StepError fails the job with that
	// diagnostic; any other error is wrapped as PRECHECK_FAILED.
	Plan(ctx context.Context, job *Job) ([]StepSpec, error)
	// Recheck runs right before a step executes (ENG-006). Return
	// ErrRecheckBlocked (optionally wrapped in a *StepError via errors.As) to
	// stop without touching anything.
	Recheck(ctx context.Context, job *Job, step *Step) error
	// Execute performs one step. ctx is cancelled when a cancel request can be
	// honoured for this step (step.Cancellable). Return ErrDisconnected when
	// the outcome was lost; a *StepError for a known failure.
	Execute(ctx context.Context, job *Job, step *Step, rep Reporter) error
	// Rollback undoes one executed step. Called in reverse order for steps
	// with a RollbackMethod. An error leaves the job in needs_attention.
	Rollback(ctx context.Context, job *Job, step *Step, rep Reporter) error
	// Reconcile asks the executor's persistent record what happened to a step
	// whose outcome the Core lost. It must not re-run the step.
	Reconcile(ctx context.Context, job *Job, step *Step) (StepReport, error)
	// Verify proves the job's real goal was reached (ENG-012): service up,
	// data readable, config consistent — not just "command returned 0".
	Verify(ctx context.Context, job *Job) (CompletionReport, error)
}

// Reporter is what a handler uses to report while a step runs. Log output is
// bounded: the engine keeps a tail and records how much was dropped, so a
// flood of logs cannot delay heartbeats or cancellation (T-034).
type Reporter interface {
	Log(line string)
	Progress(percent int)
	Heartbeat()
	// Evidence attaches an evidence link to the current step.
	Evidence(ref string)
}

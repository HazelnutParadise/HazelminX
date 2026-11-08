package nodes

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// Reconciler settles jobs whose outcome was lost when a node's session or
// this Core went away, or when the agent answered "outcome unknown". It
// never re-runs a step itself; it hands the agent's journal record to
// jobs.Engine.Reconcile, which decides (SEC-008: reconcile first, never
// resend blindly).
//
// It starts when a node connects and keeps going while that session lives:
// a job can become unknown during the session (the agent refused a step it
// had begun before a restart), and an agent still running a step answers a
// reconcile only when the step ends, which may take longer than one ask.
type Reconciler struct {
	Engine *jobs.Engine
	Runner *StepRunner
	Logger *slog.Logger
	// Settle bounds how long a job that was in flight on the previous
	// session may take to become unknown. Default 10 s.
	Settle time.Duration
	// Interval between passes while the session lives. Default 15 s.
	Interval time.Duration
}

// OnReady is the Hub callback; it returns when the session ends.
func (rc *Reconciler) OnReady(s *Session, inFlight []string) {
	ctx := context.Background()
	settle, interval := rc.Settle, rc.Interval
	if settle <= 0 {
		settle = 10 * time.Second
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	// The engine marks an interrupted step unknown on its own worker; wait
	// for that so the job is not skipped as "still running".
	for _, id := range inFlight {
		wctx, cancel := context.WithTimeout(ctx, settle)
		_, _ = rc.Engine.Wait(wctx, id, func(j *jobs.Job) bool {
			return j.State != jobs.StateRunning && j.State != jobs.StateVerifying && j.State != jobs.StateRollingBack && j.State != jobs.StateCancelRequested
		})
		cancel()
	}
	for {
		rc.pass(ctx, s)
		select {
		case <-s.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (rc *Reconciler) logger() *slog.Logger {
	if rc.Logger == nil {
		return slog.Default()
	}
	return rc.Logger
}

// pass asks about every unknown job of the session's node in parallel, so
// one long-running step does not hold up the others.
func (rc *Reconciler) pass(ctx context.Context, s *Session) {
	unknown, err := rc.Engine.List(ctx, jobs.ListOptions{States: []jobs.State{jobs.StateUnknown}, Ascending: true})
	if err != nil {
		rc.logger().Error("reconcile: list unknown jobs", "node", s.NodeID(), "error", err)
		return
	}
	var wg sync.WaitGroup
	for _, j := range unknown {
		if j.Node != s.NodeID() {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			rc.settle(ctx, s, j)
		}()
	}
	wg.Wait()
}

func (rc *Reconciler) settle(ctx context.Context, s *Session, j *jobs.Job) {
	log := rc.logger()
	var reports []jobs.StepReport
	if (j.PriorState == jobs.StateRunning || j.PriorState == jobs.StateCancelRequested) && j.CurrentStep < len(j.Steps) {
		rep, err := rc.Runner.Reconcile(ctx, j, &j.Steps[j.CurrentStep])
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			log.Info("reconcile: step still running on the agent; asking again next pass", "job", j.ID, "node", s.NodeID())
			return
		case err != nil:
			log.Warn("reconcile: agent record unavailable; job stays unknown", "job", j.ID, "node", s.NodeID(), "error", err)
			return
		}
		reports = []jobs.StepReport{rep}
	}
	after, err := rc.Engine.Reconcile(ctx, j.ID, reports)
	if err != nil {
		log.Warn("reconcile: engine refused the record; job stays unknown", "job", j.ID, "error", err)
		return
	}
	log.Info("reconcile: job settled from agent record", "job", j.ID, "node", s.NodeID(), "state", after.State, "reason", after.StateReason)
}

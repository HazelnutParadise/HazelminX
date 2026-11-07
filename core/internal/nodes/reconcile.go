package nodes

import (
	"context"
	"log/slog"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// Reconciler settles jobs whose outcome was lost when a node's session or
// this Core went away. It runs when the node connects again, before any new
// step can reach it through the same job (SEC-008: reconcile first, never
// resend blindly). It never re-runs a step itself; it hands the agent's
// journal record to jobs.Engine.Reconcile, which decides.
type Reconciler struct {
	Engine *jobs.Engine
	Runner *StepRunner
	Logger *slog.Logger
	// Settle bounds how long a job that was in flight on the previous
	// session may take to become unknown. Default 10 s.
	Settle time.Duration
}

// OnReady is the Hub callback.
func (rc *Reconciler) OnReady(s *Session, inFlight []string) {
	ctx := context.Background()
	log := rc.Logger
	if log == nil {
		log = slog.Default()
	}
	settle := rc.Settle
	if settle <= 0 {
		settle = 10 * time.Second
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
	unknown, err := rc.Engine.List(ctx, jobs.ListOptions{States: []jobs.State{jobs.StateUnknown}, Ascending: true})
	if err != nil {
		log.Error("reconcile: list unknown jobs", "node", s.NodeID(), "error", err)
		return
	}
	for _, j := range unknown {
		if j.Node != s.NodeID() {
			continue
		}
		var reports []jobs.StepReport
		if (j.PriorState == jobs.StateRunning || j.PriorState == jobs.StateCancelRequested) && j.CurrentStep < len(j.Steps) {
			rep, err := rc.Runner.Reconcile(ctx, j, &j.Steps[j.CurrentStep])
			if err != nil {
				log.Warn("reconcile: agent record unavailable; job stays unknown", "job", j.ID, "node", s.NodeID(), "error", err)
				continue
			}
			reports = []jobs.StepReport{rep}
		}
		after, err := rc.Engine.Reconcile(ctx, j.ID, reports)
		if err != nil {
			log.Warn("reconcile: engine refused the record; job stays unknown", "job", j.ID, "error", err)
			continue
		}
		log.Info("reconcile: job settled from agent record", "job", j.ID, "node", s.NodeID(), "state", after.State, "reason", after.StateReason)
	}
}

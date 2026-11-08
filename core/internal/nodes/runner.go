package nodes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Step error codes the runner reports.
const (
	CodeNodeOffline           = "NODE_OFFLINE"
	CodeCapabilityUnsupported = "CAPABILITY_UNSUPPORTED"
	CodeStepRejected          = "STEP_REJECTED"
)

// StepRunner executes job steps on agents. Job handlers for host-changing
// kinds call it from Execute and Reconcile.
type StepRunner struct {
	hub *Hub
	// StepTTL is how long a step_execute stays valid; the agent refuses it
	// after expiresAt. Default 5 minutes.
	StepTTL time.Duration
	// ReconcileTimeout bounds a step_reconcile round trip. Default 30 s.
	ReconcileTimeout time.Duration
}

// NewStepRunner returns a runner on hub.
func NewStepRunner(hub *Hub) *StepRunner {
	return &StepRunner{hub: hub, StepTTL: 5 * time.Minute, ReconcileTimeout: 30 * time.Second}
}

// Action names what a step asks the agent to do.
type Action struct {
	Name    string
	Version int
	Params  map[string]any
}

// Execute sends one step to job.Node and waits for its outcome.
//
//   - node not connected: *jobs.StepError NODE_OFFLINE (retryable; nothing
//     was sent, nothing happened);
//   - agent lacks the jobs capability or the action at that version:
//     CAPABILITY_UNSUPPORTED, nothing sent (NOD-011);
//   - agent refuses the step: the agent's code (UNSUPPORTED_ACTION,
//     INVALID_PARAMS, WRONG_NODE, EXPIRED, STALE_CONTROL_GENERATION), except
//     OUTCOME_UNKNOWN, which is jobs.ErrDisconnected so the job waits for
//     reconciliation;
//   - session ends or ctx ends before the outcome: jobs.ErrDisconnected;
//   - failed event: *jobs.StepError with the agent's code and evidence;
//   - completed event: the step's result, evidence attached to rep.
func (r *StepRunner) Execute(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter, a Action) (json.RawMessage, error) {
	s, ok := r.hub.Session(job.Node)
	if !ok {
		return nil, &jobs.StepError{Code: CodeNodeOffline, Summary: fmt.Sprintf("node %s is not connected; nothing was sent", short(job.Node)), Retryable: true,
			SuggestedActions: []string{"check-node-connection", "retry"}}
	}
	if !s.Supports(protocol.CapJobs, 1) || !s.Supports(a.Name, a.Version) {
		return nil, &jobs.StepError{Code: CodeCapabilityUnsupported,
			Summary:          fmt.Sprintf("agent %s does not support %s v%d (it declares %s); nothing was sent", s.agentVersion, a.Name, a.Version, capList(s.caps)),
			SuggestedActions: []string{"upgrade-agent"}}
	}
	params := map[string]json.RawMessage{}
	for k, v := range a.Params {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, &jobs.StepError{Code: "INVALID_PARAMS", Summary: fmt.Sprintf("param %q: %v", k, err)}
		}
		params[k] = b
	}
	reqID, err := requestID("stp", job.ID, step.Index)
	if err != nil {
		return nil, err
	}
	p := &pending{jobID: job.ID, step: step.Index, result: make(chan any, 1),
		event: func(*protocol.Event) { rep.Heartbeat() },
		progress: func(m *protocol.StepProgress) {
			for _, l := range m.Log {
				rep.Log(l)
			}
			if m.LogsDropped > 0 {
				rep.Log(fmt.Sprintf("[agent dropped %d log lines under back-pressure]", m.LogsDropped))
			}
			if m.Progress != nil {
				rep.Progress(*m.Progress)
			}
			rep.Heartbeat()
		}}
	msg := protocol.StepExecute{Type: protocol.TypeStepExecute, RequestID: reqID, JobID: job.ID, Step: step.Index, StepName: step.Name,
		Action: a.Name, ActionVersion: a.Version, Params: params, TargetNode: job.Node, ControlGeneration: job.ControlGeneration,
		ExpiresAt: r.hub.opts.Now().UTC().Add(r.StepTTL).Truncate(time.Second), Retryable: step.Retryable, Irreversible: step.Irreversible}
	if err := s.request(reqID, p, msg); err != nil {
		return nil, fmt.Errorf("%w: %v", jobs.ErrDisconnected, err)
	}
	defer s.forget(reqID)
	select {
	case v := <-p.result:
		switch m := v.(type) {
		case *protocol.Event:
			for _, ev := range m.Evidence {
				rep.Evidence(ev)
			}
			if m.Kind == protocol.EventCompleted {
				return m.Result, nil
			}
			se := &jobs.StepError{Code: "AGENT_STEP_FAILED", Summary: "agent reported failure", Evidence: m.Evidence}
			if m.Error != nil {
				se.Code, se.Summary, se.Retryable = m.Error.Code, m.Error.Summary, m.Error.Retryable
			}
			return nil, se
		case *protocol.StepRejected:
			if m.Code == protocol.StepRejectOutcomeUnknown {
				return nil, fmt.Errorf("%w: agent record shows the step began but did not finish: %s", jobs.ErrDisconnected, m.Message)
			}
			// Nothing ran. Only a journal the agent cannot write is worth a
			// retry; the other refusals need a different request.
			return nil, &jobs.StepError{Code: m.Code, Summary: "agent refused the step without running it: " + m.Message,
				Retryable: m.Code == protocol.StepRejectJournalUnavailable}
		}
		return nil, fmt.Errorf("%w: unexpected answer %T", jobs.ErrDisconnected, v)
	case <-s.Done():
		return nil, fmt.Errorf("%w: session %s ended", jobs.ErrDisconnected, s.sessionID)
	case <-ctx.Done():
		// v1 has no remote cancel: the step may still run on the agent.
		return nil, fmt.Errorf("%w: %v", jobs.ErrDisconnected, ctx.Err())
	}
}

// Reconcile asks the agent's journal what happened to a step, without
// running anything. A step the agent recorded as failed is reported as
// started and not completed: the engine then re-runs it only if it is
// retryable, rolls it back if it can, and otherwise asks a person.
func (r *StepRunner) Reconcile(ctx context.Context, job *jobs.Job, step *jobs.Step) (jobs.StepReport, error) {
	rep := jobs.StepReport{Index: step.Index}
	s, ok := r.hub.Session(job.Node)
	if !ok {
		return rep, fmt.Errorf("%w: node %s is not connected", jobs.ErrDisconnected, short(job.Node))
	}
	reqID, err := requestID("rec", job.ID, step.Index)
	if err != nil {
		return rep, err
	}
	p := &pending{jobID: job.ID, step: step.Index, result: make(chan any, 1)}
	if err := s.request(reqID, p, protocol.StepReconcile{Type: protocol.TypeStepReconcile, RequestID: reqID, JobID: job.ID, Step: step.Index, TargetNode: job.Node}); err != nil {
		return rep, fmt.Errorf("%w: %v", jobs.ErrDisconnected, err)
	}
	defer s.forget(reqID)
	ctx, cancel := context.WithTimeout(ctx, r.ReconcileTimeout)
	defer cancel()
	select {
	case v := <-p.result:
		switch m := v.(type) {
		case *protocol.ReconcileReport:
			rep.Started, rep.Completed, rep.Evidence = m.Started, m.Completed && !m.Failed, m.Evidence
			if m.Failed && m.Error != nil {
				rep.Evidence = append(rep.Evidence, fmt.Sprintf("agent-error:%s: %s", m.Error.Code, m.Error.Summary))
			}
			return rep, nil
		case *protocol.StepRejected:
			return rep, fmt.Errorf("agent refused reconcile: %s: %s", m.Code, m.Message)
		}
		return rep, fmt.Errorf("unexpected answer %T", v)
	case <-s.Done():
		return rep, fmt.Errorf("%w: session ended", jobs.ErrDisconnected)
	case <-ctx.Done():
		return rep, fmt.Errorf("%w: %w", jobs.ErrDisconnected, ctx.Err())
	}
}

func requestID(prefix, jobID string, step int) (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s_%d_%s", prefix, jobID, step, hex.EncodeToString(b)), nil
}

func capList(caps []protocol.Capability) string {
	if len(caps) == 0 {
		return "nothing"
	}
	parts := make([]string, len(caps))
	for i, c := range caps {
		parts[i] = fmt.Sprintf("%s v%d", c.Name, c.Version)
	}
	return strings.Join(parts, ", ")
}

func short(node string) string {
	if len(node) > 12 {
		return node[:12]
	}
	return node
}

package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// KindNodeRecheck is a read-only job run when a node leaves maintenance
// (NOD-008): the agent answering is the node's agent, and the host reports
// its basic values (uptime, load, memory, root filesystem).
const KindNodeRecheck = "node.recheck"

// CapHostHealth is the agent's read-only host.health action.
const CapHostHealth = "host.health"

// Re-check outcomes.
const (
	// RecheckPassed: the agent is the node's and every host value was read.
	RecheckPassed = "passed"
	// RecheckFailed: another agent answered, or a step failed.
	RecheckFailed = "failed"
	// RecheckInconclusive: the identity matched but some host values could
	// not be read (or the agent cannot read them at all).
	RecheckInconclusive = "inconclusive"
)

// RecheckResult is what a node.recheck job found.
type RecheckResult struct {
	State           string `json:"state"`
	IdentityMatches bool   `json:"identityMatches"`
	// Health is the host.health result as the agent sent it (values it
	// could not read are null).
	Health json.RawMessage `json:"health,omitempty"`
	Detail string          `json:"detail,omitempty"`
}

// RecheckHandler implements node.recheck. Record receives the result once,
// from the step that decides it.
type RecheckHandler struct {
	Runner *StepRunner
	Record func(ctx context.Context, jobID, node string, r RecheckResult)
}

func (h *RecheckHandler) Kind() string { return KindNodeRecheck }

func (h *RecheckHandler) Plan(_ context.Context, job *jobs.Job) ([]jobs.StepSpec, error) {
	if job.Node == "" {
		return nil, &jobs.StepError{Code: "NODE_REQUIRED", Summary: "node.recheck needs the node fingerprint in \"node\""}
	}
	return []jobs.StepSpec{{Name: "check agent identity", Retryable: true}, {Name: "read host basics", Retryable: true}}, nil
}

func (h *RecheckHandler) Recheck(context.Context, *jobs.Job, *jobs.Step) error { return nil }

func (h *RecheckHandler) record(ctx context.Context, job *jobs.Job, r RecheckResult) {
	if h.Record != nil {
		h.Record(ctx, job.ID, job.Node, r)
	}
}

func (h *RecheckHandler) Execute(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter) error {
	switch step.Index {
	case 0:
		res, err := h.Runner.Execute(ctx, job, step, rep, Action{Name: protocol.CapAgentInfo, Version: 1})
		if err != nil {
			if !errors.Is(err, jobs.ErrDisconnected) {
				h.record(ctx, job, RecheckResult{State: RecheckFailed, Detail: "agent.info: " + err.Error()})
			}
			return err
		}
		var info AgentInfo
		if json.Unmarshal(res, &info) != nil || info.Fingerprint != job.Node {
			h.record(ctx, job, RecheckResult{State: RecheckFailed, Detail: fmt.Sprintf("the agent answering reports %s, not this node", short(info.Fingerprint))})
			return &jobs.StepError{Code: "IDENTITY_MISMATCH", Summary: "the agent answering is not this node's agent"}
		}
		return nil
	default:
		res, err := h.Runner.Execute(ctx, job, step, rep, Action{Name: CapHostHealth, Version: 1})
		var se *jobs.StepError
		switch {
		case errors.As(err, &se) && se.Code == CodeCapabilityUnsupported:
			h.record(ctx, job, RecheckResult{State: RecheckInconclusive, IdentityMatches: true, Detail: "the agent cannot read host values (no host.health); upgrade the agent"})
			return nil
		case err != nil:
			if !errors.Is(err, jobs.ErrDisconnected) {
				h.record(ctx, job, RecheckResult{State: RecheckFailed, IdentityMatches: true, Detail: "host.health: " + err.Error()})
			}
			return err
		}
		r := RecheckResult{State: RecheckPassed, IdentityMatches: true, Health: res}
		if missing := missingHealth(res); missing != "" {
			r.State, r.Detail = RecheckInconclusive, "the host did not report "+missing
		}
		rep.Log("host basics read: " + string(res))
		h.record(ctx, job, r)
		return nil
	}
}

// missingHealth names the first host value that is absent or null.
func missingHealth(res json.RawMessage) string {
	var v map[string]json.RawMessage
	if json.Unmarshal(res, &v) != nil {
		return "a readable result"
	}
	for _, k := range []string{"uptimeSeconds", "load", "memory", "rootFilesystem"} {
		if raw, ok := v[k]; !ok || string(raw) == "null" {
			return k
		}
	}
	return ""
}

func (h *RecheckHandler) Rollback(context.Context, *jobs.Job, *jobs.Step, jobs.Reporter) error {
	return nil // read-only
}

func (h *RecheckHandler) Reconcile(ctx context.Context, job *jobs.Job, step *jobs.Step) (jobs.StepReport, error) {
	return h.Runner.Reconcile(ctx, job, step)
}

// Verify only checks that both steps ran: the outcome itself is the
// recorded result, which may be inconclusive without the job failing.
func (h *RecheckHandler) Verify(_ context.Context, job *jobs.Job) (jobs.CompletionReport, error) {
	return jobs.CompletionReport{Checks: []jobs.Check{{Name: "recheck-steps-ran", Required: true, Passed: len(job.Steps) == 2}}}, nil
}

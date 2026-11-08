package nodes

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// KindNodeInspect is a read-only job: ask a node's agent who it is.
const KindNodeInspect = "node.inspect"

// AgentInfo is the result of the agent.info action.
type AgentInfo struct {
	AgentVersion string `json:"agentVersion"`
	Fingerprint  string `json:"fingerprint"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Hostname     string `json:"hostname"`
}

// InspectHandler implements node.inspect. Its completion check is that the
// agent answering is the node the job names; asking again during Verify is
// answered from the agent's journal, so nothing runs twice.
type InspectHandler struct {
	Runner *StepRunner
}

func (h *InspectHandler) Kind() string { return KindNodeInspect }

func (h *InspectHandler) Plan(_ context.Context, job *jobs.Job) ([]jobs.StepSpec, error) {
	if job.Node == "" {
		return nil, &jobs.StepError{Code: "NODE_REQUIRED", Summary: "node.inspect needs the node fingerprint in \"node\""}
	}
	if len(job.Params) > 0 && string(job.Params) != "null" && string(job.Params) != "{}" {
		return nil, &jobs.StepError{Code: "INVALID_PARAMS", Summary: "node.inspect takes no params"}
	}
	return []jobs.StepSpec{{Name: "collect agent info", Retryable: true}}, nil
}

func (h *InspectHandler) Recheck(context.Context, *jobs.Job, *jobs.Step) error { return nil }

func (h *InspectHandler) Execute(ctx context.Context, job *jobs.Job, step *jobs.Step, rep jobs.Reporter) error {
	res, err := h.Runner.Execute(ctx, job, step, rep, Action{Name: protocol.CapAgentInfo, Version: 1})
	if err != nil {
		return err
	}
	var info AgentInfo
	if err := json.Unmarshal(res, &info); err != nil {
		return &jobs.StepError{Code: "AGENT_RESULT_INVALID", Summary: "agent.info result is not readable: " + err.Error()}
	}
	rep.Log(fmt.Sprintf("agent %s on %s (%s/%s)", info.AgentVersion, info.Hostname, info.OS, info.Arch))
	return nil
}

func (h *InspectHandler) Rollback(context.Context, *jobs.Job, *jobs.Step, jobs.Reporter) error {
	return nil // read-only
}

func (h *InspectHandler) Reconcile(ctx context.Context, job *jobs.Job, step *jobs.Step) (jobs.StepReport, error) {
	return h.Runner.Reconcile(ctx, job, step)
}

func (h *InspectHandler) Verify(ctx context.Context, job *jobs.Job) (jobs.CompletionReport, error) {
	if len(job.Steps) == 0 {
		return jobs.CompletionReport{}, fmt.Errorf("node.inspect has no steps")
	}
	res, err := h.Runner.Execute(ctx, job, &job.Steps[0], discard{}, Action{Name: protocol.CapAgentInfo, Version: 1})
	if err != nil {
		return jobs.CompletionReport{}, err
	}
	var info AgentInfo
	check := jobs.Check{Name: "agent-identity-matches-node", Required: true}
	if err := json.Unmarshal(res, &info); err != nil {
		check.Detail = "agent.info result is not readable"
	} else {
		check.Passed = info.Fingerprint == job.Node
		check.Detail = fmt.Sprintf("agent reports %s, job names %s", short(info.Fingerprint), short(job.Node))
	}
	return jobs.CompletionReport{Checks: []jobs.Check{check}}, nil
}

type discard struct{}

func (discard) Log(string)      {}
func (discard) Progress(int)    {}
func (discard) Heartbeat()      {}
func (discard) Evidence(string) {}

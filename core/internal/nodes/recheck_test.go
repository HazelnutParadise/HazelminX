package nodes

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

var fullHealth = map[string]any{"collectedAt": "2026-11-12T00:00:00Z", "uptimeSeconds": 3600,
	"load": map[string]any{"one": 0.1, "five": 0.2, "fifteen": 0.3}, "memory": map[string]any{"totalBytes": 8 << 30, "availableBytes": 4 << 30},
	"rootFilesystem": map[string]any{"totalBytes": 100 << 30, "freeBytes": 40 << 30}}

// runRecheck plays the node.recheck job's steps against a fake agent that
// answers agent.info with fp and host.health with health (nil: the agent
// does not declare host.health).
func runRecheck(t *testing.T, fp func(testNode) string, health map[string]any) (RecheckResult, error) {
	t.Helper()
	core := startCore(t, HubOptions{})
	caps := []protocol.Capability{{Name: protocol.CapJobs, Version: JobsProtocolVersion}, {Name: protocol.CapAgentInfo, Version: 1}}
	if health != nil {
		caps = append(caps, protocol.Capability{Name: CapHostHealth, Version: 1})
	}
	a, n := connect(t, core, agentOpts{caps: caps})
	seq := uint64(0)
	a.serve(t, func(m any) []any {
		s, ok := m.(*protocol.StepExecute)
		if !ok {
			return nil
		}
		seq++
		if s.Action == protocol.CapAgentInfo {
			return []any{event(s, seq, protocol.EventCompleted, map[string]any{"fingerprint": fp(n), "agentVersion": "0.1.0"})}
		}
		return []any{event(s, seq, protocol.EventCompleted, health)}
	})
	var mu sync.Mutex
	var got []RecheckResult
	h := &RecheckHandler{Runner: NewStepRunner(core.hub), Record: func(_ context.Context, jobID, node string, r RecheckResult) {
		if jobID != "req_recheck" || node != n.fp {
			t.Errorf("recorded for %s %s", jobID, node)
		}
		mu.Lock()
		got = append(got, r)
		mu.Unlock()
	}}
	job := &jobs.Job{ID: "job_recheck", RequestID: "req_recheck", Kind: KindNodeRecheck, Node: n.fp, ControlGeneration: 1}
	specs, err := h.Plan(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	for i, sp := range specs {
		job.Steps = append(job.Steps, jobs.Step{Index: i, Name: sp.Name, Retryable: sp.Retryable})
	}
	var runErr error
	for i := range job.Steps {
		if runErr = h.Execute(context.Background(), job, &job.Steps[i], &recorder{}); runErr != nil {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("recorded %d results (error %v)", len(got), runErr)
	}
	return got[0], runErr
}

func TestRecheckPassesWithIdentityAndHostBasics(t *testing.T) {
	r, err := runRecheck(t, func(n testNode) string { return n.fp }, fullHealth)
	if err != nil || r.State != RecheckPassed || !r.IdentityMatches {
		t.Fatalf("%+v %v", r, err)
	}
	var h map[string]any
	if json.Unmarshal(r.Health, &h) != nil || h["uptimeSeconds"] == nil {
		t.Fatalf("health values not kept: %s", r.Health)
	}
}

func TestRecheckFailsWhenAnotherAgentAnswers(t *testing.T) {
	r, err := runRecheck(t, func(testNode) string { return "someone-else" }, fullHealth)
	if err == nil || r.State != RecheckFailed || r.IdentityMatches {
		t.Fatalf("%+v %v", r, err)
	}
}

// A value the host could not give is not invented: the check is
// inconclusive, not passed.
func TestRecheckIsInconclusiveWithoutAllHostBasics(t *testing.T) {
	partial := map[string]any{"collectedAt": "2026-11-12T00:00:00Z", "uptimeSeconds": nil, "load": nil, "memory": nil,
		"rootFilesystem": map[string]any{"totalBytes": 1, "freeBytes": 1}}
	r, err := runRecheck(t, func(n testNode) string { return n.fp }, partial)
	if err != nil || r.State != RecheckInconclusive {
		t.Fatalf("partial: %+v %v", r, err)
	}
	r, err = runRecheck(t, func(n testNode) string { return n.fp }, nil)
	if err != nil || r.State != RecheckInconclusive || r.Detail == "" {
		t.Fatalf("agent without host.health: %+v %v", r, err)
	}
}

// An agent too old for jobs v2 cannot be checked; that is inconclusive
// (upgrade the agent), not a failed identity.
func TestRecheckOfAnOldAgentIsInconclusive(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{caps: []protocol.Capability{{Name: protocol.CapJobs, Version: 1}, {Name: protocol.CapAgentInfo, Version: 1}}})
	a.serve(t, func(any) []any { return nil })
	var got []RecheckResult
	h := &RecheckHandler{Runner: NewStepRunner(core.hub), Record: func(_ context.Context, _, _ string, r RecheckResult) { got = append(got, r) }}
	job := &jobs.Job{ID: "job_old", RequestID: "req_old", Kind: KindNodeRecheck, Node: n.fp, Steps: []jobs.Step{{Index: 0}, {Index: 1}}}
	if err := h.Execute(context.Background(), job, &job.Steps[0], &recorder{}); err != nil {
		t.Fatalf("step 0: %v", err)
	}
	if len(got) != 1 || got[0].State != RecheckInconclusive {
		t.Fatalf("%+v", got)
	}
}

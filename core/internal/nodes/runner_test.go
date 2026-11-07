package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// recorder is a jobs.Reporter that keeps what it was told.
type recorder struct {
	mu       sync.Mutex
	logs     []string
	evidence []string
	beats    int
}

func (r *recorder) Log(l string)      { r.mu.Lock(); r.logs = append(r.logs, l); r.mu.Unlock() }
func (r *recorder) Progress(int)      {}
func (r *recorder) Heartbeat()        { r.mu.Lock(); r.beats++; r.mu.Unlock() }
func (r *recorder) Evidence(e string) { r.mu.Lock(); r.evidence = append(r.evidence, e); r.mu.Unlock() }

func testJob(node string) (*jobs.Job, *jobs.Step) {
	j := &jobs.Job{ID: "job_test", Node: node, ControlGeneration: 4, Steps: []jobs.Step{{Index: 0, Name: "collect agent info", Retryable: true}}}
	return j, &j.Steps[0]
}

func connect(t *testing.T, core *testCore, o agentOpts) (*fakeAgent, testNode) {
	t.Helper()
	n := newTestNode(t, core.pki)
	a, err := dialAgent(t, core, core.url, n, o)
	if err != nil || a.ready == nil {
		t.Fatalf("connect: %v %+v", err, a)
	}
	return a, n
}

func event(m *protocol.StepExecute, seq uint64, kind string, result any) *protocol.Event {
	ev := &protocol.Event{Type: protocol.TypeEvent, Seq: seq, Kind: kind, JobID: m.JobID, Step: m.Step, RequestID: m.RequestID,
		At: time.Now().UTC().Truncate(time.Second), Evidence: []string{"agent://journal/1"}}
	if result != nil {
		ev.Result, _ = json.Marshal(result)
	}
	return ev
}

func TestRunnerExecuteSendsStepAndReturnsResult(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{})
	var got *protocol.StepExecute
	a.serve(t, func(m any) []any {
		if s, ok := m.(*protocol.StepExecute); ok {
			got = s
			return []any{event(s, 1, protocol.EventStarted, nil), event(s, 2, protocol.EventCompleted, map[string]string{"fingerprint": n.fp})}
		}
		return nil
	})
	job, step := testJob(n.fp)
	rep := &recorder{}
	res, err := NewStepRunner(core.hub).Execute(context.Background(), job, step, rep, Action{Name: protocol.CapAgentInfo, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	var info AgentInfo
	if json.Unmarshal(res, &info) != nil || info.Fingerprint != n.fp {
		t.Fatalf("result %s", res)
	}
	if got.TargetNode != n.fp || got.ControlGeneration != 4 || got.Action != protocol.CapAgentInfo || got.ActionVersion != 1 ||
		!got.Retryable || got.JobID != job.ID || !got.ExpiresAt.After(time.Now()) {
		t.Fatalf("step_execute %+v", got)
	}
	if len(rep.evidence) != 1 || rep.beats == 0 {
		t.Fatalf("reporter %+v", rep)
	}
}

// NOD-011: an action the agent did not declare is refused before anything
// is sent.
func TestRunnerRefusesUndeclaredCapabilityWithoutSending(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{caps: []protocol.Capability{{Name: protocol.CapJobs, Version: 1}}})
	var sent atomic.Int32
	a.serve(t, func(m any) []any {
		if _, ok := m.(*protocol.StepExecute); ok {
			sent.Add(1)
		}
		return nil
	})
	job, step := testJob(n.fp)
	_, err := NewStepRunner(core.hub).Execute(context.Background(), job, step, &recorder{}, Action{Name: protocol.CapAgentInfo, Version: 1})
	var se *jobs.StepError
	if !errors.As(err, &se) || se.Code != CodeCapabilityUnsupported {
		t.Fatalf("want CAPABILITY_UNSUPPORTED, got %v", err)
	}
	mustContain(t, se.Summary, "jobs v1")
	// A newer action version than declared is also refused.
	_, err = NewStepRunner(core.hub).Execute(context.Background(), job, step, &recorder{}, Action{Name: protocol.CapJobs, Version: 2})
	if !errors.As(err, &se) || se.Code != CodeCapabilityUnsupported {
		t.Fatalf("want CAPABILITY_UNSUPPORTED for v2, got %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if sent.Load() != 0 {
		t.Fatalf("%d step_execute sent to an agent that cannot run them", sent.Load())
	}
}

func TestRunnerNodeOfflineIsKnownRetryableFailure(t *testing.T) {
	core := startCore(t, HubOptions{})
	job, step := testJob("0000")
	_, err := NewStepRunner(core.hub).Execute(context.Background(), job, step, &recorder{}, Action{Name: protocol.CapAgentInfo, Version: 1})
	var se *jobs.StepError
	if !errors.As(err, &se) || se.Code != CodeNodeOffline || !se.Retryable || errors.Is(err, jobs.ErrDisconnected) {
		t.Fatalf("want NODE_OFFLINE, got %v", err)
	}
}

func TestRunnerMapsAgentRefusals(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{})
	code := make(chan string, 1)
	a.serve(t, func(m any) []any {
		if s, ok := m.(*protocol.StepExecute); ok {
			return []any{&protocol.StepRejected{Type: protocol.TypeStepRejected, RequestID: s.RequestID, JobID: s.JobID, Step: s.Step, Code: <-code, Message: "no"}}
		}
		return nil
	})
	job, step := testJob(n.fp)
	r := NewStepRunner(core.hub)
	code <- protocol.StepRejectExpired
	_, err := r.Execute(context.Background(), job, step, &recorder{}, Action{Name: protocol.CapAgentInfo, Version: 1})
	var se *jobs.StepError
	if !errors.As(err, &se) || se.Code != protocol.StepRejectExpired {
		t.Fatalf("want EXPIRED step error, got %v", err)
	}
	// The agent's journal says the step began and never finished: the
	// outcome is unknown, so the job must wait for reconciliation.
	code <- protocol.StepRejectOutcomeUnknown
	_, err = r.Execute(context.Background(), job, step, &recorder{}, Action{Name: protocol.CapAgentInfo, Version: 1})
	if !errors.Is(err, jobs.ErrDisconnected) {
		t.Fatalf("want ErrDisconnected for OUTCOME_UNKNOWN, got %v", err)
	}
}

func TestRunnerSessionLossMidStepIsUnknown(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{})
	a.serve(t, func(m any) []any {
		if s, ok := m.(*protocol.StepExecute); ok {
			go func() { time.Sleep(50 * time.Millisecond); a.conn.CloseNow() }()
			return []any{event(s, 1, protocol.EventStarted, nil)}
		}
		return nil
	})
	job, step := testJob(n.fp)
	_, err := NewStepRunner(core.hub).Execute(context.Background(), job, step, &recorder{}, Action{Name: protocol.CapAgentInfo, Version: 1})
	if !errors.Is(err, jobs.ErrDisconnected) {
		t.Fatalf("want ErrDisconnected, got %v", err)
	}
}

func TestRunnerReconcileReadsAgentRecord(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{})
	a.serve(t, func(m any) []any {
		if s, ok := m.(*protocol.StepReconcile); ok {
			return []any{&protocol.ReconcileReport{Type: protocol.TypeReconcileReport, RequestID: s.RequestID, JobID: s.JobID, Step: s.Step,
				Started: true, Failed: true, Evidence: []string{"agent://journal/9"}, Error: &protocol.StepFailure{Code: "X", Summary: "broke"}}}
		}
		return nil
	})
	job, step := testJob(n.fp)
	rep, err := NewStepRunner(core.hub).Reconcile(context.Background(), job, step)
	if err != nil {
		t.Fatal(err)
	}
	// A failed record is not "completed"; the engine then retries only a
	// retryable step and never treats it as done.
	if !rep.Started || rep.Completed || len(rep.Evidence) != 2 {
		t.Fatalf("report %+v", rep)
	}
}

// journalAgent behaves like the real agent's journal for one node: it runs
// a step at most once, answers repeats with the recorded event, and answers
// reconciles from the record.
type journalAgent struct {
	mu       sync.Mutex
	fp       string
	ran      int
	seq      uint64
	done     map[int]*protocol.Event
	started  map[int]bool
	dropNext bool // close the session right after running, before reporting
	conn     func()
}

func (j *journalAgent) handle(m any) []any {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch s := m.(type) {
	case *protocol.StepExecute:
		if ev, ok := j.done[s.Step]; ok {
			return []any{ev}
		}
		j.ran++
		j.seq++
		j.started[s.Step] = true
		start := event(s, j.seq, protocol.EventStarted, nil)
		j.seq++
		ev := event(s, j.seq, protocol.EventCompleted, AgentInfo{Fingerprint: j.fp, AgentVersion: "fake-1"})
		j.done[s.Step] = ev
		if j.dropNext {
			j.dropNext = false
			go j.conn()
			return []any{start}
		}
		return []any{start, ev}
	case *protocol.StepReconcile:
		_, done := j.done[s.Step]
		return []any{&protocol.ReconcileReport{Type: protocol.TypeReconcileReport, RequestID: s.RequestID, JobID: s.JobID, Step: s.Step,
			Started: j.started[s.Step], Completed: done}}
	}
	return nil
}

func newEngine(t *testing.T) *jobs.Engine {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return jobs.New(jobs.NewSQLStore(st.DB), jobs.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

// NOD-002 / T-028 / SEC-008: the agent did the step but the connection
// dropped before it reported. The job becomes unknown; when the agent
// reconnects the core reconciles first, accepts the recorded completion and
// finishes the job without running the step again.
func TestReconnectReconcilesInsteadOfRerunning(t *testing.T) {
	engine := newEngine(t)
	var reconciler *Reconciler
	core := startCore(t, HubOptions{OnReady: func(s *Session, inFlight []string) { reconciler.OnReady(s, inFlight) }})
	runner := NewStepRunner(core.hub)
	reconciler = &Reconciler{Engine: engine, Runner: runner, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := engine.Register(&InspectHandler{Runner: runner}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)
	defer engine.Stop()

	n := newTestNode(t, core.pki)
	ja := &journalAgent{fp: n.fp, done: map[int]*protocol.Event{}, started: map[int]bool{}, dropNext: true}
	first, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || first.ready == nil {
		t.Fatalf("%v %+v", err, first)
	}
	ja.conn = func() { time.Sleep(20 * time.Millisecond); first.conn.CloseNow() }
	first.serve(t, ja.handle)

	job, _, err := engine.Submit(ctx, jobs.SubmitRequest{RequestID: "req-1", Kind: KindNodeInspect, Node: n.fp})
	if err != nil {
		t.Fatal(err)
	}
	j, err := waitState(engine, job.ID, jobs.StateUnknown)
	if err != nil {
		t.Fatalf("job did not become unknown: %v (%s: %s)", err, j.State, j.StateReason)
	}

	second, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || second.ready == nil {
		t.Fatalf("reconnect: %v %+v", err, second)
	}
	second.serve(t, ja.handle)
	j, err = waitState(engine, job.ID, jobs.StateSucceeded)
	if err != nil {
		t.Fatalf("job did not succeed after reconcile: %v (%s: %s)", err, j.State, j.StateReason)
	}
	ja.mu.Lock()
	defer ja.mu.Unlock()
	if ja.ran != 1 {
		t.Fatalf("step ran %d times; reconnect must not re-run a completed step", ja.ran)
	}
	if j.Completion == nil || !j.Completion.RequiredPassed() {
		t.Fatalf("completion %+v", j.Completion)
	}
}

func waitState(e *jobs.Engine, id string, st jobs.State) (*jobs.Job, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return e.Wait(ctx, id, func(j *jobs.Job) bool { return j.State == st })
}

package nodereg

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
)

type fakeSubmitter struct {
	mu   sync.Mutex
	reqs []string
}

func (f *fakeSubmitter) submit(_ context.Context, fp, requestID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, requestID)
	return "job_" + requestID, nil
}

func (f *fakeSubmitter) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.reqs) }

// NOD-008: after maintenance the node is checked again (identity and host
// basics) once it reconnects, and the result is shown with its values.
func TestMaintenanceEndRunsARecheck(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub := &fakeSubmitter{}
	e.reg.opts.SubmitRecheck = sub.submit
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.reg.SetMaintenance(ctx, n.ID, true, "", "usr_owner")
	e.advance(time.Second)
	e.reg.SetMaintenance(ctx, n.ID, false, "", "usr_owner")
	got, _ := e.reg.GetNode(ctx, n.ID)
	if got.Maintenance.Recheck == nil || got.Maintenance.Recheck.State != RecheckPending || sub.count() != 0 {
		t.Fatalf("before reconnect: %+v %d", got.Maintenance.Recheck, sub.count())
	}
	e.advance(time.Second)
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: n.Certificate.Serial, at: e.now()})
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: n.Certificate.Serial, at: e.now()})
	if sub.count() != 1 {
		t.Fatalf("submitted %d checks", sub.count())
	}
	got, _ = e.reg.GetNode(ctx, n.ID)
	rc := got.Maintenance.Recheck
	if rc.State != RecheckChecking || rc.JobID == "" || !slices.Contains(got.PendingSync, SyncRecheck) {
		t.Fatalf("while checking: %+v %v", rc, got.PendingSync)
	}
	health := json.RawMessage(`{"uptimeSeconds":5}`)
	e.reg.RecordRecheck(ctx, rc.JobID, h.fp, nodes.RecheckResult{State: nodes.RecheckPassed, IdentityMatches: true, Health: health})
	got, _ = e.reg.GetNode(ctx, n.ID)
	rc = got.Maintenance.Recheck
	if rc.State != RecheckPassed || string(rc.Health) != string(health) || rc.FinishedAt == nil || slices.Contains(got.PendingSync, SyncRecheck) {
		t.Fatalf("after the check: %+v %v", rc, got.PendingSync)
	}
	// A late result of an older job does not overwrite the current one.
	e.reg.RecordRecheck(ctx, "job_old", h.fp, nodes.RecheckResult{State: nodes.RecheckFailed})
	if got, _ := e.reg.GetNode(ctx, n.ID); got.Maintenance.Recheck.State != RecheckPassed {
		t.Fatal("stale result recorded")
	}
}

// A failed check can be run again; a check whose job ended without a
// result says what the job did, without claiming more.
func TestRecheckCanBeRunAgainAndEndedJobsAreReadHonestly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub := &fakeSubmitter{}
	jobsSeen := &stateJobs{states: map[string]jobs.State{}}
	e.reg.opts.SubmitRecheck, e.reg.opts.Jobs = sub.submit, jobsSeen
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	if _, err := e.reg.RunRecheck(ctx, n.ID, "usr_owner"); err != nil {
		t.Fatal(err)
	}
	if sub.count() != 0 {
		t.Fatal("offline node: the check must wait for the next connection")
	}
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	if _, err := e.reg.RunRecheck(ctx, n.ID, "usr_owner"); err != nil || sub.count() != 1 {
		t.Fatalf("online re-run: %v %d", err, sub.count())
	}
	got, _ := e.reg.GetNode(ctx, n.ID)
	first := got.Maintenance.Recheck.JobID
	for st, want := range map[jobs.State]string{jobs.StateRunning: RecheckChecking, jobs.StateFailed: RecheckFailed,
		jobs.StateNeedsAttention: RecheckFailed, jobs.StateCancelled: RecheckInconclusive, jobs.StateUnknown: RecheckInconclusive} {
		jobsSeen.set(first, st)
		if got, _ := e.reg.GetNode(ctx, n.ID); got.Maintenance.Recheck.State != want {
			t.Errorf("job %s: state %s, want %s", st, got.Maintenance.Recheck.State, want)
		}
	}
	if _, err := e.reg.RunRecheck(ctx, n.ID, "usr_owner"); err != nil || sub.count() != 2 || sub.reqs[0] == sub.reqs[1] {
		t.Fatalf("second re-run: %v %v", err, sub.reqs)
	}
	e.reg.RevokeNode(ctx, n.ID, "", "usr_owner")
	if _, err := e.reg.RunRecheck(ctx, n.ID, "usr_owner"); !errors.Is(err, ErrState) {
		t.Fatalf("revoked node: %v", err)
	}
}

// A re-check and a certificate rotation on the same connection both happen.
func TestRecheckIsNotSkippedByARotation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub := &fakeSubmitter{}
	e.reg.opts.SubmitRecheck = sub.submit
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.reg.SetMaintenance(ctx, n.ID, true, "", "usr_owner")
	e.reg.SetMaintenance(ctx, n.ID, false, "", "usr_owner")
	e.reg.RotateCertificate(ctx, n.ID, "usr_owner")
	got, _ := e.reg.GetNode(ctx, n.ID)
	e.advance(time.Second)
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: got.Certificate.Rotation.NewSerial, at: e.now()})
	if sub.count() != 1 {
		t.Fatalf("re-check skipped by the rotation: %d", sub.count())
	}
}

type stateJobs struct {
	mu     sync.Mutex
	states map[string]jobs.State
}

func (s *stateJobs) set(id string, st jobs.State) { s.mu.Lock(); s.states[id] = st; s.mu.Unlock() }
func (s *stateJobs) List(context.Context, jobs.ListOptions) ([]*jobs.Job, error) {
	return nil, nil
}
func (s *stateJobs) Get(_ context.Context, id string) (*jobs.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[id]
	if !ok {
		st = jobs.StateQueued
	}
	return &jobs.Job{ID: id, State: st}, nil
}

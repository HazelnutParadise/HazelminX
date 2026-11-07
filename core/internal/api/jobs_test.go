package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// blockingHandler holds every job in its single step until released, so the
// API tests can observe running/queued states.
type blockingHandler struct {
	release chan struct{}
}

func (b *blockingHandler) Kind() string { return "demo.install" }
func (b *blockingHandler) Plan(context.Context, *jobs.Job) ([]jobs.StepSpec, error) {
	return []jobs.StepSpec{{Name: "install", RollbackMethod: "uninstall", Cancellable: true}}, nil
}
func (b *blockingHandler) Recheck(context.Context, *jobs.Job, *jobs.Step) error { return nil }
func (b *blockingHandler) Execute(ctx context.Context, _ *jobs.Job, _ *jobs.Step, rep jobs.Reporter) error {
	rep.Log("working")
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *blockingHandler) Rollback(context.Context, *jobs.Job, *jobs.Step, jobs.Reporter) error {
	return nil
}
func (b *blockingHandler) Reconcile(_ context.Context, _ *jobs.Job, s *jobs.Step) (jobs.StepReport, error) {
	return jobs.StepReport{Index: s.Index}, nil
}
func (b *blockingHandler) Verify(context.Context, *jobs.Job) (jobs.CompletionReport, error) {
	return jobs.CompletionReport{Checks: []jobs.Check{{Name: "installed", Required: true, Passed: true}}}, nil
}

func newJobsServer(t *testing.T) (http.Handler, *jobs.Engine, *blockingHandler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	bh := &blockingHandler{release: make(chan struct{})}
	e := jobs.New(jobs.NewSQLStore(st.DB), jobs.Options{PollInterval: 10 * time.Millisecond})
	if err := e.Register(bh); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	e.Start(ctx)
	t.Cleanup(func() { cancel(); e.Stop(); st.Close() })
	s := New(st, nil)
	s.RegisterJobs(e)
	return s.Handler(), e, bh
}

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: non-JSON body %q", method, path, rec.Body)
		}
	}
	return rec, out
}

func TestJobsAPISubmitGetCancelFlow(t *testing.T) {
	h, e, bh := newJobsServer(t)
	rec, body := do(t, h, http.MethodPost, "/management/v1/jobs", `{"requestId":"ui-1","kind":"demo.install","params":{"pkg":"nginx"}}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body)
	}
	id := body["jobId"].(string)
	if body["deduplicated"] != false || rec.Header().Get("Location") != "/management/v1/jobs/"+id {
		t.Fatalf("submit body %v location %q", body, rec.Header().Get("Location"))
	}
	// Replay (T-025): same job, deduplicated.
	rec, body = do(t, h, http.MethodPost, "/management/v1/jobs", `{"kind":"demo.install","requestId":"ui-1","params":{"pkg":"nginx"}}`)
	if rec.Code != http.StatusAccepted || body["jobId"] != id || body["deduplicated"] != true {
		t.Fatalf("replay: %d %v", rec.Code, body)
	}
	// Same id, different content (T-026): conflict.
	rec, body = do(t, h, http.MethodPost, "/management/v1/jobs", `{"requestId":"ui-1","kind":"demo.install","params":{"pkg":"apache"}}`)
	if rec.Code != http.StatusConflict || body["code"] != "REQUEST_ID_CONFLICT" {
		t.Fatalf("conflict: %d %v", rec.Code, body)
	}
	// The caller is gone; the job keeps running.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.Wait(ctx, id, func(j *jobs.Job) bool { return j.State == jobs.StateRunning }); err != nil {
		t.Fatal(err)
	}
	rec, body = do(t, h, http.MethodGet, "/management/v1/jobs/"+id, "")
	job := body["job"].(map[string]any)
	if rec.Code != http.StatusOK || job["state"] != "running" || job["cancellable"] != true {
		t.Fatalf("get: %d %v", rec.Code, job)
	}
	rec, body = do(t, h, http.MethodGet, "/management/v1/jobs?state=running,queued", "")
	if rec.Code != http.StatusOK || len(body["jobs"].([]any)) != 1 {
		t.Fatalf("list: %d %v", rec.Code, body)
	}
	rec, body = do(t, h, http.MethodGet, "/management/v1/jobs/"+id+"/events?after=0", "")
	evs := body["events"].([]any)
	if rec.Code != http.StatusOK || len(evs) < 3 {
		t.Fatalf("events: %d %v", rec.Code, body)
	}
	lastSeq := int64(body["lastSeq"].(float64))
	rec, body = do(t, h, http.MethodGet, "/management/v1/jobs/"+id+"/events?after="+strconv.FormatInt(lastSeq, 10), "")
	if rec.Code != http.StatusOK || len(body["events"].([]any)) != 0 {
		t.Fatalf("resume from last seq must be empty: %v", body)
	}
	rec, body = do(t, h, http.MethodPost, "/management/v1/jobs/"+id+"/cancel", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	if _, err := e.Wait(ctx, id, func(j *jobs.Job) bool { return j.State == jobs.StateCancelled }); err != nil {
		t.Fatal(err)
	}
	rec, body = do(t, h, http.MethodPost, "/management/v1/jobs/"+id+"/cancel", "")
	if rec.Code != http.StatusConflict || body["code"] != "JOB_NOT_CANCELLABLE" {
		t.Fatalf("cancel finished: %d %v", rec.Code, body)
	}
	close(bh.release)
}

func TestJobsAPIErrors(t *testing.T) {
	h, _, _ := newJobsServer(t)
	rec, body := do(t, h, http.MethodGet, "/management/v1/jobs/job_nope", "")
	if rec.Code != http.StatusNotFound || body["code"] != "JOB_NOT_FOUND" {
		t.Fatalf("404: %d %v", rec.Code, body)
	}
	rec, body = do(t, h, http.MethodPost, "/management/v1/jobs", `{"requestId":"x","kind":"not.registered"}`)
	if rec.Code != http.StatusBadRequest || body["code"] != "JOB_KIND_UNKNOWN" {
		t.Fatalf("unknown kind: %d %v", rec.Code, body)
	}
	rec, body = do(t, h, http.MethodPost, "/management/v1/jobs", `{"kind":"demo.install"}`)
	if rec.Code != http.StatusBadRequest || body["code"] != "INVALID_REQUEST" {
		t.Fatalf("missing requestId: %d %v", rec.Code, body)
	}
	rec, body = do(t, h, http.MethodPost, "/management/v1/jobs", `{"requestId":"x","kind":"demo.install","typo":1}`)
	if rec.Code != http.StatusBadRequest || body["code"] != "INVALID_REQUEST" {
		t.Fatalf("unknown field must be rejected, not dropped: %d %v", rec.Code, body)
	}
	rec, body = do(t, h, http.MethodPost, "/management/v1/jobs", `not json`)
	if rec.Code != http.StatusBadRequest || body["code"] != "INVALID_REQUEST" {
		t.Fatalf("bad json: %d %v", rec.Code, body)
	}
	rec, body = do(t, h, http.MethodGet, "/management/v1/jobs?state=bogus", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad state: %d %v", rec.Code, body)
	}
	rec, body = do(t, h, http.MethodGet, "/management/v1", "")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	found := false
	for _, op := range body["operations"].([]any) {
		if op.(map[string]any)["path"] == "/management/v1/jobs" {
			found = true
		}
	}
	if !found || body["jobKinds"] == nil {
		t.Fatalf("overview must list job operations and kinds: %v", body)
	}
}

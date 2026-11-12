package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
)

// A change that already happened is audited even when the caller has gone
// away: the client disconnecting right after a revocation or an approval
// cancels the request context, and the audit row must still be written.
func TestNodeAuditIsWrittenAfterTheRequestIsCancelled(t *testing.T) {
	hs, reg := nodesHarness(t)
	owner := hs.bootstrap()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/management/v1/nodes/nod_x/revoke", nil).WithContext(ctx)
	(&nodeRoutes{s: hs.srv, reg: reg}).audit(c, "nodes.revoke", "node", "nod_cancelled", iam.Global, map[string]any{"reason": "test"})

	if !hasAudit(owner, "nodes.revoke", "nod_cancelled") {
		t.Fatal("audit row missing after the request was cancelled")
	}
}

// jobsHook runs the test's function where the registry lists a node's
// jobs, which happens after maintenance is already switched on.
type jobsHook struct{ list func() error }

func (j jobsHook) List(context.Context, jobs.ListOptions) ([]*jobs.Job, error) { return nil, j.list() }
func (jobsHook) Get(_ context.Context, id string) (*jobs.Job, error) {
	return &jobs.Job{ID: id, State: jobs.StateSucceeded}, nil
}

// The caller disconnects in the middle of a change, after the registry
// committed it: the handler finishes the change and writes the audit row.
func TestNodeChangeFinishesAndIsAuditedWhenTheCallerLeavesMidway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hs, reg := nodesHarnessWith(t, jobsHook{list: func() error { cancel(); return nil }})
	owner := hs.bootstrap()
	id := enrolledNode(t, owner, reg)
	owner.ctx = ctx
	r := owner.do(http.MethodPut, "/management/v1/nodes/"+id+"/maintenance", map[string]any{"enabled": true, "reason": "kernel"}, nil)
	owner.ctx = nil
	if r.Code != http.StatusOK {
		t.Fatalf("change that went through answered %d %s", r.Code, r.Raw)
	}
	if !hasAudit(owner, "nodes.maintenance", id) {
		t.Fatal("maintenance switched on but not audited after the caller left")
	}
}

// The change is committed but its result cannot be read back: the call
// fails, and the change that happened is still audited.
func TestNodeChangeIsAuditedWhenItsResultCannotBeReadBack(t *testing.T) {
	hs, reg := nodesHarnessWith(t, jobsHook{list: func() error { return errors.New("jobs store unavailable") }})
	owner := hs.bootstrap()
	id := enrolledNode(t, owner, reg)
	r := owner.do(http.MethodPut, "/management/v1/nodes/"+id+"/maintenance", map[string]any{"enabled": true, "reason": "kernel"}, nil)
	if r.Code != http.StatusInternalServerError {
		t.Fatalf("read-back failure answered %d %s", r.Code, r.Raw)
	}
	if n, err := reg.GetNode(context.Background(), id); err != nil || !n.Maintenance.Enabled {
		t.Fatalf("maintenance not on: %+v %v", n.Maintenance, err)
	}
	if !hasAudit(owner, "nodes.maintenance", id) {
		t.Fatal("committed change not audited when its result could not be read back")
	}
}

func enrolledNode(t *testing.T, owner *client, reg *nodereg.Registry) string {
	t.Helper()
	r := owner.do(http.MethodPost, "/management/v1/node-enrollments", map[string]any{}, nil)
	enID := claimAs(t, reg, r.Body["code"].(string), "pi", "")
	r = owner.do(http.MethodPost, "/management/v1/node-enrollments/"+enID+"/approve", map[string]any{}, nil)
	if r.Code != 200 {
		t.Fatalf("approve: %d %s", r.Code, r.Raw)
	}
	return obj(r.Body["node"])["id"].(string)
}

func hasAudit(owner *client, action, id string) bool {
	r := owner.do(http.MethodGet, "/management/v1/audit-events?limit=200", nil, nil)
	for _, it := range r.Body["items"].([]any) {
		ev := obj(it)
		if ev["action"] == action && ev["resourceId"] == id {
			return true
		}
	}
	return false
}

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
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

	r := owner.do(http.MethodGet, "/management/v1/audit-events?limit=200", nil, nil)
	if r.Code != http.StatusOK || !strings.Contains(r.Raw, "nod_cancelled") {
		t.Fatalf("audit row missing after the request was cancelled: %d %s", r.Code, r.Raw)
	}
}

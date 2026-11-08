package api

import (
	"net/http"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

type fakeAgentConnections struct{}

func (fakeAgentConnections) Connections() ([]nodes.ConnectionInfo, []nodes.Rejection) {
	agent := protocol.Range{Min: 2, Max: 3}
	return []nodes.ConnectionInfo{{NodeID: "n1", Transport: nodes.TransportMTLS, ProtocolVersion: 1}},
		[]nodes.Rejection{{Code: protocol.RejectProtocolIncompatible, AgentRange: &agent, CoreRange: protocol.SupportedRange()}}
}

// T-021: an incompatible agent's refusal is readable with both protocol
// ranges, only by a signed-in caller with platform:read.
func TestAgentConnectionsShowsRefusalsWithBothRanges(t *testing.T) {
	hs := newHarness(t)
	hs.srv.RegisterAgentConnections(fakeAgentConnections{})
	if r := hs.client("192.0.2.10").do(http.MethodGet, "/management/v1/agent-connections", nil, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", r.Code)
	}
	owner := hs.bootstrap()
	r := owner.do(http.MethodGet, "/management/v1/agent-connections", nil, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("owner: %d %s", r.Code, r.Raw)
	}
	rej := r.Body["rejections"].([]any)[0].(map[string]any)
	if rej["code"] != protocol.RejectProtocolIncompatible || rej["agentProtocol"].(map[string]any)["min"] != float64(2) || rej["coreProtocol"] == nil {
		t.Fatalf("rejection %v", rej)
	}
	if r.Body["coreProtocol"] == nil || len(r.Body["connections"].([]any)) != 1 {
		t.Fatalf("body %v", r.Body)
	}
}

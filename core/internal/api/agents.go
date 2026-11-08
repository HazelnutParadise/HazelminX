package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// AgentConnections is what the agent hub reports.
type AgentConnections interface {
	Connections() ([]nodes.ConnectionInfo, []nodes.Rejection)
}

// agentConnectionsResponse is a snapshot of the agent channel, not a
// paged list: connections are one per node and refusals are capped at the
// most recent 50.
type agentConnectionsResponse struct {
	CoreProtocol protocol.Range         `json:"coreProtocol"`
	Connections  []nodes.ConnectionInfo `json:"connections"`
	Rejections   []nodes.Rejection      `json:"rejections"`
}

// RegisterAgentConnections mounts GET /management/v1/agent-connections: the
// live agent sessions (protocol version, capabilities, transport) and recent
// refused attempts with the agent's and this Core's protocol ranges, so an
// incompatible agent shows what it supports instead of silently failing
// (T-021). MountIAM must run first; reading needs platform:read.
func (s *Server) RegisterAgentConnections(src AgentConnections) {
	if s.protected == nil {
		panic("api: RegisterAgentConnections called before MountIAM")
	}
	const base = "/management/v1"
	s.handle(s.protected, base, Operation{Method: "GET", Path: base + "/agent-connections", ID: "getAgentConnections", Tag: "agents", Auth: true,
		Summary:    "connected agents (protocol, capabilities, transport) and recently refused connection attempts with both protocol ranges",
		Permission: "platform:read on the global scope",
		Responses:  []Response{{Status: 200, Body: agentConnectionsResponse{}}}},
		s.RequireGlobal(iam.ActionPlatformRead), func(c *gin.Context) {
			conns, rejected := src.Connections()
			c.JSON(http.StatusOK, agentConnectionsResponse{CoreProtocol: protocol.SupportedRange(), Connections: conns, Rejections: rejected})
		})
}

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

// RegisterAgentConnections mounts GET /management/v1/agent-connections: the
// live agent sessions (protocol version, capabilities, transport) and recent
// refused attempts with the agent's and this Core's protocol ranges, so an
// incompatible agent shows what it supports instead of silently failing
// (T-021). MountIAM must run first; reading needs platform:read.
func (s *Server) RegisterAgentConnections(src AgentConnections) {
	if s.protected == nil {
		panic("api: RegisterAgentConnections called before MountIAM")
	}
	s.protected.GET("/agent-connections", s.RequireGlobal(iam.ActionPlatformRead), func(c *gin.Context) {
		conns, rejected := src.Connections()
		c.JSON(http.StatusOK, gin.H{
			"coreProtocol": protocol.SupportedRange(),
			"connections":  conns,
			"rejections":   rejected,
		})
	})
	s.addOperation("GET", "/management/v1/agent-connections", "connected agents (protocol, capabilities, transport) and recently refused connection attempts with both protocol ranges")
}

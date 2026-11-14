package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/engines"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Container engines on a node's host (ticket 039.1, NOD-003, NOD-004,
// ENV-001, ENV-002, INS-008).

type hostEnginesResponse struct {
	// Reported is false until the node's agent sent a report; report,
	// collectedAt and receivedAt are then null.
	Reported    bool                   `json:"reported"`
	CollectedAt *time.Time             `json:"collectedAt"`
	ReceivedAt  *time.Time             `json:"receivedAt"`
	Report      *protocol.EngineReport `json:"report"`
	// LastError is the latest probe that produced no report (the report,
	// if any, is then older); null after a successful probe.
	LastError *engines.Failure `json:"lastError"`
}

// EngineProber is what the refresh route needs (engines.Prober).
type EngineProber interface {
	Probe(ctx context.Context, fingerprint string) error
}

// RegisterHostEngines mounts the container engine routes. MountIAM must
// run first.
func (s *Server) RegisterHostEngines(reg *nodereg.Registry, store *engines.Store, prober EngineProber) {
	if s.protected == nil {
		panic("api: RegisterHostEngines called before MountIAM")
	}
	const base = "/management/v1"
	nodeScope := func(c *gin.Context) (iam.Scope, bool, error) {
		if _, err := reg.GetNode(c.Request.Context(), c.Param("id")); err != nil {
			if errors.Is(err, nodereg.ErrNotFound) {
				return iam.Scope{}, false, nil
			}
			return iam.Scope{}, false, err
		}
		return iam.Scope{Kind: iam.ScopeNode, ID: c.Param("id")}, true, nil
	}
	h := &hostEnginesRoutes{reg: reg, store: store, prober: prober}
	summary := "Docker, Podman and K3s on the host, read by the privileged executor: engines lists every kind with its state (running, stopped, not_installed, incompatible, permission_denied, not_responding, unknown), program, version, systemd units and Docker CLI plugins; environments lists every endpoint separately with its owner, mode (rootful or rootless) and the kind the endpoint answered as (kindConfirmed false when it did not answer); uninspected says what could not be looked at, so a missing environment is not read as absent; null is not provided"
	s.handle(s.protected, base, Operation{Method: "GET", Path: base + "/nodes/{id}/engines", ID: "getNodeEngines",
		Auth: true, Tag: "nodes", Permission: "nodes:read on the node", Summary: summary,
		Responses: []Response{{Status: 200, Body: hostEnginesResponse{}}}}, s.RequireScoped(iam.ActionNodesRead, nodeScope), h.get)
	s.handle(s.protected, base, Operation{Method: "POST", Path: base + "/nodes/{id}/engines/refresh", ID: "refreshNodeEngines",
		Auth: true, Tag: "nodes", Permission: "nodes:write on the node",
		Summary:   "asks the node for a new report now and answers with the result (as GET); waits for the probe (up to a minute, two when one is already running); when the node is offline (NODE_OFFLINE), not active (NODE_NOT_ACTIVE) or the probe fails, the previous report is returned with lastError saying why",
		Responses: []Response{{Status: 200, Body: hostEnginesResponse{}}}}, s.RequireScoped(iam.ActionNodesWrite, nodeScope), h.refresh)
}

type hostEnginesRoutes struct {
	reg    *nodereg.Registry
	store  *engines.Store
	prober EngineProber
}

func (h *hostEnginesRoutes) respond(c *gin.Context, why *engines.Failure) {
	v, err := h.store.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, CodeInternal, "internal error")
		return
	}
	out := hostEnginesResponse{}
	if v != nil {
		out = hostEnginesResponse{Reported: v.Report != nil, CollectedAt: v.CollectedAt, ReceivedAt: v.ReceivedAt, Report: v.Report, LastError: v.LastError}
	}
	if why != nil {
		out.LastError = why
	}
	c.JSON(http.StatusOK, out)
}

func (h *hostEnginesRoutes) get(c *gin.Context) { h.respond(c, nil) }

func (h *hostEnginesRoutes) refresh(c *gin.Context) {
	n, err := h.reg.GetNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, CodeInternal, "internal error")
		return
	}
	perr := h.prober.Probe(c.Request.Context(), n.Fingerprint)
	if c.Request.Context().Err() != nil {
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	switch {
	case perr == nil:
		h.respond(c, nil)
	case isOffline(perr):
		h.respond(c, &engines.Failure{Code: "NODE_OFFLINE", Message: "the node is not connected; this is the last report it sent", At: now})
	case errors.Is(perr, engines.ErrNoNode):
		h.respond(c, &engines.Failure{Code: "NODE_NOT_ACTIVE", Message: "the node is pending, in maintenance or revoked; it is not asked", At: now})
	default:
		// The prober recorded its failure as lastError; anything it could
		// not record (a database error) is a 500.
		v, err := h.store.Get(c.Request.Context(), c.Param("id"))
		if err != nil || v == nil || v.LastError == nil || v.LastError.At.Before(now.Add(-2*time.Minute)) {
			fail(c, CodeInternal, "internal error")
			return
		}
		h.respond(c, nil)
	}
}

func isOffline(err error) bool {
	return errors.Is(err, nodes.ErrNodeOffline) || errors.Is(err, nodes.ErrSessionClosed)
}

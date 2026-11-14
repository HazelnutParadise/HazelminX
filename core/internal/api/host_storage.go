package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/hoststorage"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
)

// Storage inventory, usage and capacity alerts (ticket 030.1, STO-001,
// STO-010). Reading a node's storage needs nodes:read on the node; alert
// rules are platform-wide settings (nodes:read and nodes:write globally).

type storageRuleRequest struct {
	NodeID                    string   `json:"nodeId"`
	MountPoint                string   `json:"mountPoint"`
	WarnAvailablePercent      *float64 `json:"warnAvailablePercent"`
	CriticalAvailablePercent  *float64 `json:"criticalAvailablePercent"`
	CriticalAvailableBytes    *int64   `json:"criticalAvailableBytes"`
	WarnInodesFreePercent     *float64 `json:"warnInodesFreePercent"`
	CriticalInodesFreePercent *float64 `json:"criticalInodesFreePercent"`
	WarnFullWithinDays        *int64   `json:"warnFullWithinDays"`
}

type storageRulesResponse struct {
	Items []hoststorage.Rule `json:"items"`
}

// RegisterHostStorage mounts the storage routes. MountIAM must run first.
func (s *Server) RegisterHostStorage(reg *nodereg.Registry, store *hoststorage.Store, metrics hoststorage.Metrics) {
	if s.protected == nil {
		panic("api: RegisterHostStorage called before MountIAM")
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
	h := &hostStorageRoutes{s: s, reg: reg, store: store, metrics: metrics}
	p := s.protected
	op := func(o Operation) Operation {
		o.Auth, o.Tag = true, "storage"
		return o
	}
	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/nodes/{id}/storage", ID: "getNodeStorage",
		Summary: "the node's disks and partitions (with filesystem UUID and label), mounts with how each is referenced (uuid, label, partuuid, partlabel, id, path, mapper, device, network, virtual, other) and where it is configured (fstab line or systemd unit; none means mounted by hand), capacity and inodes from the latest sample, the 7-day growth trend, the usage breakdown and the capacity alerts with the rule that produced them; problems lists duplicate mount points, entries whose device is missing, boot-time entries not mounted and local mounts named by kernel device name (deviceName, which can point at another disk after a re-plug); " +
			"usage categories are appData, database, backupStaging, containerImages, osAndLogs and other, in that order, listing only those with bytes: when usage.state is complete they add up to usage.usedBytes and no path counts twice; partial means the bytes are lower bounds and other is left out; disk health is notCollected until SMART is read (ticket 020.3), never healthy by default; null is not provided",
		Permission: "nodes:read on the node",
		Responses:  []Response{{Status: 200, Body: hoststorage.View{}}}}), s.RequireScoped(iam.ActionNodesRead, nodeScope), h.view)
	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/storage/alert-rules", ID: "listStorageAlertRules",
		Summary:    "capacity alert rules: the default (nodeId and mountPoint empty; builtIn when none was stored) first, then rules for a node, a mount point on every node, or a node's mount point; the most specific rule applies; a null threshold is off",
		Permission: "nodes:read on the global scope",
		Responses:  []Response{{Status: 200, Body: storageRulesResponse{}}}}), s.RequireGlobal(iam.ActionNodesRead), h.listRules)
	s.handle(p, base, op(Operation{Method: "PUT", Path: base + "/storage/alert-rules", ID: "putStorageAlertRule",
		Summary:    "store the rule of one scope (nodeId empty for every node, mountPoint empty for every filesystem), replacing the one there; percentages are 0–100 of the filesystem size or inode count, criticalAvailableBytes is a byte floor for filesystems at least ten times that size, warnFullWithinDays warns when the 7-day trend fills the filesystem sooner",
		Permission: "nodes:write on the global scope", Request: storageRuleRequest{},
		Responses: []Response{{Status: 200, Body: hoststorage.Rule{}}}}), s.RequireGlobal(iam.ActionNodesWrite), h.putRule)
	s.handle(p, base, op(Operation{Method: "DELETE", Path: base + "/storage/alert-rules", ID: "deleteStorageAlertRule",
		Summary:    "remove the rule of one scope; the next less specific rule applies (the built-in default when the default rule is removed)",
		Permission: "nodes:write on the global scope",
		Params: []Param{{Name: "nodeId", In: "query", Description: "the node; empty or omitted for every node", Schema: jsonSchema{"type": "string"}},
			{Name: "mountPoint", In: "query", Description: "the mount point; empty or omitted for every filesystem", Schema: jsonSchema{"type": "string"}}},
		Responses: []Response{{Status: 204}}}), s.RequireGlobal(iam.ActionNodesWrite), h.deleteRule)
}

type hostStorageRoutes struct {
	s       *Server
	reg     *nodereg.Registry
	store   *hoststorage.Store
	metrics hoststorage.Metrics
}

func (h *hostStorageRoutes) view(c *gin.Context) {
	v, err := h.store.NodeView(c.Request.Context(), h.metrics, c.Param("id"))
	if err != nil {
		slog.Error("storage view failed", "node", c.Param("id"), "error", err)
		fail(c, CodeInternal, "internal error")
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *hostStorageRoutes) listRules(c *gin.Context) {
	rules, err := h.store.Rules(c.Request.Context())
	if err != nil {
		fail(c, CodeInternal, "internal error")
		return
	}
	c.JSON(http.StatusOK, storageRulesResponse{Items: rules})
}

// knownNode answers NOT_FOUND for a node id that does not exist.
func (h *hostStorageRoutes) knownNode(c *gin.Context, id string) bool {
	if id == "" {
		return true
	}
	if _, err := h.reg.GetNode(c.Request.Context(), id); err != nil {
		if errors.Is(err, nodereg.ErrNotFound) {
			fail(c, CodeNotFound, "no such node")
		} else {
			fail(c, CodeInternal, "internal error")
		}
		return false
	}
	return true
}

func (h *hostStorageRoutes) putRule(c *gin.Context) {
	var in storageRuleRequest
	if !bindJSON(c, &in, "body must be JSON with nodeId, mountPoint and thresholds") {
		return
	}
	if !h.knownNode(c, in.NodeID) {
		return
	}
	r := hoststorage.Rule{NodeID: in.NodeID, MountPoint: in.MountPoint, WarnAvailablePercent: in.WarnAvailablePercent,
		CriticalAvailablePercent: in.CriticalAvailablePercent, CriticalAvailableBytes: in.CriticalAvailableBytes,
		WarnInodesFreePercent: in.WarnInodesFreePercent, CriticalInodesFreePercent: in.CriticalInodesFreePercent,
		WarnFullWithinDays: in.WarnFullWithinDays}
	if err := r.Validate(); err != nil {
		fail(c, CodeValidationFailed, err.Error())
		return
	}
	ctx, done := changeCtx(c)
	defer done()
	out, err := h.store.PutRule(ctx, r, actor(c))
	if err != nil {
		fail(c, CodeInternal, "internal error")
		return
	}
	h.audit(c, "storage.alertRule.put", out.NodeID, out.MountPoint, map[string]any{
		"warnAvailablePercent": out.WarnAvailablePercent, "criticalAvailablePercent": out.CriticalAvailablePercent,
		"criticalAvailableBytes": out.CriticalAvailableBytes, "warnInodesFreePercent": out.WarnInodesFreePercent,
		"criticalInodesFreePercent": out.CriticalInodesFreePercent, "warnFullWithinDays": out.WarnFullWithinDays})
	c.JSON(http.StatusOK, out)
}

func (h *hostStorageRoutes) deleteRule(c *gin.Context) {
	nodeID, mountPoint := c.Query("nodeId"), c.Query("mountPoint")
	ctx, done := changeCtx(c)
	defer done()
	switch err := h.store.DeleteRule(ctx, nodeID, mountPoint); {
	case errors.Is(err, hoststorage.ErrNotFound):
		fail(c, CodeNotFound, "no rule is stored for this scope")
		return
	case err != nil:
		fail(c, CodeInternal, "internal error")
		return
	}
	h.audit(c, "storage.alertRule.delete", nodeID, mountPoint, nil)
	c.Status(http.StatusNoContent)
}

// audit records a rule change; it has already been made, so the row is
// written even when the caller has gone away.
func (h *hostStorageRoutes) audit(c *gin.Context, action, nodeID, mountPoint string, after map[string]any) {
	if after == nil {
		after = map[string]any{}
	}
	after["mountPoint"] = mountPoint
	scope := iam.Global
	src := source(c)
	ev := iam.AuditEvent{Action: action, Scope: scope, ResourceKind: "storageAlertRule", ResourceID: nodeID + "|" + mountPoint,
		NodeID: nodeID, After: after, TraceID: src.TraceID, SourceIP: src.IP}
	if p := Principal(c); p != nil {
		ev.ActorID, ev.ActorName = p.User.ID, p.User.Username
	}
	ctx, cancel := changeCtx(c)
	defer cancel()
	if err := h.s.iam.Record(ctx, ev); err != nil {
		slog.Error("audit row for a storage alert rule could not be written", "action", action, "trace", src.TraceID, "error", err)
	}
}

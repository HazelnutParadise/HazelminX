package api

import (
	"context"
	"errors"
	"github.com/HazelnutParadise/hazelminx/core/internal/composedeploy"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
)

// Nodes and enrollment (ticket 019). Nodes are scoped resources (scope kind
// node, id = node id); enrollment codes create nodes and need nodes:write
// on the global scope.

type nodeResponse struct {
	Node nodereg.Node `json:"node"`
}

type enrollmentResponse struct {
	Enrollment nodereg.Enrollment `json:"enrollment"`
}

// enrollmentCreatedResponse carries the code; it is never shown again.
type enrollmentCreatedResponse struct {
	Enrollment nodereg.Enrollment `json:"enrollment"`
	Code       string             `json:"code"`
}

type createEnrollmentRequest struct {
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
	Group string   `json:"group"`
	// TTLSeconds is how long the code can be used: 60 to 3600, default 600.
	TTLSeconds int `json:"ttlSeconds"`
}

type approveEnrollmentRequest struct {
	// ReplaceNodeID gives an existing node's identity to the new key.
	ReplaceNodeID string `json:"replaceNodeId"`
	// AsNewNode creates a node although one with the same machine-id exists.
	AsNewNode bool `json:"asNewNode"`
}

type revokeNodeRequest struct {
	Reason string `json:"reason"`
}

type maintenanceRequest struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

type affectedJob struct {
	ID    string     `json:"id"`
	Kind  string     `json:"kind"`
	State jobs.State `json:"state"`
}

type maintenanceResponse struct {
	Node nodereg.Node `json:"node"`
	// AffectedJobs are the node's unfinished jobs when maintenance began;
	// they continue, new ones are refused.
	AffectedJobs []affectedJob `json:"affectedJobs"`
	// AffectedServices are the Compose projects last deployed to the node
	// (NOD-008, ticket 044.2); empty when maintenance ends.
	AffectedServices []composedeploy.OnNode `json:"affectedServices"`
}

type removeNodeRequest struct {
	// Mode is unmanage, remove-agent, remove-cluster-node or delete-workloads.
	Mode string `json:"mode"`
}

var nodesList = ListSpec{Name: "nodes", Sorts: []string{"name", "createdAt"},
	DefaultSort: []SortKey{{Field: "name"}}, TieBreaker: "id",
	Filters: []FilterSpec{
		{Name: "q", Summary: "name, host name or group contains this text, or the node id or key fingerprint starts with it"},
		{Name: "tag", Summary: "nodes with this tag"},
		{Name: "group", Summary: "nodes in this group"},
		{Name: "state", Summary: "nodes in this state; removed nodes are only listed when asked for", Enum: []string{nodereg.StateActive, nodereg.StateRevoked, nodereg.StateIdentityConflict, nodereg.StateRemoved}},
	}}

var enrollmentsList = ListSpec{Name: "node-enrollments", Sorts: []string{"createdAt"},
	DefaultSort: []SortKey{{Field: "createdAt", Desc: true}}, TieBreaker: "id",
	Filters: []FilterSpec{{Name: "state", Summary: "enrollments in this state", Enum: []string{nodereg.EnrollmentWaiting, nodereg.EnrollmentExpired, nodereg.EnrollmentRevoked,
		nodereg.EnrollmentPendingApproval, nodereg.EnrollmentApproved, nodereg.EnrollmentRejected, nodereg.EnrollmentApprovalExpired}}}}

// RegisterNodes mounts the node and enrollment routes. MountIAM must run
// first.
func (s *Server) RegisterNodes(reg *nodereg.Registry) {
	if s.protected == nil {
		panic("api: RegisterNodes called before MountIAM")
	}
	const base = "/management/v1"
	p := s.protected
	h := &nodeRoutes{s: s, reg: reg}
	nodeScope := func(c *gin.Context) (iam.Scope, bool, error) {
		if _, err := reg.GetNode(c.Request.Context(), c.Param("id")); err != nil {
			if errors.Is(err, nodereg.ErrNotFound) {
				return iam.Scope{}, false, nil
			}
			return iam.Scope{}, false, err
		}
		return iam.Scope{Kind: iam.ScopeNode, ID: c.Param("id")}, true, nil
	}
	read, write := s.RequireScoped(iam.ActionNodesRead, nodeScope), s.RequireScoped(iam.ActionNodesWrite, nodeScope)
	enrollWrite, enrollRead := s.RequireGlobal(iam.ActionNodesWrite), s.RequireGlobal(iam.ActionNodesRead)
	op := func(o Operation) Operation { o.Auth, o.Tag = true, "nodes"; return o }

	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/nodes", ID: "listNodes",
		Summary:    "nodes the account can see, with connection status (online, stale, offline, never_connected; offline only means the channel is down), certificate, maintenance, revocation and what has not reached the agent yet",
		Permission: "nodes:read on each listed node", List: &nodesList,
		Responses: []Response{{Status: 200, Body: ListResponse[nodereg.Node]{}}}}), h.list)
	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/nodes/{id}", ID: "getNode",
		Summary: "one node with its component list; ETag carries its version", Permission: "nodes:read on the node",
		Responses: []Response{{Status: 200, Body: nodeResponse{}, Headers: []string{"ETag"}}}}), read, h.get)
	s.handle(p, base, op(Operation{Method: "PATCH", Path: base + "/nodes/{id}", ID: "updateNode",
		Summary: "rename, re-tag or regroup a node (identity stays the node id and key); requires If-Match", Permission: "nodes:write on the node",
		Request: nodereg.NodePatch{}, Versioned: true,
		Responses: []Response{{Status: 200, Body: nodeResponse{}, Headers: []string{"ETag"}}}}), write, h.update)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/nodes/{id}/revoke", ID: "revokeNode",
		Summary:    "stop trusting a node: its session is closed now, reconnects are refused, job steps are refused; an offline node shows the revocation as pending until it calls",
		Permission: "nodes:write on the node", Request: revokeNodeRequest{},
		Responses: []Response{{Status: 200, Body: nodeResponse{}}}, Errors: []string{CodePrecheckFailed}}), write, h.revoke)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/nodes/{id}/rotate-certificate", ID: "rotateNodeCertificate",
		Summary:    "issue a new certificate for the node's key; both are accepted until the node connects with the new one (offline nodes get it on their next connection)",
		Permission: "nodes:write on the node",
		Responses:  []Response{{Status: 200, Body: nodeResponse{}}}, Errors: []string{CodePrecheckFailed}}), write, h.rotate)
	s.handle(p, base, op(Operation{Method: "PUT", Path: base + "/nodes/{id}/maintenance", ID: "setNodeMaintenance",
		Summary:    "turn maintenance on (jobs created from now on are refused; unfinished ones are listed and continue) or off (the agent reconnects and declares its capabilities again, then a node.recheck job checks its identity and reads the host's basic values; see maintenance.recheck)",
		Permission: "nodes:write on the node", Request: maintenanceRequest{},
		Responses: []Response{{Status: 200, Body: maintenanceResponse{}}}, Errors: []string{CodePrecheckFailed}}), write, h.maintenance)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/nodes/{id}/recheck", ID: "recheckNode",
		Summary:    "check the node again now (or at its next connection): the agent answering is this node's, and the host's uptime, load, memory and root filesystem are read; the same check runs by itself when maintenance ends; the result is maintenance.recheck",
		Permission: "nodes:write on the node",
		Responses:  []Response{{Status: 200, Body: nodeResponse{}}}, Errors: []string{CodePrecheckFailed}}), write, h.recheck)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/nodes/{id}/remove", ID: "removeNode",
		Summary:    "remove a node in one of four separate ways; unmanage stops trusting it and leaves the host, the agent and every application untouched; remove-agent, remove-cluster-node and delete-workloads answer NOT_AVAILABLE until the platform can do them",
		Permission: "nodes:write on the node", Request: removeNodeRequest{},
		Responses: []Response{{Status: 200, Body: nodeResponse{}}}, Errors: []string{CodePrecheckFailed, CodeNotAvailable}}), write, h.remove)

	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/node-enrollments", ID: "createNodeEnrollment",
		Summary:    "create a single-use enrollment code (shown only in this answer) for hazelminx-agent enroll; name, tags and group are applied to the node when approved",
		Permission: "nodes:write on the global scope", Request: createEnrollmentRequest{},
		Responses: []Response{{Status: 201, Body: enrollmentCreatedResponse{}}}}), enrollWrite, h.createEnrollment)
	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/node-enrollments", ID: "listNodeEnrollments",
		Summary: "enrollment codes and the hosts that used them, newest first", Permission: "nodes:read on the global scope", List: &enrollmentsList,
		Responses: []Response{{Status: 200, Body: ListResponse[nodereg.Enrollment]{}}}}), enrollRead, h.listEnrollments)
	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/node-enrollments/{id}", ID: "getNodeEnrollment",
		Summary:    "one enrollment: whether the code was used, what the host reported (host name, machine-id, OS, source address, key fingerprint) and which existing nodes it matches",
		Permission: "nodes:read on the global scope",
		Responses:  []Response{{Status: 200, Body: enrollmentResponse{}}}}), enrollRead, h.getEnrollment)
	s.handle(p, base, op(Operation{Method: "DELETE", Path: base + "/node-enrollments/{id}", ID: "withdrawNodeEnrollment",
		Summary: "withdraw a code nobody has used yet", Permission: "nodes:write on the global scope",
		Responses: []Response{{Status: 200, Body: enrollmentResponse{}}}, Errors: []string{CodePrecheckFailed}}), enrollWrite, h.withdrawEnrollment)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/node-enrollments/{id}/approve", ID: "approveNodeEnrollment",
		Summary:    "accept the host that used the code and issue its certificate; the same key keeps its node; a new key on a machine-id that already has a node needs replaceNodeId or asNewNode (PRECHECK_FAILED with details.matches otherwise)",
		Permission: "nodes:write on the global scope", Request: approveEnrollmentRequest{},
		Responses: []Response{{Status: 200, Body: nodeResponse{}}}, Errors: []string{CodePrecheckFailed, CodeValidationFailed}}), enrollWrite, h.approve)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/node-enrollments/{id}/reject", ID: "rejectNodeEnrollment",
		Summary: "refuse the host that used the code; the code stays spent", Permission: "nodes:write on the global scope",
		Responses: []Response{{Status: 200, Body: enrollmentResponse{}}}, Errors: []string{CodePrecheckFailed}}), enrollWrite, h.reject)
}

type nodeRoutes struct {
	s   *Server
	reg *nodereg.Registry
}

func (h *nodeRoutes) fail(c *gin.Context, err error) {
	var choice *nodereg.ChoiceRequiredError
	msg := func(prefix string) string {
		return strings.TrimPrefix(strings.TrimPrefix(err.Error(), prefix), ": ")
	}
	switch {
	case errors.As(err, &choice):
		e := newError(CodePrecheckFailed, choice.Error())
		e.Details = map[string]any{"matches": choice.Matches}
		e.SuggestedActions = []string{"replace-node", "create-new-node"}
		respond(c, e)
	case errors.Is(err, nodereg.ErrNotFound):
		notFound(c)
	case errors.Is(err, nodereg.ErrValidation):
		fail(c, CodeValidationFailed, msg("nodereg: validation failed"))
	case errors.Is(err, nodereg.ErrVersionMismatch):
		cur := int64(0)
		if n, e := h.reg.GetNode(c.Request.Context(), c.Param("id")); e == nil {
			cur = n.Version
		}
		respond(c, versionMismatch(c.Param("id"), cur))
	case errors.Is(err, nodereg.ErrRemovalUnavailable):
		fail(c, CodeNotAvailable, msg("nodereg: removal mode not available"))
	case errors.Is(err, nodereg.ErrState):
		fail(c, CodePrecheckFailed, msg("nodereg: not allowed in the current state"))
	default:
		fail(c, CodeInternal, "internal error")
	}
}

// changed reports whether a change was made: it succeeded, or it was
// committed and only reading its result back failed (then the handler
// audits it and answers with failedAfter). Otherwise it answers the error.
func (h *nodeRoutes) changed(c *gin.Context, err error) bool {
	if err == nil || errors.Is(err, nodereg.ErrAfterCommit) {
		return true
	}
	h.fail(c, err)
	return false
}

// failedAfter answers the read-back error of a change that was made.
func (h *nodeRoutes) failedAfter(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	slog.Error("node change was made but its result could not be read back", "path", c.FullPath(), "error", err)
	fail(c, CodeInternal, "the change was made, but its result could not be read back; reload to see it")
	return true
}

// auditChange audits a change that was made. When its result could not be
// read back (err is ErrAfterCommit) the values taken from the result are
// unknown, so the row records only that, not empty values that would read
// as a change to empty.
func (h *nodeRoutes) auditChange(c *gin.Context, err error, action, kind, id string, scope iam.Scope, after map[string]any) {
	if err != nil {
		after = map[string]any{"readBackFailed": true}
	}
	h.audit(c, action, kind, id, scope, after)
}

// auditTimeout bounds the audit write of a change that already happened.
const auditTimeout = 10 * time.Second

// changeTimeout bounds a change to a node or an enrollment.
const changeTimeout = 30 * time.Second

// changeCtx is the context a change runs in. It does not follow the
// request's cancellation: once the registry has committed, a caller that
// went away must not turn the rest (reading the result back, the audit row)
// into an error that skips the audit.
func changeCtx(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(c.Request.Context()), changeTimeout)
}

// audit records a change to a node or an enrollment. The change has already
// been made, so the row is written even when the caller has gone away: the
// write does not follow the request's cancellation, and a failure is logged
// as an error instead of being dropped. (The registry commits its change on its
// own, often with side effects on live sessions, so the row is written
// right after it rather than in the same transaction.)
func (h *nodeRoutes) audit(c *gin.Context, action, kind, id string, scope iam.Scope, after map[string]any) {
	p := Principal(c)
	src := source(c)
	ev := iam.AuditEvent{Action: action, Scope: scope, ResourceKind: kind, ResourceID: id, After: after, TraceID: src.TraceID, SourceIP: src.IP}
	if kind == "node" {
		ev.NodeID = id
	}
	if p != nil {
		ev.ActorID, ev.ActorName = p.User.ID, p.User.Username
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), auditTimeout)
	defer cancel()
	if err := h.s.iam.Record(ctx, ev); err != nil {
		slog.Error("audit row for a node change could not be written", "action", action, "resource", kind, "id", id, "trace", src.TraceID, "error", err)
	}
}

func actor(c *gin.Context) string {
	if p := Principal(c); p != nil {
		return p.User.ID
	}
	return ""
}

func nodeScopeOf(id string) iam.Scope { return iam.Scope{Kind: iam.ScopeNode, ID: id} }

func (h *nodeRoutes) list(c *gin.Context) {
	p := Principal(c)
	q, perr := parseListQuery(c, withListDefaults(nodesList))
	if perr != nil {
		respond(c, *perr)
		return
	}
	first := func(k string) string {
		if v := q.Filters[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	all, err := h.reg.ListNodes(c.Request.Context(), nodereg.NodeFilter{Query: first("q"), Tag: first("tag"), Group: first("group"), State: first("state")})
	if err != nil {
		h.fail(c, err)
		return
	}
	visible := all[:0]
	for _, n := range all {
		if iam.Authorize(p, iam.ActionNodesRead, nodeScopeOf(n.ID)) {
			visible = append(visible, n)
		}
	}
	runList[nodereg.Node](c, nodesList, memorySource[nodereg.Node]{rows: visible, value: func(n nodereg.Node, f string) string {
		switch f {
		case "name":
			return n.Name
		case "createdAt":
			return n.CreatedAt.UTC().Format(sortTime)
		}
		return n.ID
	}})
}

func (h *nodeRoutes) get(c *gin.Context) {
	n, err := h.reg.GetNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	setETag(c, n.Version)
	c.JSON(http.StatusOK, nodeResponse{Node: n})
}

func (h *nodeRoutes) update(c *gin.Context) {
	var in nodereg.NodePatch
	if !bindJSON(c, &in, "body must be JSON with name, tags or group") {
		return
	}
	ver, ok := requireIfMatch(c)
	if !ok {
		return
	}
	ctx, done := changeCtx(c)
	defer done()
	n, err := h.reg.UpdateNode(ctx, c.Param("id"), ver, in, actor(c))
	if errors.Is(err, nodereg.ErrVersionMismatch) {
		// T-030: show what is saved now next to this edit.
		if cur, gerr := h.reg.GetNode(c.Request.Context(), c.Param("id")); gerr == nil {
			yours := map[string]any{}
			if in.Name != nil {
				yours["name"] = strings.TrimSpace(*in.Name)
			}
			if in.Tags != nil {
				yours["tags"] = *in.Tags
			}
			if in.Group != nil {
				yours["group"] = strings.TrimSpace(*in.Group)
			}
			respond(c, versionConflict(c.Param("id"), cur.Version, map[string]any{"name": cur.Name, "tags": cur.Tags, "group": cur.Group}, yours))
			return
		}
	}
	if !h.changed(c, err) {
		return
	}
	h.auditChange(c, err, "nodes.update", "node", n.ID, nodeScopeOf(n.ID), map[string]any{"name": n.Name, "tags": n.Tags, "group": n.Group})
	if h.failedAfter(c, err) {
		return
	}
	setETag(c, n.Version)
	c.JSON(http.StatusOK, nodeResponse{Node: n})
}

func (h *nodeRoutes) revoke(c *gin.Context) {
	var in revokeNodeRequest
	if !bindJSON(c, &in, "body must be JSON, optionally with reason") {
		return
	}
	ctx, done := changeCtx(c)
	defer done()
	n, err := h.reg.RevokeNode(ctx, c.Param("id"), in.Reason, actor(c))
	if !h.changed(c, err) {
		return
	}
	after := map[string]any{"state": n.State, "reason": in.Reason}
	if n.Revocation != nil {
		after["sessionClosedAt"], after["pending"] = n.Revocation.SessionClosedAt, n.Revocation.Pending
	}
	h.auditChange(c, err, "nodes.revoke", "node", n.ID, nodeScopeOf(n.ID), after)
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusOK, nodeResponse{Node: n})
}

func (h *nodeRoutes) rotate(c *gin.Context) {
	ctx, done := changeCtx(c)
	defer done()
	n, err := h.reg.RotateCertificate(ctx, c.Param("id"), actor(c))
	if !h.changed(c, err) {
		return
	}
	after := map[string]any{}
	if r := n.Certificate.Rotation; r != nil {
		after["newSerial"], after["deliveredAt"], after["error"] = r.NewSerial, r.DeliveredAt, r.Error
	}
	h.auditChange(c, err, "nodes.rotate-certificate", "node", n.ID, nodeScopeOf(n.ID), after)
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusOK, nodeResponse{Node: n})
}

func (h *nodeRoutes) maintenance(c *gin.Context) {
	var in maintenanceRequest
	if !bindJSON(c, &in, "body must be JSON with enabled and optionally reason") {
		return
	}
	ctx, done := changeCtx(c)
	defer done()
	n, affected, err := h.reg.SetMaintenance(ctx, c.Param("id"), in.Enabled, in.Reason, actor(c))
	if !h.changed(c, err) {
		return
	}
	out := maintenanceResponse{Node: n, AffectedJobs: []affectedJob{}, AffectedServices: []composedeploy.OnNode{}}
	for _, j := range affected {
		out.AffectedJobs = append(out.AffectedJobs, affectedJob{ID: j.ID, Kind: j.Kind, State: j.State})
	}
	if in.Enabled && err == nil && h.s.nodeServices != nil {
		svcs, serr := h.s.nodeServices(ctx, n.ID)
		if serr != nil {
			err = serr
		} else {
			out.AffectedServices = svcs
		}
	}
	h.auditChange(c, err, "nodes.maintenance", "node", n.ID, nodeScopeOf(n.ID), map[string]any{"enabled": in.Enabled, "reason": in.Reason, "affectedJobs": len(out.AffectedJobs), "affectedServices": len(out.AffectedServices)})
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *nodeRoutes) recheck(c *gin.Context) {
	ctx, done := changeCtx(c)
	defer done()
	n, err := h.reg.RunRecheck(ctx, c.Param("id"), actor(c))
	if !h.changed(c, err) {
		return
	}
	h.auditChange(c, err, "nodes.recheck", "node", n.ID, nodeScopeOf(n.ID), nil)
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusOK, nodeResponse{Node: n})
}

func (h *nodeRoutes) remove(c *gin.Context) {
	var in removeNodeRequest
	if !bindJSON(c, &in, "body must be JSON with mode") {
		return
	}
	ctx, done := changeCtx(c)
	defer done()
	n, err := h.reg.Remove(ctx, c.Param("id"), in.Mode, actor(c))
	if !h.changed(c, err) {
		return
	}
	h.auditChange(c, err, "nodes.remove", "node", n.ID, nodeScopeOf(n.ID), map[string]any{"mode": in.Mode})
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusOK, nodeResponse{Node: n})
}

func (h *nodeRoutes) createEnrollment(c *gin.Context) {
	var in createEnrollmentRequest
	if !bindJSON(c, &in, "body must be JSON, optionally with name, tags, group and ttlSeconds") {
		return
	}
	if in.TTLSeconds < 0 {
		fail(c, CodeValidationFailed, "ttlSeconds must not be negative")
		return
	}
	ctx, done := changeCtx(c)
	defer done()
	e, code, err := h.reg.CreateEnrollment(ctx, nodereg.CreateInput{Name: in.Name, Tags: in.Tags, Group: in.Group,
		TTL: time.Duration(in.TTLSeconds) * time.Second, Actor: actor(c)})
	if !h.changed(c, err) {
		return
	}
	// The audit row names the code by its public id only.
	h.auditChange(c, err, "nodes.enrollment.create", "node-enrollment", e.ID, iam.Global, map[string]any{"codeId": e.CodeID, "expiresAt": e.ExpiresAt, "name": e.Name, "group": e.Group})
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusCreated, enrollmentCreatedResponse{Enrollment: e, Code: code.String()})
}

func (h *nodeRoutes) listEnrollments(c *gin.Context) {
	q, perr := parseListQuery(c, withListDefaults(enrollmentsList))
	if perr != nil {
		respond(c, *perr)
		return
	}
	state := ""
	if v := q.Filters["state"]; len(v) > 0 {
		state = v[0]
	}
	all, err := h.reg.ListEnrollments(c.Request.Context(), state)
	if err != nil {
		h.fail(c, err)
		return
	}
	runList[nodereg.Enrollment](c, enrollmentsList, memorySource[nodereg.Enrollment]{rows: all, value: func(e nodereg.Enrollment, f string) string {
		if f == "createdAt" {
			return e.CreatedAt.UTC().Format(sortTime)
		}
		return e.ID
	}})
}

func (h *nodeRoutes) getEnrollment(c *gin.Context) {
	e, err := h.reg.GetEnrollment(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, enrollmentResponse{Enrollment: e})
}

func (h *nodeRoutes) withdrawEnrollment(c *gin.Context) {
	ctx, done := changeCtx(c)
	defer done()
	e, err := h.reg.RevokeEnrollment(ctx, c.Param("id"), actor(c))
	if !h.changed(c, err) {
		return
	}
	h.auditChange(c, err, "nodes.enrollment.withdraw", "node-enrollment", e.ID, iam.Global, map[string]any{"codeId": e.CodeID})
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusOK, enrollmentResponse{Enrollment: e})
}

func (h *nodeRoutes) approve(c *gin.Context) {
	var in approveEnrollmentRequest
	if !bindJSON(c, &in, "body must be JSON, optionally with replaceNodeId or asNewNode") {
		return
	}
	if in.ReplaceNodeID != "" && in.AsNewNode {
		fail(c, CodeValidationFailed, "replaceNodeId and asNewNode exclude each other")
		return
	}
	ctx, done := changeCtx(c)
	defer done()
	n, err := h.reg.Approve(ctx, c.Param("id"), nodereg.ApproveInput{Actor: actor(c), ReplaceNodeID: in.ReplaceNodeID, AsNewNode: in.AsNewNode})
	if !h.changed(c, err) {
		return
	}
	h.auditChange(c, err, "nodes.enrollment.approve", "node-enrollment", c.Param("id"), iam.Global,
		map[string]any{"nodeId": n.ID, "fingerprint": n.Fingerprint, "replaceNodeId": in.ReplaceNodeID, "asNewNode": in.AsNewNode})
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusOK, nodeResponse{Node: n})
}

func (h *nodeRoutes) reject(c *gin.Context) {
	ctx, done := changeCtx(c)
	defer done()
	e, err := h.reg.Reject(ctx, c.Param("id"), actor(c))
	if !h.changed(c, err) {
		return
	}
	h.auditChange(c, err, "nodes.enrollment.reject", "node-enrollment", e.ID, iam.Global, nil)
	if h.failedAfter(c, err) {
		return
	}
	c.JSON(http.StatusOK, enrollmentResponse{Enrollment: e})
}

// sortTime has a fixed width so times sort as text.
const sortTime = "2006-01-02T15:04:05.000000000Z"

// memorySource pages a list held in memory (nodes and enrollments are
// small). Sort values are text compared as text.
type memorySource[T any] struct {
	rows  []T
	value func(row T, field string) string
}

func (m memorySource[T]) Count(context.Context, ListQuery) (int, error) { return len(m.rows), nil }

func (m memorySource[T]) Key(row T, keys []SortKey) []string {
	return sortValues(keys, func(f string) string { return m.value(row, f) })
}

func (m memorySource[T]) less(a, b []string, keys []SortKey) int {
	for i, k := range keys {
		if c := strings.Compare(a[i], b[i]); c != 0 {
			if k.Desc {
				return -c
			}
			return c
		}
	}
	return 0
}

func (m memorySource[T]) Fetch(_ context.Context, q ListQuery, n int) ([]T, error) {
	rows := append([]T(nil), m.rows...)
	sort.SliceStable(rows, func(i, j int) bool { return m.less(m.Key(rows[i], q.Sort), m.Key(rows[j], q.Sort), q.Sort) < 0 })
	out := []T{}
	for _, r := range rows {
		if q.After != nil && m.less(m.Key(r, q.Sort), q.After, q.Sort) <= 0 {
			continue
		}
		out = append(out, r)
		if len(out) == n {
			break
		}
	}
	return out, nil
}

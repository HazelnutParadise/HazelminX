package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/plans"
)

// MountChangePlans mounts /management/v1/change-plans (spec 36.2, ticket
// 016.1). Permissions are decided by the plan service on every call: the
// plan kind's action on every target, owners only for high-risk plans, and
// 404 for a plan with a target the caller cannot see. Job kinds owned by a
// plan kind are refused by POST /jobs from now on.
func (s *Server) MountChangePlans(p *plans.Service) {
	if s.protected == nil {
		panic("api: MountChangePlans called before MountIAM; change-plan routes must not be unauthenticated")
	}
	s.plans = p
	p.SetGate(func(ctx context.Context, req jobs.SubmitRequest) error {
		blockers, err := s.writeBlockers(ctx, req.Kind, ResourceState{}, req.Resources)
		if err != nil {
			return err
		}
		if top := pickBlocking(blockers); top != nil {
			return gateError{*top}
		}
		return nil
	})
	h := &plansHandler{svc: p}
	const base = "/management/v1"
	g := s.protected
	perm := "the plan kind's action on every target scope; a plan with a target the caller cannot see is 404"
	common := []string{CodeChangePlanState, CodeChangePlanExpired}
	s.handle(g, base, Operation{Method: "GET", Path: base + "/change-plans", ID: "listChangePlans", Tag: "change-plans", Auth: true,
		Summary: "list the change plans whose every target the caller can see; state filters by effective state (past the deadline counts as expired)", Permission: "read access to every target scope",
		List: &changePlansList, Responses: []Response{{Status: 200, Body: ListResponse[*plans.Plan]{}}}}, h.list)
	s.handle(g, base, Operation{Method: "POST", Path: base + "/change-plans", ID: "createChangePlan", Tag: "change-plans", Auth: true,
		Summary:    "compute a change plan: targets with real names and versions, diff, affected services, prechecks, risk, required approval and deadline; nothing is changed",
		Permission: perm, Request: createPlanRequest{},
		Responses: []Response{{Status: 201, Body: planResponse{}, Headers: []string{"Location"}}}}, h.create)
	s.handle(g, base, Operation{Method: "GET", Path: base + "/change-plans/{id}", ID: "getChangePlan", Tag: "change-plans", Auth: true,
		Summary: "a change plan with its approval, decision, execution or invalidation", Permission: "read access to every target scope",
		Responses: []Response{{Status: 200, Body: planResponse{}}}}, h.get)
	s.handle(g, base, Operation{Method: "POST", Path: base + "/change-plans/{id}/approve", ID: "approveChangePlan", Tag: "change-plans", Auth: true,
		Summary:    "approve the plan with the digest you reviewed; the plan is computed again and a changed content or target version invalidates it (CHANGE_PLAN_CHANGED with the difference)",
		Permission: perm + "; owner for a high-risk plan", Request: approveRequest{},
		Responses: []Response{{Status: 200, Body: planResponse{}}}, Errors: append([]string{CodeChangePlanChanged, CodePrecheckFailed}, common...)}, h.approve)
	s.handle(g, base, Operation{Method: "POST", Path: base + "/change-plans/{id}/reject", ID: "rejectChangePlan", Tag: "change-plans", Auth: true,
		Summary: "reject a waiting or approved plan with a reason", Permission: perm, Request: reasonRequest{},
		Responses: []Response{{Status: 200, Body: planResponse{}}}, Errors: common}, h.reject)
	s.handle(g, base, Operation{Method: "POST", Path: base + "/change-plans/{id}/withdraw", ID: "withdrawChangePlan", Tag: "change-plans", Auth: true,
		Summary: "withdraw your own waiting or approved plan", Permission: "the requester of the plan", Request: reasonRequest{},
		Responses: []Response{{Status: 200, Body: planResponse{}}}, Errors: common}, h.withdraw)
	s.handle(g, base, Operation{Method: "POST", Path: base + "/change-plans/{id}/execute", ID: "executeChangePlan", Tag: "change-plans", Auth: true,
		Summary:    "run an approved plan as a job; the same requestId answers the same job, another is REQUEST_ID_CONFLICT; a target changed since approval stops the run (CHANGE_PLAN_CHANGED)",
		Permission: perm, Request: executeRequest{}, LongRunning: true,
		Responses: []Response{{Status: 202, Body: JobAccepted{}, Headers: []string{"Location"}}},
		Errors: append([]string{CodeChangePlanChanged, CodeRequestIDConflict, CodeNotAvailable, CodePlatformDegraded, CodeConfirmRequired, CodeScopeDegraded,
			CodeControllerConflict, CodeResourceReadOnly}, common...)}, h.execute)
	s.extras = append(s.extras, func(o *overviewResponse) { o.ChangePlanKinds = p.Kinds() })
}

// gateError carries the write-gate refusal through the plan service.
type gateError struct{ e apiError }

func (g gateError) Error() string { return g.e.Code + ": " + g.e.Message }

var changePlansList = ListSpec{Name: "change-plans", Sorts: []string{"createdAt", "expiresAt", "kind"},
	DefaultSort: []SortKey{{Field: "createdAt", Desc: true}}, TieBreaker: "id",
	Filters: []FilterSpec{
		{Name: "state", Summary: "only plans in these effective states", Multi: true, Enum: planStates()},
		{Name: "kind", Summary: "only plans of this kind"},
		{Name: "requestedBy", Summary: "only plans requested by this account id"},
	}}

func planStates() []string {
	var out []string
	for _, st := range plans.States {
		out = append(out, string(st))
	}
	return out
}

type createPlanRequest struct {
	Kind   string          `json:"kind"`
	Params json.RawMessage `json:"params"`
}

type approveRequest struct {
	// Digest of the plan the approver reviewed.
	Digest string `json:"digest"`
}

type reasonRequest struct {
	Reason string `json:"reason"`
}

type executeRequest struct {
	Digest    string `json:"digest"`
	RequestID string `json:"requestId"`
}

type planResponse struct {
	Plan *plans.Plan `json:"plan"`
}

type plansHandler struct{ svc *plans.Service }

func planLocation(id string) string { return "/management/v1/change-plans/" + id }

// planError maps the plan service's errors to the code table.
func planError(c *gin.Context, err error) {
	var ce *plans.ChangedError
	var fe *plans.ForbiddenError
	var ge gateError
	switch {
	case errors.As(err, &ge):
		respond(c, ge.e)
	case errors.As(err, &ce):
		msg := "the plan you reviewed is not what would run now; review the difference and create a new plan"
		if !ce.Invalidated {
			msg = "the digest you sent is not the digest of this plan; reload the plan and review it again"
		}
		e := newError(CodeChangePlanChanged, msg)
		e.ResourceID = c.Param("id")
		e.Details = map[string]any{"planDigest": ce.PlanDigest, "currentDigest": ce.CurrentDigest, "sentDigest": ce.SentDigest,
			"targets": ce.Targets, "planned": ce.Planned, "current": ce.Current, "invalidated": ce.Invalidated}
		if ce.Invalidated {
			e.SuggestedActions = []string{"review-difference", "create-new-plan"}
		} else {
			e.SuggestedActions = []string{"reload-plan"}
		}
		respond(c, e)
	case errors.As(err, &fe):
		e := newError(CodeForbidden, "this account is not allowed to perform the operation")
		switch fe.Reason {
		case "owner-required":
			e.Message = "a high-risk plan can only be approved by an owner"
		case "not-requester":
			e.Message = "only the account that requested the plan can withdraw it"
		}
		e.Details = map[string]any{"reason": fe.Reason}
		respond(c, e)
	case errors.Is(err, plans.ErrNotFound):
		notFound(c)
	case errors.Is(err, plans.ErrUnknownKind):
		e := newError(CodeValidationFailed, err.Error())
		e.Fields = []FieldError{{Field: "kind", Message: "no such plan kind; the overview lists changePlanKinds"}}
		respond(c, e)
	case errors.Is(err, plans.ErrInvalid):
		respond(c, newError(CodeValidationFailed, err.Error()))
	case errors.Is(err, plans.ErrExpired):
		e := newError(CodeChangePlanExpired, err.Error())
		e.SuggestedActions = []string{"create-new-plan"}
		respond(c, e)
	case errors.Is(err, plans.ErrState):
		respond(c, newError(CodeChangePlanState, err.Error()))
	case errors.Is(err, plans.ErrUnavailable):
		respond(c, newError(CodeNotAvailable, err.Error()))
	case errors.Is(err, plans.ErrPrecheck):
		respond(c, newError(CodePrecheckFailed, err.Error()))
	case errors.Is(err, plans.ErrRequestIDTaken):
		e := newError(CodeRequestIDConflict, "this plan was already run with another requestId, or the requestId is used by another plan")
		e.SuggestedActions = []string{"use-the-original-request-id"}
		respond(c, e)
	default:
		respond(c, newError(CodeInternal, "internal error"))
	}
}

type changePlansSource struct {
	h    *plansHandler
	c    *gin.Context
	opts plans.ListOptions
}

func (s changePlansSource) Count(ctx context.Context, _ ListQuery) (int, error) {
	return s.h.svc.Count(ctx, Principal(s.c), s.opts)
}
func (s changePlansSource) Fetch(ctx context.Context, q ListQuery, n int) ([]*plans.Plan, error) {
	o := s.opts
	o.Page = q.paging(n)
	return s.h.svc.List(ctx, Principal(s.c), o)
}
func (s changePlansSource) Key(row *plans.Plan, sort []SortKey) []string {
	return sortValues(sort, func(f string) string {
		if f == "id" {
			return row.ID
		}
		return row.SortValue(f)
	})
}

func (h *plansHandler) list(c *gin.Context) {
	q, perr := parseListQuery(c, changePlansList)
	if perr != nil {
		respond(c, *perr)
		return
	}
	var o plans.ListOptions
	for _, st := range q.Filters["state"] {
		o.States = append(o.States, plans.State(st))
	}
	if v := q.Filters["kind"]; len(v) > 0 {
		o.Kind = v[0]
	}
	if v := q.Filters["requestedBy"]; len(v) > 0 {
		o.RequestedBy = v[0]
	}
	runList[*plans.Plan](c, changePlansList, changePlansSource{h: h, c: c, opts: o})
}

func (h *plansHandler) create(c *gin.Context) {
	var in createPlanRequest
	if !bindJSON(c, &in, "body must be JSON with kind and params") {
		return
	}
	pl, err := h.svc.Create(c.Request.Context(), Principal(c), in.Kind, in.Params, source(c))
	if err != nil {
		planError(c, err)
		return
	}
	c.Header("Location", planLocation(pl.ID))
	c.JSON(http.StatusCreated, planResponse{Plan: pl})
}

func (h *plansHandler) get(c *gin.Context) {
	pl, err := h.svc.Get(c.Request.Context(), Principal(c), c.Param("id"))
	if err != nil {
		planError(c, err)
		return
	}
	c.JSON(http.StatusOK, planResponse{Plan: pl})
}

func (h *plansHandler) approve(c *gin.Context) {
	var in approveRequest
	if !bindJSON(c, &in, "body must be JSON with the digest you reviewed") {
		return
	}
	pl, err := h.svc.Approve(c.Request.Context(), Principal(c), c.Param("id"), in.Digest, source(c))
	if err != nil {
		planError(c, err)
		return
	}
	c.JSON(http.StatusOK, planResponse{Plan: pl})
}

func (h *plansHandler) reject(c *gin.Context) {
	var in reasonRequest
	if !bindJSON(c, &in, "body must be JSON with a reason") {
		return
	}
	pl, err := h.svc.Reject(c.Request.Context(), Principal(c), c.Param("id"), in.Reason, source(c))
	if err != nil {
		planError(c, err)
		return
	}
	c.JSON(http.StatusOK, planResponse{Plan: pl})
}

func (h *plansHandler) withdraw(c *gin.Context) {
	var in reasonRequest
	if !bindJSON(c, &in, "body must be JSON with an optional reason") {
		return
	}
	pl, err := h.svc.Withdraw(c.Request.Context(), Principal(c), c.Param("id"), in.Reason, source(c))
	if err != nil {
		planError(c, err)
		return
	}
	c.JSON(http.StatusOK, planResponse{Plan: pl})
}

func (h *plansHandler) execute(c *gin.Context) {
	var in executeRequest
	if !bindJSON(c, &in, "body must be JSON with digest and requestId") {
		return
	}
	res, err := h.svc.Execute(c.Request.Context(), Principal(c), c.Param("id"), in.Digest, in.RequestID, source(c))
	if err != nil {
		planError(c, err)
		return
	}
	acceptJob(c, res.Job, res.Replayed)
}

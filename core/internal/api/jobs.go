package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// RegisterJobs mounts the job engine under /management/v1/jobs (spec 33,
// 36.2; ticket 008). Long operations are submitted here and answered with
// 202 and the job record; the record outlives the request (ENG-001).
//
// The routes sit on the authenticated group from MountIAM (ticket 009), so
// MountIAM must run first. Reading needs platform:read and submitting or
// cancelling needs platform:write, both on the global scope; job kinds that
// need narrower permissions get their own routes.
func (s *Server) RegisterJobs(e *jobs.Engine) {
	if s.protected == nil {
		panic("api: RegisterJobs called before MountIAM; job routes must not be unauthenticated")
	}
	s.jobs = e
	h := &jobsHandler{engine: e, srv: s}
	const base = "/management/v1"
	read, write := s.RequireGlobal(iam.ActionPlatformRead), s.RequireGlobal(iam.ActionPlatformWrite)
	g := s.protected
	s.handle(g, base, Operation{Method: "GET", Path: base + "/jobs", ID: "listJobs", Tag: "jobs", Auth: true,
		Summary: "list jobs", Permission: "platform:read on the global scope", List: &jobsList,
		Responses: []Response{{Status: 200, Body: ListResponse[*jobs.Job]{}}}}, read, h.list)
	s.handle(g, base, Operation{Method: "POST", Path: base + "/jobs", ID: "submitJob", Tag: "jobs", Auth: true,
		Summary:    "submit a job; the same requestId replays the same job, changed content is REQUEST_ID_CONFLICT",
		Permission: "platform:write on the global scope", Request: jobs.SubmitRequest{}, LongRunning: true, DryRun: true,
		Responses: []Response{{Status: 202, Body: JobAccepted{}, Headers: []string{"Location"}}, {Status: 200, Body: DryRunResult{}, Description: "dryRun=true preview; preview is a jobs.Preview"}},
		Errors:    []string{CodeRequestIDConflict, CodePayloadTooLarge, CodePlatformDegraded, CodeConfirmRequired, CodeChangePlanRequired}}, write, h.submit)
	s.handle(g, base, Operation{Method: "GET", Path: base + "/jobs/{id}", ID: "getJob", Tag: "jobs", Auth: true,
		Summary: "job record: state, steps, progress, diagnostics, completion checks, cancellable", Permission: "platform:read on the global scope",
		Responses: []Response{{Status: 200, Body: jobResponse{}}}}, read, h.get)
	s.handle(g, base, Operation{Method: "GET", Path: base + "/jobs/{id}/events", ID: "listJobEvents", Tag: "jobs", Auth: true,
		Summary:    "events after a sequence number; resume from the last seq seen (event stream, not a list)",
		Permission: "platform:read on the global scope",
		Params: []Param{
			{Name: "after", In: "query", Description: "return events with seq greater than this", Schema: jsonSchema{"type": "integer", "minimum": 0}},
			{Name: "limit", In: "query", Description: "at most this many events (1..1000, default 1000)", Schema: jsonSchema{"type": "integer", "minimum": 1, "maximum": 1000}},
		},
		Responses: []Response{{Status: 200, Body: jobEventsResponse{}}}}, read, h.events)
	s.handle(g, base, Operation{Method: "POST", Path: base + "/jobs/{id}/cancel", ID: "cancelJob", Tag: "jobs", Auth: true,
		Summary: "request cancellation; stopped=false means requested, not yet stopped", Permission: "platform:write on the global scope",
		Responses: []Response{{Status: 202, Body: JobAccepted{}}}, Errors: []string{CodeJobNotCancellable}}, write, h.cancel)
	s.extras = append(s.extras, func(o *overviewResponse) { o.JobKinds = e.Kinds() })
}

var jobsList = ListSpec{Name: "jobs", Sorts: []string{"createdAt", "updatedAt", "kind", "state"},
	DefaultSort: []SortKey{{Field: "createdAt", Desc: true}}, TieBreaker: "id",
	Filters: []FilterSpec{
		{Name: "state", Summary: "only jobs in these states", Multi: true, Enum: jobStates()},
		{Name: "kind", Summary: "only jobs of this kind"},
		{Name: "node", Summary: "only jobs on this node"},
	}}

func jobStates() []string {
	var out []string
	for _, st := range jobs.States {
		out = append(out, string(st))
	}
	return out
}

type jobsHandler struct {
	engine *jobs.Engine
	srv    *Server
}

type jobResponse struct {
	Job *jobs.Job `json:"job"`
}

type jobEventsResponse struct {
	Events  []jobs.Event `json:"events"`
	LastSeq int64        `json:"lastSeq"`
	Limit   int          `json:"limit"`
}

// jobError maps engine errors to the code table.
func jobError(err error, requestID string) apiError {
	var e apiError
	switch {
	case errors.Is(err, jobs.ErrNotFound):
		e = newError(CodeNotFound, "no such resource")
	case errors.Is(err, jobs.ErrConflict):
		e = newError(CodeRequestIDConflict, err.Error())
		e.SuggestedActions = []string{"use-a-new-request-id"}
	case errors.Is(err, jobs.ErrUnknownKind):
		e = newError(CodeValidationFailed, err.Error())
		e.Fields = []FieldError{{Field: "kind", Message: "no such job kind; the overview lists jobKinds"}}
	case errors.Is(err, jobs.ErrInvalidRequest):
		e = newError(CodeValidationFailed, err.Error())
	case errors.Is(err, jobs.ErrNotCancellable):
		e = newError(CodeJobNotCancellable, err.Error())
	default:
		e = newError(CodeInternal, "internal error")
	}
	e.RequestID = requestID
	return e
}

// jobsSource lists jobs.
type jobsSource struct {
	engine *jobs.Engine
	opts   jobs.ListOptions
}

func (j jobsSource) Count(ctx context.Context, _ ListQuery) (int, error) {
	return j.engine.Count(ctx, j.opts)
}
func (j jobsSource) Fetch(ctx context.Context, q ListQuery, n int) ([]*jobs.Job, error) {
	o := j.opts
	o.Page = q.paging(n)
	return j.engine.List(ctx, o)
}
func (j jobsSource) Key(row *jobs.Job, sort []SortKey) []string {
	return sortValues(sort, row.SortValue)
}

func jobListOptions(q ListQuery) jobs.ListOptions {
	var o jobs.ListOptions
	for _, st := range q.Filters["state"] {
		o.States = append(o.States, jobs.State(st))
	}
	if v := q.Filters["kind"]; len(v) > 0 {
		o.Kind = v[0]
	}
	if v := q.Filters["node"]; len(v) > 0 {
		o.Node = v[0]
	}
	return o
}

func (h *jobsHandler) list(c *gin.Context) {
	q, perr := parseListQuery(c, jobsList)
	if perr != nil {
		respond(c, *perr)
		return
	}
	runList[*jobs.Job](c, jobsList, jobsSource{engine: h.engine, opts: jobListOptions(q)})
}

func (h *jobsHandler) submit(c *gin.Context) {
	var req jobs.SubmitRequest
	if !bindJSON(c, &req, "body must be a JSON submit request") {
		return
	}
	ctx := c.Request.Context()
	// Kinds owned by a change plan run only from an approved plan.
	if h.srv.plans != nil && h.srv.plans.RequiresPlan(req.Kind) {
		e := newError(CodeChangePlanRequired, "jobs of kind "+req.Kind+" run only from an approved change plan; create one with POST /management/v1/change-plans")
		e.RequestID = req.RequestID
		e.SuggestedActions = []string{"create-change-plan"}
		respond(c, e)
		return
	}
	// Kinds with their own endpoint check permissions on the resource they
	// act on; POST /jobs cannot (ticket 044.2).
	if ep, ok := h.srv.reservedKinds[req.Kind]; ok {
		e := newError(CodeValidationFailed, "jobs of kind "+req.Kind+" are submitted through "+ep)
		e.RequestID = req.RequestID
		respond(c, e)
		return
	}
	// The requester is always the signed-in account; a body cannot file a
	// job under someone else's name.
	if p := Principal(c); p != nil {
		req.Requester = p.User.ID
	}
	// Input and replay come first (51.6): an accepted requestId answers with
	// its job even while the platform is degraded.
	pv, err := h.engine.Preview(ctx, req)
	if err != nil {
		respond(c, jobError(err, req.RequestID))
		return
	}
	if pv.Replay != nil && pv.Replay.Requester != req.Requester {
		respond(c, requestIDTaken(req.RequestID))
		return
	}
	var blockers []apiError
	if pv.Replay == nil {
		blockers, err = h.srv.writeBlockers(ctx, req.Kind, ResourceState{}, req.Resources)
		if err != nil {
			respond(c, jobError(err, req.RequestID))
			return
		}
	}
	if isDryRun(c) {
		var impact Impact
		for _, r := range req.Resources {
			impact.Direct = append(impact.Direct, ImpactItem{Kind: r.Kind, ID: resourceName(r), Scope: r.Node, Relation: "locked"})
		}
		for _, id := range pv.QueuedBehind {
			impact.Direct = append(impact.Direct, ImpactItem{Kind: "job", ID: id, Relation: "queued-behind"})
		}
		respondDryRun(c, blockers, impact, pv)
		return
	}
	if top := pickBlocking(blockers); top != nil {
		top.RequestID = req.RequestID
		respond(c, *top)
		return
	}
	job, dedup, err := h.engine.Submit(ctx, req)
	if err != nil {
		respond(c, jobError(err, req.RequestID))
		return
	}
	if dedup && job.Requester != req.Requester { // taken between preview and submit
		respond(c, requestIDTaken(req.RequestID))
		return
	}
	acceptJob(c, job, dedup)
}

// requestIDTaken refuses a requestId another account already used. The
// other job is not described: the request ids of an account are its own.
func requestIDTaken(requestID string) apiError {
	e := newError(CodeRequestIDConflict, "this requestId has already been used; send the request again with a new requestId")
	e.RequestID = requestID
	e.SuggestedActions = []string{"use-a-new-request-id"}
	return e
}

// resourceName is the identity of a job resource as one string.
func resourceName(r jobs.ResourceRef) string {
	for _, v := range []string{r.ID, r.ResolvedPath, r.Path, r.Mount, r.Device} {
		if v != "" {
			return v
		}
	}
	return ""
}

func (h *jobsHandler) get(c *gin.Context) {
	job, err := h.engine.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		respond(c, jobError(err, ""))
		return
	}
	c.JSON(http.StatusOK, jobResponse{Job: job})
}

func (h *jobsHandler) events(c *gin.Context) {
	var after int64
	if v := c.Query("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			e := newError(CodeValidationFailed, "after must be a non-negative integer")
			e.Fields = []FieldError{{Field: "after", Message: "non-negative integer"}}
			respond(c, e)
			return
		}
		after = n
	}
	limit := 1000
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			e := newError(CodeValidationFailed, "limit must be an integer from 1 to 1000")
			e.Fields = []FieldError{{Field: "limit", Message: "1..1000"}}
			respond(c, e)
			return
		}
		limit = n
	}
	evs, err := h.engine.Events(c.Request.Context(), c.Param("id"), after, limit)
	if err != nil {
		respond(c, jobError(err, ""))
		return
	}
	if evs == nil {
		evs = []jobs.Event{}
	}
	last := after
	if len(evs) > 0 {
		last = evs[len(evs)-1].Seq
	}
	c.JSON(http.StatusOK, jobEventsResponse{Events: evs, LastSeq: last, Limit: limit})
}

func (h *jobsHandler) cancel(c *gin.Context) {
	job, err := h.engine.Cancel(c.Request.Context(), c.Param("id"))
	if err != nil {
		respond(c, jobError(err, ""))
		return
	}
	stopped := job.State == jobs.StateCancelled
	acceptJobWith(c, job, false, &stopped)
}

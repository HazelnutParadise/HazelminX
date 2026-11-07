package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// RegisterJobs mounts the job engine under /management/v1/jobs (spec 33,
// 36.2; ticket 008). Long operations are submitted here and answered with
// 202 and the job record; the record outlives the request (ENG-001).
//
// The routes sit on the authenticated group from MountIAM (ticket 009), so
// MountIAM must run first. Until job kinds declare their own action and
// scope (ticket 010), reading needs platform:read and submitting or
// cancelling needs platform:write, both on the global scope.
func (s *Server) RegisterJobs(e *jobs.Engine) {
	if s.protected == nil {
		panic("api: RegisterJobs called before MountIAM; job routes must not be unauthenticated")
	}
	h := &jobsHandler{engine: e}
	g := s.protected.Group("/jobs")
	read, write := s.RequireGlobal(iam.ActionPlatformRead), s.RequireGlobal(iam.ActionPlatformWrite)
	g.GET("", read, h.list)
	g.POST("", write, h.submit)
	g.GET("/:id", read, h.get)
	g.GET("/:id/events", read, h.events)
	g.POST("/:id/cancel", write, h.cancel)
	s.addOperation("GET", "/management/v1/jobs", "list jobs; ?state=a,b&kind=&limit=")
	s.addOperation("POST", "/management/v1/jobs", "submit a job {requestId, kind, params, resources, node, requiredBytes}; 202 + job, same requestId replays the same job, changed content is 409")
	s.addOperation("GET", "/management/v1/jobs/{id}", "job record: state, steps, progress, diagnostics, completion checks, cancellable")
	s.addOperation("GET", "/management/v1/jobs/{id}/events", "events after ?after=<seq> (&limit=); resume from the last seq seen")
	s.addOperation("POST", "/management/v1/jobs/{id}/cancel", "request cancellation; 202 with the job: cancel_requested means not yet stopped")
	s.extras = append(s.extras, func() gin.H { return gin.H{"jobKinds": e.Kinds()} })
}

type jobsHandler struct {
	engine *jobs.Engine
}

// maxSubmitBody bounds a submit request; params belong to the job, bulk
// data goes through uploads (spec 36.2).
const maxSubmitBody = 1 << 20

func (h *jobsHandler) fail(c *gin.Context, err error, requestID string) {
	e := apiError{RequestID: requestID, Message: err.Error()}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, jobs.ErrNotFound):
		status, e.Code = http.StatusNotFound, "JOB_NOT_FOUND"
	case errors.Is(err, jobs.ErrConflict):
		status, e.Code = http.StatusConflict, "REQUEST_ID_CONFLICT"
		e.SuggestedActions = []string{"use-a-new-request-id"}
	case errors.Is(err, jobs.ErrUnknownKind):
		status, e.Code = http.StatusBadRequest, "JOB_KIND_UNKNOWN"
	case errors.Is(err, jobs.ErrInvalidRequest):
		status, e.Code = http.StatusBadRequest, "INVALID_REQUEST"
	case errors.Is(err, jobs.ErrNotCancellable):
		status, e.Code = http.StatusConflict, "JOB_NOT_CANCELLABLE"
	default:
		e.Code, e.Retryable = "INTERNAL", true
	}
	writeError(c, status, e)
}

func (h *jobsHandler) list(c *gin.Context) {
	opts := jobs.ListOptions{Kind: c.Query("kind")}
	if v := c.Query("state"); v != "" {
		for _, s := range strings.Split(v, ",") {
			st := jobs.State(strings.TrimSpace(s))
			if !st.Valid() {
				writeError(c, http.StatusBadRequest, apiError{Code: "INVALID_REQUEST", Message: "unknown state " + string(st)})
				return
			}
			opts.States = append(opts.States, st)
		}
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(c, http.StatusBadRequest, apiError{Code: "INVALID_REQUEST", Message: "limit must be a positive integer"})
			return
		}
		opts.Limit = n
	}
	list, err := h.engine.List(c.Request.Context(), opts)
	if err != nil {
		h.fail(c, err, "")
		return
	}
	if list == nil {
		list = []*jobs.Job{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": list})
}

func (h *jobsHandler) submit(c *gin.Context) {
	var req jobs.SubmitRequest
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxSubmitBody))
	dec.DisallowUnknownFields() // an unknown field is never dropped silently (spec 34.3)
	if err := dec.Decode(&req); err != nil {
		writeError(c, http.StatusBadRequest, apiError{Code: "INVALID_REQUEST", Message: "body must be a JSON submit request: " + err.Error()})
		return
	}
	job, dedup, err := h.engine.Submit(c.Request.Context(), req)
	if err != nil {
		h.fail(c, err, req.RequestID)
		return
	}
	c.Header("Location", "/management/v1/jobs/"+job.ID)
	c.JSON(http.StatusAccepted, gin.H{"job": job, "jobId": job.ID, "deduplicated": dedup})
}

func (h *jobsHandler) get(c *gin.Context) {
	job, err := h.engine.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": job})
}

func (h *jobsHandler) events(c *gin.Context) {
	var after int64
	if v := c.Query("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(c, http.StatusBadRequest, apiError{Code: "INVALID_REQUEST", Message: "after must be a non-negative integer"})
			return
		}
		after = n
	}
	limit := 0
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(c, http.StatusBadRequest, apiError{Code: "INVALID_REQUEST", Message: "limit must be a positive integer"})
			return
		}
		limit = n
	}
	evs, err := h.engine.Events(c.Request.Context(), c.Param("id"), after, limit)
	if err != nil {
		h.fail(c, err, "")
		return
	}
	if evs == nil {
		evs = []jobs.Event{}
	}
	last := after
	if len(evs) > 0 {
		last = evs[len(evs)-1].Seq
	}
	c.JSON(http.StatusOK, gin.H{"events": evs, "lastSeq": last})
}

func (h *jobsHandler) cancel(c *gin.Context) {
	job, err := h.engine.Cancel(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err, "")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job": job, "stopped": job.State == jobs.StateCancelled})
}

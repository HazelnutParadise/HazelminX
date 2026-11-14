package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/composedeploy"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/plans"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
)

// Deploying a Compose unit (ticket 044.2). The deployment itself is a
// change plan of kind compose.deploy (POST /management/v1/change-plans with
// {"kind":"compose.deploy","params":{"projectId":…,"environmentId":…}});
// these routes add the host check on its own and the unit's deployments.

type composeHostCheckRequest struct {
	RequestID string `json:"requestId"`
}

type composeDeploymentsResponse struct {
	Deployments []composedeploy.Deployment `json:"deployments"`
}

// MountComposeDeploy mounts the host check and deployment history of a
// unit. MountProjects must run first.
func (s *Server) MountComposeDeploy(svc *projects.Service, d *composedeploy.Deployer, engine *jobs.Engine) {
	if s.protected == nil {
		panic("api: MountComposeDeploy called before MountIAM")
	}
	const base = "/management/v1"
	scope := func(c *gin.Context) (iam.Scope, bool, error) {
		_, err := svc.GetProject(c.Request.Context(), c.Param("id"))
		if errors.Is(err, projects.ErrNotFound) {
			return iam.Scope{}, false, nil
		}
		if err != nil {
			return iam.Scope{}, false, err
		}
		return projects.Scope(c.Param("id")), true, nil
	}
	read := s.RequireScoped(iam.ActionProjectsRead, scope)
	deploy := s.RequireScoped(iam.ActionProjectsDeploy, scope)
	op := func(o Operation) Operation { o.Auth, o.Tag = true, "projects"; return o }
	u := base + "/projects/{id}/environments/{envId}/compose"
	h := &composeDeployRoutes{svc: svc, d: d, engine: engine}
	if s.reservedKinds == nil {
		s.reservedKinds = map[string]string{}
	}
	s.reservedKinds[composedeploy.KindCheck] = "POST " + u + "/host-checks"
	s.nodeServices = d.Store.DeployedOn

	s.handle(s.protected, base, op(Operation{Method: "POST", Path: u + "/host-checks", ID: "checkComposeUnitOnHost",
		Summary: "check the unit's target host before deploying (images, disk space, host ports, bind mount sources); a read-only job; " +
			"the same requestId replays the same job",
		Permission: "projects:deploy on the project", Request: composeHostCheckRequest{},
		Responses: []Response{{Status: 202, Body: JobAccepted{}, Headers: []string{"Location"}}},
		Errors:    []string{CodeValidationFailed, CodeRequestIDConflict}}), deploy, h.hostCheck)
	s.handle(s.protected, base, op(Operation{Method: "GET", Path: u + "/deployments", ID: "listComposeDeployments",
		Summary: "the unit's deployments, newest first", Permission: "projects:read on the project",
		Responses: []Response{{Status: 200, Body: composeDeploymentsResponse{}}}}), read, h.list)
}

type composeDeployRoutes struct {
	svc    *projects.Service
	d      *composedeploy.Deployer
	engine *jobs.Engine
}

func (h *composeDeployRoutes) hostCheck(c *gin.Context) {
	var in composeHostCheckRequest
	if err := c.ShouldBindJSON(&in); err != nil || in.RequestID == "" {
		fail(c, CodeValidationFailed, "requestId is required")
		return
	}
	req, err := h.d.CheckRequest(c.Request.Context(), c.Param("id"), c.Param("envId"))
	switch {
	case errors.Is(err, projects.ErrNotFound):
		notFound(c)
		return
	case errors.Is(err, plans.ErrPrecheck):
		fail(c, CodeValidationFailed, err.Error())
		return
	case err != nil:
		fail(c, CodeInternal, "internal error")
		return
	}
	req.RequestID = in.RequestID
	req.Requester = projectActor(c).UserID
	job, dedup, err := h.engine.Submit(c.Request.Context(), req)
	if err != nil {
		respond(c, jobError(err, in.RequestID))
		return
	}
	acceptJob(c, job, dedup)
}

func (h *composeDeployRoutes) list(c *gin.Context) {
	u, err := h.svc.GetUnit(c.Request.Context(), c.Param("id"), c.Param("envId"), false)
	if errors.Is(err, projects.ErrNotFound) {
		notFound(c)
		return
	}
	if err != nil {
		fail(c, CodeInternal, "internal error")
		return
	}
	list, err := h.d.Store.List(c.Request.Context(), u.ID)
	if err != nil {
		fail(c, CodeInternal, "internal error")
		return
	}
	// A row still running whose job has ended (failed, cancelled, waiting
	// for a person) is failed with the job's reason: its outcome is not
	// "running", and the job holds the detail.
	for i := range list {
		if list[i].State != composedeploy.StateRunning {
			continue
		}
		j, err := h.engine.Get(c.Request.Context(), list[i].JobID)
		if err != nil {
			continue
		}
		switch j.State {
		case jobs.StateFailed, jobs.StateCancelled, jobs.StateRolledBack, jobs.StateNeedsAttention:
			list[i].State = composedeploy.StateFailed
			list[i].ErrorCode = "JOB_" + strings.ToUpper(string(j.State))
			list[i].ErrorMessage = j.StateReason
			// Record it, so every reader (NOD-008 among them) agrees.
			if err := h.d.Store.Finish(c.Request.Context(), list[i].JobID, list[i].State, nil, list[i].ErrorCode, list[i].ErrorMessage); err != nil {
				fail(c, CodeInternal, "internal error")
				return
			}
		}
	}
	c.JSON(http.StatusOK, composeDeploymentsResponse{Deployments: list})
}

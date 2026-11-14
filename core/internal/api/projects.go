package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
)

// Projects, project environments and Compose units (ticket 044.1; spec 15,
// addendum 04 52.1). Every route below a project is guarded on that
// project's scope: projects:read to look, projects:write to change. A
// project the caller cannot see answers 404 like one that does not exist.
// Creating a project needs projects:write on the global scope, since the
// project scope does not exist yet. Choosing a deployment target also
// needs the caller to see that node.

var projectsList = ListSpec{Name: "projects", Sorts: []string{"name", "createdAt", "updatedAt"},
	DefaultSort: []SortKey{{Field: "name"}}, TieBreaker: "id"}

var projectEnvironmentsList = ListSpec{Name: "project-environments", Sorts: []string{"name", "createdAt"},
	DefaultSort: []SortKey{{Field: "name"}}, TieBreaker: "id"}

type projectResponse struct {
	Project *projects.Project `json:"project"`
}

type projectEnvironmentResponse struct {
	Environment *projects.Environment `json:"environment"`
}

type composeUnitResponse struct {
	Unit     *projects.Unit    `json:"unit"`
	Analysis projects.Analysis `json:"analysis"`
}

type composeCheckResponse struct {
	Analysis projects.Analysis `json:"analysis"`
}

type composePreviewResponse struct {
	Preview projects.Preview `json:"preview"`
}

// MountProjects mounts /management/v1/projects. MountIAM must run first.
func (s *Server) MountProjects(svc *projects.Service, views projects.EngineViews) {
	if s.protected == nil {
		panic("api: MountProjects called before MountIAM; project routes must not be unauthenticated")
	}
	const base = "/management/v1"
	g := s.protected
	h := &projectRoutes{s: s, svc: svc, views: views}
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
	write := s.RequireScoped(iam.ActionProjectsWrite, scope)
	op := func(o Operation) Operation { o.Auth, o.Tag = true, "projects"; return o }
	const readPerm, writePerm = "projects:read on the project", "projects:write on the project"
	p := base + "/projects/{id}"
	e := p + "/environments/{envId}"
	u := e + "/compose"

	s.handle(g, base, op(Operation{Method: "GET", Path: base + "/projects", ID: "listProjects",
		Summary: "projects the caller can read", Permission: "projects:read on each project listed",
		List: &projectsList, Responses: []Response{{Status: 200, Body: ListResponse[projects.Project]{}}}}), h.listProjects)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/projects", ID: "createProject",
		Summary:    "create a project; name follows 52.1 (lowercase letter first, lowercase letters, digits and '-', at most 30)",
		Permission: "projects:write on the global scope", Request: projects.ProjectInput{},
		Responses: []Response{{Status: 201, Body: projectResponse{}, Headers: []string{"Location", "ETag"}}}, Errors: []string{CodeValidationFailed, CodeAlreadyExists}}),
		s.RequireGlobal(iam.ActionProjectsWrite), h.createProject)
	s.handle(g, base, op(Operation{Method: "GET", Path: p, ID: "getProject", Summary: "one project; ETag carries its version", Permission: readPerm,
		Responses: []Response{{Status: 200, Body: projectResponse{}, Headers: []string{"ETag"}}}}), read, h.getProject)
	s.handle(g, base, op(Operation{Method: "PATCH", Path: p, ID: "updateProject",
		Summary:    "rename or describe a project; Compose project names of its units do not change; requires If-Match",
		Permission: writePerm, Request: projects.ProjectPatch{}, Versioned: true,
		Responses: []Response{{Status: 200, Body: projectResponse{}, Headers: []string{"ETag"}}}, Errors: []string{CodeValidationFailed, CodeAlreadyExists}}), write, h.updateProject)
	s.handle(g, base, op(Operation{Method: "DELETE", Path: p, ID: "deleteProject",
		Summary: "delete a project; refused while it has environments; requires If-Match", Permission: writePerm, Versioned: true,
		Responses: []Response{{Status: 204}}, Errors: []string{CodeResourceReferenced}}), write, h.deleteProject)

	s.handle(g, base, op(Operation{Method: "GET", Path: p + "/environments", ID: "listProjectEnvironments",
		Summary: "the project's environments", Permission: readPerm, List: &projectEnvironmentsList,
		Responses: []Response{{Status: 200, Body: ListResponse[projects.Environment]{}}}}), read, h.listEnvironments)
	s.handle(g, base, op(Operation{Method: "POST", Path: p + "/environments", ID: "createProjectEnvironment",
		Summary: "add an environment (production, staging…); name follows 52.1", Permission: writePerm, Request: projects.EnvironmentInput{},
		Responses: []Response{{Status: 201, Body: projectEnvironmentResponse{}, Headers: []string{"Location", "ETag"}}}, Errors: []string{CodeValidationFailed, CodeAlreadyExists}}), write, h.createEnvironment)
	s.handle(g, base, op(Operation{Method: "GET", Path: e, ID: "getProjectEnvironment", Summary: "one environment; ETag carries its version", Permission: readPerm,
		Responses: []Response{{Status: 200, Body: projectEnvironmentResponse{}, Headers: []string{"ETag"}}}}), read, h.getEnvironment)
	s.handle(g, base, op(Operation{Method: "PATCH", Path: e, ID: "updateProjectEnvironment",
		Summary: "rename or describe an environment; the unit's Compose project name does not change; requires If-Match", Permission: writePerm,
		Request: projects.EnvironmentPatch{}, Versioned: true,
		Responses: []Response{{Status: 200, Body: projectEnvironmentResponse{}, Headers: []string{"ETag"}}}, Errors: []string{CodeValidationFailed, CodeAlreadyExists}}), write, h.updateEnvironment)
	s.handle(g, base, op(Operation{Method: "DELETE", Path: e, ID: "deleteProjectEnvironment",
		Summary: "delete an environment; refused while it has a Compose unit; requires If-Match", Permission: writePerm, Versioned: true,
		Responses: []Response{{Status: 204}}, Errors: []string{CodeResourceReferenced}}), write, h.deleteEnvironment)

	const unitIn = "files are kept byte for byte (UTF-8 text, at most 200 files, 256 KiB each, 768 KiB in all); mainFile and extraFiles are the -f order; " +
		"secret files go to the secret store and are never returned; the answer carries the check of the saved files"
	s.handle(g, base, op(Operation{Method: "GET", Path: u, ID: "getComposeUnit",
		Summary: "the environment's Compose unit with the content of its files that are not secret, and the check of those files", Permission: readPerm,
		Responses: []Response{{Status: 200, Body: composeUnitResponse{}, Headers: []string{"ETag"}}}}), read, h.getUnit)
	s.handle(g, base, op(Operation{Method: "POST", Path: u, ID: "createComposeUnit",
		Summary:    "import a complete Compose project as the environment's unit (upload or editor); its Compose project name is fixed as <project>-<environment>-<unit>; " + unitIn,
		Permission: writePerm + "; a target node must be visible to the caller", Request: projects.UnitInput{},
		Responses: []Response{{Status: 201, Body: composeUnitResponse{}, Headers: []string{"Location", "ETag"}}}, Errors: []string{CodeValidationFailed, CodeAlreadyExists}}), write, h.createUnit)
	s.handle(g, base, op(Operation{Method: "PUT", Path: u, ID: "replaceComposeUnit",
		Summary:    "replace the unit's files and settings; name and Compose project name stay; a stored secret file is kept with unchanged=true; " + unitIn + "; requires If-Match",
		Permission: writePerm + "; a target node must be visible to the caller", Request: projects.UnitInput{}, Versioned: true,
		Responses: []Response{{Status: 200, Body: composeUnitResponse{}, Headers: []string{"ETag"}}}, Errors: []string{CodeValidationFailed}}), write, h.replaceUnit)
	s.handle(g, base, op(Operation{Method: "DELETE", Path: u, ID: "deleteComposeUnit",
		Summary: "remove the unit and its secret files from HazelminX; nothing on any host is changed; requires If-Match", Permission: writePerm, Versioned: true,
		Responses: []Response{{Status: 204}}}), write, h.deleteUnit)
	s.handle(g, base, op(Operation{Method: "POST", Path: u + "/check", ID: "checkComposeUnit",
		Summary:    "check files as they would be saved, without saving: what Docker Compose accepts, refuses and why, what is kept but not applied, variables, services, mounts and high-risk settings",
		Permission: writePerm, Request: projects.UnitInput{},
		Responses: []Response{{Status: 200, Body: composeCheckResponse{}}}, Errors: []string{CodeValidationFailed}}), write, h.checkUnit)
	s.handle(g, base, op(Operation{Method: "GET", Path: u + "/preview", ID: "previewComposeUnit",
		Summary:    "what deploying the saved unit would do: the check, the target engine's state from the node's latest engine report, whether it can deploy, whether it needs approval, and the host checks still to run (ticket 044.2)",
		Permission: readPerm,
		Responses:  []Response{{Status: 200, Body: composePreviewResponse{}}}}), read, h.previewUnit)
}

type projectRoutes struct {
	s     *Server
	svc   *projects.Service
	views projects.EngineViews
}

func projectActor(c *gin.Context) projects.Actor {
	src := source(c)
	a := projects.Actor{TraceID: src.TraceID, SourceIP: src.IP}
	if p := Principal(c); p != nil {
		a.UserID, a.Username = p.User.ID, p.User.Username
	}
	return a
}

func (h *projectRoutes) fail(c *gin.Context, err error, current func() int64) {
	var ve *projects.ValidationError
	var re *projects.ReferencedError
	switch {
	case errors.As(err, &ve):
		e := newError(CodeValidationFailed, ve.Error())
		e.Fields = []FieldError{{Field: ve.Field, Message: ve.Message}}
		respond(c, e)
	case errors.As(err, &re):
		e := newError(CodeResourceReferenced, "delete its "+re.What+" first")
		e.Details = map[string]any{"ids": re.IDs}
		respond(c, e)
	case errors.Is(err, projects.ErrNotFound):
		notFound(c)
	case errors.Is(err, projects.ErrExists):
		fail(c, CodeAlreadyExists, "the name is already used")
	case errors.Is(err, projects.ErrVersionMismatch):
		var cur int64
		if current != nil {
			cur = current()
		}
		respond(c, versionMismatch(c.Param("id"), cur))
	default:
		slog.Error("projects route failed", "path", c.FullPath(), "trace", c.GetString(TraceIDHdr), "error", err)
		fail(c, CodeInternal, "internal error")
	}
}

// visibleProjects is the caller's read filter for lists.
func visibleProjects(p *iam.Principal) func(id string) bool {
	global, scopes := iam.ScopesFor(p, iam.ActionProjectsRead)
	ids := map[string]bool{}
	for _, sc := range scopes {
		if sc.Kind == iam.ScopeProject {
			ids[sc.ID] = true
		}
	}
	return func(id string) bool { return global || ids[id] }
}

func (h *projectRoutes) listProjects(c *gin.Context) {
	all, err := h.svc.ListProjects(c.Request.Context())
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	can := visibleProjects(Principal(c))
	rows := make([]projects.Project, 0, len(all))
	for _, p := range all {
		if can(p.ID) {
			rows = append(rows, p)
		}
	}
	runList[projects.Project](c, projectsList, memorySource[projects.Project]{rows: rows, value: func(p projects.Project, f string) string {
		switch f {
		case "name":
			return p.Name
		case "createdAt":
			return p.CreatedAt.UTC().Format(sortTime)
		case "updatedAt":
			return p.UpdatedAt.UTC().Format(sortTime)
		}
		return p.ID
	}})
}

func (h *projectRoutes) createProject(c *gin.Context) {
	var in projects.ProjectInput
	if !bindJSON(c, &in, "body must be JSON with name and optional displayName and description") {
		return
	}
	p, err := h.svc.CreateProject(c.Request.Context(), projectActor(c), in)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	c.Header("Location", "/management/v1/projects/"+p.ID)
	setETag(c, p.Version)
	c.JSON(http.StatusCreated, projectResponse{Project: p})
}

func (h *projectRoutes) projectVersion(c *gin.Context) func() int64 {
	return func() int64 {
		if p, err := h.svc.GetProject(c.Request.Context(), c.Param("id")); err == nil {
			return p.Version
		}
		return 0
	}
}

func (h *projectRoutes) getProject(c *gin.Context) {
	p, err := h.svc.GetProject(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	setETag(c, p.Version)
	c.JSON(http.StatusOK, projectResponse{Project: p})
}

func (h *projectRoutes) updateProject(c *gin.Context) {
	v, ok := requireIfMatch(c)
	if !ok {
		return
	}
	var in projects.ProjectPatch
	if !bindJSON(c, &in, "body must be JSON with any of name, displayName, description") {
		return
	}
	p, err := h.svc.UpdateProject(c.Request.Context(), projectActor(c), c.Param("id"), v, in)
	if err != nil {
		h.fail(c, err, h.projectVersion(c))
		return
	}
	setETag(c, p.Version)
	c.JSON(http.StatusOK, projectResponse{Project: p})
}

func (h *projectRoutes) deleteProject(c *gin.Context) {
	v, ok := requireIfMatch(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteProject(c.Request.Context(), projectActor(c), c.Param("id"), v); err != nil {
		h.fail(c, err, h.projectVersion(c))
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *projectRoutes) listEnvironments(c *gin.Context) {
	rows, err := h.svc.ListEnvironments(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	runList[projects.Environment](c, projectEnvironmentsList, memorySource[projects.Environment]{rows: rows, value: func(e projects.Environment, f string) string {
		switch f {
		case "name":
			return e.Name
		case "createdAt":
			return e.CreatedAt.UTC().Format(sortTime)
		}
		return e.ID
	}})
}

func (h *projectRoutes) createEnvironment(c *gin.Context) {
	var in projects.EnvironmentInput
	if !bindJSON(c, &in, "body must be JSON with name and optional description") {
		return
	}
	e, err := h.svc.CreateEnvironment(c.Request.Context(), projectActor(c), c.Param("id"), in)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	c.Header("Location", "/management/v1/projects/"+e.ProjectID+"/environments/"+e.ID)
	setETag(c, e.Version)
	c.JSON(http.StatusCreated, projectEnvironmentResponse{Environment: e})
}

func (h *projectRoutes) envVersion(c *gin.Context) func() int64 {
	return func() int64 {
		if e, err := h.svc.GetEnvironment(c.Request.Context(), c.Param("id"), c.Param("envId")); err == nil {
			return e.Version
		}
		return 0
	}
}

func (h *projectRoutes) getEnvironment(c *gin.Context) {
	e, err := h.svc.GetEnvironment(c.Request.Context(), c.Param("id"), c.Param("envId"))
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	setETag(c, e.Version)
	c.JSON(http.StatusOK, projectEnvironmentResponse{Environment: e})
}

func (h *projectRoutes) updateEnvironment(c *gin.Context) {
	v, ok := requireIfMatch(c)
	if !ok {
		return
	}
	var in projects.EnvironmentPatch
	if !bindJSON(c, &in, "body must be JSON with any of name, description") {
		return
	}
	e, err := h.svc.UpdateEnvironment(c.Request.Context(), projectActor(c), c.Param("id"), c.Param("envId"), v, in)
	if err != nil {
		h.fail(c, err, h.envVersion(c))
		return
	}
	setETag(c, e.Version)
	c.JSON(http.StatusOK, projectEnvironmentResponse{Environment: e})
}

func (h *projectRoutes) deleteEnvironment(c *gin.Context) {
	v, ok := requireIfMatch(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteEnvironment(c.Request.Context(), projectActor(c), c.Param("id"), c.Param("envId"), v); err != nil {
		h.fail(c, err, h.envVersion(c))
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *projectRoutes) unitVersion(c *gin.Context) func() int64 {
	return func() int64 {
		if u, err := h.svc.GetUnit(c.Request.Context(), c.Param("id"), c.Param("envId"), false); err == nil {
			return u.Version
		}
		return 0
	}
}

// targetVisible: a deployment target must be a node the caller may see.
// An unknown and an invisible node get the same answer.
func (h *projectRoutes) targetVisible(c *gin.Context, in projects.UnitInput) bool {
	if in.Target == nil {
		return true
	}
	if iam.Visible(Principal(c), iam.Scope{Kind: iam.ScopeNode, ID: in.Target.NodeID}) {
		return true
	}
	e := newError(CodeValidationFailed, "target.nodeId: no such node")
	e.Fields = []FieldError{{Field: "target.nodeId", Message: "no such node"}}
	respond(c, e)
	return false
}

const unitHint = "body must be JSON with source, mainFile, files and optional name, extraFiles, workingDir, profiles and target"

func (h *projectRoutes) respondUnit(c *gin.Context, status int, u *projects.Unit) {
	a, err := h.svc.AnalyzeUnit(c.Request.Context(), u)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	setETag(c, u.Version)
	c.JSON(status, composeUnitResponse{Unit: u, Analysis: a})
}

func (h *projectRoutes) getUnit(c *gin.Context) {
	u, err := h.svc.GetUnit(c.Request.Context(), c.Param("id"), c.Param("envId"), true)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	h.respondUnit(c, http.StatusOK, u)
}

func (h *projectRoutes) createUnit(c *gin.Context) {
	var in projects.UnitInput
	if !bindJSON(c, &in, unitHint) || !h.targetVisible(c, in) {
		return
	}
	u, err := h.svc.CreateUnit(c.Request.Context(), projectActor(c), c.Param("id"), c.Param("envId"), in)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	c.Header("Location", "/management/v1/projects/"+u.ProjectID+"/environments/"+u.EnvironmentID+"/compose")
	h.respondUnit(c, http.StatusCreated, u)
}

func (h *projectRoutes) replaceUnit(c *gin.Context) {
	v, ok := requireIfMatch(c)
	if !ok {
		return
	}
	var in projects.UnitInput
	if !bindJSON(c, &in, unitHint) || !h.targetVisible(c, in) {
		return
	}
	u, err := h.svc.ReplaceUnit(c.Request.Context(), projectActor(c), c.Param("id"), c.Param("envId"), v, in)
	if err != nil {
		h.fail(c, err, h.unitVersion(c))
		return
	}
	h.respondUnit(c, http.StatusOK, u)
}

func (h *projectRoutes) deleteUnit(c *gin.Context) {
	v, ok := requireIfMatch(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteUnit(c.Request.Context(), projectActor(c), c.Param("id"), c.Param("envId"), v); err != nil {
		h.fail(c, err, h.unitVersion(c))
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *projectRoutes) checkUnit(c *gin.Context) {
	var in projects.UnitInput
	if !bindJSON(c, &in, unitHint) {
		return
	}
	a, err := h.svc.CheckInput(c.Request.Context(), c.Param("id"), c.Param("envId"), in)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	c.JSON(http.StatusOK, composeCheckResponse{Analysis: a})
}

func (h *projectRoutes) previewUnit(c *gin.Context) {
	u, err := h.svc.GetUnit(c.Request.Context(), c.Param("id"), c.Param("envId"), false)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	see := u.Target == nil || iam.Visible(Principal(c), iam.Scope{Kind: iam.ScopeNode, ID: u.Target.NodeID})
	pv, err := h.svc.PreviewUnit(c.Request.Context(), u, h.views, see)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	c.JSON(http.StatusOK, composePreviewResponse{Preview: pv})
}

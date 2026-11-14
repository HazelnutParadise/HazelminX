package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/github"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitsource"
	"github.com/HazelnutParadise/hazelminx/core/internal/gitunits"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/projects"
)

// Compose units whose files come from Git (ticket 049.1, decision 0036).

type gitUnitResponse struct {
	Unit     *projects.Unit    `json:"unit"`
	Source   gitunits.Source   `json:"source"`
	Analysis projects.Analysis `json:"analysis"`
}

type gitCheckResponse struct {
	Source   gitunits.Source   `json:"source"`
	Analysis projects.Analysis `json:"analysis"`
}

type gitSourceResponse struct {
	Source gitunits.Source `json:"source"`
}

// MountGitUnits mounts the Git source routes of a unit. MountProjects and
// MountGitHub must run first.
func (s *Server) MountGitUnits(svc *gitunits.Service, gh *github.Service) {
	if s.protected == nil {
		panic("api: MountGitUnits called before MountIAM")
	}
	const base = "/management/v1"
	scope := func(c *gin.Context) (iam.Scope, bool, error) {
		_, err := svc.Units.GetProject(c.Request.Context(), c.Param("id"))
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
	u := base + "/projects/{id}/environments/{envId}/compose/git"
	h := &gitRoutes{svc: svc, pr: &projectRoutes{s: s, svc: svc.Units}, gh: gh}
	const how = "either github {connectionId, repoId} (a connection of the caller) or url (https:// or ssh://) with an optional credential " +
		"{kind: none | token | ssh-key, username, token, privateKey, hostKey}; ref is a branch, tag or commit, fixed to a commit SHA by Core; " +
		"Core fetches only that commit and takes mainFile, extraFiles and the files they mount, as env files, secrets and configs; " +
		"symbolic links, submodules, Git LFS files and local builds are refused (ticket 049.2)"
	errs := []string{CodeValidationFailed, CodeGitSourceRefused, CodeGitAuthFailed, CodeGitUnavailable, CodeGitHubReauthRequired, CodeGitHubUnavailable, CodeGitHubRepoEmpty}
	s.handle(s.protected, base, op(Operation{Method: "GET", Path: u, ID: "getComposeGitSource",
		Summary: "where the unit's files came from: repository, ref, commit and the paths taken; credentials are never returned", Permission: "projects:read on the project",
		Responses: []Response{{Status: 200, Body: gitSourceResponse{}}}}), read, h.get)
	s.handle(s.protected, base, op(Operation{Method: "POST", Path: u, ID: "importComposeUnitFromGit",
		Summary:    "make the environment's unit from one commit of a Git repository; " + how,
		Permission: "projects:write on the project; a target node must be visible to the caller", Request: gitunits.Input{},
		Responses: []Response{{Status: 201, Body: gitUnitResponse{}, Headers: []string{"Location", "ETag"}}}, Errors: append(errs, CodeAlreadyExists)}), write, h.create)
	s.handle(s.protected, base, op(Operation{Method: "PUT", Path: u, ID: "updateComposeUnitFromGit",
		Summary: "move a Git unit to the commit ref names now, or change its repository or files; credential {unchanged: true} keeps the stored one for the same URL; " +
			how + "; requires If-Match",
		Permission: "projects:write on the project; a target node must be visible to the caller", Request: gitunits.Input{}, Versioned: true,
		Responses: []Response{{Status: 200, Body: gitUnitResponse{}, Headers: []string{"ETag"}}}, Errors: errs}), write, h.replace)
	s.handle(s.protected, base, op(Operation{Method: "POST", Path: u + "/check", ID: "checkComposeUnitFromGit",
		Summary:    "fetch and check a Git source without saving: the commit it resolves to, the files that would be taken and their check; " + how,
		Permission: "projects:write on the project", Request: gitunits.Input{},
		Responses: []Response{{Status: 200, Body: gitCheckResponse{}}}, Errors: errs}), write, h.check)
}

type gitRoutes struct {
	svc *gitunits.Service
	pr  *projectRoutes
	gh  *github.Service
}

const gitHint = "body must be JSON with github or url, ref, mainFile and optional name, credential, extraFiles, workingDir, profiles and target"

func (h *gitRoutes) fail(c *gin.Context, err error, current func() int64) {
	var re *gitunits.RefusedError
	switch {
	case errors.As(err, &re):
		e := newError(CodeGitSourceRefused, re.Error())
		e.Details = map[string]any{"refusals": re.Refusals}
		respond(c, e)
	case errors.Is(err, gitsource.ErrAuth), errors.Is(err, gitsource.ErrHostKey):
		fail(c, CodeGitAuthFailed, strings.TrimPrefix(err.Error(), "gitsource: "))
	case errors.Is(err, gitsource.ErrNotFound):
		e := newError(CodeValidationFailed, strings.TrimPrefix(err.Error(), "gitsource: "))
		e.Fields = []FieldError{{Field: "ref", Message: "no such repository, branch, tag or commit"}}
		respond(c, e)
	case errors.Is(err, gitsource.ErrInvalidURL), errors.Is(err, gitsource.ErrAddress):
		e := newError(CodeValidationFailed, strings.TrimPrefix(err.Error(), "gitsource: "))
		e.Fields = []FieldError{{Field: "url", Message: strings.TrimPrefix(err.Error(), "gitsource: ")}}
		respond(c, e)
	case errors.Is(err, gitsource.ErrTooLarge), errors.Is(err, gitsource.ErrUnavailable), errors.Is(err, gitsource.ErrCommitChanged):
		fail(c, CodeGitUnavailable, strings.TrimPrefix(err.Error(), "gitsource: "))
	case errors.Is(err, github.ErrNotFound):
		notFound(c)
	case isGitHubErr(err):
		(&githubRoutes{svc: h.gh}).fail(c, err)
	default:
		h.pr.fail(c, err, current)
	}
}

func isGitHubErr(err error) bool {
	var rl *github.RateLimitedError
	var ge *github.APIError
	return errors.As(err, &rl) || errors.As(err, &ge) || errors.Is(err, github.ErrUnavailable) || errors.Is(err, github.ErrNeedsReauth) ||
		errors.Is(err, github.ErrDisabled) || errors.Is(err, github.ErrEmptyRepository) || errors.Is(err, github.ErrRefreshBackoff)
}

func (h *gitRoutes) bind(c *gin.Context) (gitunits.Input, bool) {
	var in gitunits.Input
	if !bindJSON(c, &in, gitHint) {
		return in, false
	}
	return in, h.pr.targetVisible(c, projects.UnitInput{Target: in.Target})
}

func (h *gitRoutes) respond(c *gin.Context, status int, r gitunits.Result) {
	setETag(c, r.Unit.Version)
	c.JSON(status, gitUnitResponse{Unit: r.Unit, Source: r.Source, Analysis: r.Analysis})
}

func (h *gitRoutes) get(c *gin.Context) {
	u, err := h.svc.Units.GetUnit(c.Request.Context(), c.Param("id"), c.Param("envId"), false)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	src, err := h.svc.Get(c.Request.Context(), u.ID)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	setETag(c, u.Version)
	c.JSON(http.StatusOK, gitSourceResponse{Source: *src})
}

func (h *gitRoutes) create(c *gin.Context) {
	in, ok := h.bind(c)
	if !ok {
		return
	}
	r, err := h.svc.Import(c.Request.Context(), projectActor(c), c.Param("id"), c.Param("envId"), 0, in)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	c.Header("Location", "/management/v1/projects/"+r.Unit.ProjectID+"/environments/"+r.Unit.EnvironmentID+"/compose")
	h.respond(c, http.StatusCreated, r)
}

func (h *gitRoutes) replace(c *gin.Context) {
	v, ok := requireIfMatch(c)
	if !ok {
		return
	}
	in, ok := h.bind(c)
	if !ok {
		return
	}
	r, err := h.svc.Import(c.Request.Context(), projectActor(c), c.Param("id"), c.Param("envId"), v, in)
	if err != nil {
		h.fail(c, err, h.pr.unitVersion(c))
		return
	}
	h.respond(c, http.StatusOK, r)
}

func (h *gitRoutes) check(c *gin.Context) {
	in, ok := h.bind(c)
	if !ok {
		return
	}
	r, err := h.svc.Check(c.Request.Context(), projectActor(c), c.Param("id"), c.Param("envId"), in)
	if err != nil {
		h.fail(c, err, nil)
		return
	}
	c.JSON(http.StatusOK, gitCheckResponse{Source: r.Source, Analysis: r.Analysis})
}

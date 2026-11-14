package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HazelnutParadise/hazelminx/core/internal/github"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
)

// GitHub Apps and connections (ticket 047.1, spec 15.4～15.9). Connecting
// GitHub gives HazelminX a deployment source; it is not a HazelminX login
// and never changes HazelminX roles. A connection belongs to the account
// that made it: everyone else gets 404. Apps are registered by holders of
// platform:write on the global scope. No answer carries a GitHub secret.

var githubConnectionsList = ListSpec{Name: "github-connections", Sorts: []string{"name", "createdAt"},
	DefaultSort: []SortKey{{Field: "createdAt"}}, TieBreaker: "id",
	Filters: []FilterSpec{
		{Name: "kind", Summary: "only connections of this kind", Enum: []string{github.KindPAT, github.KindAppUser, github.KindAppInstallation}},
		{Name: "state", Summary: "only connections in this state", Enum: []string{github.StateActive, github.StateDisabled, github.StateNeedsReauth}},
		{Name: "accountId", Summary: "only connections of this GitHub account id"},
	}}

var githubAppsList = ListSpec{Name: "github-apps", Sorts: []string{"name", "createdAt"},
	DefaultSort: []SortKey{{Field: "name"}}, TieBreaker: "id"}

type githubAppResponse struct {
	App github.App `json:"app"`
}

type githubConnectionResponse struct {
	Connection github.Connection `json:"connection"`
}

type githubStartResponse struct {
	Authorization github.Started `json:"authorization"`
}

type githubCompleteResponse struct {
	Result github.Completed `json:"result"`
}

type githubCancelRequest struct {
	State string `json:"state"`
}

type githubCredentialRequest struct {
	Token string `json:"token"`
}

type githubReachabilityResponse struct {
	Reachability github.Reachability `json:"reachability"`
}

// githubCanUse: connections are for accounts that can deploy somewhere
// (projects:deploy on any scope) or manage the platform.
func githubCanUse(p *iam.Principal) bool {
	if iam.Authorize(p, iam.ActionPlatformWrite, iam.Global) {
		return true
	}
	global, scopes := iam.ScopesFor(p, iam.ActionProjectsDeploy)
	return global || len(scopes) > 0
}

// MountGitHub mounts /management/v1/github. MountIAM must run first.
func (s *Server) MountGitHub(svc *github.Service) {
	if s.protected == nil {
		panic("api: MountGitHub called before MountIAM; GitHub routes must not be unauthenticated")
	}
	const base = "/management/v1"
	g := s.protected
	h := &githubRoutes{s: s, svc: svc}
	manage := s.RequireGlobal(iam.ActionPlatformWrite)
	use := func(c *gin.Context) {
		p := Principal(c)
		if !githubCanUse(p) {
			s.iam.RecordDenied(c.Request.Context(), p, iam.ActionProjectsDeploy, iam.Global, source(c))
			fail(c, CodeForbidden, "GitHub connections need projects:deploy on some scope or platform:write")
			c.Abort()
			return
		}
		c.Next()
	}
	op := func(o Operation) Operation { o.Auth, o.Tag = true, "github"; return o }
	gh := []string{CodeGitHubRejected, CodeGitHubUnavailable}
	const usePerm = "projects:deploy on any scope, or platform:write; a connection is visible only to the account that created it"

	s.handle(g, base, op(Operation{Method: "GET", Path: base + "/github/apps", ID: "listGitHubApps",
		Summary: "GitHub Apps registered in this HazelminX (no secrets), with their permissions, events and install address", Permission: usePerm,
		List: &githubAppsList, Responses: []Response{{Status: 200, Body: ListResponse[github.App]{}}}}), use, h.listApps)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/apps", ID: "createGitHubApp",
		Summary:    "register an App created on GitHub by hand: App ID, client ID and secret, private key and optional webhook secret; GitHub is asked which App the key signs for, a mismatch is refused; the secrets stay encrypted in Core",
		Permission: "platform:write on the global scope", Request: github.ManualApp{},
		Responses: []Response{{Status: 201, Body: githubAppResponse{}, Headers: []string{"Location"}}}, Errors: append([]string{CodeValidationFailed}, gh...)}), manage, h.createApp)
	s.handle(g, base, op(Operation{Method: "GET", Path: base + "/github/apps/{id}", ID: "getGitHubApp",
		Summary: "one registered App", Permission: usePerm,
		Responses: []Response{{Status: 200, Body: githubAppResponse{}}}}), use, h.getApp)
	s.handle(g, base, op(Operation{Method: "DELETE", Path: base + "/github/apps/{id}", ID: "deleteGitHubApp",
		Summary:    "remove a registered App and its secrets here (the App stays on GitHub); refused while connections use it",
		Permission: "platform:write on the global scope",
		Responses:  []Response{{Status: 204}}, Errors: []string{CodeResourceReferenced}}), manage, h.deleteApp)
	s.handle(g, base, op(Operation{Method: "GET", Path: base + "/github/apps/{id}/reachability", ID: "checkGitHubReachability",
		Summary:    "two separate checks: whether GitHub sends the browser back to a configured panel address, and what GitHub reports about delivering the App's webhook; periodic checks work without either; nothing is opened or published",
		Permission: usePerm,
		Responses:  []Response{{Status: 200, Body: githubReachabilityResponse{}}}}), use, h.reachability)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/app-manifests", ID: "startGitHubAppManifest",
		Summary:    "start the guided App creation: a manifest asking for contents and metadata read only, plus the capabilities named (webhook, status-write); the browser POSTs it to authorization.url; needs the panel's public origin",
		Permission: "platform:write on the global scope; a signed-in browser session", Request: github.ManifestInput{},
		Responses: []Response{{Status: 201, Body: githubStartResponse{}}}, Errors: []string{CodeValidationFailed, CodeNotAvailable}}), manage, h.startManifest)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/authorizations", ID: "startGitHubAuthorization",
		Summary:    "start a browser authorization with a registered App: mode authorize (user authorization with PKCE) or install (install on an account, then authorize); connectionId authorizes an existing connection again; the state is one-time, bound to this session and valid for 10 minutes",
		Permission: usePerm + "; a signed-in browser session", Request: github.AuthorizeInput{},
		Responses: []Response{{Status: 201, Body: githubStartResponse{}}}, Errors: []string{CodeValidationFailed, CodeNotAvailable}}), use, h.startAuthorization)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/authorizations/complete", ID: "completeGitHubAuthorization",
		Summary:    "finish a browser authorization with what GitHub sent to the callback page; the code is exchanged and the account, App and installation are checked with GitHub before anything is saved; a refused callback changes no connection",
		Permission: usePerm + "; the same account and session that started it", Request: github.Callback{},
		Responses: []Response{{Status: 200, Body: githubCompleteResponse{}}}, Errors: append([]string{CodeGitHubAuthInvalid, CodeValidationFailed}, gh...)}), use, h.complete)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/authorizations/cancel", ID: "cancelGitHubAuthorization",
		Summary: "abandon a browser authorization; nothing else changes", Permission: usePerm + "; the same account and session that started it",
		Request: githubCancelRequest{}, Responses: []Response{{Status: 204}}, Errors: []string{CodeGitHubAuthInvalid}}), use, h.cancel)

	s.handle(g, base, op(Operation{Method: "GET", Path: base + "/github/connections", ID: "listGitHubConnections",
		Summary:    "the caller's GitHub connections: GitHub account (stable id and current login), kind, granted scopes or permissions, capabilities, state and expiry; never a token",
		Permission: usePerm, List: &githubConnectionsList,
		Responses: []Response{{Status: 200, Body: ListResponse[github.Connection]{}}}}), use, h.listConnections)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/connections", ID: "createGitHubPATConnection",
		Summary:    "save a personal access token as a reusable connection; GitHub is asked who it is, whether it can list repositories and when it expires; the token is stored encrypted and never shown again",
		Permission: usePerm, Request: github.PATInput{},
		Responses: []Response{{Status: 201, Body: githubConnectionResponse{}, Headers: []string{"Location", "ETag"}}}, Errors: append([]string{CodeValidationFailed}, gh...)}), use, h.createPAT)
	s.handle(g, base, op(Operation{Method: "GET", Path: base + "/github/connections/{id}", ID: "getGitHubConnection",
		Summary: "one connection; ETag carries its version", Permission: usePerm,
		Responses: []Response{{Status: 200, Body: githubConnectionResponse{}, Headers: []string{"ETag"}}}}), use, h.getConnection)
	s.handle(g, base, op(Operation{Method: "PATCH", Path: base + "/github/connections/{id}", ID: "updateGitHubConnection",
		Summary: "rename a connection; the GitHub identity is not changed; requires If-Match", Permission: usePerm,
		Request: github.Patch{}, Versioned: true,
		Responses: []Response{{Status: 200, Body: githubConnectionResponse{}, Headers: []string{"ETag"}}}, Errors: []string{CodeValidationFailed}}), use, h.updateConnection)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/connections/{id}/disable", ID: "disableGitHubConnection",
		Summary: "stop using a connection until it is enabled again; nothing is revoked on GitHub", Permission: usePerm,
		Responses: []Response{{Status: 200, Body: githubConnectionResponse{}}}}), use, h.disable)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/connections/{id}/enable", ID: "enableGitHubConnection",
		Summary: "use a disabled connection again", Permission: usePerm,
		Responses: []Response{{Status: 200, Body: githubConnectionResponse{}}}}), use, h.enable)
	s.handle(g, base, op(Operation{Method: "POST", Path: base + "/github/connections/{id}/check", ID: "checkGitHubConnection",
		Summary:    "ask GitHub about the connection now: identity (a renamed account keeps its id; another id marks the connection needs-reauth), expiry and, for installations, permissions and suspension; renews an App user token close to expiry",
		Permission: usePerm,
		Responses:  []Response{{Status: 200, Body: githubConnectionResponse{}}}, Errors: gh}), use, h.check)
	s.handle(g, base, op(Operation{Method: "PUT", Path: base + "/github/connections/{id}/credential", ID: "rotateGitHubConnectionCredential",
		Summary:    "replace the token of a PAT connection; the new token must belong to the same GitHub account; other connections are not touched; requires If-Match",
		Permission: usePerm, Request: githubCredentialRequest{}, Versioned: true,
		Responses: []Response{{Status: 200, Body: githubConnectionResponse{}}}, Errors: append([]string{CodeValidationFailed, CodeGitHubAuthInvalid}, gh...)}), use, h.rotate)
	s.handle(g, base, op(Operation{Method: "DELETE", Path: base + "/github/connections/{id}", ID: "deleteGitHubConnection",
		Summary: "remove a connection and its stored credentials here; nothing is revoked on GitHub; requires If-Match", Permission: usePerm, Versioned: true,
		Responses: []Response{{Status: 204}}}), use, h.deleteConnection)

	h.mountRepos(g, base, op, use, usePerm)
}

type githubRoutes struct {
	s   *Server
	svc *github.Service
}

func githubActor(c *gin.Context) github.Actor {
	p := Principal(c)
	src := source(c)
	a := github.Actor{TraceID: src.TraceID, SourceIP: src.IP}
	if p != nil {
		a.UserID, a.Username = p.User.ID, p.User.Username
		if p.Kind == "session" {
			a.SessionID = p.SessionID
		}
		a.CanManageApps = iam.Authorize(p, iam.ActionPlatformWrite, iam.Global)
	}
	return a
}

// githubCtx: a call that may reach GitHub and then write runs to its end
// even when the caller goes away, so a code exchanged is not lost halfway.
func githubCtx(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(c.Request.Context()), 60*time.Second)
}

func (h *githubRoutes) fail(c *gin.Context, err error) {
	var ve *github.ValidationError
	var ae *github.AuthorizationError
	var re *github.ReferencedError
	var ge *github.APIError
	var rl *github.RateLimitedError
	switch {
	case errors.As(err, &ve):
		e := newError(CodeValidationFailed, ve.Error())
		e.Fields = []FieldError{{Field: ve.Field, Message: ve.Message}}
		respond(c, e)
	case errors.As(err, &ae):
		e := newError(CodeGitHubAuthInvalid, "this GitHub authorization was not accepted; start it again")
		e.Details = map[string]any{"reason": ae.Reason}
		respond(c, e)
	case errors.As(err, &re):
		e := newError(CodeResourceReferenced, re.Error())
		e.Details = map[string]any{"connections": re.Connections}
		respond(c, e)
	case errors.Is(err, github.ErrNotFound):
		notFound(c)
	case errors.Is(err, github.ErrForbidden):
		fail(c, CodeForbidden, "registering GitHub Apps needs platform:write on the global scope")
	case errors.Is(err, github.ErrVersionMismatch):
		cur := int64(0)
		if conn, e := h.svc.GetConnection(c.Request.Context(), c.Param("id"), githubActor(c)); e == nil {
			cur = conn.Version
		}
		respond(c, versionMismatch(c.Param("id"), cur))
	case errors.Is(err, github.ErrNotAvailable):
		fail(c, CodeNotAvailable, strings.TrimPrefix(err.Error(), "github: not available: "))
	case errors.As(err, &rl):
		e := newError(CodeGitHubUnavailable, "GitHub rate limited this connection; it is not asked again, and no other connection is used, before retryAfterSeconds")
		e.RetryAfter = max(int(time.Until(rl.RetryAt).Seconds())+1, 1)
		respond(c, e)
	case errors.Is(err, github.ErrEmptyRepository):
		fail(c, CodeGitHubRepoEmpty, "the repository has no commits yet")
	case errors.Is(err, github.ErrNeedsReauth), errors.Is(err, github.ErrDisabled):
		fail(c, CodeGitHubReauthRequired, strings.TrimPrefix(err.Error(), "github: "))
	case errors.As(err, &ge) && errors.Is(err, github.ErrUnavailable), errors.Is(err, github.ErrUnavailable), errors.Is(err, github.ErrRefreshBackoff):
		e := newError(CodeGitHubUnavailable, "GitHub could not be reached or rate limited the request; nothing was changed")
		if ge != nil && ge.RetryAfter > 0 {
			e.RetryAfter = int(ge.RetryAfter.Seconds())
		}
		respond(c, e)
	case errors.Is(err, github.ErrRejected):
		fail(c, CodeGitHubRejected, err.Error())
	default:
		slog.Error("github route failed", "path", c.FullPath(), "trace", c.GetString(TraceIDHdr), "error", err)
		fail(c, CodeInternal, "internal error")
	}
}

func (h *githubRoutes) listApps(c *gin.Context) {
	apps, err := h.svc.ListApps(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	runList[github.App](c, githubAppsList, memorySource[github.App]{rows: apps, value: func(a github.App, f string) string {
		switch f {
		case "name":
			return a.Name
		case "createdAt":
			return a.CreatedAt.UTC().Format(sortTime)
		}
		return a.ID
	}})
}

func (h *githubRoutes) createApp(c *gin.Context) {
	var in github.ManualApp
	if !bindJSON(c, &in, "body must be JSON with appId, clientId, clientSecret, privateKey and optional name and webhookSecret") {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	a, err := h.svc.CreateManualApp(ctx, in, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Location", "/management/v1/github/apps/"+a.ID)
	c.JSON(http.StatusCreated, githubAppResponse{App: a})
}

func (h *githubRoutes) getApp(c *gin.Context) {
	a, err := h.svc.GetApp(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubAppResponse{App: a})
}

func (h *githubRoutes) deleteApp(c *gin.Context) {
	ctx, cancel := githubCtx(c)
	defer cancel()
	if err := h.svc.DeleteApp(ctx, c.Param("id"), githubActor(c)); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *githubRoutes) reachability(c *gin.Context) {
	r, err := h.svc.CheckReachability(c.Request.Context(), c.Param("id"), c.Request.Host)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubReachabilityResponse{Reachability: r})
}

func (h *githubRoutes) startManifest(c *gin.Context) {
	var in github.ManifestInput
	if !bindJSON(c, &in, "body must be JSON with name and optional organization, capabilities and webhookUrl") {
		return
	}
	st, err := h.svc.StartManifest(c.Request.Context(), in, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, githubStartResponse{Authorization: st})
}

func (h *githubRoutes) startAuthorization(c *gin.Context) {
	var in github.AuthorizeInput
	if !bindJSON(c, &in, "body must be JSON with appId and optional mode, name and connectionId") {
		return
	}
	st, err := h.svc.StartAuthorization(c.Request.Context(), in, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, githubStartResponse{Authorization: st})
}

func (h *githubRoutes) complete(c *gin.Context) {
	var in github.Callback
	if !bindJSON(c, &in, "body must be JSON with state, code and, after an installation, installationId and setupAction") {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	out, err := h.svc.Complete(ctx, in, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, githubCompleteResponse{Result: out})
}

func (h *githubRoutes) cancel(c *gin.Context) {
	var in githubCancelRequest
	if !bindJSON(c, &in, "body must be JSON with state") {
		return
	}
	if err := h.svc.Cancel(c.Request.Context(), in.State, githubActor(c)); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *githubRoutes) listConnections(c *gin.Context) {
	q, perr := parseListQuery(c, withListDefaults(githubConnectionsList))
	if perr != nil {
		respond(c, *perr)
		return
	}
	all, err := h.svc.ListConnections(c.Request.Context(), githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	first := func(k string) string {
		if v := q.Filters[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	kind, state, account := first("kind"), first("state"), first("accountId")
	rows := all[:0]
	for _, conn := range all {
		if (kind == "" || conn.Kind == kind) && (state == "" || conn.State == state) && (account == "" || strconv.FormatInt(conn.Account.ID, 10) == account) {
			rows = append(rows, conn)
		}
	}
	runList[github.Connection](c, githubConnectionsList, memorySource[github.Connection]{rows: rows, value: func(conn github.Connection, f string) string {
		switch f {
		case "name":
			return conn.Name
		case "createdAt":
			return conn.CreatedAt.UTC().Format(sortTime)
		}
		return conn.ID
	}})
}

func (h *githubRoutes) createPAT(c *gin.Context) {
	var in github.PATInput
	if !bindJSON(c, &in, "body must be JSON with token and optional name") {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	conn, err := h.svc.CreatePAT(ctx, in, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Location", "/management/v1/github/connections/"+conn.ID)
	setETag(c, conn.Version)
	c.JSON(http.StatusCreated, githubConnectionResponse{Connection: conn})
}

func (h *githubRoutes) getConnection(c *gin.Context) {
	conn, err := h.svc.GetConnection(c.Request.Context(), c.Param("id"), githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	setETag(c, conn.Version)
	c.JSON(http.StatusOK, githubConnectionResponse{Connection: conn})
}

func (h *githubRoutes) updateConnection(c *gin.Context) {
	// Existence before the version condition: another account's connection
	// is 404 whatever header it sends.
	if _, err := h.svc.GetConnection(c.Request.Context(), c.Param("id"), githubActor(c)); err != nil {
		h.fail(c, err)
		return
	}
	version, ok := requireIfMatch(c)
	if !ok {
		return
	}
	var in github.Patch
	if !bindJSON(c, &in, "body must be JSON with name") {
		return
	}
	conn, err := h.svc.UpdateConnection(c.Request.Context(), c.Param("id"), version, in, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	setETag(c, conn.Version)
	c.JSON(http.StatusOK, githubConnectionResponse{Connection: conn})
}

func (h *githubRoutes) setEnabled(c *gin.Context, enabled bool) {
	conn, err := h.svc.SetEnabled(c.Request.Context(), c.Param("id"), enabled, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	setETag(c, conn.Version)
	c.JSON(http.StatusOK, githubConnectionResponse{Connection: conn})
}

func (h *githubRoutes) disable(c *gin.Context) { h.setEnabled(c, false) }
func (h *githubRoutes) enable(c *gin.Context)  { h.setEnabled(c, true) }

func (h *githubRoutes) check(c *gin.Context) {
	ctx, cancel := githubCtx(c)
	defer cancel()
	conn, err := h.svc.Check(ctx, c.Param("id"), githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	setETag(c, conn.Version)
	c.JSON(http.StatusOK, githubConnectionResponse{Connection: conn})
}

func (h *githubRoutes) rotate(c *gin.Context) {
	if _, err := h.svc.GetConnection(c.Request.Context(), c.Param("id"), githubActor(c)); err != nil {
		h.fail(c, err)
		return
	}
	version, ok := requireIfMatch(c)
	if !ok {
		return
	}
	var in githubCredentialRequest
	if !bindJSON(c, &in, "body must be JSON with token") {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	conn, err := h.svc.RotatePAT(ctx, c.Param("id"), version, in.Token, githubActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	setETag(c, conn.Version)
	c.JSON(http.StatusOK, githubConnectionResponse{Connection: conn})
}

func (h *githubRoutes) deleteConnection(c *gin.Context) {
	if _, err := h.svc.GetConnection(c.Request.Context(), c.Param("id"), githubActor(c)); err != nil {
		h.fail(c, err)
		return
	}
	version, ok := requireIfMatch(c)
	if !ok {
		return
	}
	ctx, cancel := githubCtx(c)
	defer cancel()
	if err := h.svc.DeleteConnection(ctx, c.Param("id"), version, githubActor(c)); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

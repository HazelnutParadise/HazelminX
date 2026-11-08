package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Cookie and header names of the browser session (36.3).
const (
	SessionCookie = "hazelminx_session"
	CSRFCookie    = "hazelminx_csrf"
	CSRFHeader    = "X-CSRF-Token"
	TraceIDHdr    = "X-Trace-ID"
)

const principalKey = "hazelminx.principal"
const sessionKey = "hazelminx.session"

// IAMOptions configures MountIAM.
type IAMOptions struct {
	// Tokens accepts "Authorization: Bearer" credentials (ticket 015). Nil
	// means only cookies authenticate.
	Tokens iam.TokenAuthenticator
	// TrustedProxies are the CIDRs whose X-Forwarded-For is believed when
	// deriving the client IP for throttling and audit. Empty trusts nobody,
	// so a direct client cannot forge its source (ticket 065 wires Zentinel).
	TrustedProxies []string
	// PublicOrigin is the management address users open, e.g.
	// "https://panel.example.com". Passkeys are bound to its host name. Empty
	// derives the origin from each request (scheme and Host header); the
	// browser still enforces that a passkey only works on its own domain.
	PublicOrigin string
}

// MountIAM installs authentication, CSRF and authorization on /management/v1
// and registers the IAM operations. Routes added through Management()
// afterwards inherit the session requirement.
func (s *Server) MountIAM(svc *iam.Service, opts IAMOptions) {
	s.iam = svc
	s.origin = strings.TrimSuffix(opts.PublicOrigin, "/")
	_ = s.engine.SetTrustedProxies(opts.TrustedProxies)

	const base = "/management/v1"
	pub := s.engine.Group(base)
	s.handle(pub, base, Operation{Method: "GET", Path: base + "/setup", ID: "getSetup", Tag: "auth",
		Summary:   "whether the first owner still has to be created",
		Responses: []Response{{Status: 200, Body: setupStatusResponse{}}}}, s.setupStatus)
	s.handle(pub, base, Operation{Method: "POST", Path: base + "/setup", ID: "completeSetup", Tag: "auth",
		Summary: "create the first owner with the one-time setup code and sign them in (held to second-factor enrolment)", Request: setupRequest{},
		Responses: []Response{{Status: 201, Body: loginResponse{}, Headers: []string{"Set-Cookie"}}},
		Errors:    []string{CodeSetupDone, CodeSetupCodeInvalid, CodeTooManyAttempts}}, s.setupOwner)
	s.handle(pub, base, Operation{Method: "POST", Path: base + "/auth/login", ID: "login", Tag: "auth",
		Summary: "password login; signs in (step signed-in, sets the cookies) or, when the account has a second factor, returns a single-use challenge (step second-factor)",
		Request: loginRequest{},
		Responses: []Response{{Status: 200, Body: loginResponse{}, Headers: []string{"Set-Cookie"},
			Description: "signed in, or a second-factor challenge (step tells which)"}},
		Errors: []string{CodeInvalidCredentials, CodeTooManyAttempts}}, s.login)
	s.mountSecondFactorLogin(pub, base)

	s.protected = s.engine.Group(base, s.requireSession(opts.Tokens), s.csrf(), s.enrollmentGate())
	p := s.protected
	auth := func(op Operation) Operation { op.Auth = true; return op }
	s.handle(p, base, auth(Operation{Method: "GET", Path: base + "/auth/session", ID: "getSession", Tag: "auth",
		Summary: "current user and role assignments", Responses: []Response{{Status: 200, Body: sessionResponse{}}}}), s.whoami)
	s.handle(p, base, auth(Operation{Method: "POST", Path: base + "/auth/logout", ID: "logout", Tag: "auth",
		Summary: "revoke the current session", Responses: []Response{{Status: 204, Description: "signed out"}}}), s.logout)
	s.handle(p, base, auth(Operation{Method: "GET", Path: base + "/roles", ID: "listRoles", Tag: "users",
		Summary: "role presets and the actions they grant", Responses: []Response{{Status: 200, Body: rolesResponse{}}}}), s.listRoles)
	s.handle(p, base, auth(Operation{Method: "GET", Path: base + "/users", ID: "listUsers", Tag: "users",
		Summary: "list accounts", Permission: "users:read on the global scope", List: &usersList,
		Responses: []Response{{Status: 200, Body: ListResponse[iam.UserDetail]{}}}}), s.listUsers)
	s.handle(p, base, auth(Operation{Method: "POST", Path: base + "/users", ID: "createUser", Tag: "users",
		Summary: "create a local account", Permission: "users:write on the global scope", Request: iam.CreateUserInput{},
		Responses: []Response{{Status: 201, Body: userResponse{}, Headers: []string{"ETag"}}},
		Errors:    []string{CodeAlreadyExists}}), s.createUser)
	s.handle(p, base, auth(Operation{Method: "GET", Path: base + "/users/{id}", ID: "getUser", Tag: "users",
		Summary: "one account with assignments; ETag carries its version", Permission: "users:read on the global scope",
		Responses: []Response{{Status: 200, Body: userDetailResponse{}, Headers: []string{"ETag"}}}}), s.getUser)
	s.handle(p, base, auth(Operation{Method: "PATCH", Path: base + "/users/{id}", ID: "updateUser", Tag: "users",
		Summary: "change account fields (displayName); requires If-Match", Permission: "users:write on the global scope",
		Request: iam.UpdateUserInput{}, Versioned: true,
		Responses: []Response{{Status: 200, Body: userDetailResponse{}, Headers: []string{"ETag"}}}}), s.updateUser)
	s.handle(p, base, auth(Operation{Method: "POST", Path: base + "/users/{id}/roles", ID: "assignRole", Tag: "users",
		Summary: "assign a role on a scope; If-Match optional; advances the account version", Permission: "roles:assign on the global scope",
		Request: assignRequest{}, Params: []Param{optionalIfMatch},
		Responses: []Response{{Status: 201, Body: assignmentResponse{}, Headers: []string{"ETag"}}},
		Errors:    []string{CodeAlreadyExists, CodeVersionMismatch}}), s.assignRole)
	s.handle(p, base, auth(Operation{Method: "DELETE", Path: base + "/users/{id}/roles/{assignmentId}", ID: "revokeRole", Tag: "users",
		Summary: "revoke an assignment; If-Match optional; advances the account version", Permission: "roles:assign on the global scope",
		Params:    []Param{optionalIfMatch},
		Responses: []Response{{Status: 204, Description: "revoked", Headers: []string{"ETag"}}},
		Errors:    []string{CodeVersionMismatch}}), s.revokeRole)
	s.handle(p, base, auth(Operation{Method: "GET", Path: base + "/audit-events", ID: "listAuditEvents", Tag: "audit",
		Summary: "audit ledger, limited to scopes where the account holds audit:read", Permission: "audit:read on at least one scope",
		List: &auditList, Responses: []Response{{Status: 200, Body: ListResponse[iam.AuditEvent]{}}}}), s.listAudit)
	s.mountSecondFactors(p, base)
}

var optionalIfMatch = Param{Name: "If-Match", In: "header", Description: `optional "v<version>" of the account; when sent it must be current`,
	Schema: jsonSchema{"type": "string", "pattern": `^"v[0-9]+"$`}}

var usersList = ListSpec{Name: "users", Sorts: []string{"username", "createdAt"},
	DefaultSort: []SortKey{{Field: "username"}}, TieBreaker: "id"}

var auditList = ListSpec{Name: "audit-events", Sorts: []string{"seq", "at"},
	DefaultSort: []SortKey{{Field: "seq", Desc: true}}, TieBreaker: "seq",
	Filters: []FilterSpec{
		{Name: "actorId", Summary: "events by this user id"},
		{Name: "jobId", Summary: "events of this job"},
		{Name: "resourceId", Summary: "events on this resource"},
		{Name: "scopeKind", Summary: "events on this scope kind (with scopeId)", Enum: []string{"global", "node", "project", "database"}},
		{Name: "scopeId", Summary: "events on this scope id (with scopeKind)"},
		{Name: "afterSeq", Summary: "only events with seq greater than this; with sort=seq, follows the ledger forward", Int: true},
	}}

// Management is the authenticated /management/v1 group for later tickets.
// It is nil until MountIAM runs.
func (s *Server) Management() *gin.RouterGroup { return s.protected }

// Principal returns the authenticated principal of the request, if any.
func Principal(c *gin.Context) *iam.Principal {
	if v, ok := c.Get(principalKey); ok {
		return v.(*iam.Principal)
	}
	return nil
}

// ---- error format (36.2) -------------------------------------------------

type apiError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"requestId,omitempty"` // operation request identifier (spec 33)
	TraceID    string `json:"traceId,omitempty"`   // this HTTP call, also in X-Trace-ID
	JobID      string `json:"jobId,omitempty"`
	ResourceID string `json:"resourceId,omitempty"`
	Retryable  bool   `json:"retryable"`
	RetryAfter int    `json:"retryAfterSeconds,omitempty"`
	// Evidence links (event ids, log refs) that back the refusal (36.2).
	Evidence []string `json:"evidence,omitempty"`
	// SuggestedActions are possible next operations; none is pre-authorized.
	SuggestedActions []string `json:"suggestedActions,omitempty"`
	// Fields names the inputs a synchronous validation rejected (36.1).
	Fields []FieldError `json:"fields,omitempty"`
	// Details is the code-specific structured data documented in the error
	// table (currentVersion, referencedBy, mode, confirmationId, checks…).
	Details map[string]any `json:"details,omitempty"`
	// AlsoBlockedBy lists lower-precedence codes that also applied.
	AlsoBlockedBy []string `json:"alsoBlockedBy,omitempty"`
}

// FieldError is one rejected input.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// newError builds an error with the table's default retryability.
func newError(code, msg string) apiError {
	return apiError{Code: code, Message: msg, Retryable: lookupCode(code).Retryable}
}

// fail answers with code from the error table and a plain message.
func fail(c *gin.Context, code, msg string) { respond(c, newError(code, msg)) }

// notFound is the single answer for "does not exist" and "you may not know
// whether it exists" (T-014). Nothing in it depends on the lookup result.
func notFound(c *gin.Context) { fail(c, CodeNotFound, "no such resource") }

func failErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, iam.ErrValidation):
		fail(c, "VALIDATION_FAILED", strings.TrimPrefix(err.Error(), "iam: validation failed: "))
	case errors.Is(err, iam.ErrConflict):
		fail(c, CodeAlreadyExists, strings.TrimPrefix(err.Error(), "iam: conflict: "))
	case errors.Is(err, iam.ErrVersionMismatch):
		var vm *iam.VersionMismatchError
		errors.As(err, &vm)
		e := versionMismatch(c.Param("id"), vm.Current)
		respond(c, e)
	case errors.Is(err, iam.ErrForbidden):
		fail(c, "FORBIDDEN", "this account is not allowed to perform the operation")
	case errors.Is(err, iam.ErrNotFound):
		notFound(c)
	case errors.Is(err, iam.ErrSetupDone):
		fail(c, "SETUP_DONE", "setup has already been completed")
	case errors.Is(err, iam.ErrSetupCode):
		fail(c, "SETUP_CODE_INVALID", "setup code is invalid or expired")
	case errors.Is(err, iam.ErrThrottled):
		fail(c, "TOO_MANY_ATTEMPTS", "too many attempts; try again later")
	case errors.Is(err, iam.ErrInvalidCredentials):
		fail(c, CodeInvalidCredentials, "username or password is incorrect")
	case errors.Is(err, iam.ErrChallengeInvalid):
		fail(c, CodeLoginChallengeInvalid, "the sign-in step expired or was already used; sign in again")
	case errors.Is(err, iam.ErrSecondFactor):
		fail(c, CodeSecondFactorInvalid, "the verification code, recovery code or passkey was not accepted")
	case errors.Is(err, iam.ErrEnrollmentRequired):
		e := newError(CodeEnrollmentRequired, "this account must add an authenticator app or passkey before doing anything else")
		e.SuggestedActions = []string{"enroll-second-factor"}
		respond(c, e)
	case errors.Is(err, iam.ErrReauthRequired):
		e := newError(CodeReauthRequired, "confirm your password and second factor again before changing sign-in methods")
		e.SuggestedActions = []string{"reauthenticate"}
		respond(c, e)
	case errors.Is(err, iam.ErrLastFactor):
		fail(c, CodeLastSecondFactor, "this account must keep at least one authenticator app or passkey; add another one first")
	case errors.Is(err, iam.ErrUnavailable):
		fail(c, CodeNotAvailable, strings.TrimPrefix(err.Error(), "iam: not available: "))
	default:
		fail(c, "INTERNAL", "internal error")
	}
}

// ---- middlewares ----------------------------------------------------------

// traceID gives every call a trace identifier (client-supplied or generated)
// that errors and audit events carry as traceId. It is unrelated to the
// requestId that names an operation for deduplication.
func traceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(TraceIDHdr)
		if id == "" || len(id) > 128 {
			id = uuid.NewString()
		}
		c.Set(TraceIDHdr, id)
		c.Header(TraceIDHdr, id)
		c.Next()
	}
}

func source(c *gin.Context) iam.Source {
	return iam.Source{IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), TraceID: c.GetString(TraceIDHdr)}
}

// requireSession authenticates by cookie, or by bearer token when a
// TokenAuthenticator is configured. Anything else is 401.
func (s *Server) requireSession(tokens iam.TokenAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cookie, err := c.Cookie(SessionCookie); err == nil && cookie != "" {
			p, sess, err := s.iam.Authenticate(c.Request.Context(), cookie)
			if err != nil {
				clearSessionCookies(c)
				fail(c, "UNAUTHENTICATED", "sign in to continue")
				return
			}
			c.Set(principalKey, p)
			c.Set(sessionKey, sess)
			if sess.Renewed {
				// The idle expiry moved; give the browser the new lifetime
				// too, or the cookie dies at its first Max-Age.
				renewed := *sess
				renewed.Token = cookie
				setSessionCookies(c, &renewed)
			}
			c.Next()
			return
		}
		if auth := c.GetHeader("Authorization"); tokens != nil && strings.HasPrefix(auth, "Bearer ") {
			p, err := tokens.Authenticate(c.Request.Context(), strings.TrimPrefix(auth, "Bearer "))
			if err != nil {
				fail(c, "UNAUTHENTICATED", "token rejected")
				return
			}
			p.Kind = "token"
			c.Set(principalKey, p)
			c.Next()
			return
		}
		fail(c, "UNAUTHENTICATED", "sign in to continue")
	}
}

// csrf protects cookie-authenticated state changes (36.3): the request must
// carry the session's CSRF token in a header, and when the browser sends
// Origin or Referer it must match this host. Token principals skip it.
func (s *Server) csrf() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		v, ok := c.Get(sessionKey)
		if !ok {
			c.Next() // token principal
			return
		}
		sess := v.(*iam.Session)
		if c.GetHeader(CSRFHeader) != sess.CSRFToken {
			fail(c, "CSRF_TOKEN_MISMATCH", "missing or wrong "+CSRFHeader+" header")
			return
		}
		if o := c.GetHeader("Origin"); o != "" && !sameHost(o, c.Request.Host) {
			fail(c, "ORIGIN_MISMATCH", "cross-site request refused")
			return
		} else if r := c.GetHeader("Referer"); o == "" && r != "" && !sameHost(r, c.Request.Host) {
			fail(c, "ORIGIN_MISMATCH", "cross-site request refused")
			return
		}
		c.Next()
	}
}

func sameHost(raw, host string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

// RequireGlobal refuses with 403 unless the principal holds action globally.
func (s *Server) RequireGlobal(a iam.Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := Principal(c)
		if !iam.Authorize(p, a, iam.Global) {
			s.iam.RecordDenied(c.Request.Context(), p, a, iam.Global, source(c))
			fail(c, "FORBIDDEN", "this account is not allowed to perform the operation")
			return
		}
		c.Next()
	}
}

// ScopeLookup finds the owning scope of the resource a request addresses.
// found=false means the resource does not exist.
type ScopeLookup func(c *gin.Context) (scope iam.Scope, found bool, err error)

// RequireScoped guards a resource route. Missing resources and resources the
// principal may not even see both answer the same 404; visible but not
// permitted answers 403 and is audited.
func (s *Server) RequireScoped(a iam.Action, lookup ScopeLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := Principal(c)
		scope, found, err := lookup(c)
		if err != nil {
			fail(c, "INTERNAL", "internal error")
			return
		}
		if !found || !iam.Visible(p, scope) {
			if found {
				s.iam.RecordDenied(c.Request.Context(), p, a, scope, source(c))
			}
			notFound(c)
			return
		}
		if !iam.Authorize(p, a, scope) {
			s.iam.RecordDenied(c.Request.Context(), p, a, scope, source(c))
			fail(c, "FORBIDDEN", "this account is not allowed to perform the operation")
			return
		}
		c.Set("hazelminx.scope", scope)
		c.Next()
	}
}

// ---- handlers --------------------------------------------------------------

type setupStatusResponse struct {
	NeedsSetup bool `json:"needsSetup"`
}

type userResponse struct {
	User *iam.User `json:"user"`
}

type userDetailResponse struct {
	User *iam.UserDetail `json:"user"`
}

// loginResponse answers login, setup and the second login step. Step is
// "signed-in" (cookies set; User, CSRFToken, ExpiresAt and
// SecondFactorEnrollmentRequired filled) or "second-factor" (no cookie;
// ChallengeToken, Methods and ChallengeExpiresAt filled).
type loginResponse struct {
	Step                           string     `json:"step"`
	User                           *iam.User  `json:"user,omitempty"`
	CSRFToken                      string     `json:"csrfToken,omitempty"`
	ExpiresAt                      *time.Time `json:"expiresAt,omitempty"`
	SecondFactorEnrollmentRequired bool       `json:"secondFactorEnrollmentRequired"`
	ChallengeToken                 string     `json:"challengeToken,omitempty"`
	Methods                        []string   `json:"methods,omitempty"`
	ChallengeExpiresAt             *time.Time `json:"challengeExpiresAt,omitempty"`
}

type sessionResponse struct {
	Kind        string           `json:"kind"`
	User        iam.User         `json:"user"`
	Assignments []iam.Assignment `json:"assignments"`
	Session     *iam.Session     `json:"session,omitempty"`
	CSRFToken   string           `json:"csrfToken,omitempty"`
	// SecondFactorEnrollmentRequired: the account must add a second factor
	// before anything but enrolment works (IAM-002).
	SecondFactorEnrollmentRequired bool `json:"secondFactorEnrollmentRequired"`
}

type roleInfo struct {
	Role iam.Role `json:"role"`
	// Actions is the granted action list, or ["*"] for the owner.
	Actions []string `json:"actions"`
}

type rolesResponse struct {
	Roles []roleInfo `json:"roles"`
}

type assignmentResponse struct {
	Assignment *iam.Assignment `json:"assignment"`
}

func (s *Server) setupStatus(c *gin.Context) {
	need, err := s.iam.NeedsSetup(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, setupStatusResponse{NeedsSetup: need})
}

type setupRequest struct {
	SetupCode string `json:"setupCode"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

func (s *Server) setupOwner(c *gin.Context) {
	var in setupRequest
	if !bindJSON(c, &in, "body must be JSON with setupCode, username and password") {
		return
	}
	res, err := s.iam.CompleteSetup(c.Request.Context(), in.SetupCode, in.Username, in.Password, source(c))
	if errors.Is(err, iam.ErrThrottled) {
		throttled(c, res.RetryAfter)
		return
	}
	if err != nil {
		failErr(c, err)
		return
	}
	// The new owner is signed in; the session is held to second-factor
	// enrolment until they add one (Setup board: account → 2FA → codes).
	s.signedIn(c, http.StatusCreated, res)
}

// signedIn sets the session cookies and answers with the signed-in body.
func (s *Server) signedIn(c *gin.Context, status int, res *iam.LoginResult) {
	setSessionCookies(c, res.Session)
	exp := res.Session.ExpiresAt.UTC().Truncate(time.Second)
	c.JSON(status, loginResponse{Step: "signed-in", User: res.User, CSRFToken: res.Session.CSRFToken, ExpiresAt: &exp,
		SecondFactorEnrollmentRequired: res.EnrollmentRequired})
}

func throttled(c *gin.Context, retry time.Duration) {
	secs := int(retry.Seconds()) + 1
	c.Header("Retry-After", strconv.Itoa(secs))
	e := newError(CodeTooManyAttempts, "too many attempts; try again later")
	e.RetryAfter = secs
	respond(c, e)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) login(c *gin.Context) {
	var in loginRequest
	if !bindJSON(c, &in, "body must be JSON with username and password") {
		return
	}
	src := source(c)
	src.RPID = s.relyingParty(c).ID
	res, err := s.iam.Login(c.Request.Context(), in.Username, in.Password, src)
	if errors.Is(err, iam.ErrThrottled) {
		throttled(c, res.RetryAfter)
		return
	}
	if err != nil {
		failErr(c, err)
		return
	}
	if res.Challenge != nil {
		// Password accepted; no cookie until the second factor is.
		exp := res.Challenge.ExpiresAt.UTC().Truncate(time.Second)
		c.JSON(http.StatusOK, loginResponse{Step: "second-factor", ChallengeToken: res.Challenge.Token,
			Methods: res.Challenge.Methods, ChallengeExpiresAt: &exp})
		return
	}
	s.signedIn(c, http.StatusOK, res)
}

func (s *Server) logout(c *gin.Context) {
	if err := s.iam.Logout(c.Request.Context(), Principal(c), source(c)); err != nil {
		failErr(c, err)
		return
	}
	clearSessionCookies(c)
	c.Status(http.StatusNoContent)
}

func (s *Server) whoami(c *gin.Context) {
	p := Principal(c)
	out := sessionResponse{Kind: p.Kind, User: p.User, Assignments: p.Assignments, SecondFactorEnrollmentRequired: p.EnrollmentRequired}
	if v, ok := c.Get(sessionKey); ok {
		out.Session = v.(*iam.Session)
		out.CSRFToken = out.Session.CSRFToken
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) listRoles(c *gin.Context) {
	out := rolesResponse{Roles: []roleInfo{}}
	for _, r := range iam.Roles {
		if r == iam.RoleOwner {
			out.Roles = append(out.Roles, roleInfo{Role: r, Actions: []string{"*"}})
			continue
		}
		var acts []string
		for _, a := range iam.Grants(r) {
			acts = append(acts, string(a))
		}
		out.Roles = append(out.Roles, roleInfo{Role: r, Actions: acts})
	}
	c.JSON(http.StatusOK, out)
}

// usersSource lists accounts for one principal.
type usersSource struct {
	svc *iam.Service
	p   *iam.Principal
}

func (u usersSource) Count(ctx context.Context, _ ListQuery) (int, error) {
	return u.svc.CountUsers(ctx, u.p)
}
func (u usersSource) Fetch(ctx context.Context, q ListQuery, n int) ([]iam.UserDetail, error) {
	return u.svc.ListUsersPage(ctx, u.p, q.paging(n))
}
func (u usersSource) Key(row iam.UserDetail, sort []SortKey) []string {
	return sortValues(sort, row.User.SortValue)
}

func (s *Server) listUsers(c *gin.Context) {
	if !iam.Authorize(Principal(c), iam.ActionUsersRead, iam.Global) {
		s.iam.RecordDenied(c.Request.Context(), Principal(c), iam.ActionUsersRead, iam.Global, source(c))
		fail(c, CodeForbidden, "this account is not allowed to perform the operation")
		return
	}
	runList[iam.UserDetail](c, usersList, usersSource{svc: s.iam, p: Principal(c)})
}

func (s *Server) createUser(c *gin.Context) {
	var in iam.CreateUserInput
	if !bindJSON(c, &in, "body must be JSON with username, displayName and password") {
		return
	}
	u, err := s.iam.CreateUser(c.Request.Context(), Principal(c), in, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	setETag(c, u.Version)
	c.JSON(http.StatusCreated, userResponse{User: u})
}

func (s *Server) getUser(c *gin.Context) {
	u, err := s.iam.GetUser(c.Request.Context(), Principal(c), c.Param("id"))
	if err != nil {
		failErr(c, err)
		return
	}
	setETag(c, u.Version)
	c.JSON(http.StatusOK, userDetailResponse{User: u})
}

func (s *Server) updateUser(c *gin.Context) {
	ctx, p := c.Request.Context(), Principal(c)
	// Existence and permission come before the version check (51.6).
	if _, err := s.iam.GetUser(ctx, p, c.Param("id")); err != nil {
		failErr(c, err)
		return
	}
	if !iam.Authorize(p, iam.ActionUsersWrite, iam.Global) {
		s.iam.RecordDenied(ctx, p, iam.ActionUsersWrite, iam.Global, source(c))
		fail(c, CodeForbidden, "this account is not allowed to perform the operation")
		return
	}
	var in iam.UpdateUserInput
	if !bindJSON(c, &in, "body must be JSON with displayName") {
		return
	}
	ver, ok := requireIfMatch(c)
	if !ok {
		return
	}
	u, err := s.iam.UpdateUser(ctx, p, c.Param("id"), ver, in, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	setETag(c, u.Version)
	c.JSON(http.StatusOK, userDetailResponse{User: u})
}

type assignRequest struct {
	Role  iam.Role  `json:"role"`
	Scope iam.Scope `json:"scope"`
}

// optionalVersion reads an If-Match that may be absent (0).
func optionalVersion(c *gin.Context) (int64, bool) {
	if c.GetHeader("If-Match") == "" {
		return 0, true
	}
	return requireIfMatch(c)
}

func (s *Server) assignRole(c *gin.Context) {
	var in assignRequest
	if !bindJSON(c, &in, "body must be JSON with role and scope{kind,id}") {
		return
	}
	ver, ok := optionalVersion(c)
	if !ok {
		return
	}
	if in.Scope.Kind == "" {
		in.Scope = iam.Global
	}
	ctx := c.Request.Context()
	as, newVer, err := s.iam.AssignRoleIf(ctx, Principal(c), c.Param("id"), in.Role, in.Scope, ver, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	setETag(c, newVer)
	c.JSON(http.StatusCreated, assignmentResponse{Assignment: as})
}

func (s *Server) revokeRole(c *gin.Context) {
	ver, ok := optionalVersion(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	newVer, err := s.iam.RevokeRoleIf(ctx, Principal(c), c.Param("assignmentId"), c.Param("id"), ver, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	setETag(c, newVer)
	c.Status(http.StatusNoContent)
}

// auditSource lists the ledger for one principal.
type auditSource struct {
	svc *iam.Service
	p   *iam.Principal
	f   iam.AuditFilter
}

func (a auditSource) Count(ctx context.Context, _ ListQuery) (int, error) {
	return a.svc.CountAudit(ctx, a.p, a.f)
}
func (a auditSource) Fetch(ctx context.Context, q ListQuery, n int) ([]iam.AuditEvent, error) {
	f := a.f
	f.Page = q.paging(n)
	return a.svc.ListAudit(ctx, a.p, f)
}
func (a auditSource) Key(row iam.AuditEvent, sort []SortKey) []string {
	return sortValues(sort, row.SortValue)
}

// auditFilter turns validated list filters into an AuditFilter.
func auditFilter(q ListQuery) (iam.AuditFilter, *apiError) {
	one := func(k string) string {
		if v := q.Filters[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	f := iam.AuditFilter{ActorID: one("actorId"), JobID: one("jobId"), ResourceID: one("resourceId")}
	if v := one("afterSeq"); v != "" {
		f.AfterSeq, _ = strconv.ParseInt(v, 10, 64)
	}
	kind, id := one("scopeKind"), one("scopeId")
	if (kind == "") != (id == "") {
		e := newError(CodeValidationFailed, "scopeKind and scopeId go together")
		e.Fields = []FieldError{{Field: "scopeKind", Message: "send both or neither"}, {Field: "scopeId", Message: "send both or neither"}}
		return f, &e
	}
	if kind != "" {
		f.Scope = &iam.Scope{Kind: iam.ScopeKind(kind), ID: id}
	}
	return f, nil
}

func (s *Server) listAudit(c *gin.Context) {
	p := Principal(c)
	if global, scopes := iam.ScopesFor(p, iam.ActionAuditRead); !global && len(scopes) == 0 {
		s.iam.RecordDenied(c.Request.Context(), p, iam.ActionAuditRead, iam.Global, source(c))
		fail(c, CodeForbidden, "this account is not allowed to perform the operation")
		return
	}
	q, perr := parseListQuery(c, auditList)
	if perr != nil {
		respond(c, *perr)
		return
	}
	f, ferr := auditFilter(q)
	if ferr != nil {
		respond(c, *ferr)
		return
	}
	runList[iam.AuditEvent](c, auditList, auditSource{svc: s.iam, p: p, f: f})
}

// ---- cookies ---------------------------------------------------------------

func secureRequest(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

func setSessionCookies(c *gin.Context, sess *iam.Session) {
	secure := secureRequest(c)
	// The lifetime comes from the session itself, not the wall clock, so it
	// agrees with the service clock (tests inject one).
	maxAge := int(sess.ExpiresAt.Sub(sess.LastSeenAt).Seconds())
	http.SetCookie(c.Writer, &http.Cookie{Name: SessionCookie, Value: sess.Token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
	http.SetCookie(c.Writer, &http.Cookie{Name: CSRFCookie, Value: sess.CSRFToken, Path: "/", HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func clearSessionCookies(c *gin.Context) {
	for _, n := range []string{SessionCookie, CSRFCookie} {
		http.SetCookie(c.Writer, &http.Cookie{Name: n, Value: "", Path: "/", HttpOnly: n == SessionCookie, MaxAge: -1})
	}
}

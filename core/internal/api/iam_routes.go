package api

import (
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
}

// MountIAM installs authentication, CSRF and authorization on /management/v1
// and registers the IAM operations. Routes added through Management()
// afterwards inherit the session requirement.
func (s *Server) MountIAM(svc *iam.Service, opts IAMOptions) {
	s.iam = svc
	_ = s.engine.SetTrustedProxies(opts.TrustedProxies)

	pub := s.engine.Group("/management/v1", traceID())
	pub.GET("/setup", s.setupStatus)
	pub.POST("/setup", s.setupOwner)
	pub.POST("/auth/login", s.login)

	s.protected = s.engine.Group("/management/v1", traceID(), s.requireSession(opts.Tokens), s.csrf())
	p := s.protected
	p.GET("/auth/session", s.whoami)
	p.POST("/auth/logout", s.logout)
	p.GET("/roles", s.listRoles)
	p.GET("/users", s.listUsers)
	p.POST("/users", s.createUser)
	p.GET("/users/:id", s.getUser)
	p.POST("/users/:id/roles", s.assignRole)
	p.DELETE("/users/:id/roles/:assignmentId", s.revokeRole)
	p.GET("/audit-events", s.listAudit)

	s.addOperation("GET", "/management/v1/setup", "whether the first owner still has to be created")
	s.addOperation("POST", "/management/v1/setup", "create the first owner with the one-time setup code")
	s.addOperation("POST", "/management/v1/auth/login", "password login; sets the session cookie")
	s.addOperation("POST", "/management/v1/auth/logout", "revoke the current session")
	s.addOperation("GET", "/management/v1/auth/session", "current user and role assignments")
	s.addOperation("GET", "/management/v1/roles", "role presets and the actions they grant")
	s.addOperation("GET", "/management/v1/users", "list accounts (users:read)")
	s.addOperation("POST", "/management/v1/users", "create a local account (users:write)")
	s.addOperation("GET", "/management/v1/users/{id}", "one account with assignments (users:read)")
	s.addOperation("POST", "/management/v1/users/{id}/roles", "assign a role on a scope (roles:assign)")
	s.addOperation("DELETE", "/management/v1/users/{id}/roles/{assignmentId}", "revoke an assignment (roles:assign)")
	s.addOperation("GET", "/management/v1/audit-events", "audit ledger page, limited to scopes with audit:read")
}

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
	Code             string   `json:"code"`
	Message          string   `json:"message"`
	RequestID        string   `json:"requestId,omitempty"` // operation request identifier (spec 33)
	TraceID          string   `json:"traceId,omitempty"`   // this HTTP call, also in X-Trace-ID
	JobID            string   `json:"jobId,omitempty"`
	Retryable        bool     `json:"retryable"`
	RetryAfter       int      `json:"retryAfterSeconds,omitempty"`
	SuggestedActions []string `json:"suggestedActions,omitempty"`
}

func fail(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, apiError{Code: code, Message: msg, TraceID: c.GetString(TraceIDHdr)})
}

// writeError sends an error body stamped with the call's trace id.
func writeError(c *gin.Context, status int, e apiError) {
	e.TraceID = c.GetString(TraceIDHdr)
	c.JSON(status, e)
}

// notFound is the single answer for "does not exist" and "you may not know
// whether it exists" (T-014). Nothing in it depends on the lookup result.
func notFound(c *gin.Context) { fail(c, http.StatusNotFound, "NOT_FOUND", "no such resource") }

func failErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, iam.ErrValidation):
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", strings.TrimPrefix(err.Error(), "iam: validation failed: "))
	case errors.Is(err, iam.ErrConflict):
		fail(c, http.StatusConflict, "CONFLICT", strings.TrimPrefix(err.Error(), "iam: conflict: "))
	case errors.Is(err, iam.ErrForbidden):
		fail(c, http.StatusForbidden, "FORBIDDEN", "this account is not allowed to perform the operation")
	case errors.Is(err, iam.ErrNotFound):
		notFound(c)
	case errors.Is(err, iam.ErrSetupDone):
		fail(c, http.StatusConflict, "SETUP_DONE", "setup has already been completed")
	case errors.Is(err, iam.ErrSetupCode):
		fail(c, http.StatusForbidden, "SETUP_CODE_INVALID", "setup code is invalid or expired")
	case errors.Is(err, iam.ErrThrottled):
		fail(c, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", "too many attempts; try again later")
	default:
		fail(c, http.StatusInternalServerError, "INTERNAL", "internal error")
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
				fail(c, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in to continue")
				return
			}
			c.Set(principalKey, p)
			c.Set(sessionKey, sess)
			c.Next()
			return
		}
		if auth := c.GetHeader("Authorization"); tokens != nil && strings.HasPrefix(auth, "Bearer ") {
			p, err := tokens.Authenticate(c.Request.Context(), strings.TrimPrefix(auth, "Bearer "))
			if err != nil {
				fail(c, http.StatusUnauthorized, "UNAUTHENTICATED", "token rejected")
				return
			}
			p.Kind = "token"
			c.Set(principalKey, p)
			c.Next()
			return
		}
		fail(c, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in to continue")
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
			fail(c, http.StatusForbidden, "CSRF_TOKEN_MISMATCH", "missing or wrong "+CSRFHeader+" header")
			return
		}
		if o := c.GetHeader("Origin"); o != "" && !sameHost(o, c.Request.Host) {
			fail(c, http.StatusForbidden, "ORIGIN_MISMATCH", "cross-site request refused")
			return
		} else if r := c.GetHeader("Referer"); o == "" && r != "" && !sameHost(r, c.Request.Host) {
			fail(c, http.StatusForbidden, "ORIGIN_MISMATCH", "cross-site request refused")
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
			fail(c, http.StatusForbidden, "FORBIDDEN", "this account is not allowed to perform the operation")
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
			fail(c, http.StatusInternalServerError, "INTERNAL", "internal error")
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
			fail(c, http.StatusForbidden, "FORBIDDEN", "this account is not allowed to perform the operation")
			return
		}
		c.Set("hazelminx.scope", scope)
		c.Next()
	}
}

// ---- handlers --------------------------------------------------------------

func (s *Server) setupStatus(c *gin.Context) {
	need, err := s.iam.NeedsSetup(c.Request.Context())
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"needsSetup": need})
}

type setupRequest struct {
	SetupCode string `json:"setupCode"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

func (s *Server) setupOwner(c *gin.Context) {
	var in setupRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with setupCode, username and password")
		return
	}
	u, err := s.iam.CompleteSetup(c.Request.Context(), in.SetupCode, in.Username, in.Password, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": u})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) login(c *gin.Context) {
	var in loginRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with username and password")
		return
	}
	sess, u, retry, err := s.iam.Login(c.Request.Context(), in.Username, in.Password, source(c))
	if errors.Is(err, iam.ErrThrottled) {
		secs := int(retry.Seconds()) + 1
		c.Header("Retry-After", strconv.Itoa(secs))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, apiError{Code: "TOO_MANY_ATTEMPTS", Message: "too many attempts; try again later",
			TraceID: c.GetString(TraceIDHdr), Retryable: true, RetryAfter: secs})
		return
	}
	if errors.Is(err, iam.ErrInvalidCredentials) {
		fail(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "username or password is incorrect")
		return
	}
	if err != nil {
		failErr(c, err)
		return
	}
	setSessionCookies(c, sess)
	c.JSON(http.StatusOK, gin.H{"user": u, "csrfToken": sess.CSRFToken, "expiresAt": sess.ExpiresAt.UTC().Format(time.RFC3339)})
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
	out := gin.H{"kind": p.Kind, "user": p.User, "assignments": p.Assignments}
	if v, ok := c.Get(sessionKey); ok {
		out["session"] = v.(*iam.Session)
		out["csrfToken"] = v.(*iam.Session).CSRFToken
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) listRoles(c *gin.Context) {
	var out []gin.H
	for _, r := range iam.Roles {
		g := iam.Grants(r)
		if r == iam.RoleOwner {
			out = append(out, gin.H{"role": r, "actions": "*"})
			continue
		}
		out = append(out, gin.H{"role": r, "actions": g})
	}
	c.JSON(http.StatusOK, gin.H{"roles": out})
}

func (s *Server) listUsers(c *gin.Context) {
	users, err := s.iam.ListUsers(c.Request.Context(), Principal(c))
	if err != nil {
		if errors.Is(err, iam.ErrForbidden) {
			s.iam.RecordDenied(c.Request.Context(), Principal(c), iam.ActionUsersRead, iam.Global, source(c))
		}
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

func (s *Server) createUser(c *gin.Context) {
	var in iam.CreateUserInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with username, displayName and password")
		return
	}
	u, err := s.iam.CreateUser(c.Request.Context(), Principal(c), in, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": u})
}

func (s *Server) getUser(c *gin.Context) {
	u, err := s.iam.GetUser(c.Request.Context(), Principal(c), c.Param("id"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}

type assignRequest struct {
	Role  iam.Role  `json:"role"`
	Scope iam.Scope `json:"scope"`
}

func (s *Server) assignRole(c *gin.Context) {
	var in assignRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with role and scope{kind,id}")
		return
	}
	if in.Scope.Kind == "" {
		in.Scope = iam.Global
	}
	as, err := s.iam.AssignRole(c.Request.Context(), Principal(c), c.Param("id"), in.Role, in.Scope, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"assignment": as})
}

func (s *Server) revokeRole(c *gin.Context) {
	if err := s.iam.RevokeRole(c.Request.Context(), Principal(c), c.Param("assignmentId"), source(c)); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listAudit(c *gin.Context) {
	f := iam.AuditFilter{ActorID: c.Query("actorId"), JobID: c.Query("jobId"), ResourceID: c.Query("resourceId")}
	if k := c.Query("scopeKind"); k != "" {
		f.Scope = &iam.Scope{Kind: iam.ScopeKind(k), ID: c.Query("scopeId")}
	}
	f.AfterSeq, _ = strconv.ParseInt(c.Query("afterSeq"), 10, 64)
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	if f.Limit <= 0 || f.Limit > iam.MaxAuditPage {
		f.Limit = iam.MaxAuditPage
	}
	evs, err := s.iam.ListAudit(c.Request.Context(), Principal(c), f)
	if err != nil {
		if errors.Is(err, iam.ErrForbidden) {
			s.iam.RecordDenied(c.Request.Context(), Principal(c), iam.ActionAuditRead, iam.Global, source(c))
		}
		failErr(c, err)
		return
	}
	var next int64
	if n := len(evs); n > 0 {
		next = evs[n-1].Seq
	}
	c.JSON(http.StatusOK, gin.H{"events": evs, "nextAfterSeq": next, "limit": f.Limit})
}

// ---- cookies ---------------------------------------------------------------

func secureRequest(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

func setSessionCookies(c *gin.Context, sess *iam.Session) {
	secure := secureRequest(c)
	maxAge := int(time.Until(sess.ExpiresAt).Seconds())
	http.SetCookie(c.Writer, &http.Cookie{Name: SessionCookie, Value: sess.Token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
	http.SetCookie(c.Writer, &http.Cookie{Name: CSRFCookie, Value: sess.CSRFToken, Path: "/", HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func clearSessionCookies(c *gin.Context) {
	for _, n := range []string{SessionCookie, CSRFCookie} {
		http.SetCookie(c.Writer, &http.Cookie{Name: n, Value: "", Path: "/", HttpOnly: n == SessionCookie, MaxAge: -1})
	}
}

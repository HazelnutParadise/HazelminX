package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/gin-gonic/gin"
)

// Second factors, reauthentication, the user's own sessions and the
// passkey domain-impact check (ticket 014). Errors use the current apiError
// format; ticket 010's shared error and pagination middleware replaces it
// when it lands.

func (s *Server) mountSecondFactors(p *gin.RouterGroup) {
	p.POST("/auth/reauthenticate", s.reauthenticate)
	p.POST("/auth/reauthenticate/passkey-options", s.reauthPasskeyOptions)
	p.GET("/auth/passkeys/domain-impact", s.passkeyDomainImpact)

	p.GET("/me/second-factors", s.listFactors)
	p.POST("/me/second-factors/totp", s.beginTOTP)
	p.POST("/me/second-factors/totp/confirm", s.confirmTOTP)
	p.DELETE("/me/second-factors/totp", s.removeTOTP)
	p.POST("/me/second-factors/passkeys/options", s.passkeyRegistrationOptions)
	p.POST("/me/second-factors/passkeys", s.registerPasskey)
	p.DELETE("/me/second-factors/passkeys/:id", s.removePasskey)
	p.POST("/me/recovery-codes", s.regenerateRecoveryCodes)

	p.GET("/me/sessions", s.listSessions)
	p.DELETE("/me/sessions/:id", s.revokeSession)
	p.POST("/me/sessions/revoke-others", s.revokeOtherSessions)
	p.GET("/me/session-settings", s.getSessionSettings)
	p.PUT("/me/session-settings", s.putSessionSettings)

	for _, op := range [][3]string{
		{"POST", "/management/v1/auth/login/second-factor", "finish a login with totp, recovery-code or passkey; sets the session cookie"},
		{"POST", "/management/v1/auth/login/passkey-options", "WebAuthn assertion options for the second login step"},
		{"POST", "/management/v1/auth/reauthenticate", "prove password and second factor again; counts as recent for 10 minutes"},
		{"POST", "/management/v1/auth/reauthenticate/passkey-options", "WebAuthn assertion options for reauthentication"},
		{"GET", "/management/v1/auth/passkeys/domain-impact", "passkeys and users affected if the management domain becomes ?domain= (platform:write)"},
		{"GET", "/management/v1/me/second-factors", "own authenticator app, passkeys and remaining recovery codes"},
		{"POST", "/management/v1/me/second-factors/totp", "start authenticator-app enrolment; returns the seed once (recent reauthentication)"},
		{"POST", "/management/v1/me/second-factors/totp/confirm", "confirm the authenticator app with a first code; may return recovery codes once"},
		{"DELETE", "/management/v1/me/second-factors/totp", "remove the authenticator app (recent reauthentication)"},
		{"POST", "/management/v1/me/second-factors/passkeys/options", "WebAuthn creation options for a new passkey (recent reauthentication)"},
		{"POST", "/management/v1/me/second-factors/passkeys", "register a passkey; may return recovery codes once"},
		{"DELETE", "/management/v1/me/second-factors/passkeys/{id}", "remove a passkey (recent reauthentication)"},
		{"POST", "/management/v1/me/recovery-codes", "replace the recovery codes; returns the new set once (recent reauthentication)"},
		{"GET", "/management/v1/me/sessions", "own signed-in sessions"},
		{"DELETE", "/management/v1/me/sessions/{id}", "sign out one of your sessions now"},
		{"POST", "/management/v1/me/sessions/revoke-others", "sign out every other session now"},
		{"GET", "/management/v1/me/session-settings", "own idle timeout and its bounds"},
		{"PUT", "/management/v1/me/session-settings", "set own idle timeout (0 = platform default)"},
	} {
		s.addOperation(op[0], op[1], op[2])
	}
}

// enrollmentAllowed are the routes a session that still has to add a
// second factor may use (IAM-002). Everything else, reads included, is
// refused: reads here cover terminals, downloads and event streams.
var enrollmentAllowed = map[string]bool{
	"GET /management/v1/auth/session":                         true,
	"POST /management/v1/auth/logout":                         true,
	"POST /management/v1/auth/reauthenticate":                 true,
	"POST /management/v1/auth/reauthenticate/passkey-options": true,
	"GET /management/v1/me/second-factors":                    true,
	"POST /management/v1/me/second-factors/totp":              true,
	"POST /management/v1/me/second-factors/totp/confirm":      true,
	"DELETE /management/v1/me/second-factors/totp":            true,
	"POST /management/v1/me/second-factors/passkeys/options":  true,
	"POST /management/v1/me/second-factors/passkeys":          true,
	"GET /management/v1/me/sessions":                          true,
	"DELETE /management/v1/me/sessions/:id":                   true,
	"POST /management/v1/me/sessions/revoke-others":           true,
	"GET /management/v1/me/session-settings":                  true,
}

// enrollmentGate holds sessions of accounts that must use a second factor
// but have none to the enrolment routes. Token principals are not affected
// (ticket 015 requires an enrolled, reauthenticated session to issue one).
func (s *Server) enrollmentGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := Principal(c)
		if p != nil && p.EnrollmentRequired && !enrollmentAllowed[c.Request.Method+" "+c.FullPath()] {
			failErr(c, iam.ErrEnrollmentRequired)
			return
		}
		c.Next()
	}
}

// relyingParty is the WebAuthn RP of this request: the configured public
// origin, or the request's own scheme and host.
func (s *Server) relyingParty(c *gin.Context) iam.RelyingParty {
	origin := s.origin
	if origin == "" {
		scheme := "http"
		if secureRequest(c) {
			scheme = "https"
		}
		origin = scheme + "://" + c.Request.Host
	}
	u, err := url.Parse(origin)
	if err != nil {
		return iam.RelyingParty{}
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	return iam.RelyingParty{ID: strings.ToLower(host), Origin: origin}
}

// proofRequest is the second-factor part of a login or reauthentication.
type proofRequest struct {
	Method     string          `json:"method"`
	Code       string          `json:"code"`
	Credential json.RawMessage `json:"credential"`
}

func (r proofRequest) proof() iam.Proof {
	return iam.Proof{Method: r.Method, Code: r.Code, Credential: r.Credential}
}

type secondFactorRequest struct {
	ChallengeToken string `json:"challengeToken"`
	proofRequest
}

func (s *Server) loginSecondFactor(c *gin.Context) {
	var in secondFactorRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with challengeToken, method and code or credential")
		return
	}
	res, err := s.iam.CompleteSecondFactor(c.Request.Context(), in.ChallengeToken, in.proof(), s.relyingParty(c), source(c))
	if errors.Is(err, iam.ErrThrottled) {
		throttled(c, res.RetryAfter)
		return
	}
	if err != nil {
		failErr(c, err)
		return
	}
	s.signedIn(c, http.StatusOK, res)
}

func (s *Server) loginPasskeyOptions(c *gin.Context) {
	var in struct {
		ChallengeToken string `json:"challengeToken"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with challengeToken")
		return
	}
	opts, err := s.iam.BeginPasskeyLogin(c.Request.Context(), in.ChallengeToken, s.relyingParty(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", opts)
}

func (s *Server) reauthenticate(c *gin.Context) {
	var in struct {
		Password string `json:"password"`
		proofRequest
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with password and, when enrolled, method and code or credential")
		return
	}
	if err := s.iam.Reauthenticate(c.Request.Context(), Principal(c), in.Password, in.proof(), s.relyingParty(c), source(c)); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) reauthPasskeyOptions(c *gin.Context) {
	opts, err := s.iam.BeginPasskeyReauth(c.Request.Context(), Principal(c), s.relyingParty(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", opts)
}

func (s *Server) passkeyDomainImpact(c *gin.Context) {
	out, err := s.iam.PasskeyDomainImpact(c.Request.Context(), Principal(c), c.Query("domain"), source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) listFactors(c *gin.Context) {
	out, err := s.iam.Factors(c.Request.Context(), Principal(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) beginTOTP(c *gin.Context) {
	out, err := s.iam.BeginTOTP(c.Request.Context(), Principal(c), source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, out)
}

func (s *Server) confirmTOTP(c *gin.Context) {
	var in struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with code")
		return
	}
	out, err := s.iam.ConfirmTOTP(c.Request.Context(), Principal(c), in.Code, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

func (s *Server) removeTOTP(c *gin.Context) {
	if err := s.iam.RemoveTOTP(c.Request.Context(), Principal(c), source(c)); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) passkeyRegistrationOptions(c *gin.Context) {
	opts, err := s.iam.BeginPasskeyRegistration(c.Request.Context(), Principal(c), s.relyingParty(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", opts)
}

func (s *Server) registerPasskey(c *gin.Context) {
	var in struct {
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.Credential) == 0 {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with name and credential")
		return
	}
	out, err := s.iam.FinishPasskeyRegistration(c.Request.Context(), Principal(c), s.relyingParty(c), in.Name, in.Credential, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, out)
}

func (s *Server) removePasskey(c *gin.Context) {
	if err := s.iam.RemovePasskey(c.Request.Context(), Principal(c), c.Param("id"), source(c)); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) regenerateRecoveryCodes(c *gin.Context) {
	codes, err := s.iam.RegenerateRecoveryCodes(c.Request.Context(), Principal(c), source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"recoveryCodes": codes})
}

func (s *Server) listSessions(c *gin.Context) {
	out, err := s.iam.ListSessions(c.Request.Context(), Principal(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": out})
}

func (s *Server) revokeSession(c *gin.Context) {
	p := Principal(c)
	id := c.Param("id")
	if err := s.iam.RevokeSession(c.Request.Context(), p, id, source(c)); err != nil {
		failErr(c, err)
		return
	}
	if id == p.SessionID {
		clearSessionCookies(c)
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) revokeOtherSessions(c *gin.Context) {
	n, err := s.iam.RevokeOtherSessions(c.Request.Context(), Principal(c), source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": n})
}

func (s *Server) getSessionSettings(c *gin.Context) {
	out, err := s.iam.SessionSettings(c.Request.Context(), Principal(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) putSessionSettings(c *gin.Context) {
	var in struct {
		IdleTimeoutSeconds *int `json:"idleTimeoutSeconds"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.IdleTimeoutSeconds == nil {
		fail(c, http.StatusBadRequest, "VALIDATION_FAILED", "body must be JSON with idleTimeoutSeconds (0 = platform default)")
		return
	}
	out, err := s.iam.SetSessionIdle(c.Request.Context(), Principal(c), time.Duration(*in.IdleTimeoutSeconds)*time.Second, source(c))
	if err != nil {
		failErr(c, err)
		return
	}
	// The current session's expiry moved; refresh the cookie lifetime.
	if v, ok := c.Get(sessionKey); ok {
		sess := *v.(*iam.Session)
		sess.LastSeenAt = time.Now()
		sess.ExpiresAt = sess.LastSeenAt.Add(time.Duration(out.IdleTimeoutSeconds) * time.Second)
		if cookie, err := c.Cookie(SessionCookie); err == nil {
			sess.Token = cookie
			setSessionCookies(c, &sess)
		}
	}
	c.JSON(http.StatusOK, out)
}

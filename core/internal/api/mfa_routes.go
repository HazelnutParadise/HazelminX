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
// passkey domain-impact check (ticket 014), registered on the shared
// management contract (Server.handle, the error code table).

// Request and response bodies of these operations (documented in OpenAPI).
type secondFactorRequest struct {
	ChallengeToken string          `json:"challengeToken"`
	Method         string          `json:"method"`               // totp | recovery-code | passkey
	Code           string          `json:"code,omitempty"`       // totp or recovery code
	Credential     json.RawMessage `json:"credential,omitempty"` // passkey assertion from navigator.credentials.get()
}

type challengeRequest struct {
	ChallengeToken string `json:"challengeToken"`
}

type reauthRequest struct {
	Password   string          `json:"password"`
	Method     string          `json:"method,omitempty"` // required when the account has a second factor
	Code       string          `json:"code,omitempty"`
	Credential json.RawMessage `json:"credential,omitempty"`
}

type totpConfirmRequest struct {
	Code string `json:"code"`
}

type passkeyRegisterRequest struct {
	Name       string          `json:"name,omitempty"`
	Credential json.RawMessage `json:"credential"` // attestation from navigator.credentials.create()
}

type sessionSettingsRequest struct {
	IdleTimeoutSeconds *int `json:"idleTimeoutSeconds"` // 0 = platform default
}

type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

type sessionsResponse struct {
	Sessions []iam.SessionInfo `json:"sessions"`
}

type revokedResponse struct {
	Revoked int `json:"revoked"`
}

// webauthnOptions is the JSON handed to navigator.credentials.create() or
// .get() as is ({"publicKey": {...}}).
type webauthnOptions = json.RawMessage

// mountSecondFactorLogin registers the public second login step.
func (s *Server) mountSecondFactorLogin(pub *gin.RouterGroup, base string) {
	s.handle(pub, base, Operation{Method: "POST", Path: base + "/auth/login/second-factor", ID: "completeSecondFactor", Tag: "auth",
		Summary: "finish a login with totp, recovery-code or passkey; sets the session and CSRF cookies", Request: secondFactorRequest{},
		Responses: []Response{{Status: 200, Body: loginResponse{}, Headers: []string{"Set-Cookie"}}},
		Errors:    []string{CodeLoginChallengeInvalid, CodeSecondFactorInvalid, CodeTooManyAttempts}}, s.loginSecondFactor)
	s.handle(pub, base, Operation{Method: "POST", Path: base + "/auth/login/passkey-options", ID: "loginPasskeyOptions", Tag: "auth",
		Summary: "WebAuthn assertion options for the second login step", Request: challengeRequest{},
		Responses: []Response{{Status: 200, Body: webauthnOptions{}}},
		Errors:    []string{CodeLoginChallengeInvalid, CodeNotAvailable}}, s.loginPasskeyOptions)
}

// mountSecondFactors registers the signed-in operations of ticket 014.
func (s *Server) mountSecondFactors(p *gin.RouterGroup, base string) {
	op := func(o Operation) Operation {
		o.Auth = true
		if o.Tag == "" {
			o.Tag = "auth"
		}
		return o
	}
	recent := []string{CodeReauthRequired}
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/auth/reauthenticate", ID: "reauthenticate",
		Summary: "prove password and, when enrolled, a second factor again; counts as recent for 10 minutes", Request: reauthRequest{},
		Responses: []Response{{Status: 204, Description: "reauthenticated"}},
		Errors:    []string{CodeInvalidCredentials, CodeSecondFactorInvalid, CodeTooManyAttempts}}), s.reauthenticate)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/auth/reauthenticate/passkey-options", ID: "reauthPasskeyOptions",
		Summary:   "WebAuthn assertion options for reauthentication",
		Responses: []Response{{Status: 200, Body: webauthnOptions{}}}, Errors: []string{CodeNotAvailable}}), s.reauthPasskeyOptions)
	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/auth/passkeys/domain-impact", ID: "passkeyDomainImpact",
		Summary:    "passkeys and accounts affected if the management domain became ?domain=; changes nothing",
		Permission: "platform:write on the global scope",
		Params: []Param{{Name: "domain", In: "query", Required: true, Description: "host name such as panel.example.com",
			Schema: jsonSchema{"type": "string"}}},
		Responses: []Response{{Status: 200, Body: iam.DomainImpact{}}}}), s.passkeyDomainImpact)

	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/me/second-factors", ID: "listSecondFactors",
		Summary:   "own authenticator app, passkeys and remaining recovery codes",
		Responses: []Response{{Status: 200, Body: iam.FactorSummary{}}}}), s.listFactors)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/me/second-factors/totp", ID: "beginTOTP",
		Summary:   "start authenticator-app enrolment; returns the seed once (needs recent authentication)",
		Responses: []Response{{Status: 201, Body: iam.TOTPEnrollment{}}},
		Errors:    append([]string{CodeAlreadyExists, CodeNotAvailable}, recent...)}), s.beginTOTP)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/me/second-factors/totp/confirm", ID: "confirmTOTP",
		Summary: "confirm the authenticator app with a first code; returns recovery codes once when it is the first factor",
		Request: totpConfirmRequest{}, Responses: []Response{{Status: 200, Body: iam.EnrollmentResult{}}},
		Errors: []string{CodeAlreadyExists, CodeSecondFactorInvalid, CodeTooManyAttempts}}), s.confirmTOTP)
	s.handle(p, base, op(Operation{Method: "DELETE", Path: base + "/me/second-factors/totp", ID: "removeTOTP",
		Summary:   "remove the authenticator app, or cancel an unconfirmed enrolment (needs recent authentication)",
		Responses: []Response{{Status: 204, Description: "removed"}},
		Errors:    append([]string{CodeLastSecondFactor}, recent...)}), s.removeTOTP)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/me/second-factors/passkeys/options", ID: "passkeyRegistrationOptions",
		Summary:   "WebAuthn creation options for a new passkey (needs recent authentication)",
		Responses: []Response{{Status: 200, Body: webauthnOptions{}}},
		Errors:    append([]string{CodeNotAvailable}, recent...)}), s.passkeyRegistrationOptions)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/me/second-factors/passkeys", ID: "registerPasskey",
		Summary: "register a passkey; returns recovery codes once when it is the first factor",
		Request: passkeyRegisterRequest{}, Responses: []Response{{Status: 201, Body: iam.EnrollmentResult{}}},
		Errors: append([]string{CodeAlreadyExists, CodeSecondFactorInvalid, CodeNotAvailable}, recent...)}), s.registerPasskey)
	s.handle(p, base, op(Operation{Method: "DELETE", Path: base + "/me/second-factors/passkeys/{id}", ID: "removePasskey",
		Summary:   "remove one of your passkeys (needs recent authentication)",
		Responses: []Response{{Status: 204, Description: "removed"}},
		Errors:    append([]string{CodeLastSecondFactor}, recent...)}), s.removePasskey)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/me/recovery-codes", ID: "regenerateRecoveryCodes",
		Summary:   "replace your recovery codes; the old set stops working; the new set is shown once (needs recent authentication)",
		Responses: []Response{{Status: 201, Body: recoveryCodesResponse{}}},
		Errors:    append([]string{CodeAlreadyExists}, recent...)}), s.regenerateRecoveryCodes)

	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/me/sessions", ID: "listOwnSessions", Tag: "sessions",
		Summary: "your signed-in sessions", Responses: []Response{{Status: 200, Body: sessionsResponse{}}}}), s.listSessions)
	s.handle(p, base, op(Operation{Method: "DELETE", Path: base + "/me/sessions/{id}", ID: "revokeOwnSession", Tag: "sessions",
		Summary: "sign out one of your sessions now", Responses: []Response{{Status: 204, Description: "revoked"}}}), s.revokeSession)
	s.handle(p, base, op(Operation{Method: "POST", Path: base + "/me/sessions/revoke-others", ID: "revokeOtherSessions", Tag: "sessions",
		Summary: "sign out every other session now", Responses: []Response{{Status: 200, Body: revokedResponse{}}}}), s.revokeOtherSessions)
	s.handle(p, base, op(Operation{Method: "GET", Path: base + "/me/session-settings", ID: "getSessionSettings", Tag: "sessions",
		Summary: "your idle timeout and its bounds", Responses: []Response{{Status: 200, Body: iam.SessionSettings{}}}}), s.getSessionSettings)
	s.handle(p, base, op(Operation{Method: "PUT", Path: base + "/me/session-settings", ID: "putSessionSettings", Tag: "sessions",
		Summary: "set your idle timeout (0 = platform default); applies to the current session at once",
		Request: sessionSettingsRequest{}, Responses: []Response{{Status: 200, Body: iam.SessionSettings{}}}}), s.putSessionSettings)
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

func (s *Server) loginSecondFactor(c *gin.Context) {
	var in secondFactorRequest
	if !bindJSON(c, &in, "body must be JSON with challengeToken, method and code or credential") {
		return
	}
	proof := iam.Proof{Method: in.Method, Code: in.Code, Credential: in.Credential}
	res, err := s.iam.CompleteSecondFactor(c.Request.Context(), in.ChallengeToken, proof, s.relyingParty(c), source(c))
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
	var in challengeRequest
	if !bindJSON(c, &in, "body must be JSON with challengeToken") {
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
	var in reauthRequest
	if !bindJSON(c, &in, "body must be JSON with password and, when enrolled, method and code or credential") {
		return
	}
	proof := iam.Proof{Method: in.Method, Code: in.Code, Credential: in.Credential}
	if err := s.iam.Reauthenticate(c.Request.Context(), Principal(c), in.Password, proof, s.relyingParty(c), source(c)); err != nil {
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
	var in totpConfirmRequest
	if !bindJSON(c, &in, "body must be JSON with code") {
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
	var in passkeyRegisterRequest
	if !bindJSON(c, &in, "body must be JSON with name and credential") {
		return
	}
	if len(in.Credential) == 0 {
		e := newError(CodeValidationFailed, "credential is required")
		e.Fields = []FieldError{{Field: "credential", Message: "required"}}
		respond(c, e)
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
	c.JSON(http.StatusCreated, recoveryCodesResponse{RecoveryCodes: codes})
}

func (s *Server) listSessions(c *gin.Context) {
	out, err := s.iam.ListSessions(c.Request.Context(), Principal(c))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, sessionsResponse{Sessions: out})
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
	c.JSON(http.StatusOK, revokedResponse{Revoked: n})
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
	var in sessionSettingsRequest
	if !bindJSON(c, &in, "body must be JSON with idleTimeoutSeconds (0 = platform default)") {
		return
	}
	if in.IdleTimeoutSeconds == nil {
		e := newError(CodeValidationFailed, "idleTimeoutSeconds is required (0 = platform default)")
		e.Fields = []FieldError{{Field: "idleTimeoutSeconds", Message: "required"}}
		respond(c, e)
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

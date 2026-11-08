package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// Acceptance flows of ticket 014 against the real store, secret store,
// service and router.

// passwordStep runs only the first login step and returns its body.
func (c *client) passwordStep(username, password string) resp {
	c.hs.t.Helper()
	r := c.do(http.MethodPost, "/management/v1/auth/login", map[string]string{"username": username, "password": password}, nil)
	if r.Code != http.StatusOK || r.Body["step"] != "second-factor" {
		c.hs.t.Fatalf("password step for %s: %+v", username, r)
	}
	for _, ck := range c.cookies {
		if ck.Name == SessionCookie {
			c.hs.t.Fatal("no session cookie may be set before the second factor")
		}
	}
	return r
}

func (c *client) secondStep(challenge, method, code string) resp {
	r := c.do(http.MethodPost, "/management/v1/auth/login/second-factor", map[string]string{"challengeToken": challenge, "method": method, "code": code}, nil)
	if r.Code == http.StatusOK {
		c.csrf, _ = r.Body["csrfToken"].(string)
	}
	return r
}

func methodsOf(r resp) []string {
	var out []string
	for _, m := range r.Body["methods"].([]any) {
		out = append(out, m.(string))
	}
	return out
}

// IAM-002 / IAM-011: TOTP tolerates one step of clock drift but nothing
// older, a used code never works twice, a lost device is rescued by a
// single-use recovery code, and challenges are single use and bounded.
func TestSecondFactorLoginReplayExpiryAndRecoveryCodes(t *testing.T) {
	hs := newHarness(t)
	hs.bootstrap()
	dev := hs.totp["owner"]
	c := hs.client("10.0.0.1")

	r := c.passwordStep("owner", ownerPW)
	if m := strings.Join(methodsOf(r), ","); m != "totp,recovery-code" {
		t.Fatalf("methods = %q, want totp,recovery-code (no passkey enrolled)", m)
	}
	ch := r.Body["challengeToken"].(string)
	if r := c.secondStep(ch, "totp", "000000"); r.Code != http.StatusUnauthorized || r.Body["code"] != "SECOND_FACTOR_INVALID" {
		t.Fatalf("wrong code: %+v", r)
	}
	// Replay: the code of the last accepted step.
	if r := c.secondStep(ch, "totp", iam.TOTPCode(dev.seed, dev.lastStep)); r.Code != http.StatusUnauthorized {
		t.Fatalf("replayed code must be refused: %+v", r)
	}
	// Expired: two steps in the past is outside the allowed drift.
	hs.clock = hs.clock.Add(5 * iam.TOTPPeriod)
	if r := c.secondStep(ch, "totp", iam.TOTPCode(dev.seed, iam.TOTPStep(hs.clock)-2)); r.Code != http.StatusUnauthorized {
		t.Fatalf("expired code must be refused: %+v", r)
	}
	// One step of drift either way is fine.
	behind := iam.TOTPStep(hs.clock) - 1
	if r := c.secondStep(ch, "totp", iam.TOTPCode(dev.seed, behind)); r.Code != http.StatusOK || r.Body["step"] != "signed-in" {
		t.Fatalf("code one step behind must be accepted: %+v", r)
	}
	dev.lastStep = behind
	if r := c.secondStep(ch, "totp", hs.nextCode(dev)); r.Code != http.StatusUnauthorized || r.Body["code"] != "LOGIN_CHALLENGE_INVALID" {
		t.Fatalf("a used challenge must not sign in again: %+v", r)
	}

	// Lost device: a recovery code (any case, with or without the dash) signs in once.
	codes := hs.recovery["owner"]
	lost := hs.client("198.51.100.20")
	r = lost.passwordStep("owner", ownerPW)
	if r := lost.secondStep(r.Body["challengeToken"].(string), "recovery-code", strings.ToLower(strings.ReplaceAll(codes[0], "-", ""))); r.Code != http.StatusOK {
		t.Fatalf("recovery code login: %+v", r)
	}
	f := lost.do(http.MethodGet, "/management/v1/me/second-factors", nil, nil)
	if f.Body["recoveryCodesRemaining"] != float64(iam.RecoveryCodeCount-1) {
		t.Fatalf("one code must be spent: %+v", f.Body)
	}
	again := hs.client("198.51.100.21")
	r = again.passwordStep("owner", ownerPW)
	if r := again.secondStep(r.Body["challengeToken"].(string), "recovery-code", codes[0]); r.Code != http.StatusUnauthorized || r.Body["code"] != "SECOND_FACTOR_INVALID" {
		t.Fatalf("a used recovery code must not work again: %+v", r)
	}

	// A challenge dies after MaxChallengeAttempts wrong answers, even if the
	// next answer would be right. (Let the earlier failures age out of the
	// limiter window first so the limiter does not answer before the
	// challenge does.)
	hs.clock = hs.clock.Add(iam.DefaultLimiterPolicy.AccountWindow + time.Second)
	brute := hs.client("198.51.100.22")
	r = brute.passwordStep("owner", ownerPW)
	ch = r.Body["challengeToken"].(string)
	for i := 0; i < iam.MaxChallengeAttempts; i++ {
		brute.secondStep(ch, "totp", "123456")
	}
	if r := brute.secondStep(ch, "recovery-code", codes[1]); r.Code != http.StatusUnauthorized || r.Body["code"] != "LOGIN_CHALLENGE_INVALID" {
		t.Fatalf("challenge must be spent after %d failures: %+v", iam.MaxChallengeAttempts, r)
	}

	// The ledger records methods and outcomes, never a code.
	audit := c.do(http.MethodGet, "/management/v1/audit-events", nil, nil)
	for _, secret := range []string{codes[0], codes[1], strings.ReplaceAll(codes[0], "-", ""), ch} {
		if strings.Contains(strings.ToUpper(audit.Raw), strings.ToUpper(secret)) {
			t.Fatalf("audit ledger leaks %q", secret)
		}
	}
	if !strings.Contains(audit.Raw, `"login.second-factor"`) || !strings.Contains(audit.Raw, `"secondFactor":"recovery-code"`) {
		t.Fatalf("second-factor outcomes must be audited: %s", audit.Raw)
	}
}

// Second-factor guesses are limited per account even from the owner's
// trusted address: behind a proxy everyone shares it.
func TestSecondFactorThrottleIgnoresTrustedSource(t *testing.T) {
	hs := newHarness(t)
	hs.bootstrap() // 10.0.0.1 is now a trusted source for owner
	var last resp
	for i := 0; i < 4; i++ {
		c := hs.client("10.0.0.1")
		ch := c.passwordStep("owner", ownerPW).Body["challengeToken"].(string)
		for j := 0; j < 2; j++ {
			if last = c.secondStep(ch, "totp", "999999"); last.Code == http.StatusTooManyRequests {
				break
			}
		}
		if last.Code == http.StatusTooManyRequests {
			break
		}
	}
	if last.Code != http.StatusTooManyRequests || last.Hdr.Get("Retry-After") == "" {
		t.Fatalf("repeated wrong codes from a trusted source must throttle: %+v", last)
	}
	// Bounded: after the maximum cool-down the owner gets in.
	hs.clock = hs.clock.Add(iam.DefaultLimiterPolicy.AccountMaxDelay + time.Second)
	if r := hs.client("10.0.0.1").login("owner", ownerPW); r.Code != http.StatusOK || r.Body["step"] != "signed-in" {
		t.Fatalf("cool-down must end: %+v", r)
	}
}

// IAM-002 安全預設: owner, platform admin and host operator accounts can only
// enrol until they have a second factor, and cannot drop their last one.
func TestEnrollmentGateHoldsPrivilegedAccounts(t *testing.T) {
	hs := newHarness(t)
	code, _, _ := hs.svc.SetupCode(context.Background())
	c := hs.client("10.0.0.1")
	r := c.do(http.MethodPost, "/management/v1/setup", map[string]string{"setupCode": code, "username": "owner", "password": ownerPW}, nil)
	if r.Code != http.StatusCreated {
		t.Fatalf("setup: %+v", r)
	}
	c.csrf = r.Body["csrfToken"].(string)
	if r := c.do(http.MethodGet, "/management/v1/auth/session", nil, nil); r.Code != http.StatusOK || r.Body["secondFactorEnrollmentRequired"] != true {
		t.Fatalf("whoami must say enrolment is required: %+v", r)
	}
	for _, call := range [][2]string{{"POST", "/management/v1/users"}, {"GET", "/management/v1/users"}, {"GET", "/management/v1/audit-events"}, {"GET", "/management/v1/roles"}} {
		var body any
		if call[0] == "POST" {
			body = map[string]string{"username": "eve", "password": "eve-password-123"}
		}
		if r := c.do(call[0], call[1], body, nil); r.Code != http.StatusForbidden || r.Body["code"] != "SECOND_FACTOR_ENROLLMENT_REQUIRED" {
			t.Fatalf("%s %s before enrolment: %+v", call[0], call[1], r)
		}
	}
	if r := c.do(http.MethodGet, "/management/v1/me/sessions", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("own sessions stay reachable: %+v", r)
	}
	c.enrollTOTP("owner")
	if r := c.do(http.MethodGet, "/management/v1/users", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("enrolled owner: %+v", r)
	}
	if r := c.do(http.MethodDelete, "/management/v1/me/second-factors/totp", nil, nil); r.Code != http.StatusConflict || r.Body["code"] != "LAST_SECOND_FACTOR" {
		t.Fatalf("removing the last factor of an owner must be refused: %+v", r)
	}

	for _, role := range []iam.Role{iam.RolePlatformAdmin, iam.RoleHostOperator} {
		name := strings.ReplaceAll(string(role), "_", "")
		u := hs.createUser(c, name, role, iam.Global, "10.0.1.1")
		if r := u.do(http.MethodGet, "/management/v1/users", nil, nil); r.Code != http.StatusForbidden || r.Body["code"] != "SECOND_FACTOR_ENROLLMENT_REQUIRED" {
			t.Fatalf("%s before enrolment: %+v", role, r)
		}
	}
	auditor := hs.createUser(c, "auditor", iam.RoleAuditor, iam.Global, "10.0.1.2")
	if r := auditor.do(http.MethodGet, "/management/v1/users", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("auditors are not held to enrolment: %+v", r)
	}
	if r := auditor.do(http.MethodDelete, "/management/v1/me/second-factors/totp", nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("removing a factor one does not have: %+v", r)
	}
}

// IAM-011 新增或移除憑證要再次驗證.
func TestReauthenticationGuardsCredentialChanges(t *testing.T) {
	hs := newHarness(t)
	owner := hs.bootstrap()
	dev := hs.totp["owner"]
	old := hs.recovery["owner"]
	hs.clock = hs.clock.Add(11 * time.Minute)
	if r := owner.do(http.MethodPost, "/management/v1/me/recovery-codes", nil, nil); r.Code != http.StatusForbidden || r.Body["code"] != "REAUTHENTICATION_REQUIRED" {
		t.Fatalf("stale session must reauthenticate: %+v", r)
	}
	if r := owner.do(http.MethodPost, "/management/v1/me/second-factors/totp", nil, nil); r.Code != http.StatusForbidden || r.Body["code"] != "REAUTHENTICATION_REQUIRED" {
		t.Fatalf("stale session must reauthenticate before adding TOTP: %+v", r)
	}
	if r := owner.do(http.MethodPost, "/management/v1/auth/reauthenticate", map[string]string{"password": "wrong-password-xx"}, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %+v", r)
	}
	if r := owner.do(http.MethodPost, "/management/v1/auth/reauthenticate", map[string]string{"password": ownerPW}, nil); r.Code != http.StatusUnauthorized || r.Body["code"] != "SECOND_FACTOR_INVALID" {
		t.Fatalf("an enrolled account must also prove its second factor: %+v", r)
	}
	if r := owner.do(http.MethodPost, "/management/v1/auth/reauthenticate", map[string]string{"password": ownerPW, "method": "totp", "code": hs.nextCode(dev)}, nil); r.Code != http.StatusNoContent {
		t.Fatalf("reauthenticate: %+v", r)
	}
	r := owner.do(http.MethodPost, "/management/v1/me/recovery-codes", nil, nil)
	if r.Code != http.StatusCreated || len(r.Body["recoveryCodes"].([]any)) != iam.RecoveryCodeCount {
		t.Fatalf("regenerate: %+v", r)
	}
	if r.Hdr.Get("Cache-Control") != "no-store" {
		t.Fatal("one-time codes must not be cached")
	}
	c := hs.client("198.51.100.30")
	ch := c.passwordStep("owner", ownerPW).Body["challengeToken"].(string)
	if r := c.secondStep(ch, "recovery-code", old[2]); r.Code != http.StatusUnauthorized {
		t.Fatalf("codes from the replaced set must stop working: %+v", r)
	}
}

// IAM-003: a revoked browser cannot keep writing; nobody can revoke someone
// else's session through their own list.
func TestSessionListAndImmediateRevocation(t *testing.T) {
	hs := newHarness(t)
	a := hs.bootstrap()
	b := hs.client("10.0.0.5")
	if r := b.login("owner", ownerPW); r.Code != http.StatusOK {
		t.Fatalf("second browser: %+v", r)
	}
	r := a.do(http.MethodGet, "/management/v1/me/sessions", nil, nil)
	list := r.Body["sessions"].([]any)
	var bID string
	current := 0
	for _, it := range list {
		m := it.(map[string]any)
		if m["current"] == true {
			current++
		}
		if m["sourceIp"] == "10.0.0.5" {
			bID = m["id"].(string)
		}
		if strings.Contains(strings.ToLower(it.(map[string]any)["id"].(string)), "csrf") {
			t.Fatal("list must not expose tokens")
		}
	}
	if r.Code != http.StatusOK || current != 1 || bID == "" || len(list) < 2 {
		t.Fatalf("session list: %+v", r)
	}
	if strings.Contains(r.Raw, b.csrf) {
		t.Fatal("session list must not carry CSRF tokens")
	}
	if r := a.do(http.MethodDelete, "/management/v1/me/sessions/"+bID, nil, nil); r.Code != http.StatusNoContent {
		t.Fatalf("revoke: %+v", r)
	}
	if r := b.do(http.MethodPost, "/management/v1/users", map[string]string{"username": "late", "password": "late-password-123"}, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("revoked browser must not write: %+v", r)
	}
	if r := b.do(http.MethodGet, "/management/v1/auth/session", nil, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("revoked browser must not read: %+v", r)
	}

	auditor := hs.createUser(a, "auditor", iam.RoleAuditor, iam.Global, "10.0.0.6")
	var auditorSession string
	for _, it := range auditor.do(http.MethodGet, "/management/v1/me/sessions", nil, nil).Body["sessions"].([]any) {
		auditorSession = it.(map[string]any)["id"].(string)
	}
	if r := a.do(http.MethodDelete, "/management/v1/me/sessions/"+auditorSession, nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("another user's session must look nonexistent: %+v", r)
	}
	if r := auditor.do(http.MethodGet, "/management/v1/auth/session", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("auditor session must survive: %+v", r)
	}

	r = a.do(http.MethodPost, "/management/v1/me/sessions/revoke-others", nil, nil)
	if r.Code != http.StatusOK || r.Body["revoked"].(float64) < 1 {
		t.Fatalf("revoke others: %+v", r)
	}
	if r := a.do(http.MethodGet, "/management/v1/auth/session", nil, nil); r.Code != http.StatusOK {
		t.Fatalf("the current session stays: %+v", r)
	}
	if !strings.Contains(a.do(http.MethodGet, "/management/v1/audit-events", nil, nil).Raw, `"session.revoke"`) {
		t.Fatal("revocation must be audited")
	}
}

// SET-004: the idle timeout is the user's choice within bounds and applies
// to the current session at once.
func TestSessionIdleTimeoutSetting(t *testing.T) {
	hs := newHarness(t)
	owner := hs.bootstrap()
	if r := owner.do(http.MethodPut, "/management/v1/me/session-settings", map[string]int{"idleTimeoutSeconds": 60}, nil); r.Code != http.StatusBadRequest {
		t.Fatalf("below the minimum: %+v", r)
	}
	r := owner.do(http.MethodPut, "/management/v1/me/session-settings", map[string]int{"idleTimeoutSeconds": 1800}, nil)
	if r.Code != http.StatusOK || r.Body["idleTimeoutSeconds"] != float64(1800) || r.Body["default"] != false {
		t.Fatalf("set: %+v", r)
	}
	hs.clock = hs.clock.Add(31 * time.Minute)
	if r := owner.do(http.MethodGet, "/management/v1/auth/session", nil, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("session must expire after the user's idle timeout: %+v", r)
	}
	if r := owner.login("owner", ownerPW); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	r = owner.do(http.MethodPut, "/management/v1/me/session-settings", map[string]int{"idleTimeoutSeconds": 0}, nil)
	if r.Code != http.StatusOK || r.Body["default"] != true || r.Body["idleTimeoutSeconds"] != float64(12*3600) {
		t.Fatalf("reset to default: %+v", r)
	}
}

// SET-004: a long job is not cancelled when the browser's login ends, and
// signing in again shows its result.
func TestLongJobSurvivesSessionEnd(t *testing.T) {
	_, e, bh, hs := newJobsHarness(t)
	c := hs.client("10.0.0.1")
	if r := c.login("owner", ownerPW); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	r := c.do(http.MethodPost, "/management/v1/jobs", map[string]any{"requestId": "long-1", "kind": "demo.install"}, nil)
	if r.Code != http.StatusAccepted {
		t.Fatalf("submit: %+v", r)
	}
	id := r.Body["jobId"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.Wait(ctx, id, func(j *jobs.Job) bool { return j.State == jobs.StateRunning }); err != nil {
		t.Fatal(err)
	}
	// The login ends both ways: explicit logout and idle expiry.
	if r := c.do(http.MethodPost, "/management/v1/auth/logout", nil, nil); r.Code != http.StatusNoContent {
		t.Fatal(r)
	}
	hs.clock = hs.clock.Add(13 * time.Hour)
	if j, err := e.Get(ctx, id); err != nil || j.State != jobs.StateRunning {
		t.Fatalf("job must keep running after the login ends: %v %+v", err, j)
	}
	close(bh.release)
	if _, err := e.Wait(ctx, id, func(j *jobs.Job) bool { return j.State == jobs.StateSucceeded }); err != nil {
		t.Fatal(err)
	}
	again := hs.client("10.0.0.1")
	if r := again.login("owner", ownerPW); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	r = again.do(http.MethodGet, "/management/v1/jobs/"+id, nil, nil)
	if r.Code != http.StatusOK || r.Body["job"].(map[string]any)["state"] != "succeeded" {
		t.Fatalf("result after signing in again: %+v", r)
	}
}

// SET-004: while the user keeps working, the browser cookie's lifetime moves
// with the server-side idle expiry instead of dying at its first Max-Age.
func TestSlidingSessionRenewsCookie(t *testing.T) {
	hs := newHarness(t)
	owner := hs.bootstrap()
	if r := owner.do(http.MethodPut, "/management/v1/me/session-settings", map[string]int{"idleTimeoutSeconds": 900}, nil); r.Code != http.StatusOK {
		t.Fatal(r)
	}
	maxAge := func(r resp) int {
		for _, line := range r.Hdr.Values("Set-Cookie") {
			if strings.HasPrefix(line, SessionCookie+"=") {
				for _, part := range strings.Split(line, ";") {
					if v, ok := strings.CutPrefix(strings.TrimSpace(part), "Max-Age="); ok {
						n := 0
						for _, ch := range v {
							n = n*10 + int(ch-'0')
						}
						return n
					}
				}
			}
		}
		return -1
	}
	for i := 0; i < 4; i++ {
		hs.clock = hs.clock.Add(5 * time.Minute)
		r := owner.do(http.MethodGet, "/management/v1/auth/session", nil, nil)
		if r.Code != http.StatusOK {
			t.Fatalf("active user signed out after %d minutes: %+v", 5*(i+1), r)
		}
		if got := maxAge(r); got != 900 {
			t.Fatalf("after %d minutes the session cookie must be re-sent with Max-Age=900, got %d (%v)", 5*(i+1), got, r.Hdr.Values("Set-Cookie"))
		}
	}
}

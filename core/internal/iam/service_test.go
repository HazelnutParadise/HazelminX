package iam

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

func newTestService(t *testing.T) (*Service, *fakeClock) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clk := &fakeClock{t: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}
	svc, err := New(st.DB, Config{PasswordParams: TestPasswordParams, Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	return svc, clk
}

const ownerPW = "owner-password-123"

func setupOwner(t *testing.T, svc *Service) *User {
	t.Helper()
	ctx := context.Background()
	code, _, err := svc.SetupCode(ctx)
	if err != nil || code == "" {
		t.Fatalf("setup code: %q %v", code, err)
	}
	u, err := svc.CompleteSetup(ctx, code, "owner", ownerPW, Source{IP: "10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSetupIsOneShot(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	if _, err := svc.CompleteSetup(ctx, "wrong", "owner", ownerPW, Source{IP: "1.1.1.1"}); !errors.Is(err, ErrSetupCode) {
		t.Fatalf("wrong code: %v", err)
	}
	setupOwner(t, svc)
	if need, _ := svc.NeedsSetup(ctx); need {
		t.Fatal("setup must be complete")
	}
	if code, _, _ := svc.SetupCode(ctx); code != "" {
		t.Fatal("no code after setup")
	}
	if _, err := svc.CompleteSetup(ctx, "anything", "evil", ownerPW, Source{}); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("second setup: %v", err)
	}
}

func TestConcurrentSetupYieldsExactlyOneOwner(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	code, _, _ := svc.SetupCode(ctx)
	results := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		go func(name string) {
			_, err := svc.CompleteSetup(ctx, code, name, ownerPW, Source{IP: "1.1.1.1"})
			results <- err
		}(name)
	}
	var okCount int
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			okCount++
		} else if !errors.Is(err, ErrSetupCode) && !errors.Is(err, ErrSetupDone) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if okCount != 1 {
		t.Fatalf("exactly one caller may become owner, got %d", okCount)
	}
	users, _ := svc.ListUsers(ctx, principalWith(RoleOwner, Global))
	if len(users) != 1 || len(users[0].Assignments) != 1 || users[0].Assignments[0].Role != RoleOwner {
		t.Fatalf("owner must exist with exactly the owner assignment: %+v", users)
	}
}

func TestSetupRejectsBadInputBeforeConsumingCode(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	code, _, _ := svc.SetupCode(ctx)
	if _, err := svc.CompleteSetup(ctx, code, "owner", "short", Source{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("short password: %v", err)
	}
	if _, err := svc.CompleteSetup(ctx, code, "owner", ownerPW, Source{}); err != nil {
		t.Fatalf("code must survive a validation error: %v", err)
	}
}

func TestSetupCodeExpires(t *testing.T) {
	svc, clk := newTestService(t)
	ctx := context.Background()
	code, exp, _ := svc.SetupCode(ctx)
	clk.t = exp.Add(time.Second)
	if _, err := svc.CompleteSetup(ctx, code, "owner", ownerPW, Source{}); !errors.Is(err, ErrSetupCode) {
		t.Fatalf("expired code must be refused: %v", err)
	}
	code2, _, _ := svc.SetupCode(ctx)
	if code2 == "" || code2 == code {
		t.Fatal("a fresh code must be minted after expiry")
	}
}

func TestLoginDoesNotRevealAccounts(t *testing.T) {
	svc, _ := newTestService(t)
	setupOwner(t, svc)
	ctx := context.Background()
	_, _, _, errUnknown := svc.Login(ctx, "nobody", "whatever-password", Source{IP: "1.1.1.1"})
	_, _, _, errWrong := svc.Login(ctx, "owner", "wrong-password-xx", Source{IP: "1.1.1.1"})
	if !errors.Is(errUnknown, ErrInvalidCredentials) || !errors.Is(errWrong, ErrInvalidCredentials) || errUnknown.Error() != errWrong.Error() {
		t.Fatalf("unknown=%v wrong=%v", errUnknown, errWrong)
	}
}

func TestLoginSessionLifecycle(t *testing.T) {
	svc, clk := newTestService(t)
	owner := setupOwner(t, svc)
	ctx := context.Background()
	sess, u, _, err := svc.Login(ctx, "Owner", ownerPW, Source{IP: "10.0.0.1", UserAgent: "test"})
	if err != nil || u.ID != owner.ID || sess.Token == "" || sess.CSRFToken == "" {
		t.Fatalf("login: %v %+v", err, sess)
	}
	p, got, err := svc.Authenticate(ctx, sess.Token)
	if err != nil || p.User.ID != owner.ID || got.ID != sess.ID || p.Kind != "session" {
		t.Fatalf("authenticate: %v %+v", err, p)
	}
	if !Authorize(p, ActionUsersWrite, Global) {
		t.Fatal("owner session must carry the owner assignment")
	}
	if err := svc.Logout(ctx, p, Source{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session must fail: %v", err)
	}

	sess2, _, _, _ := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1"})
	clk.advance(13 * time.Hour)
	if _, _, err := svc.Authenticate(ctx, sess2.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("idle session must expire: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, "garbage"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal("unknown token must fail")
	}
}

func TestThrottleCannotLockOutAdminPermanently(t *testing.T) {
	svc, clk := newTestService(t)
	setupOwner(t, svc)
	ctx := context.Background()
	// Owner has logged in from 10.0.0.1 before: that source is trusted.
	if _, _, _, err := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	attacker := Source{IP: "203.0.113.9"}
	var throttled bool
	for i := 0; i < 10; i++ {
		_, _, _, err := svc.Login(ctx, "owner", "guess-number-"+string(rune('0'+i)), attacker)
		if errors.Is(err, ErrThrottled) {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Fatal("repeated failures on one account must be throttled")
	}
	if _, _, _, err := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1"}); err != nil {
		t.Fatalf("trusted source must still log in while the account is under attack: %v", err)
	}
	// Unknown accounts throttle identically, so the throttle itself leaks nothing.
	var unknownThrottled bool
	for i := 0; i < 10; i++ {
		if _, _, _, err := svc.Login(ctx, "ghost", "guess-number-x", attacker); errors.Is(err, ErrThrottled) {
			unknownThrottled = true
			break
		}
	}
	if !unknownThrottled {
		t.Fatal("unknown usernames must throttle like real ones")
	}
	// A new, untrusted source for the owner is throttled now, but only for a bounded time.
	newDevice := Source{IP: "198.51.100.7"}
	if _, _, _, err := svc.Login(ctx, "owner", ownerPW, newDevice); !errors.Is(err, ErrThrottled) {
		t.Fatalf("untrusted source during cool-down: %v", err)
	}
	clk.advance(DefaultLimiterPolicy.AccountMaxDelay + time.Second)
	if _, _, _, err := svc.Login(ctx, "owner", ownerPW, newDevice); err != nil {
		t.Fatalf("cool-down must end: %v", err)
	}
}

func TestUsersRolesAndAudit(t *testing.T) {
	svc, _ := newTestService(t)
	owner := setupOwner(t, svc)
	ctx := context.Background()
	ownerP, _ := svc.principal(ctx, owner.ID)
	src := Source{IP: "10.0.0.1", TraceID: "trace-1"}

	bob, err := svc.CreateUser(ctx, ownerP, CreateUserInput{Username: "bob", DisplayName: "Bob", Password: "bob-password-123"}, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, ownerP, CreateUserInput{Username: "BOB", Password: "bob-password-123"}, src); !errors.Is(err, ErrConflict) {
		t.Fatalf("case-insensitive duplicate: %v", err)
	}
	if _, err := svc.CreateUser(ctx, ownerP, CreateUserInput{Username: "x", Password: "short"}, src); !errors.Is(err, ErrValidation) {
		t.Fatalf("validation: %v", err)
	}
	bobP, _ := svc.principal(ctx, bob.ID)
	if Authorize(bobP, ActionProjectsRead, Scope{Kind: ScopeProject, ID: "A"}) {
		t.Fatal("new users have no permissions (IAM-001)")
	}
	if _, err := svc.CreateUser(ctx, bobP, CreateUserInput{Username: "eve", Password: "eve-password-123"}, src); !errors.Is(err, ErrForbidden) {
		t.Fatalf("bob must not create users: %v", err)
	}
	if _, err := svc.AssignRole(ctx, bobP, bob.ID, RoleOwner, Global, src); !errors.Is(err, ErrForbidden) {
		t.Fatalf("bob must not grant himself owner: %v", err)
	}
	as, err := svc.AssignRole(ctx, ownerP, bob.ID, RoleProjectAdmin, Scope{Kind: ScopeProject, ID: "A"}, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AssignRole(ctx, ownerP, bob.ID, RoleOwner, Scope{Kind: ScopeProject, ID: "A"}, src); !errors.Is(err, ErrValidation) {
		t.Fatalf("scoped owner must be refused: %v", err)
	}
	if _, err := svc.AssignRole(ctx, ownerP, bob.ID, Role("root"), Global, src); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown role: %v", err)
	}
	bobP, _ = svc.principal(ctx, bob.ID)
	if !Authorize(bobP, ActionProjectsDeploy, Scope{Kind: ScopeProject, ID: "A"}) || Visible(bobP, Scope{Kind: ScopeProject, ID: "B"}) {
		t.Fatal("assignment must grant A only")
	}

	// Audit: the ledger links actor, resource, before/after, request and masks the password.
	events, err := svc.ListAudit(ctx, ownerP, AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var sawCreate, sawAssign, sawDeny bool
	for _, ev := range events {
		switch ev.Action {
		case "user.create":
			sawCreate = true
			if ev.ActorID != owner.ID || ev.ResourceID != bob.ID || ev.TraceID != "trace-1" || ev.SourceIP != "10.0.0.1" {
				t.Fatalf("user.create event incomplete: %+v", ev)
			}
			if ev.After["password"] != "***" || ev.After["username"] != "bob" {
				t.Fatalf("password must be masked, username kept: %v", ev.After)
			}
		case "role.assign":
			sawAssign = true
			if ev.Scope.ID != "A" || ev.After["role"] != string(RoleProjectAdmin) {
				t.Fatalf("role.assign event: %+v", ev)
			}
		case "authz.deny":
			sawDeny = true
		}
	}
	if !sawCreate || !sawAssign || !sawDeny {
		t.Fatalf("missing audit rows: create=%v assign=%v deny=%v", sawCreate, sawAssign, sawDeny)
	}

	// Bob (project admin, no audit:read) cannot read the ledger at all.
	if _, err := svc.ListAudit(ctx, bobP, AuditFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("bob audit: %v", err)
	}
	// An auditor scoped to project A sees A's rows and an empty page for B.
	aud, _ := svc.CreateUser(ctx, ownerP, CreateUserInput{Username: "aud", Password: "aud-password-123"}, src)
	svc.AssignRole(ctx, ownerP, aud.ID, RoleAuditor, Scope{Kind: ScopeProject, ID: "A"}, src)
	svc.Record(ctx, AuditEvent{Action: "deploy", Scope: Scope{Kind: ScopeProject, ID: "B"}, JobID: "job-b", NodeID: "n1", Result: ResultOK})
	svc.Record(ctx, AuditEvent{Action: "deploy", Scope: Scope{Kind: ScopeProject, ID: "A"}, JobID: "job-a", NodeID: "n1", Result: ResultOK, After: map[string]any{"image": "x:1", "dbPassword": "zzz"}})
	audP, _ := svc.principal(ctx, aud.ID)
	evs, err := svc.ListAudit(ctx, audP, AuditFilter{})
	if err != nil || len(evs) == 0 {
		t.Fatalf("auditor list: %v %d", err, len(evs))
	}
	for _, ev := range evs {
		if ev.Scope.ID != "A" {
			t.Fatalf("auditor of A saw %+v", ev.Scope)
		}
	}
	if evs[len(evs)-1].JobID != "job-a" || evs[len(evs)-1].NodeID != "n1" || evs[len(evs)-1].After["dbPassword"] != "***" {
		t.Fatalf("job/node/mask: %+v", evs[len(evs)-1])
	}
	b := Scope{Kind: ScopeProject, ID: "B"}
	evs, err = svc.ListAudit(ctx, audP, AuditFilter{Scope: &b})
	if err != nil || len(evs) != 0 {
		t.Fatalf("project B must look empty to A's auditor: %v %d", err, len(evs))
	}
	evs, _ = svc.ListAudit(ctx, audP, AuditFilter{JobID: "job-b"})
	if len(evs) != 0 {
		t.Fatal("job search must not leak other projects")
	}

	// Revoke removes the grant.
	if err := svc.RevokeRole(ctx, ownerP, as.ID, src); err != nil {
		t.Fatal(err)
	}
	bobP, _ = svc.principal(ctx, bob.ID)
	if Authorize(bobP, ActionProjectsRead, Scope{Kind: ScopeProject, ID: "A"}) {
		t.Fatal("revoked assignment must stop granting")
	}
	if err := svc.RevokeRole(ctx, ownerP, as.ID, src); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
}

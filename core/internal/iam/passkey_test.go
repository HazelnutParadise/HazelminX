package iam

import (
	"context"
	"encoding/base32"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// newMFATestService is newTestService with a secret store, which TOTP seeds
// and the recovery-code key need.
func newMFATestService(t *testing.T) (*Service, *fakeClock) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	kr, err := secrets.LoadOrCreateKeyring(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kr.Close() })
	vault, err := secrets.Open(ctx, st, kr)
	if err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{t: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}
	svc, err := New(st.DB, Config{PasswordParams: TestPasswordParams, Secrets: vault, Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	return svc, clk
}

var testRP = RelyingParty{ID: "panel.example.test", Origin: "https://panel.example.test"}

// ownerSession completes setup and returns the owner's principal and
// session token.
func ownerSession(t *testing.T, svc *Service) (*Principal, string) {
	t.Helper()
	ctx := context.Background()
	code, _, _ := svc.SetupCode(ctx)
	res, err := svc.CompleteSetup(ctx, code, "owner", ownerPW, Source{IP: "10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	return mustAuth(t, svc, res.Session.Token), res.Session.Token
}

func mustAuth(t *testing.T, svc *Service, token string) *Principal {
	t.Helper()
	p, _, err := svc.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	return p
}

func registerPasskey(t *testing.T, svc *Service, p *Principal, a *softAuthenticator) *EnrollmentResult {
	t.Helper()
	ctx := context.Background()
	opts, err := svc.BeginPasskeyRegistration(ctx, p, testRP)
	if err != nil {
		t.Fatalf("begin passkey registration: %v", err)
	}
	res, err := svc.FinishPasskeyRegistration(ctx, p, testRP, "Laptop", a.register(t, opts, testRP.Origin), Source{IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("finish passkey registration: %v", err)
	}
	return res
}

func enrollTOTP(t *testing.T, svc *Service, clk *fakeClock, p *Principal) []byte {
	t.Helper()
	ctx := context.Background()
	en, err := svc.BeginTOTP(ctx, p, Source{})
	if err != nil {
		t.Fatalf("begin totp: %v", err)
	}
	seed, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(en.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmTOTP(ctx, p, TOTPCode(seed, TOTPStep(clk.now())), Source{}); err != nil {
		t.Fatalf("confirm totp: %v", err)
	}
	return seed
}

// T-130 and IAM-011: passkeys work as a second factor, are bound to their
// domain, and a domain change is checked for its effect before it happens.
func TestPasskeySecondFactorAndDomainImpact(t *testing.T) {
	svc, clk := newMFATestService(t)
	ctx := context.Background()
	p, token := ownerSession(t, svc)
	if !p.EnrollmentRequired {
		t.Fatal("a fresh owner must be held to enrolment")
	}
	auth := newSoftAuthenticator(t)
	res := registerPasskey(t, svc, p, auth)
	if len(res.RecoveryCodes) != RecoveryCodeCount || res.Passkey == nil || res.Passkey.RPID != testRP.ID {
		t.Fatalf("first factor must come with recovery codes: %+v", res)
	}
	if p = mustAuth(t, svc, token); p.EnrollmentRequired {
		t.Fatal("an owner with a passkey is no longer held to enrolment")
	}

	// Second login step with the passkey, offered only on its own domain.
	login, err := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1", RPID: "elsewhere.example"})
	if err != nil || login.Challenge == nil {
		t.Fatalf("password step: %v %+v", err, login)
	}
	for _, m := range login.Challenge.Methods {
		if m == MethodPasskey {
			t.Fatal("a passkey for another domain must not be offered")
		}
	}
	login, _ = svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1", RPID: testRP.ID})
	if login.Challenge.Methods[0] != MethodPasskey {
		t.Fatalf("methods on the passkey's domain: %v", login.Challenge.Methods)
	}
	opts, err := svc.BeginPasskeyLogin(ctx, login.Challenge.Token, testRP)
	if err != nil {
		t.Fatal(err)
	}
	evil := auth.assert(t, opts, "https://evil.example")
	if _, err := svc.CompleteSecondFactor(ctx, login.Challenge.Token, Proof{Method: MethodPasskey, Credential: evil}, testRP, Source{}); !errors.Is(err, ErrSecondFactor) {
		t.Fatalf("an assertion made for another origin must fail: %v", err)
	}
	good := auth.assert(t, opts, testRP.Origin)
	done, err := svc.CompleteSecondFactor(ctx, login.Challenge.Token, Proof{Method: MethodPasskey, Credential: good}, testRP, Source{IP: "10.0.0.1"})
	if err != nil || done.Session == nil || done.Session.SecondFactor != MethodPasskey {
		t.Fatalf("passkey login: %v %+v", err, done)
	}
	// Replaying the same assertion on a new challenge fails: wrong challenge.
	login, _ = svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1", RPID: testRP.ID})
	if _, err := svc.BeginPasskeyLogin(ctx, login.Challenge.Token, testRP); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteSecondFactor(ctx, login.Challenge.Token, Proof{Method: MethodPasskey, Credential: good}, testRP, Source{}); !errors.Is(err, ErrSecondFactor) {
		t.Fatalf("a replayed assertion must fail: %v", err)
	}

	// Domain impact, before any change.
	impact := func(d string) *DomainImpact {
		t.Helper()
		out, err := svc.PasskeyDomainImpact(ctx, p, d, Source{})
		if err != nil {
			t.Fatalf("impact %s: %v", d, err)
		}
		return out
	}
	if out := impact("panel.example.test"); out.PasskeysLost != 0 || !out.Safe {
		t.Fatalf("same domain: %+v", out)
	}
	if out := impact("Admin.Panel.Example.Test."); out.PasskeysLost != 0 {
		t.Fatalf("a subdomain keeps passkeys of its parent RP ID: %+v", out)
	}
	out := impact("new.example.org")
	if out.PasskeysTotal != 1 || out.PasskeysLost != 1 || out.UsersLockedOut != 1 || out.Safe || len(out.Users) != 1 ||
		out.Users[0].KeepsSecondFactor || !out.Users[0].Enforced || out.Users[0].RecoveryCodesRemaining != RecoveryCodeCount {
		t.Fatalf("moving away from the only passkey's domain must be reported as unsafe: %+v", out)
	}
	for _, bad := range []string{"10.0.0.1", "https://new.example.org", "new.example.org:8443", "", "bad_label.example"} {
		if _, err := svc.PasskeyDomainImpact(ctx, p, bad, Source{}); !errors.Is(err, ErrValidation) {
			t.Fatalf("domain %q must be refused: %v", bad, err)
		}
	}
	// A rescue path for the new domain makes it safe.
	enrollTOTP(t, svc, clk, p)
	if out := impact("new.example.org"); !out.Safe || !out.Users[0].KeepsSecondFactor || !out.Users[0].HasTOTP {
		t.Fatalf("with TOTP the owner keeps a second factor: %+v", out)
	}

	// Only those who may change the platform address may ask.
	if _, err := svc.CreateUser(ctx, p, CreateUserInput{Username: "audrey", Password: "audrey-password-1"}, Source{}); err != nil {
		t.Fatal(err)
	}
	users, _ := svc.ListUsers(ctx, p)
	for _, u := range users {
		if u.Username == "audrey" {
			if _, err := svc.AssignRole(ctx, p, u.ID, RoleAuditor, Global, Source{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	aud := mustAuth(t, svc, mustSessionToken(t, svc, "audrey", "audrey-password-1"))
	if _, err := svc.PasskeyDomainImpact(ctx, aud, "new.example.org", Source{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("auditor: %v", err)
	}

	// Removing a passkey needs a fresh proof; the last factor stays.
	clk.advance(11 * time.Minute)
	if err := svc.RemovePasskey(ctx, p, res.Passkey.ID, Source{}); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("stale session: %v", err)
	}
	ro, err := svc.BeginPasskeyReauth(ctx, p, testRP)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Reauthenticate(ctx, p, ownerPW, Proof{Method: MethodPasskey, Credential: auth.assert(t, ro, testRP.Origin)}, testRP, Source{}); err != nil {
		t.Fatalf("reauthenticate with passkey: %v", err)
	}
	if err := svc.RemovePasskey(ctx, aud, res.Passkey.ID, Source{}); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("someone else's passkey: %v", err)
	}
	if err := svc.RemovePasskey(ctx, p, res.Passkey.ID, Source{}); err != nil {
		t.Fatalf("remove passkey: %v", err)
	}
	if err := svc.RemoveTOTP(ctx, p, Source{}); !errors.Is(err, ErrLastFactor) {
		t.Fatalf("the owner's last factor must stay: %v", err)
	}
}

// mustSessionToken signs in an account that has no second factor and
// returns its session token.
func mustSessionToken(t *testing.T, svc *Service, username, password string) string {
	t.Helper()
	res, err := svc.Login(context.Background(), username, password, Source{IP: "10.0.0.1"})
	if err != nil || res.Session == nil {
		t.Fatalf("login %s: %v %+v", username, err, res)
	}
	return res.Session.Token
}

// Two requests racing with the same recovery code: exactly one signs in.
func TestRecoveryCodeConcurrentUseSucceedsOnce(t *testing.T) {
	svc, clk := newMFATestService(t)
	ctx := context.Background()
	p, _ := ownerSession(t, svc)
	en, err := svc.BeginTOTP(ctx, p, Source{})
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(en.Secret)
	res, err := svc.ConfirmTOTP(ctx, p, TOTPCode(seed, TOTPStep(clk.now())), Source{})
	if err != nil || len(res.RecoveryCodes) != RecoveryCodeCount {
		t.Fatalf("confirm: %v %+v", err, res)
	}
	for _, method := range []string{MethodRecovery, MethodTOTP} {
		// The losers' failures count against the second-factor limit; let the
		// bounded cool-down pass so each round starts clean.
		clk.advance(DefaultLimiterPolicy.AccountMaxDelay + time.Second)
		proof := Proof{Method: method, Code: res.RecoveryCodes[0]}
		if method == MethodTOTP {
			proof.Code = TOTPCode(seed, TOTPStep(clk.now()))
		}
		login, err := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1"})
		if err != nil || login.Challenge == nil {
			t.Fatalf("password step: %v", err)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := 0
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if r, err := svc.CompleteSecondFactor(ctx, login.Challenge.Token, proof, testRP, Source{IP: "10.0.0.1"}); err == nil && r.Session != nil {
					mu.Lock()
					ok++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if ok != 1 {
			t.Fatalf("%s used concurrently: %d sign-ins, want exactly 1", proof.Method, ok)
		}
	}
}

func TestRelyingPartyUsable(t *testing.T) {
	for _, c := range []struct {
		rp RelyingParty
		ok bool
	}{
		{testRP, true},
		{RelyingParty{ID: "localhost", Origin: "http://localhost:9800"}, true},
		{RelyingParty{ID: "example.test", Origin: "https://panel.example.test"}, true},
		{RelyingParty{ID: "10.0.0.1", Origin: "https://10.0.0.1"}, false},
		{RelyingParty{ID: "panel.example.test", Origin: "http://panel.example.test"}, false},
		{RelyingParty{ID: "other.example", Origin: "https://panel.example.test"}, false},
		{RelyingParty{}, false},
	} {
		if err := c.rp.Usable(); (err == nil) != c.ok {
			t.Errorf("%+v: usable=%v, want %v (%v)", c.rp, err == nil, c.ok, err)
		}
	}
}

// Concurrent passkey option requests and answers on one challenge must not
// race (run with -race).
func TestPasskeyChallengeConcurrentAccess(t *testing.T) {
	svc, _ := newMFATestService(t)
	ctx := context.Background()
	p, _ := ownerSession(t, svc)
	auth := newSoftAuthenticator(t)
	registerPasskey(t, svc, p, auth)
	login, err := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1", RPID: testRP.ID})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); svc.BeginPasskeyLogin(ctx, login.Challenge.Token, testRP) }()
		go func() {
			defer wg.Done()
			svc.CompleteSecondFactor(ctx, login.Challenge.Token, Proof{Method: MethodPasskey, Credential: []byte("{}")}, testRP, Source{})
		}()
	}
	wg.Wait()
}

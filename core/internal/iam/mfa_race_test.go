package iam

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

// raceSecondFactor fires every proof at one challenge at the same moment and
// counts the outcomes.
func raceSecondFactor(t *testing.T, svc *Service, token string, proofs []Proof) map[string]int {
	t.Helper()
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for _, pr := range proofs {
		wg.Add(1)
		go func(pr Proof) {
			defer wg.Done()
			<-start
			r, err := svc.CompleteSecondFactor(ctx, token, pr, testRP, Source{IP: "10.0.0.9"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && r.Session != nil:
				counts["signed-in"]++
			case errors.Is(err, ErrSecondFactor):
				counts["rejected"]++
			case errors.Is(err, ErrThrottled), errors.Is(err, ErrChallengeInvalid):
				counts["refused"]++
			default:
				counts[fmt.Sprint(err)]++
			}
		}(pr)
	}
	close(start)
	wg.Wait()
	return counts
}

// IAM-012: a burst of simultaneous guesses on one challenge gets no more
// answers checked than sequential guessing would.
func TestConcurrentGuessesRespectAttemptLimit(t *testing.T) {
	svc, clk := newMFATestService(t)
	ctx := context.Background()
	p, _ := ownerSession(t, svc)
	seed := enrollTOTP(t, svc, clk, p)
	clk.advance(time.Minute)
	login, err := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1"})
	if err != nil || login.Challenge == nil {
		t.Fatal(err)
	}
	good := TOTPCode(seed, TOTPStep(clk.now()))
	var proofs []Proof
	for i := 0; i < 200; i++ {
		code := fmt.Sprintf("%06d", i)
		if code == good {
			code = "999999"
		}
		proofs = append(proofs, Proof{Method: MethodTOTP, Code: code})
	}
	proofs = append(proofs, Proof{Method: MethodTOTP, Code: good})
	counts := raceSecondFactor(t, svc, login.Challenge.Token, proofs)
	if counts["rejected"] > MaxChallengeAttempts || counts["signed-in"] > 1 || len(counts) > 3 {
		t.Fatalf("burst on one challenge: %v (at most %d answers may be checked, at most one sign-in)", counts, MaxChallengeAttempts)
	}
}

// A challenge signs in once even when two different valid proofs race.
func TestTwoValidProofsOpenOneSession(t *testing.T) {
	svc, clk := newMFATestService(t)
	ctx := context.Background()
	p, _ := ownerSession(t, svc)
	seed := enrollTOTP(t, svc, clk, p)
	f, _ := svc.Factors(ctx, p)
	if f.RecoveryCodesRemaining != RecoveryCodeCount {
		t.Fatalf("factors: %+v", f)
	}
	codes, err := svc.RegenerateRecoveryCodes(ctx, p, Source{})
	if err != nil {
		t.Fatal(err)
	}
	clk.advance(time.Minute)
	login, _ := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1"})
	counts := raceSecondFactor(t, svc, login.Challenge.Token, []Proof{
		{Method: MethodTOTP, Code: TOTPCode(seed, TOTPStep(clk.now()))},
		{Method: MethodRecovery, Code: codes[0]},
	})
	if counts["signed-in"] != 1 {
		t.Fatalf("two valid proofs on one challenge: %v, want exactly one sign-in", counts)
	}
}

// A passkey whose signature counter stays 0 (common for synced passkeys)
// cannot use one assertion twice.
func TestZeroCounterPasskeyAssertionSignsInOnce(t *testing.T) {
	svc, _ := newMFATestService(t)
	ctx := context.Background()
	p, _ := ownerSession(t, svc)
	auth := newSoftAuthenticator(t)
	registerPasskey(t, svc, p, auth)
	login, _ := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.1", RPID: testRP.ID})
	opts, err := svc.BeginPasskeyLogin(ctx, login.Challenge.Token, testRP)
	if err != nil {
		t.Fatal(err)
	}
	auth.signCount = math.MaxUint32 // the next assertion wraps to 0
	body := auth.assert(t, opts, testRP.Origin)
	proof := Proof{Method: MethodPasskey, Credential: body}
	counts := raceSecondFactor(t, svc, login.Challenge.Token, []Proof{proof, proof, proof})
	if counts["signed-in"] != 1 {
		t.Fatalf("one assertion used three times at once: %v, want exactly one sign-in", counts)
	}
}

// IAM-002: a password-only session opened while a privileged account had no
// second factor does not become a full session when the factor is added
// elsewhere.
func TestFirstFactorEndsPreEnrolmentSessions(t *testing.T) {
	svc, clk := newMFATestService(t)
	ctx := context.Background()
	p, token := ownerSession(t, svc)
	other := mustSessionToken(t, svc, "owner", ownerPW)
	if q := mustAuth(t, svc, other); !q.EnrollmentRequired {
		t.Fatal("precondition: the second session is held to enrolment")
	}
	enrollTOTP(t, svc, clk, p)
	if _, _, err := svc.Authenticate(ctx, other); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("a session that never passed a second factor must end when the first factor is added: %v", err)
	}
	// The session that enrolled keeps going.
	if q, _, err := svc.Authenticate(ctx, token); err != nil || q.EnrollmentRequired {
		t.Fatalf("the enrolling session must survive with full access: %v", err)
	}
}

// SET-004: shortening the idle timeout also shortens the user's other idle
// sessions, so a stolen idle cookie does not outlive the new setting.
func TestShorterIdleTimeoutCapsOtherSessions(t *testing.T) {
	svc, clk := newMFATestService(t)
	ctx := context.Background()
	p, _ := ownerSession(t, svc)
	auth := newSoftAuthenticator(t)
	registerPasskey(t, svc, p, auth)
	// A second session, signed in properly with a recovery code.
	codes, err := svc.RegenerateRecoveryCodes(ctx, p, Source{})
	if err != nil {
		t.Fatal(err)
	}
	login, _ := svc.Login(ctx, "owner", ownerPW, Source{IP: "10.0.0.2"})
	res, err := svc.CompleteSecondFactor(ctx, login.Challenge.Token, Proof{Method: MethodRecovery, Code: codes[0]}, testRP, Source{IP: "10.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSessionIdle(ctx, p, MinSessionIdle, Source{}); err != nil {
		t.Fatal(err)
	}
	clk.advance(MinSessionIdle + time.Minute)
	if _, _, err := svc.Authenticate(ctx, res.Session.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("an idle session must expire under the shorter timeout: %v", err)
	}
}

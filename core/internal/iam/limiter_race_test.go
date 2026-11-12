package iam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// raceOutcomes runs attempt n times at the same moment and counts the
// outcomes by error.
func raceOutcomes(n int, attempt func(i int) error) map[string]int {
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := attempt(i)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, ErrThrottled):
				counts["throttled"]++
			case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrSetupCode):
				counts["checked"]++
			default:
				counts[fmt.Sprint(err)]++
			}
		}()
	}
	close(start)
	wg.Wait()
	return counts
}

// Ticket 126: one source guessing many different accounts at once gets no
// more passwords checked than the source limit, as if it had guessed one at
// a time.
func TestConcurrentSprayFromOneSourceRespectsSourceLimit(t *testing.T) {
	svc, _ := newTestService(t)
	setupOwner(t, svc)
	const burst = 60
	limit := DefaultLimiterPolicy.SourceMaxFailures
	got := raceOutcomes(burst, func(i int) error {
		_, err := svc.Login(context.Background(), fmt.Sprintf("user-%d", i), "wrong-password", Source{IP: "10.9.9.9"})
		return err
	})
	want := map[string]int{"checked": limit, "throttled": burst - limit}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
}

// Ticket 126: setup has no account lock, so simultaneous wrong setup codes
// must still be held to the account cool-down by the limiter itself.
func TestConcurrentWrongSetupCodesRespectCooldown(t *testing.T) {
	svc, _ := newTestService(t)
	if code, _, err := svc.SetupCode(context.Background()); err != nil || code == "" {
		t.Fatalf("setup code: %v", err)
	}
	const burst = 40
	limit := DefaultLimiterPolicy.AccountFreeFailures
	got := raceOutcomes(burst, func(i int) error {
		_, err := svc.CompleteSetup(context.Background(), "wrong-code", "owner", ownerPW,
			Source{IP: fmt.Sprintf("10.2.%d.%d", i/250, i%250+1)})
		return err
	})
	want := map[string]int{"checked": limit, "throttled": burst - limit}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
}

func pendingCounts(l *Limiter) (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pendingAccounts), len(l.pendingSources)
}

// Ticket 126: attempts in flight count against the budgets, an attempt that
// ends without an answer (an error) gives its place back, and settling twice
// gives back nothing extra.
func TestAttemptsInFlightHoldTheirPlace(t *testing.T) {
	l, _ := newTestLimiter()
	free := DefaultLimiterPolicy.AccountFreeFailures
	var held []*Attempt
	for i := range free {
		a, _ := l.Begin("Owner", fmt.Sprintf("1.1.1.%d", i), false)
		if a == nil {
			t.Fatalf("attempt %d of %d must be admitted", i+1, free)
		}
		held = append(held, a)
	}
	if a, retry := l.Begin("owner ", "9.9.9.9", false); a != nil || retry != 0 {
		t.Fatalf("with the whole account budget in flight the next attempt waits for them (a=%v retry=%v)", a, retry)
	}
	if a, _ := l.Begin("owner", "9.9.9.9", true); a == nil {
		t.Fatal("a trusted source skips the account budget")
	} else {
		a.Done()
	}

	held[0].Done() // ended by an error: no failure recorded
	held[0].Done()
	held[0].Failure() // already settled: records nothing
	a, _ := l.Begin("owner", "9.9.9.9", false)
	if a == nil {
		t.Fatal("a settled attempt must give its place back")
	}
	if b, _ := l.Begin("owner", "9.9.9.8", false); b != nil {
		t.Fatal("settling twice must not give back a second place")
	}
	a.Done()
	for _, h := range held[1:] {
		h.Failure()
	}
	if ok, _ := check(l, "owner", "9.9.9.9", false); !ok {
		t.Fatalf("%d failures stay under the %d free ones", free-1, free)
	}
	if acc, src := pendingCounts(l); acc != 0 || src != 0 {
		t.Fatalf("pending entries left: accounts=%d sources=%d", acc, src)
	}
}

// Ticket 126: once a cool-down expires, only one attempt is in flight at a
// time, exactly as guessing one after another would allow.
func TestExpiredCoolDownAdmitsOneAttemptAtATime(t *testing.T) {
	l, clk := newTestLimiter()
	for i := range DefaultLimiterPolicy.AccountFreeFailures {
		l.Failure("owner", fmt.Sprintf("2.2.2.%d", i))
	}
	clk.advance(DefaultLimiterPolicy.AccountBaseDelay + time.Second)
	a, _ := l.Begin("owner", "3.3.3.3", false)
	if a == nil {
		t.Fatal("after the cool-down one attempt is admitted")
	}
	if b, _ := l.Begin("owner", "3.3.3.4", false); b != nil {
		t.Fatal("a second parallel attempt after the cool-down must wait")
	}
	a.Failure()
	if ok, retry := check(l, "owner", "3.3.3.4", false); ok || retry <= 0 {
		t.Fatalf("the failure re-arms a longer cool-down (ok=%v retry=%v)", ok, retry)
	}
}

// Ticket 126: after a burst over several accounts and sources, admitted and
// refused, the bookkeeping for attempts in flight is empty again.
func TestConcurrentSprayLeavesNoPendingEntries(t *testing.T) {
	svc, _ := newTestService(t)
	setupOwner(t, svc)
	raceOutcomes(30, func(i int) error {
		_, err := svc.Login(context.Background(), fmt.Sprintf("user-%d", i%7), "wrong-password", Source{IP: fmt.Sprintf("10.3.0.%d", i%3+1)})
		return err
	})
	if acc, src := pendingCounts(svc.limiter); acc != 0 || src != 0 {
		t.Fatalf("pending entries left: accounts=%d sources=%d", acc, src)
	}
}

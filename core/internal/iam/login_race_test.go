package iam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// raceLogin fires n wrong-password logins for username at the same moment,
// each from its own address so only the account counter can stop them, and
// counts the outcomes.
func raceLogin(t *testing.T, svc *Service, username string, n int) map[string]int {
	t.Helper()
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := svc.Login(ctx, username, "wrong-password", Source{IP: fmt.Sprintf("10.1.%d.%d", i/250, i%250+1)})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, ErrInvalidCredentials):
				counts["verified"]++
			case errors.Is(err, ErrThrottled):
				counts["throttled"]++
			default:
				counts[fmt.Sprint(err)]++
			}
		}(i)
	}
	close(start)
	wg.Wait()
	return counts
}

// IAM-012 (ticket 117): a burst of simultaneous wrong passwords for one
// account gets no more passwords checked than the cool-down allows, whether
// or not the account exists.
func TestConcurrentLoginGuessesRespectAccountCooldown(t *testing.T) {
	const burst = 40
	limit := DefaultLimiterPolicy.AccountFreeFailures
	for _, username := range []string{"owner", "ghost"} {
		t.Run(username, func(t *testing.T) {
			svc, _ := newTestService(t)
			setupOwner(t, svc)
			got := raceLogin(t, svc, username, burst)
			want := map[string]int{"verified": limit, "throttled": burst - limit}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("outcomes = %v, want %v", got, want)
			}
		})
	}
}

// Ticket 117: while one account's attempt is in progress, a login for another
// account goes straight through, and the held account's next attempt waits.
func TestLoginAttemptsOnDifferentAccountsDoNotWait(t *testing.T) {
	svc, _ := newTestService(t)
	setupOwner(t, svc)
	ctx := context.Background()

	release := svc.locks.lock(loginLockKey("owner"))
	waiting := make(chan error, 1)
	go func() {
		_, err := svc.Login(ctx, "OWNER", ownerPW, Source{IP: "10.0.0.1"})
		waiting <- err
	}()

	for _, other := range []string{"ghost", "someone-else"} {
		done := make(chan error, 1)
		go func() {
			_, err := svc.Login(ctx, other, "wrong-password", Source{IP: "10.0.0.2"})
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("%s: %v", other, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("login for %s waited on another account's attempt", other)
		}
	}

	select {
	case err := <-waiting:
		t.Fatalf("an attempt on the held account (any case) must wait, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case err := <-waiting:
		if err != nil {
			t.Fatalf("owner login after release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("owner login never ran after release")
	}
}

// Ticket 117: bursts on several accounts at once each get exactly their own
// budget, and the lock table is empty again afterwards, so made-up usernames
// leave nothing behind.
func TestConcurrentLoginBurstsAcrossAccountsLeaveNoLocks(t *testing.T) {
	svc, _ := newTestService(t)
	setupOwner(t, svc)
	limit := DefaultLimiterPolicy.AccountFreeFailures
	names := []string{"owner", "ghost-1", "ghost-2", "ghost-3"}
	var wg sync.WaitGroup
	results := make([]map[string]int, len(names))
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = raceLogin(t, svc, name, 15)
		}()
	}
	wg.Wait()
	for i, got := range results {
		if want := fmt.Sprint(map[string]int{"verified": limit, "throttled": 15 - limit}); fmt.Sprint(got) != want {
			t.Errorf("%s: outcomes = %v, want %v", names[i], got, want)
		}
	}
	if n := svc.locks.size(); n != 0 {
		t.Fatalf("%d lock entries left after every attempt finished", n)
	}
}

func TestKeyedLocksSerializeOneKeyOnly(t *testing.T) {
	k := newKeyedLocks()
	var mu sync.Mutex
	inside, maxInside := 0, 0
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := k.lock("a")
			mu.Lock()
			inside++
			maxInside = max(maxInside, inside)
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
			unlock()
		}()
	}
	wg.Wait()
	if maxInside != 1 {
		t.Fatalf("%d holders of one key at once", maxInside)
	}
	unlockA := k.lock("a")
	unlockB := k.lock("b") // would deadlock if keys shared a lock
	if k.size() != 2 {
		t.Fatalf("size = %d, want 2", k.size())
	}
	unlockB()
	unlockA()
	if k.size() != 0 {
		t.Fatalf("size = %d after release, want 0", k.size())
	}
}

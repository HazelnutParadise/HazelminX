package iam

import (
	"strings"
	"sync"
	"time"
)

// LimiterPolicy bounds login (and, in ticket 014, recovery-code) attempts.
//
// Two counters run side by side (IAM-012):
//   - per account key: after AccountFreeFailures the account cools down for
//     AccountBaseDelay doubling per extra failure, never above AccountMaxDelay;
//     the counter forgets failures older than AccountWindow.
//   - per source: SourceMaxFailures within SourceWindow blocks that source
//     for SourceBlock.
//
// Nothing is permanent: the longest an account can be cooled down after the
// last failed attempt is AccountMaxDelay, and a trusted source (one that
// already logged into that account successfully) skips the account cool-down
// entirely, so an attacker spraying a username cannot lock the real
// administrator out. Unknown usernames are counted exactly like real ones so
// the throttle never reveals whether an account exists.
type LimiterPolicy struct {
	AccountFreeFailures int
	AccountBaseDelay    time.Duration
	AccountMaxDelay     time.Duration
	AccountWindow       time.Duration
	SourceMaxFailures   int
	SourceWindow        time.Duration
	SourceBlock         time.Duration
}

// DefaultLimiterPolicy is the production setting.
var DefaultLimiterPolicy = LimiterPolicy{
	AccountFreeFailures: 5,
	AccountBaseDelay:    30 * time.Second,
	AccountMaxDelay:     15 * time.Minute,
	AccountWindow:       time.Hour,
	SourceMaxFailures:   20,
	SourceWindow:        15 * time.Minute,
	SourceBlock:         15 * time.Minute,
}

type attemptState struct {
	failures     int
	lastFailure  time.Time
	windowStart  time.Time
	blockedUntil time.Time
}

// Limiter is an in-memory attempt counter. Core is a single process with a
// single writer, so memory is enough; a restart clears cool-downs, which is
// the safe direction (it can only let legitimate users in sooner).
type Limiter struct {
	policy LimiterPolicy
	now    func() time.Time

	mu       sync.Mutex
	accounts map[string]*attemptState
	sources  map[string]*attemptState
}

// NewLimiter builds a limiter; now may be nil for time.Now.
func NewLimiter(p LimiterPolicy, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{policy: p, now: now, accounts: map[string]*attemptState{}, sources: map[string]*attemptState{}}
}

// Check reports whether an attempt for account from source may proceed.
// trustedSource skips the account cool-down. retryAfter is how long the caller
// should wait when blocked.
func (l *Limiter) Check(account, source string, trustedSource bool) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if st := l.sources[source]; st != nil && st.blockedUntil.After(now) {
		return false, st.blockedUntil.Sub(now)
	}
	if trustedSource {
		return true, 0
	}
	if st := l.accounts[normalizeAccount(account)]; st != nil && st.blockedUntil.After(now) {
		return false, st.blockedUntil.Sub(now)
	}
	return true, 0
}

// Failure records a failed attempt and updates both counters.
func (l *Limiter) Failure(account, source string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	p := l.policy

	a := l.state(l.accounts, normalizeAccount(account))
	if !a.lastFailure.IsZero() && now.Sub(a.lastFailure) > p.AccountWindow {
		a.failures = 0
	}
	a.failures++
	a.lastFailure = now
	if extra := a.failures - p.AccountFreeFailures; extra >= 0 {
		delay := p.AccountBaseDelay
		for i := 0; i < extra && delay < p.AccountMaxDelay; i++ {
			delay *= 2
		}
		if delay > p.AccountMaxDelay {
			delay = p.AccountMaxDelay
		}
		a.blockedUntil = now.Add(delay)
	}

	if len(l.accounts)+len(l.sources) > pruneAbove {
		l.prune(now)
	}
	s := l.state(l.sources, source)
	if s.windowStart.IsZero() || now.Sub(s.windowStart) > p.SourceWindow {
		s.windowStart = now
		s.failures = 0
	}
	s.failures++
	if s.failures >= p.SourceMaxFailures {
		s.blockedUntil = now.Add(p.SourceBlock)
	}
}

// Success resets the account's failure count. An active cool-down stays in
// place so a legitimate login from a trusted source does not hand an attacker
// a fresh budget; the source counter keeps its window for the same reason.
func (l *Limiter) Success(account string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if st := l.accounts[normalizeAccount(account)]; st != nil {
		st.failures = 0
		st.lastFailure = time.Time{}
	}
}

// pruneAbove bounds memory: once this many keys exist, stale entries are
// dropped before adding more, so an attacker with many addresses cannot grow
// the maps without bound.
const pruneAbove = 10000

func (l *Limiter) prune(now time.Time) {
	p := l.policy
	for k, st := range l.accounts {
		if !st.blockedUntil.After(now) && (st.lastFailure.IsZero() || now.Sub(st.lastFailure) > p.AccountWindow) {
			delete(l.accounts, k)
		}
	}
	for k, st := range l.sources {
		if !st.blockedUntil.After(now) && now.Sub(st.windowStart) > p.SourceWindow {
			delete(l.sources, k)
		}
	}
}

func (l *Limiter) state(m map[string]*attemptState, key string) *attemptState {
	st := m[key]
	if st == nil {
		st = &attemptState{}
		m[key] = st
	}
	return st
}

func normalizeAccount(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

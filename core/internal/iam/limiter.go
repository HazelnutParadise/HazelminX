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

	// Attempts admitted by Begin and not yet settled, per key. Entries are
	// deleted at zero, so these hold no more keys than attempts in flight,
	// and prune never sees them.
	pendingAccounts map[string]int
	pendingSources  map[string]int
}

// NewLimiter builds a limiter; now may be nil for time.Now.
func NewLimiter(p LimiterPolicy, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{policy: p, now: now, accounts: map[string]*attemptState{}, sources: map[string]*attemptState{},
		pendingAccounts: map[string]int{}, pendingSources: map[string]int{}}
}

// Attempt is one admitted attempt. It holds a place in the account and
// source budgets until it is settled by Failure, Success or Done, so
// attempts still in progress count when admitting others. A nil *Attempt is
// a refused one, and its methods do nothing.
type Attempt struct {
	l           *Limiter
	account     string // normalized
	source      string
	tookAccount bool // false for a trusted source, which skips the account budget
	settled     bool // guarded by l.mu
}

// Begin admits an attempt for account from source, or returns nil and how
// long the caller should wait. trustedSource skips the account cool-down.
// The caller must settle the attempt (defer a.Done() right after Begin).
//
// A key with nothing in flight is admitted exactly when it is not blocked,
// as before attempts were counted. With attempts in flight, each one counts
// as a failure that has not happened yet, so a burst gets no more attempts
// than one-at-a-time guessing would. A refusal caused only by attempts in
// flight returns retryAfter 0: they may succeed and free the budget.
func (l *Limiter) Begin(account, source string, trustedSource bool) (*Attempt, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	p := l.policy
	acct := normalizeAccount(account)

	// Blocks first, so a blocked caller always learns the real wait.
	src := l.sources[source]
	if src != nil && src.blockedUntil.After(now) {
		return nil, src.blockedUntil.Sub(now)
	}
	st := l.accounts[acct]
	if !trustedSource && st != nil && st.blockedUntil.After(now) {
		return nil, st.blockedUntil.Sub(now)
	}
	if !admit(l.sourceFailures(src, now), l.pendingSources[source], p.SourceMaxFailures) {
		return nil, 0
	}
	if !trustedSource {
		if !admit(l.accountFailures(st, now), l.pendingAccounts[acct], p.AccountFreeFailures) {
			return nil, 0
		}
		l.pendingAccounts[acct]++
	}
	l.pendingSources[source]++
	return &Attempt{l: l, account: acct, source: source, tookAccount: !trustedSource}, 0
}

// admit allows an attempt when nothing is in flight (the block check already
// passed), or when the failures so far plus those in flight stay below limit.
func admit(failures, pending, limit int) bool { return pending == 0 || failures+pending < limit }

// accountFailures and sourceFailures are the counts Failure would build on,
// with the same window resets, without changing anything.
func (l *Limiter) accountFailures(st *attemptState, now time.Time) int {
	if st == nil || (!st.lastFailure.IsZero() && now.Sub(st.lastFailure) > l.policy.AccountWindow) {
		return 0
	}
	return st.failures
}

func (l *Limiter) sourceFailures(st *attemptState, now time.Time) int {
	if st == nil || st.windowStart.IsZero() || now.Sub(st.windowStart) > l.policy.SourceWindow {
		return 0
	}
	return st.failures
}

// Failure records the attempt as failed and settles it.
func (a *Attempt) Failure() {
	a.settle(func(l *Limiter) { l.failure(a.account, a.source) })
}

// Success resets the account's failure count and settles the attempt.
func (a *Attempt) Success() {
	a.settle(func(l *Limiter) { l.success(a.account) })
}

// Done settles the attempt without an outcome (an error before the answer
// was judged). It does nothing once the attempt is settled, so it is safe to
// defer next to Failure or Success.
func (a *Attempt) Done() { a.settle(nil) }

func (a *Attempt) settle(record func(*Limiter)) {
	if a == nil {
		return
	}
	l := a.l
	l.mu.Lock()
	defer l.mu.Unlock()
	if a.settled {
		return
	}
	a.settled = true
	if record != nil {
		record(l)
	}
	if a.tookAccount {
		release(l.pendingAccounts, a.account)
	}
	release(l.pendingSources, a.source)
}

func release(m map[string]int, key string) {
	if m[key]--; m[key] <= 0 {
		delete(m, key)
	}
}

// Failure records a failed attempt and updates both counters. It does not
// settle an Attempt; callers holding one use Attempt.Failure.
func (l *Limiter) Failure(account, source string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failure(normalizeAccount(account), source)
}

func (l *Limiter) failure(account, source string) {
	now := l.now()
	p := l.policy

	a := l.state(l.accounts, account)
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
	l.success(normalizeAccount(account))
}

func (l *Limiter) success(account string) {
	if st := l.accounts[account]; st != nil {
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

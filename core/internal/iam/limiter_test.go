package iam

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time          { return f.t }
func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

func newTestLimiter() (*Limiter, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}
	return NewLimiter(DefaultLimiterPolicy, clk.now), clk
}

func TestAccountCoolDownIsBoundedAndNotPermanent(t *testing.T) {
	l, clk := newTestLimiter()
	for i := 0; i < DefaultLimiterPolicy.AccountFreeFailures; i++ {
		if ok, _ := l.Check("Owner", "1.1.1.1", false); !ok {
			t.Fatalf("attempt %d must still be free", i)
		}
		l.Failure("owner", "1.1.1.1")
	}
	if ok, retry := l.Check("OWNER ", "1.1.1.1", false); ok || retry <= 0 {
		t.Fatalf("after free failures the account must cool down (ok=%v retry=%v); keys are case/space-insensitive", ok, retry)
	}
	for i := 0; i < 50; i++ {
		l.Failure("owner", "2.2.2.2") // keep hammering from many sources
	}
	_, retry := l.Check("owner", "9.9.9.9", false)
	if retry > DefaultLimiterPolicy.AccountMaxDelay {
		t.Fatalf("cool-down %v exceeds the cap %v", retry, DefaultLimiterPolicy.AccountMaxDelay)
	}
	clk.advance(DefaultLimiterPolicy.AccountMaxDelay + time.Second)
	if ok, _ := l.Check("owner", "9.9.9.9", false); !ok {
		t.Fatal("after the maximum cool-down the account must be reachable again")
	}
}

func TestTrustedSourceBypassesAccountCoolDown(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 0; i < 10; i++ {
		l.Failure("owner", "6.6.6.6")
	}
	if ok, _ := l.Check("owner", "6.6.6.6", false); ok {
		t.Fatal("attacker source must be throttled")
	}
	if ok, _ := l.Check("owner", "10.0.0.5", true); !ok {
		t.Fatal("a trusted source must still reach the account")
	}
}

func TestSourceBlockAppliesEvenToTrustedSource(t *testing.T) {
	l, clk := newTestLimiter()
	for i := 0; i < DefaultLimiterPolicy.SourceMaxFailures; i++ {
		l.Failure("user"+string(rune('a'+i%26)), "7.7.7.7")
	}
	if ok, _ := l.Check("someone-else", "7.7.7.7", true); ok {
		t.Fatal("a source spraying many accounts is blocked regardless of trust")
	}
	clk.advance(DefaultLimiterPolicy.SourceBlock + time.Second)
	if ok, _ := l.Check("someone-else", "7.7.7.7", false); !ok {
		t.Fatal("source block must expire")
	}
}

func TestSuccessResetsCountButKeepsActiveCoolDown(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 0; i < 3; i++ {
		l.Failure("owner", "1.1.1.1")
	}
	l.Success("owner")
	for i := 0; i < 3; i++ {
		l.Failure("owner", "1.1.1.1")
	}
	if ok, _ := l.Check("owner", "1.1.1.1", false); !ok {
		t.Fatal("success must reset the failure count")
	}
	for i := 0; i < 5; i++ {
		l.Failure("owner", "1.1.1.1")
	}
	l.Success("owner") // trusted login while under attack
	if ok, _ := l.Check("owner", "2.2.2.2", false); ok {
		t.Fatal("an active cool-down must survive a legitimate login")
	}
}

func TestPruneKeepsMemoryBounded(t *testing.T) {
	l, clk := newTestLimiter()
	for i := 0; i < pruneAbove+10; i++ {
		l.Failure("spray", "10.0."+string(rune('0'+i%10))+"."+string(rune('0'+i/10%10))+string(rune('a'+i/100%26))+string(rune('a'+i/2600%26)))
	}
	clk.advance(DefaultLimiterPolicy.SourceWindow + DefaultLimiterPolicy.AccountWindow + time.Second)
	l.Failure("other", "9.9.9.9")
	if n := len(l.sources); n > 2 {
		t.Fatalf("stale sources must be pruned, still %d", n)
	}
}

package nodereg

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
)

// Regressions from the ticket 019 review.

// A host that claimed a code with a key that was retired while its claim
// waited must not become a node again.
func TestApprovingAClaimWhoseKeyWasRetiredMeanwhileIsRefused(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("vm", "m1")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	stale, code := e.newCode(CreateInput{})
	if st, _ := e.claim(h, code); st != 202 {
		t.Fatal("claim with the current key")
	}
	fresh := newHost("vm", "m1")
	en, code2 := e.newCode(CreateInput{})
	e.claim(fresh, code2)
	if _, err := e.reg.Approve(ctx, en.ID, ApproveInput{Actor: "usr_owner", ReplaceNodeID: n.ID}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []ApproveInput{{Actor: "usr_owner", AsNewNode: true}, {Actor: "usr_owner", ReplaceNodeID: n.ID}} {
		if got, err := e.reg.Approve(ctx, stale.ID, in); !errors.Is(err, ErrState) {
			t.Fatalf("%+v: retired key approved: %+v %v", in, got, err)
		}
	}
	if err := e.reg.AuthorizeNode(ctx, identity(h.fp, n.Certificate.Serial)); !errors.Is(err, nodes.ErrNodeRevoked) {
		t.Fatalf("retired key: %v", err)
	}
}

// An agent that fell back to its old certificate gets the new one again
// after a while, so rotation (and renewal before expiry) does not stall.
func TestRotationIsDeliveredAgainAfterTheAgentFellBack(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial)
	if _, err := e.reg.RotateCertificate(ctx, n.ID, "usr_owner"); err != nil || e.ch.renewals(h.fp) != 1 {
		t.Fatalf("first delivery: %v %d", err, e.ch.renewals(h.fp))
	}
	e.advance(time.Second)
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: n.Certificate.Serial, at: e.now()})
	got, _ := e.reg.GetNode(ctx, n.ID)
	if got.Certificate.Rotation == nil || got.Certificate.Rotation.Error == "" || e.ch.renewals(h.fp) != 1 {
		t.Fatalf("right after falling back: %+v %d", got.Certificate.Rotation, e.ch.renewals(h.fp))
	}
	e.advance(RedeliverAfter)
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: n.Certificate.Serial, at: e.now()})
	if e.ch.renewals(h.fp) != 2 {
		t.Fatalf("not delivered again: %d", e.ch.renewals(h.fp))
	}
}

// A revocation that lands while the node is in the middle of its handshake
// still closes the session that handshake opens.
func TestSessionThatStartsAfterRevocationIsClosed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := newHost("a", "")
	n := e.enroll(h, CreateInput{}, ApproveInput{})
	if _, err := e.reg.RevokeNode(ctx, n.ID, "", "usr_owner"); err != nil {
		t.Fatal(err)
	}
	e.ch.connect(h.fp, e.now(), n.Certificate.Serial) // registered after the revocation
	e.reg.onSession(ctx, sessionFacts{fp: h.fp, serial: n.Certificate.Serial, at: e.now()})
	if !slices.Contains(e.ch.disconnected, h.fp) {
		t.Fatal("session of a revoked node left open")
	}
	got, _ := e.reg.GetNode(ctx, n.ID)
	if got.Revocation.Pending {
		t.Fatalf("revocation still pending: %+v", got.Revocation)
	}
}

// Stored times have one width, so SQL text comparison agrees with time order.
func TestStampsHaveFixedWidth(t *testing.T) {
	a := stamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	b := stamp(time.Date(2026, 1, 1, 0, 0, 0, 500, time.UTC))
	if len(a) != len(b) || !(a < b) || !parseStamp(b).Equal(time.Date(2026, 1, 1, 0, 0, 0, 500, time.UTC)) {
		t.Fatalf("%q %q", a, b)
	}
}

func TestFailureLimiterStaysBounded(t *testing.T) {
	l := newFailureLimiter(20, 10*time.Minute)
	now := time.Now()
	for i := range 3 * maxLimiterSources {
		l.fail("198.51."+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune(i)), now)
	}
	if len(l.seen) > maxLimiterSources {
		t.Fatalf("%d sources kept", len(l.seen))
	}
}

func TestDiscoveryOfUnknownCodesCountsAsWrongCodes(t *testing.T) {
	e := newEnv(t)
	var err error
	for range 21 {
		_, err = e.reg.Discovery(context.Background(), "ZZZZ", "203.0.113.9")
	}
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("21st unknown code id: %v", err)
	}
}

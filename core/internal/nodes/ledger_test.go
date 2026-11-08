package nodes

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

var ledgerCaps = []protocol.Capability{{Name: protocol.CapJobs, Version: 1}, {Name: protocol.CapHostLedger, Version: 1}}

func testGrant() protocol.ExecutorGrant {
	return protocol.ExecutorGrant{ID: "grt-1", Subject: "system:ledger-sync", Permissions: []string{"host.ledger.read"},
		NotBefore: "2026-11-08T00:00:00Z", ExpiresAt: "2026-11-08T00:05:00Z", Signature: "ab"}
}

func TestPullLedgerSendsTheGrantAndReturnsThePage(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{caps: ledgerCaps})
	got := make(chan *protocol.LedgerPull, 1)
	a.serve(t, func(m any) []any {
		p, ok := m.(*protocol.LedgerPull)
		if !ok {
			return nil
		}
		got <- p
		return []any{protocol.LedgerPage{Type: protocol.TypeLedgerPage, RequestID: p.RequestID, LedgerID: "aa", AfterSeq: p.AfterSeq,
			Lines: []string{"l1"}, Head: protocol.LedgerHead{Seq: 1, Hash: "h"}, More: false}}
	})
	page, err := core.hub.PullLedger(context.Background(), n.fp, 3, 50, testGrant())
	if err != nil {
		t.Fatal(err)
	}
	if page.LedgerID != "aa" || len(page.Lines) != 1 || page.AfterSeq != 3 {
		t.Fatalf("page %+v", page)
	}
	p := <-got
	if p.AfterSeq != 3 || p.Limit != 50 || !reflect.DeepEqual(p.Grant, testGrant()) {
		t.Fatalf("pull %+v", p)
	}
}

func TestPullLedgerReportsTheAgentsRefusal(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{caps: ledgerCaps})
	a.serve(t, func(m any) []any {
		if p, ok := m.(*protocol.LedgerPull); ok {
			return []any{protocol.LedgerUnavailable{Type: protocol.TypeLedgerUnavailable, RequestID: p.RequestID,
				Code: protocol.LedgerExecutorUnreachable, Message: "no socket"}}
		}
		return nil
	})
	_, err := core.hub.PullLedger(context.Background(), n.fp, 0, 10, testGrant())
	var u *LedgerUnavailableError
	if !errors.As(err, &u) || u.Code != protocol.LedgerExecutorUnreachable {
		t.Fatalf("err %v", err)
	}
}

func TestPullLedgerSendsNothingToOldOrAbsentAgents(t *testing.T) {
	core := startCore(t, HubOptions{})
	if _, err := core.hub.PullLedger(context.Background(), "nobody", 0, 10, testGrant()); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("offline: %v", err)
	}
	a, n := connect(t, core, agentOpts{}) // jobs + agent.info only
	sent := make(chan any, 1)
	a.serve(t, func(m any) []any {
		if _, ok := m.(*protocol.LedgerPull); ok {
			sent <- m
		}
		return nil
	})
	if _, err := core.hub.PullLedger(context.Background(), n.fp, 0, 10, testGrant()); !errors.Is(err, ErrLedgerUnsupported) {
		t.Fatalf("old agent: %v", err)
	}
	select {
	case m := <-sent:
		t.Fatalf("sent %T to an agent without host.ledger", m)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPullLedgerEndsWithTheSessionOrTheContext(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{caps: ledgerCaps})
	a.serve(t, func(any) []any { return nil }) // never answers
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := core.hub.PullLedger(ctx, n.fp, 0, 10, testGrant()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := core.hub.PullLedger(context.Background(), n.fp, 0, 10, testGrant())
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	_ = a.conn.CloseNow()
	select {
	case err := <-done:
		if !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("closed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PullLedger did not return after the session ended")
	}
}

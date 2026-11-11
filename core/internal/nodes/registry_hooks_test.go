package nodes

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Hooks the node registry (ticket 019) relies on: certificate serial in
// the identity, duplicate handling that tells a reconnect from a clone,
// disconnecting a revoked node, certificate renewal, clock skew, and the
// step gate.

func TestIdentityCarriesCertificateSerial(t *testing.T) {
	p := testPKI(t)
	n := newTestNode(t, p)
	id, err := p.VerifyNodeCertificate(n.cert, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(n.cert)
	c, _ := x509.ParseCertificate(block.Bytes)
	if id.Serial == "" || id.Serial != fmt.Sprintf("%x", c.SerialNumber) {
		t.Fatalf("serial %q, certificate %x", id.Serial, c.SerialNumber)
	}
}

// An agent that restarted (or lost its network) reconnects while Core still
// holds its old session: the old one no longer answers a ping, so it is
// replaced instead of being refused for 30 seconds.
func TestDuplicateWhoseOldSessionIsDeadReplacesIt(t *testing.T) {
	core := startCore(t, HubOptions{DuplicatePingTimeout: 300 * time.Millisecond})
	n := newTestNode(t, core.pki)
	first, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || first.ready == nil {
		t.Fatalf("first: %v %+v", err, first)
	}
	// first never reads again, so it cannot answer a ping.
	second, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || second.ready == nil {
		t.Fatalf("second: %v %+v", err, second)
	}
	s, ok := core.hub.Session(n.fp)
	if !ok || s.ID() != second.sessionID {
		t.Fatalf("live session is not the new one")
	}
}

// Two live copies of one key (T-017): with a conflict handler, both
// sessions end and the node is reported, so data from the two is never mixed.
func TestDuplicateWhoseOldSessionAnswersIsAConflict(t *testing.T) {
	var reported atomic.Value
	core := startCore(t, HubOptions{DuplicatePingTimeout: 2 * time.Second, OnConflict: func(fp string) { reported.Store(fp) }})
	n := newTestNode(t, core.pki)
	first, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || first.ready == nil {
		t.Fatalf("first: %v %+v", err, first)
	}
	first.serve(t, func(any) []any { return nil })
	second, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if second.reject == nil || second.reject.Code != protocol.RejectIdentityConflict {
		t.Fatalf("want IDENTITY_CONFLICT, got %+v", second.reject)
	}
	if reported.Load() != n.fp {
		t.Fatalf("conflict not reported: %v", reported.Load())
	}
	waitFor(t, "first session to be closed", func() bool { _, ok := core.hub.Session(n.fp); return !ok })
}

func TestAuthorizerConflictAndRevocationCodes(t *testing.T) {
	for _, c := range []struct {
		err  error
		code string
	}{
		{ErrIdentityConflict, protocol.RejectIdentityConflict},
		{fmt.Errorf("wrapped: %w", ErrNodeRevoked), protocol.RejectNodeRevoked},
		{errors.New("node is not enrolled"), protocol.RejectIdentityInvalid},
	} {
		core := startCore(t, HubOptions{Authorizer: authorizerFunc(func(NodeIdentity) error { return c.err })})
		a, err := dialAgent(t, core, core.url, newTestNode(t, core.pki), agentOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if a.reject == nil || a.reject.Code != c.code {
			t.Fatalf("%v: want %s, got %+v", c.err, c.code, a.reject)
		}
	}
}

// SEC-003: a revoked node's live session is closed at once.
func TestDisconnectClosesTheSessionAndReportsIt(t *testing.T) {
	var closed atomic.Int32
	core := startCore(t, HubOptions{OnClosed: func(*Session) { closed.Add(1) }})
	a, n := connect(t, core, agentOpts{})
	a.serve(t, func(any) []any { return nil })
	if core.hub.Disconnect("nope", "revoked") {
		t.Fatal("disconnect of an unknown node reported true")
	}
	if !core.hub.Disconnect(n.fp, "node revoked") {
		t.Fatal("disconnect of a live node reported false")
	}
	if _, ok := core.hub.Session(n.fp); ok {
		t.Fatal("session still registered")
	}
	waitFor(t, "OnClosed", func() bool { return closed.Load() == 1 })
}

func TestRenewCertificateWaitsForTheAgentsAnswer(t *testing.T) {
	core := startCore(t, HubOptions{})
	caps := []protocol.Capability{{Name: protocol.CapJobs, Version: 1}, {Name: protocol.CapNodeCertificate, Version: 1}}
	a, n := connect(t, core, agentOpts{caps: caps})
	var mu sync.Mutex
	var got []string
	answer := "installed"
	a.serve(t, func(m any) []any {
		r, ok := m.(*protocol.CertificateRenew)
		if !ok {
			return nil
		}
		mu.Lock()
		got = append(got, r.Certificate)
		ans := answer
		mu.Unlock()
		if ans == "installed" {
			return []any{protocol.CertificateInstalled{Type: protocol.TypeCertificateInstalled, RequestID: r.RequestID}}
		}
		return []any{protocol.CertificateRefused{Type: protocol.TypeCertificateRefused, RequestID: r.RequestID, Code: protocol.CertificateInvalid, Message: "bad"}}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := core.hub.RenewCertificate(ctx, n.fp, []byte("PEM-1")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	answer = "refused"
	mu.Unlock()
	err := core.hub.RenewCertificate(ctx, n.fp, []byte("PEM-2"))
	var refused *CertificateRefusedError
	if !errors.As(err, &refused) || refused.Code != protocol.CertificateInvalid {
		t.Fatalf("refusal: %v", err)
	}
	if len(got) != 2 || got[0] != "PEM-1" {
		t.Fatalf("agent saw %v", got)
	}
	if err := core.hub.RenewCertificate(ctx, "missing", []byte("x")); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("offline: %v", err)
	}
}

func TestRenewCertificateNeedsTheCapability(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{})
	a.serve(t, func(any) []any { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := core.hub.RenewCertificate(ctx, n.fp, []byte("x")); !errors.Is(err, ErrCapabilityMissing) {
		t.Fatalf("want ErrCapabilityMissing, got %v", err)
	}
}

// SEC-002: the clock difference the agent's heartbeats show is reported.
func TestHeartbeatShowsClockSkew(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{})
	b, _ := json.Marshal(protocol.Heartbeat{Type: protocol.TypeHeartbeat, At: time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second)})
	if err := a.sendBody(b); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "skew", func() bool {
		conns, _ := core.hub.Connections()
		for _, c := range conns {
			if c.NodeID == n.fp && c.ClockSkewSeconds != nil {
				return *c.ClockSkewSeconds >= 118 && *c.ClockSkewSeconds <= 122
			}
		}
		return false
	})
}

// A gate refusal (revoked, maintenance) fails the step before anything is
// sent, so nothing can happen on the host.
func TestRunnerGateRefusesBeforeSending(t *testing.T) {
	core := startCore(t, HubOptions{})
	a, n := connect(t, core, agentOpts{})
	var sent atomic.Int32
	a.serve(t, func(m any) []any {
		if _, ok := m.(*protocol.StepExecute); ok {
			sent.Add(1)
		}
		return nil
	})
	runner := NewStepRunner(core.hub)
	runner.Gate = func(_ context.Context, j *jobs.Job) *jobs.StepError {
		return &jobs.StepError{Code: "NODE_IN_MAINTENANCE", Summary: "node is in maintenance", NothingChanged: true}
	}
	j, s := testJob(n.fp)
	_, err := runner.Execute(context.Background(), j, s, &recorder{}, Action{Name: protocol.CapAgentInfo, Version: 1})
	var se *jobs.StepError
	if !errors.As(err, &se) || se.Code != "NODE_IN_MAINTENANCE" {
		t.Fatalf("want gate error, got %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if sent.Load() != 0 {
		t.Fatal("step was sent despite the gate")
	}
}

// Replacing an unresponsive session must not use up the new handshake's
// time: the old peer is gone (its packets vanish, TCP stays open), so
// neither a pong nor a close frame ever comes back.
func TestReplacingASilentSessionLeavesTimeForReady(t *testing.T) {
	core := startCore(t, HubOptions{HandshakeTimeout: 3 * time.Second, DuplicatePingTimeout: time.Second})
	relay := newSilenceableRelay(t, core.srv.Listener.Addr().String())
	n := newTestNode(t, core.pki)
	first, err := dialAgent(t, core, "wss://"+relay.addr+protocol.ConnectPath, n, agentOpts{})
	if err != nil || first.ready == nil {
		t.Fatalf("first: %v %+v", err, first)
	}
	first.serve(t, func(any) []any { return nil })
	relay.silence()
	start := time.Now()
	second, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || second.ready == nil {
		t.Fatalf("reconnect after a silent session (%s): %v %+v", time.Since(start), err, second)
	}
}

// silenceableRelay forwards TCP to target until silence(), after which it
// keeps the connections open but drops every byte in both directions.
type silenceableRelay struct {
	addr   string
	silent atomic.Bool
}

func newSilenceableRelay(t *testing.T, target string) *silenceableRelay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	r := &silenceableRelay{addr: ln.Addr().String()}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			u, err := net.Dial("tcp", target)
			if err != nil {
				c.Close()
				continue
			}
			t.Cleanup(func() { c.Close(); u.Close() })
			pipe := func(dst, src net.Conn) {
				buf := make([]byte, 32<<10)
				for {
					k, err := src.Read(buf)
					if err != nil {
						return
					}
					if !r.silent.Load() {
						if _, err := dst.Write(buf[:k]); err != nil {
							return
						}
					}
				}
			}
			go pipe(u, c)
			go pipe(c, u)
		}
	}()
	return r
}

func (r *silenceableRelay) silence() { r.silent.Store(true) }

package nodes

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Both sides prove who they are: the agent checks the core's certificate
// and proof (in dialAgent), the core checks the agent's certificate, the
// TLS client certificate and the proof.
func TestHandshakeProvesBothSides(t *testing.T) {
	core := startCore(t, HubOptions{})
	n := newTestNode(t, core.pki)
	cert := n.tlsCert(t)
	a, err := dialAgent(t, core, core.url, n, agentOpts{tlsCert: &cert})
	if err != nil {
		t.Fatal(err)
	}
	if a.ready == nil {
		t.Fatalf("refused: %+v", a.reject)
	}
	if a.welcome.ProtocolVersion != 1 || a.welcome.HeartbeatSeconds < 1 {
		t.Fatalf("welcome %+v", a.welcome)
	}
	conns, _ := core.hub.Connections()
	if len(conns) != 1 || conns[0].NodeID != n.fp || conns[0].Transport != TransportMTLS || conns[0].ProtocolVersion != 1 {
		t.Fatalf("connections %+v", conns)
	}
	s, ok := core.hub.Session(n.fp)
	if !ok || !s.Supports(protocol.CapAgentInfo, 1) || s.Supports(protocol.CapAgentInfo, 2) {
		t.Fatalf("session capabilities wrong: %+v", conns[0].Capabilities)
	}
}

func TestTLSClientCertificateMustMatchHelloIdentity(t *testing.T) {
	core := startCore(t, HubOptions{})
	n, other := newTestNode(t, core.pki), newTestNode(t, core.pki)
	cert := other.tlsCert(t)
	a, err := dialAgent(t, core, core.url, n, agentOpts{tlsCert: &cert})
	if err != nil {
		t.Fatal(err)
	}
	if a.reject == nil || a.reject.Code != protocol.RejectIdentityInvalid {
		t.Fatalf("want IDENTITY_INVALID, got reject=%+v ready=%+v", a.reject, a.ready)
	}
}

func TestCertificateFromAnotherCoreIsRefused(t *testing.T) {
	core := startCore(t, HubOptions{})
	foreign := testPKI(t)
	n := newTestNode(t, foreign)
	a, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if a.reject == nil || a.reject.Code != protocol.RejectIdentityInvalid {
		t.Fatalf("want IDENTITY_INVALID, got %+v", a.reject)
	}
}

func TestProofWithAnotherKeyIsRefused(t *testing.T) {
	core := startCore(t, HubOptions{})
	n := newTestNode(t, core.pki)
	_, wrong, _ := ed25519.GenerateKey(rand.Reader)
	a, err := dialAgent(t, core, core.url, n, agentOpts{signer: wrong})
	if err != nil {
		t.Fatal(err)
	}
	if a.reject == nil || a.reject.Code != protocol.RejectProofInvalid {
		t.Fatalf("want PROOF_INVALID, got %+v", a.reject)
	}
	if conns, _ := core.hub.Connections(); len(conns) != 0 {
		t.Fatalf("refused agent is registered: %+v", conns)
	}
}

// A proof from an earlier session cannot be replayed: the core draws a new
// nonce and session id each time.
func TestOldProofCannotBeReplayed(t *testing.T) {
	core := startCore(t, HubOptions{})
	n := newTestNode(t, core.pki)
	a, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || a.ready == nil {
		t.Fatalf("first session: %v %+v", err, a)
	}
	a.conn.CloseNow()
	waitFor(t, "session to end", func() bool { _, ok := core.hub.Session(n.fp); return !ok })
	b, err := dialAgent(t, core, core.url, n, agentOpts{authProof: a.proof})
	if err != nil {
		t.Fatal(err)
	}
	if b.reject == nil || b.reject.Code != protocol.RejectProofInvalid {
		t.Fatalf("replayed proof accepted: %+v", b)
	}
}

// T-021: an agent speaking only newer protocol versions is refused with the
// core's range, and the refusal is visible with both ranges.
func TestIncompatibleProtocolIsRefusedWithSupportedRange(t *testing.T) {
	core := startCore(t, HubOptions{})
	n := newTestNode(t, core.pki)
	a, err := dialAgent(t, core, core.url, n, agentOpts{protocol: &protocol.Range{Min: 2, Max: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if a.reject == nil || a.reject.Code != protocol.RejectProtocolIncompatible {
		t.Fatalf("want PROTOCOL_INCOMPATIBLE, got %+v", a.reject)
	}
	if a.reject.Supported == nil || *a.reject.Supported != protocol.SupportedRange() {
		t.Fatalf("reject does not carry the core range: %+v", a.reject)
	}
	mustContain(t, a.reject.Message, "2..3")
	conns, rej := core.hub.Connections()
	if len(conns) != 0 || len(rej) != 1 {
		t.Fatalf("conns=%+v rejections=%+v", conns, rej)
	}
	r := rej[0]
	if r.Code != protocol.RejectProtocolIncompatible || r.AgentRange == nil || *r.AgentRange != (protocol.Range{Min: 2, Max: 3}) ||
		r.CoreRange != protocol.SupportedRange() || r.NodeID != n.fp || r.IdentityProven || r.AgentVersion != "fake-1" {
		t.Fatalf("rejection record %+v", r)
	}
}

func TestRevokedNodeIsRefused(t *testing.T) {
	core := startCore(t, HubOptions{Authorizer: authorizerFunc(func(id NodeIdentity) error { return ErrNodeRevoked })})
	n := newTestNode(t, core.pki)
	a, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if a.reject == nil || a.reject.Code != protocol.RejectNodeRevoked {
		t.Fatalf("want NODE_REVOKED, got %+v", a.reject)
	}
}

type authorizerFunc func(NodeIdentity) error

func (f authorizerFunc) AuthorizeNode(_ context.Context, id NodeIdentity) error { return f(id) }

// A second live copy of the same node key (a cloned VM) is refused while
// the first session lives; once it ends, the node may connect again.
func TestSecondSessionForSameNodeIsRefused(t *testing.T) {
	core := startCore(t, HubOptions{})
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
	if second.reject == nil || second.reject.Code != protocol.RejectDuplicateSession {
		t.Fatalf("want DUPLICATE_SESSION, got %+v", second.reject)
	}
	first.conn.CloseNow()
	waitFor(t, "first session to end", func() bool { _, ok := core.hub.Session(n.fp); return !ok })
	third, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || third.ready == nil {
		t.Fatalf("reconnect after the first session ended: %v %+v", err, third)
	}
}

func TestMalformedHelloIsRefused(t *testing.T) {
	core := startCore(t, HubOptions{})
	n := newTestNode(t, core.pki)
	a, err := dialAgent(t, core, core.url, n, agentOpts{helloCert: []byte("not a certificate")})
	if err != nil {
		t.Fatal(err)
	}
	if a.reject == nil || a.reject.Code != protocol.RejectIdentityInvalid {
		t.Fatalf("want IDENTITY_INVALID, got %+v", a.reject)
	}
}

// NOD-011: after the handshake, a message the core cannot read exactly is
// answered with protocol_error and nothing acts on it; the session lives on.
func TestUnreadableSessionMessageIsNotActedOn(t *testing.T) {
	core := startCore(t, HubOptions{})
	n := newTestNode(t, core.pki)
	a, err := dialAgent(t, core, core.url, n, agentOpts{})
	if err != nil || a.ready == nil {
		t.Fatalf("%v %+v", err, a)
	}
	for _, raw := range []string{
		`{"type":"step_teleport","jobId":"x"}`,
		`{"type":"event","seq":1,"kind":"completed","jobId":"j","step":0,"requestId":"r","at":"2026-11-08T00:00:00Z","runAsRoot":true}`,
		`{"type":"step_execute","requestId":"r","jobId":"j","step":0,"stepName":"x","action":"agent.info","actionVersion":1,"params":{},"targetNode":"n","controlGeneration":0,"expiresAt":"2026-11-08T00:00:00Z","retryable":true,"irreversible":false}`,
	} {
		a.writeRaw(t, []byte(raw))
		m, ok := a.next(t, 2*time.Second).(*protocol.ProtocolError)
		if !ok {
			t.Fatalf("%s: want protocol_error", raw)
		}
		want := protocol.ErrorMalformed
		if strings.Contains(raw, "teleport") || strings.Contains(raw, "step_execute") {
			want = protocol.ErrorUnknownMessage
		}
		if m.Code != want {
			t.Fatalf("%s: code %s, want %s", raw, m.Code, want)
		}
	}
	if _, ok := core.hub.Session(n.fp); !ok {
		t.Fatal("session closed")
	}
}

// T-018: the agent verifies the core by its certificate and the fixed core
// name, not by address, so reaching the same core through another address
// works without new trust material.
func TestCoreReachableUnderAnotherAddressKeepsIdentity(t *testing.T) {
	core := startCore(t, HubOptions{})
	n := newTestNode(t, core.pki)
	_, port, _ := net.SplitHostPort(core.srv.Listener.Addr().String())
	for _, host := range []string{"127.0.0.1", "localhost"} {
		u := "wss://" + net.JoinHostPort(host, port) + protocol.ConnectPath
		a, err := dialAgent(t, core, u, n, agentOpts{})
		if err != nil || a.ready == nil {
			t.Fatalf("%s: %v %+v", host, err, a)
		}
		a.conn.CloseNow()
		waitFor(t, "session to end", func() bool { _, ok := core.hub.Session(n.fp); return !ok })
	}
}

// The agent listener serves nothing but the connect path.
func TestOnlyConnectPathIsServed(t *testing.T) {
	core := startCore(t, HubOptions{})
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: coreCAPool(t, core.pki), ServerName: protocol.CoreServerName}}}
	resp, err := c.Get("https://" + core.srv.Listener.Addr().String() + "/management/v1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

// tlsTerminatingProxy terminates TLS with its own certificate and forwards
// to the core over a new TLS connection without a client certificate,
// adding headers that claim a node identity.
func tlsTerminatingProxy(t *testing.T, core *testCore, claim string) *httptest.Server {
	t.Helper()
	target, _ := url.Parse("https://" + core.srv.Listener.Addr().String())
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: coreCAPool(t, core.pki), ServerName: protocol.CoreServerName}}
	director := rp.Director
	rp.Director = func(r *http.Request) {
		director(r)
		r.Header.Set("X-Hazelminx-Node", claim)
		r.Header.Set("X-Forwarded-Client-Cert", "Hash="+claim)
		r.Header.Set("X-Client-Verify", "SUCCESS")
	}
	p := httptest.NewTLSServer(rp)
	t.Cleanup(p.Close)
	return p
}

// T-024: through a TLS-terminating proxy the core ignores forged identity
// headers; only the certificate plus proof in the handshake identify a node.
func TestThroughTLSTerminatingProxyIdentityRestsOnProofNotHeaders(t *testing.T) {
	core := startCore(t, HubOptions{})
	victim := newTestNode(t, core.pki)
	proxy := tlsTerminatingProxy(t, core, victim.fp)
	proxyRoots := proxy.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	pu := "wss://" + proxy.Listener.Addr().String() + protocol.ConnectPath

	// An attacker with headers claiming the victim but a certificate from
	// another CA is refused.
	attacker := newTestNode(t, testPKI(t))
	a, err := dialAgent(t, core, pu, attacker, agentOpts{rootCAs: proxyRoots, serverName: "example.com",
		header: http.Header{"X-Hazelminx-Node": {victim.fp}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.reject == nil || a.reject.Code != protocol.RejectIdentityInvalid {
		t.Fatalf("forged headers accepted: %+v", a)
	}
	// An attacker holding the victim's certificate but not its key fails
	// the proof.
	_, notVictimKey, _ := ed25519.GenerateKey(rand.Reader)
	b, err := dialAgent(t, core, pu, victim, agentOpts{rootCAs: proxyRoots, serverName: "example.com", signer: notVictimKey})
	if err != nil {
		t.Fatal(err)
	}
	if b.reject == nil || b.reject.Code != protocol.RejectProofInvalid {
		t.Fatalf("certificate without key accepted: %+v", b)
	}
	// The real node connects through the proxy; the session is marked as
	// proxied, not mTLS.
	c, err := dialAgent(t, core, pu, victim, agentOpts{rootCAs: proxyRoots, serverName: "example.com"})
	if err != nil || c.ready == nil {
		t.Fatalf("real node through proxy: %v %+v", err, c)
	}
	conns, _ := core.hub.Connections()
	if len(conns) != 1 || conns[0].Transport != TransportProxied || conns[0].NodeID != victim.fp {
		t.Fatalf("connections %+v", conns)
	}
}

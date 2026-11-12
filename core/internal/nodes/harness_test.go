package nodes

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

// memSecrets is an in-memory SecretStore for tests.
type memSecrets struct {
	mu   sync.Mutex
	list []secrets.Secret
	vals map[string][]byte
}

func newMemSecrets() *memSecrets { return &memSecrets{vals: map[string][]byte{}} }

func (m *memSecrets) List(context.Context) ([]secrets.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]secrets.Secret(nil), m.list...), nil
}

func (m *memSecrets) Create(_ context.Context, in secrets.CreateInput) (secrets.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.list {
		if s.Name == in.Name {
			return secrets.Secret{}, secrets.ErrNameTaken
		}
	}
	s := secrets.Secret{ID: fmt.Sprintf("sec_%d", len(m.list)+1), Kind: in.Kind, Name: in.Name, Version: 1, Scope: in.Scope}
	m.list = append(m.list, s)
	m.vals[s.ID] = append([]byte(nil), in.Value...)
	return s, nil
}

func (m *memSecrets) Reveal(_ context.Context, req secrets.RevealRequest) ([]byte, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.list {
		if s.ID == req.ID {
			ok := false
			for _, c := range s.Scope {
				ok = ok || c == req.Consumer
			}
			if !ok {
				return nil, 0, secrets.ErrOutOfScope
			}
			return append([]byte(nil), m.vals[s.ID]...), 1, nil
		}
	}
	return nil, 0, secrets.ErrNotFound
}

func testPKI(t *testing.T) *PKI {
	t.Helper()
	p, err := LoadOrCreatePKI(context.Background(), newMemSecrets(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// testNode is a node key with a certificate from p.
type testNode struct {
	key  ed25519.PrivateKey
	cert []byte // PEM
	fp   string
}

func newTestNode(t *testing.T, p *PKI) testNode {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := p.IssueNodeCertificate(pub, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return testNode{key: key, cert: cert, fp: protocol.Fingerprint(pub)}
}

func (n testNode) tlsCert(t *testing.T) tls.Certificate {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(n.key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := tls.X509KeyPair(n.cert, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// testCore is a hub served over TLS by httptest.
type testCore struct {
	pki *PKI
	hub *Hub
	srv *httptest.Server
	url string // wss://127.0.0.1:port/agent/v1/connect
}

func startCore(t *testing.T, opts HubOptions) *testCore {
	t.Helper()
	if opts.PKI == nil {
		opts.PKI = testPKI(t)
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Heartbeat == 0 {
		opts.Heartbeat = time.Second
	}
	if opts.CoreVersion == "" {
		opts.CoreVersion = "test"
	}
	hub := NewHub(opts)
	srv := httptest.NewUnstartedServer(hub)
	srv.TLS = opts.PKI.ServerTLSConfig()
	srv.StartTLS()
	t.Cleanup(func() { hub.Close(); srv.Close() })
	return &testCore{pki: opts.PKI, hub: hub, srv: srv, url: "wss://" + srv.Listener.Addr().String() + protocol.ConnectPath}
}

// agentOpts shapes a fake agent's handshake.
type agentOpts struct {
	// tlsCert is presented as the TLS client certificate (nil: none).
	tlsCert *tls.Certificate
	// helloCert overrides the certificate in hello.
	helloCert []byte
	// signer overrides the key that signs auth.
	signer ed25519.PrivateKey
	// authProof, when set, is sent as the auth proof verbatim (replay).
	authProof []byte
	// beforeAuth runs after welcome is verified and before auth is sent.
	beforeAuth func()
	// agentVersion overrides the hello agentVersion; badNonce sends an
	// invalid nonce.
	agentVersion string
	badNonce     bool
	// signedKex, when set, is the agent kex the auth proof claims while
	// hello carried the real one (a proxy swapping keys).
	signedKex string
	// protocol overrides the hello range.
	protocol *protocol.Range
	caps     []protocol.Capability
	// rootCAs and serverName verify the TLS server; defaults pin the core CA
	// and the fixed core name.
	rootCAs    *x509.CertPool
	serverName string
	header     http.Header
}

// fakeAgent is the agent side of a session in tests.
type fakeAgent struct {
	conn      *websocket.Conn
	welcome   *protocol.Welcome
	ready     *protocol.Ready
	reject    *protocol.Reject
	node      testNode
	sessionID string
	// proof is the auth proof this agent sent.
	proof []byte
	// keys and counters of the authenticated session (after ready).
	keys  protocol.SessionKeys
	wmu   sync.Mutex
	sendN uint64
	recvN uint64
}

func coreCAPool(t *testing.T, p *PKI) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(p.CACertificatePEM()) {
		t.Fatal("CA PEM did not parse")
	}
	return pool
}

// dialAgent runs the agent side of the handshake. It returns the agent
// whether the core accepted (ready set) or refused (reject set).
func dialAgent(t *testing.T, core *testCore, url string, n testNode, o agentOpts) (*fakeAgent, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tc := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: o.rootCAs, ServerName: o.serverName}
	if tc.RootCAs == nil {
		tc.RootCAs = coreCAPool(t, core.pki)
	}
	if tc.ServerName == "" {
		tc.ServerName = protocol.CoreServerName
	}
	if o.tlsCert != nil {
		tc.Certificates = []tls.Certificate{*o.tlsCert}
	}
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tc}},
		HTTPHeader: o.header,
	})
	if err != nil {
		return nil, err
	}
	a := &fakeAgent{conn: conn, node: n}
	t.Cleanup(func() { conn.CloseNow() })
	nonce, _ := protocol.NewNonce()
	r := protocol.SupportedRange()
	if o.protocol != nil {
		r = *o.protocol
	}
	cert := n.cert
	if o.helloCert != nil {
		cert = o.helloCert
	}
	caps := o.caps
	if caps == nil {
		caps = []protocol.Capability{{Name: protocol.CapJobs, Version: JobsProtocolVersion}, {Name: protocol.CapAgentInfo, Version: 1}}
	}
	kexKey, kex, err := protocol.NewKex()
	if err != nil {
		t.Fatal(err)
	}
	version := "fake-1"
	if o.agentVersion != "" {
		version = o.agentVersion
	}
	helloNonce := nonce
	if o.badNonce {
		helloNonce = "short"
	}
	helloRaw, err := json.Marshal(protocol.Hello{Type: protocol.TypeHello, Protocol: r, AgentVersion: version, NodeCertificate: string(cert), Nonce: helloNonce, Kex: kex, Capabilities: caps})
	if err != nil {
		t.Fatal(err)
	}
	a.writeFrame(t, helloRaw)
	msg, err := a.readHandshake(ctx)
	if err != nil {
		return a, err
	}
	switch m := msg.(type) {
	case *protocol.Reject:
		a.reject = m
		return a, nil
	case *protocol.Welcome:
		a.welcome = m
	default:
		return a, fmt.Errorf("unexpected %T", msg)
	}
	// Verify the core like the real agent does: certificate chain, usage,
	// name, and proof over the core transcript.
	tin := protocol.TranscriptInput{AgentNonce: nonce, CoreNonce: a.welcome.Nonce, AgentKex: kex, CoreKex: a.welcome.Kex,
		ProtocolVersion: a.welcome.ProtocolVersion, NodeFingerprint: n.fp, SessionID: a.welcome.SessionID,
		HelloDigest:   protocol.HelloDigest(helloRaw),
		WelcomeDigest: protocol.WelcomeDigest(a.welcome.CoreVersion, a.welcome.HeartbeatSeconds, a.welcome.Capabilities)}
	if err := verifyCoreForTest(core.pki, a.welcome, tin); err != nil {
		return a, err
	}
	if a.keys, err = protocol.DeriveKeys(protocol.RoleAgent, kexKey, a.welcome.Kex, a.welcome.SessionID); err != nil {
		return a, err
	}
	signedKex := kex
	if o.signedKex != "" {
		signedKex = o.signedKex
	}
	signer := n.key
	if o.signer != nil {
		signer = o.signer
	}
	signed := tin
	signed.AgentKex = signedKex
	proof := ed25519.Sign(signer, protocol.Transcript(protocol.RoleAgent, signed))
	if o.authProof != nil {
		proof = o.authProof
	}
	a.proof = proof
	if o.beforeAuth != nil {
		o.beforeAuth()
	}
	a.writeHandshake(t, protocol.Auth{Type: protocol.TypeAuth, Proof: base64.StdEncoding.EncodeToString(proof)})
	msg, err = a.readHandshake(ctx)
	if err != nil {
		return a, err
	}
	switch m := msg.(type) {
	case *protocol.Reject:
		a.reject = m
	case *protocol.Ready:
		a.ready = m
		a.sessionID = m.SessionID
	default:
		return a, fmt.Errorf("unexpected %T", msg)
	}
	return a, nil
}

func verifyCoreForTest(p *PKI, w *protocol.Welcome, tin protocol.TranscriptInput) error {
	block, _ := pem.Decode([]byte(w.CoreCertificate))
	if block == nil {
		return errors.New("core certificate PEM")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(p.CACertificatePEM())
	if _, err := c.Verify(x509.VerifyOptions{Roots: pool, DNSName: protocol.CoreServerName, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(w.Proof)
	if err != nil {
		return err
	}
	if !ed25519.Verify(c.PublicKey.(ed25519.PublicKey), protocol.Transcript(protocol.RoleCore, tin), sig) {
		return errors.New("core proof does not verify")
	}
	return nil
}

func (a *fakeAgent) writeHandshake(t *testing.T, msg any) {
	t.Helper()
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	a.writeFrame(t, b)
}

// write sends a session message in the next authenticated frame.
func (a *fakeAgent) write(t *testing.T, msg any) {
	t.Helper()
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	a.writeRaw(t, b)
}

// writeRaw seals body (any bytes) in the next authenticated frame.
func (a *fakeAgent) writeRaw(t *testing.T, body []byte) {
	t.Helper()
	if err := a.sendBody(body); err != nil {
		t.Fatalf("agent write: %v", err)
	}
}

func (a *fakeAgent) sendBody(body []byte) error {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	a.sendN++
	frame, err := protocol.SealFrame(a.keys.Send, a.sendN, body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return a.conn.Write(ctx, websocket.MessageText, frame)
}

// writeFrame sends bytes as they are (handshake messages, forged frames).
func (a *fakeAgent) writeFrame(t *testing.T, b []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("agent write: %v", err)
	}
}

// readBody reads and authenticates the next frame from the core.
func (a *fakeAgent) readBody(ctx context.Context) ([]byte, error) {
	_, b, err := a.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	body, err := protocol.OpenFrame(a.keys.Recv, a.recvN+1, b)
	if err != nil {
		return nil, err
	}
	a.recvN++
	return body, nil
}

func (a *fakeAgent) readHandshake(ctx context.Context) (any, error) {
	_, b, err := a.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	return protocol.DecodeHandshake(b)
}

// next reads session messages until one that is not a heartbeat or ack.
func (a *fakeAgent) next(t *testing.T, within time.Duration) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		b, err := a.readBody(ctx)
		if err != nil {
			t.Fatalf("agent read: %v", err)
		}
		m, err := protocol.Decode(b)
		if err != nil {
			t.Fatalf("core sent an undecodable message %s: %v", b, err)
		}
		switch m.(type) {
		case *protocol.Heartbeat, *protocol.Ack:
			continue
		}
		return m
	}
}

// serve answers session messages with fn until the connection ends. It also
// sends heartbeats so the core keeps the session.
func (a *fakeAgent) serve(t *testing.T, fn func(any) []any) {
	go func() {
		for {
			b, err := a.readBody(context.Background())
			if err != nil {
				return
			}
			m, err := protocol.Decode(b)
			if err != nil {
				return
			}
			if _, ok := m.(*protocol.Heartbeat); ok {
				b, _ := json.Marshal(protocol.Heartbeat{Type: protocol.TypeHeartbeat, At: time.Now().UTC().Truncate(time.Second)})
				_ = a.sendBody(b)
				continue
			}
			for _, out := range fn(m) {
				b, _ := json.Marshal(out)
				if a.sendBody(b) != nil {
					return
				}
			}
		}
	}()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("%q does not contain %q", s, sub)
	}
}

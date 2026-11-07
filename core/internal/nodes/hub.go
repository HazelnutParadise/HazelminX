package nodes

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// ErrNodeRevoked is what an Authorizer returns for a node that must not
// connect any more (ticket 019 keeps the revocation list).
var ErrNodeRevoked = errors.New("nodes: node is revoked")

// Authorizer decides whether a node with a valid certificate may connect.
type Authorizer interface {
	AuthorizeNode(ctx context.Context, id NodeIdentity) error
}

// AllowCertified accepts every node whose certificate this Core's CA
// issued. Enrollment (ticket 019) replaces it with the node registry.
type AllowCertified struct{}

func (AllowCertified) AuthorizeNode(context.Context, NodeIdentity) error { return nil }

// Transport says how the agent's identity reached the hub.
const (
	// TransportMTLS: direct TLS with a client certificate that matches the
	// handshake identity.
	TransportMTLS = "mtls"
	// TransportProxied: no client certificate (a TLS-terminating proxy sits
	// in between); identity rests on the handshake proof alone.
	TransportProxied = "proxied"
)

// HubOptions configures a Hub.
type HubOptions struct {
	PKI         *PKI
	CoreVersion string
	Authorizer  Authorizer
	// Heartbeat is the interval both sides send heartbeats at; a session
	// with no message for three intervals is closed. Default 10 s.
	Heartbeat time.Duration
	// HandshakeTimeout bounds the four handshake messages. Default 10 s.
	HandshakeTimeout time.Duration
	// OnReady runs in its own goroutine after a session is registered.
	// inFlight lists the jobs that had a step outstanding on the node's
	// previous session when it ended.
	OnReady func(s *Session, inFlight []string)
	Now     func() time.Time
	Logger  *slog.Logger
	// Range is the protocol range this hub speaks; zero means
	// protocol.SupportedRange(). Tests use it to stand in for an older or
	// newer Core.
	Range protocol.Range
}

// maxMessageBytes bounds one inbound message.
const maxMessageBytes = 1 << 20

// maxRejections is how many recent refusals Connections reports.
const maxRejections = 50

// Hub accepts agent connections on the agent listener.
type Hub struct {
	opts HubOptions

	mu         sync.Mutex
	sessions   map[string]*Session // by node fingerprint
	rejections []Rejection
	// orphaned lists jobs whose step was outstanding when a node's session
	// ended; handed to OnReady on the next session.
	orphaned map[string][]string
	closed   bool
}

// NewHub builds a hub. PKI is required.
func NewHub(opts HubOptions) *Hub {
	if opts.PKI == nil {
		panic("nodes: NewHub needs a PKI")
	}
	if opts.Authorizer == nil {
		opts.Authorizer = AllowCertified{}
	}
	// The interval travels as whole seconds in welcome, so both sides use
	// the same rounded value.
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 10 * time.Second
	}
	opts.Heartbeat = max(time.Second, opts.Heartbeat.Round(time.Second))
	if opts.HandshakeTimeout <= 0 {
		opts.HandshakeTimeout = 10 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Range == (protocol.Range{}) {
		opts.Range = protocol.SupportedRange()
	}
	return &Hub{opts: opts, sessions: map[string]*Session{}, orphaned: map[string][]string{}}
}

// Rejection is a refused connection attempt, kept so operators can see
// why a node is not connected (for example its supported protocol range).
type Rejection struct {
	At           time.Time       `json:"at"`
	RemoteAddr   string          `json:"remoteAddr"`
	Code         string          `json:"code"`
	Message      string          `json:"message"`
	NodeID       string          `json:"nodeId,omitempty"`
	AgentVersion string          `json:"agentVersion,omitempty"`
	AgentRange   *protocol.Range `json:"agentProtocol,omitempty"`
	CoreRange    protocol.Range  `json:"coreProtocol"`
	// IdentityProven is false when the refusal happened before the agent
	// proved possession of the node key (the node ID is then only claimed by
	// a certificate this CA issued).
	IdentityProven bool `json:"identityProven"`
}

// ConnectionInfo describes a live session.
type ConnectionInfo struct {
	NodeID          string                `json:"nodeId"`
	SessionID       string                `json:"sessionId"`
	AgentVersion    string                `json:"agentVersion"`
	ProtocolVersion int                   `json:"protocolVersion"`
	AgentRange      protocol.Range        `json:"agentProtocol"`
	Capabilities    []protocol.Capability `json:"capabilities"`
	Transport       string                `json:"transport"`
	RemoteAddr      string                `json:"remoteAddr"`
	ConnectedAt     time.Time             `json:"connectedAt"`
	LastSeenAt      time.Time             `json:"lastSeenAt"`
}

// Connections lists live sessions (by node ID) and recent refusals (newest
// first).
func (h *Hub) Connections() ([]ConnectionInfo, []Rejection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]ConnectionInfo, 0, len(h.sessions))
	for _, s := range h.sessions {
		out = append(out, s.info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	rej := make([]Rejection, len(h.rejections))
	for i := range h.rejections {
		rej[i] = h.rejections[len(h.rejections)-1-i]
	}
	return out, rej
}

// Session returns the live session of a node.
func (h *Hub) Session(nodeID string) (*Session, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[nodeID]
	return s, ok
}

// Close ends every session and refuses new ones.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	list := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		list = append(list, s)
	}
	h.mu.Unlock()
	for _, s := range list {
		s.close(websocket.StatusGoingAway, "core shutting down")
	}
}

func (h *Hub) reject(rec Rejection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rejections = append(h.rejections, rec)
	if len(h.rejections) > maxRejections {
		h.rejections = h.rejections[len(h.rejections)-maxRejections:]
	}
}

// ServeHTTP upgrades protocol.ConnectPath to a WebSocket and runs the
// handshake. Nothing else is served on the agent listener. No HTTP header
// is consulted for identity: X-Forwarded-*, X-Client-Cert and the like are
// ignored, so a proxy (or anyone) cannot vouch for a node (T-024).
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != protocol.ConnectPath {
		http.NotFound(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return // Accept already answered
	}
	conn.SetReadLimit(maxMessageBytes)
	h.handshake(r, conn)
}

type handshakeFailure struct {
	code, message  string
	supported      bool
	nodeID         string
	agentVersion   string
	agentRange     *protocol.Range
	identityProven bool
}

func (h *Hub) handshake(r *http.Request, conn *websocket.Conn) {
	ctx, cancel := context.WithTimeout(r.Context(), h.opts.HandshakeTimeout)
	defer cancel()
	fail := func(f handshakeFailure) {
		msg := protocol.Reject{Type: protocol.TypeReject, Code: f.code, Message: f.message}
		core := h.opts.Range
		if f.supported {
			msg.Supported = &core
		}
		// Record first: once the agent reads the reject, the refusal must
		// already be visible to operators.
		h.reject(Rejection{At: h.opts.Now().UTC(), RemoteAddr: r.RemoteAddr, Code: f.code, Message: f.message, NodeID: f.nodeID,
			AgentVersion: f.agentVersion, AgentRange: f.agentRange, CoreRange: core, IdentityProven: f.identityProven})
		if b, err := protocol.Encode(msg); err == nil {
			_ = conn.Write(ctx, websocket.MessageText, b)
		}
		_ = conn.Close(websocket.StatusPolicyViolation, f.code)
		h.opts.Logger.Warn("agent connection refused", "code", f.code, "reason", f.message, "node", f.nodeID, "remote", r.RemoteAddr)
	}

	hello, err := readHandshake[*protocol.Hello](ctx, conn)
	if err != nil {
		fail(handshakeFailure{code: protocol.RejectHandshakeMalformed, message: "expected hello: " + err.Error(), supported: true})
		return
	}
	agentRange := hello.Protocol
	base := handshakeFailure{agentVersion: hello.AgentVersion, agentRange: &agentRange}
	with := func(code, message string) handshakeFailure {
		f := base
		f.code, f.message = code, message
		return f
	}
	if !protocol.ValidNonce(hello.Nonce) {
		fail(with(protocol.RejectHandshakeMalformed, "hello nonce must be 32 random bytes, base64url without padding"))
		return
	}
	id, err := h.opts.PKI.VerifyNodeCertificate([]byte(hello.NodeCertificate), h.opts.Now())
	if err != nil {
		fail(with(protocol.RejectIdentityInvalid, err.Error()))
		return
	}
	base.nodeID = id.Fingerprint
	transport := TransportProxied
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		tlsID, err := certFingerprint(r.TLS.PeerCertificates[0])
		if err != nil || tlsID != id.Fingerprint {
			fail(with(protocol.RejectIdentityInvalid, "TLS client certificate and hello certificate are not the same node"))
			return
		}
		transport = TransportMTLS
	}
	version, err := protocol.Negotiate(h.opts.Range, hello.Protocol)
	if err != nil {
		core := h.opts.Range
		f := with(protocol.RejectProtocolIncompatible, fmt.Sprintf("agent %s speaks protocol %d..%d; this core speaks %d..%d; upgrade the older side",
			hello.AgentVersion, hello.Protocol.Min, hello.Protocol.Max, core.Min, core.Max))
		f.supported = true
		fail(f)
		return
	}
	if err := h.opts.Authorizer.AuthorizeNode(ctx, id); err != nil {
		code := protocol.RejectIdentityInvalid
		if errors.Is(err, ErrNodeRevoked) {
			code = protocol.RejectNodeRevoked
		}
		fail(with(code, err.Error()))
		return
	}

	coreNonce, err := protocol.NewNonce()
	if err != nil {
		fail(with(protocol.RejectHandshakeMalformed, "core could not draw a nonce"))
		return
	}
	sessionID, err := newSessionID()
	if err != nil {
		fail(with(protocol.RejectHandshakeMalformed, "core could not draw a session id"))
		return
	}
	coreCaps := []protocol.Capability{{Name: protocol.CapJobs, Version: 1}}
	welcome := protocol.Welcome{
		Type: protocol.TypeWelcome, ProtocolVersion: version, CoreVersion: h.opts.CoreVersion,
		CoreCertificate: string(h.opts.PKI.CoreCertificatePEM()), Nonce: coreNonce, SessionID: sessionID,
		Proof:            base64.StdEncoding.EncodeToString(h.opts.PKI.SignAsCore(protocol.Transcript(protocol.RoleCore, hello.Nonce, coreNonce, version, id.Fingerprint, sessionID))),
		HeartbeatSeconds: int(h.opts.Heartbeat / time.Second), Capabilities: coreCaps,
	}
	if err := writeMessage(ctx, conn, welcome); err != nil {
		return
	}
	auth, err := readHandshake[*protocol.Auth](ctx, conn)
	if err != nil {
		fail(with(protocol.RejectHandshakeMalformed, "expected auth: "+err.Error()))
		return
	}
	proof, err := base64.StdEncoding.DecodeString(auth.Proof)
	if err != nil || !ed25519.Verify(id.PublicKey, protocol.Transcript(protocol.RoleAgent, hello.Nonce, coreNonce, version, id.Fingerprint, sessionID), proof) {
		fail(with(protocol.RejectProofInvalid, "node proof does not verify against the presented certificate"))
		return
	}
	base.identityProven = true

	s := &Session{
		hub: h, conn: conn, id: id, sessionID: sessionID, protocolVersion: version, agentVersion: hello.AgentVersion,
		agentRange: hello.Protocol, caps: hello.Capabilities, transport: transport, remote: r.RemoteAddr,
		connectedAt: h.opts.Now().UTC(), out: make(chan []byte, 256), done: make(chan struct{}), pending: map[string]*pending{},
	}
	s.touch()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusGoingAway, "core shutting down")
		return
	}
	if old, ok := h.sessions[id.Fingerprint]; ok {
		h.mu.Unlock()
		fail(with(protocol.RejectDuplicateSession, fmt.Sprintf("node already has session %s from %s; a second live copy of this node key (for example a cloned VM) is refused", old.sessionID, old.remote)))
		return
	}
	h.sessions[id.Fingerprint] = s
	inFlight := h.orphaned[id.Fingerprint]
	delete(h.orphaned, id.Fingerprint)
	h.mu.Unlock()

	if err := writeMessage(ctx, conn, protocol.Ready{Type: protocol.TypeReady, SessionID: sessionID}); err != nil {
		h.unregister(s)
		return
	}
	h.opts.Logger.Info("agent connected", "node", id.Fingerprint, "session", sessionID, "protocol", version, "agentVersion", hello.AgentVersion, "transport", transport, "remote", r.RemoteAddr)
	if h.opts.OnReady != nil {
		go h.opts.OnReady(s, inFlight)
	}
	// The request context ends when ServeHTTP returns, so the session runs
	// here on the handler goroutine until it closes.
	s.run(context.WithoutCancel(r.Context()))
}

func (h *Hub) unregister(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.sessions[s.id.Fingerprint]; ok && cur == s {
		delete(h.sessions, s.id.Fingerprint)
	}
	if jobs := s.outstandingJobs(); len(jobs) > 0 {
		h.orphaned[s.id.Fingerprint] = append(h.orphaned[s.id.Fingerprint], jobs...)
	}
}

func certFingerprint(c *x509.Certificate) (string, error) {
	pub, ok := c.PublicKey.(ed25519.PublicKey)
	if !ok {
		return "", errors.New("not an Ed25519 key")
	}
	return protocol.Fingerprint(pub), nil
}

func newSessionID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ses_" + hex.EncodeToString(b), nil
}

func readHandshake[T any](ctx context.Context, conn *websocket.Conn) (T, error) {
	var zero T
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return zero, err
	}
	if typ != websocket.MessageText {
		return zero, errors.New("binary frames are not part of the protocol")
	}
	msg, err := protocol.DecodeHandshake(data)
	if err != nil {
		return zero, err
	}
	v, ok := msg.(T)
	if !ok {
		return zero, fmt.Errorf("unexpected %T", msg)
	}
	return v, nil
}

func writeMessage(ctx context.Context, conn *websocket.Conn, msg any) error {
	b, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}

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
	"net"
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

// Limits on inbound data. Before the agent has proven who it is, messages
// are small and few handshakes may run at once, so an anonymous client
// cannot make the hub hold much memory.
const (
	maxMessageBytes   = 1 << 20  // one message in an established session
	maxHandshakeBytes = 64 << 10 // one handshake message
	maxHandshakes     = 256      // handshakes in progress at once, all sources
	maxHandshakesPer  = 32       // handshakes in progress from one source (IPv4 address or IPv6 /64)
	helloTimeout      = 3 * time.Second
	maxRecordedText   = 256 // characters kept from agent-supplied strings in refusals
)

// maxRejections is how many recent refusals Connections reports.
const maxRejections = 50

// Hub accepts agent connections on the agent listener.
type Hub struct {
	opts HubOptions

	slotMu sync.Mutex
	slots  []*handshakeSlot // handshakes in progress, oldest first

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
	slot := h.acquireSlot(sourceOf(r.RemoteAddr), func() { conn.CloseNow() })
	released := false
	release := func() {
		if !released {
			released = true
			h.releaseSlot(slot)
		}
	}
	defer release()
	conn.SetReadLimit(maxHandshakeBytes)
	h.handshake(r, conn, slot, release)
}

// Handshake stages, in the order a slot is protected from eviction.
const (
	stageAwaitingHello = iota // connected, nothing received yet
	stageCertified            // hello received and its certificate verified
)

// handshakeSlot is one handshake in progress.
type handshakeSlot struct {
	src     string
	evict   func()
	stage   int
	gone    bool // evicted or released
	evicted bool
}

// acquireSlot registers a handshake. When the source (or the hub) already
// has the maximum in progress, the oldest of them is closed to make room:
// genuine handshakes finish in well under a second, so the connections that
// linger are the ones holding slots without finishing, and new arrivals,
// including agents behind the same proxy or NAT as an attacker, still get
// through.
func (h *Hub) acquireSlot(src string, evict func()) *handshakeSlot {
	h.slotMu.Lock()
	defer h.slotMu.Unlock()
	sameSource := 0
	for _, sl := range h.slots {
		if sl.src == src {
			sameSource++
		}
	}
	if sameSource >= maxHandshakesPer {
		h.evictLocked(h.victimLocked(func(sl *handshakeSlot) bool { return sl.src == src }))
	}
	if len(h.slots) >= maxHandshakes {
		h.evictLocked(h.victimLocked(func(*handshakeSlot) bool { return true }))
	}
	sl := &handshakeSlot{src: src, evict: evict}
	h.slots = append(h.slots, sl)
	return sl
}

// victimLocked picks the slot to close: the oldest at the earliest stage,
// so a handshake that already presented a valid certificate is closed only
// when every other candidate is just as far along.
func (h *Hub) victimLocked(match func(*handshakeSlot) bool) *handshakeSlot {
	var best *handshakeSlot
	for _, sl := range h.slots { // oldest first
		if match(sl) && (best == nil || sl.stage < best.stage) {
			best = sl
		}
	}
	return best
}

func (h *Hub) evictLocked(sl *handshakeSlot) {
	if sl == nil {
		return
	}
	sl.evicted = true
	h.removeLocked(sl)
	go sl.evict()
}

// advance records that a handshake reached stage.
func (h *Hub) advance(sl *handshakeSlot, stage int) {
	h.slotMu.Lock()
	defer h.slotMu.Unlock()
	sl.stage = stage
}

func (h *Hub) wasEvicted(sl *handshakeSlot) bool {
	h.slotMu.Lock()
	defer h.slotMu.Unlock()
	return sl.evicted
}

func (h *Hub) removeLocked(sl *handshakeSlot) {
	if sl.gone {
		return
	}
	sl.gone = true
	for i, x := range h.slots {
		if x == sl {
			h.slots = append(h.slots[:i], h.slots[i+1:]...)
			return
		}
	}
}

func (h *Hub) releaseSlot(sl *handshakeSlot) {
	h.slotMu.Lock()
	defer h.slotMu.Unlock()
	h.removeLocked(sl)
}

// sourceOf groups remote addresses: an IPv4 address, or the /64 of an IPv6
// address (one host can hold a whole /64).
func sourceOf(remote string) string {
	host := remote
	if h, _, err := net.SplitHostPort(remote); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return (&net.IPNet{IP: ip.Mask(net.CIDRMask(64, 128)), Mask: net.CIDRMask(64, 128)}).String()
}

// clip keeps agent-supplied text short in what the hub stores and logs.
func clip(s string) string {
	if r := []rune(s); len(r) > maxRecordedText {
		return string(r[:maxRecordedText]) + "…"
	}
	return s
}

type handshakeFailure struct {
	code, message  string
	supported      bool
	nodeID         string
	agentVersion   string
	agentRange     *protocol.Range
	identityProven bool
}

func (h *Hub) handshake(r *http.Request, conn *websocket.Conn, slot *handshakeSlot, handshakeDone func()) {
	ctx, cancel := context.WithTimeout(r.Context(), h.opts.HandshakeTimeout)
	defer cancel()
	fail := func(f handshakeFailure) {
		if h.wasEvicted(slot) {
			// Closed to make room for a newer handshake; not a refusal worth
			// keeping (a flood would wash the real refusals out of the list).
			return
		}
		msg := protocol.Reject{Type: protocol.TypeReject, Code: f.code, Message: f.message}
		core := h.opts.Range
		if f.supported {
			msg.Supported = &core
		}
		// Record first: once the agent reads the reject, the refusal must
		// already be visible to operators.
		h.reject(Rejection{At: h.opts.Now().UTC(), RemoteAddr: r.RemoteAddr, Code: f.code, Message: clip(f.message), NodeID: f.nodeID,
			AgentVersion: clip(f.agentVersion), AgentRange: f.agentRange, CoreRange: core, IdentityProven: f.identityProven})
		if b, err := protocol.Encode(msg); err == nil {
			_ = conn.Write(ctx, websocket.MessageText, b)
		}
		_ = conn.Close(websocket.StatusPolicyViolation, f.code)
		h.opts.Logger.Warn("agent connection refused", "code", f.code, "reason", clip(f.message), "node", f.nodeID, "remote", r.RemoteAddr)
	}

	// A client that connects and says nothing gives its slot back quickly.
	hctx, hcancel := context.WithTimeout(ctx, helloTimeout)
	raw, hello, err := readHandshakeRaw[*protocol.Hello](hctx, conn)
	hcancel()
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
	h.advance(slot, stageCertified)
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
	kexKey, coreKex, err := protocol.NewKex()
	if err != nil {
		fail(with(protocol.RejectHandshakeMalformed, "core could not draw a key-exchange key"))
		return
	}
	keys, err := protocol.DeriveKeys(protocol.RoleCore, kexKey, hello.Kex, sessionID)
	if err != nil {
		fail(with(protocol.RejectHandshakeMalformed, "hello kex: "+err.Error()))
		return
	}
	coreCaps := []protocol.Capability{{Name: protocol.CapJobs, Version: 1}}
	heartbeat := int(h.opts.Heartbeat / time.Second)
	// The signatures cover the whole hello as received and the welcome's
	// parameters, so a proxy cannot alter either side's offer.
	tin := protocol.TranscriptInput{AgentNonce: hello.Nonce, CoreNonce: coreNonce, AgentKex: hello.Kex, CoreKex: coreKex,
		ProtocolVersion: version, NodeFingerprint: id.Fingerprint, SessionID: sessionID,
		HelloDigest: protocol.HelloDigest(raw), WelcomeDigest: protocol.WelcomeDigest(h.opts.CoreVersion, heartbeat, coreCaps)}
	welcome := protocol.Welcome{
		Type: protocol.TypeWelcome, ProtocolVersion: version, CoreVersion: h.opts.CoreVersion,
		CoreCertificate: string(h.opts.PKI.CoreCertificatePEM()), Nonce: coreNonce, Kex: coreKex, SessionID: sessionID,
		Proof:            base64.StdEncoding.EncodeToString(h.opts.PKI.SignAsCore(protocol.Transcript(protocol.RoleCore, tin))),
		HeartbeatSeconds: heartbeat, Capabilities: coreCaps,
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
	if err != nil || !ed25519.Verify(id.PublicKey, protocol.Transcript(protocol.RoleAgent, tin), proof) {
		fail(with(protocol.RejectProofInvalid, "node proof does not verify against the presented certificate"))
		return
	}
	base.identityProven = true
	// Proven: the slot is no longer needed, and releasing it now means a
	// later eviction can never close this connection.
	handshakeDone()

	s := &Session{
		hub: h, conn: conn, id: id, sessionID: sessionID, protocolVersion: version, agentVersion: hello.AgentVersion,
		agentRange: hello.Protocol, caps: hello.Capabilities, transport: transport, remote: r.RemoteAddr,
		connectedAt: h.opts.Now().UTC(), out: make(chan []byte, 256), done: make(chan struct{}), pending: map[string]*pending{},
		keys: keys,
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
		s.close(websocket.StatusGoingAway, "could not send ready")
		return
	}
	handshakeDone()
	conn.SetReadLimit(maxMessageBytes)
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
	_, v, err := readHandshakeRaw[T](ctx, conn)
	return v, err
}

// readHandshakeRaw also returns the exact bytes received (the hello digest
// in the transcript is taken over them).
func readHandshakeRaw[T any](ctx context.Context, conn *websocket.Conn) ([]byte, T, error) {
	var zero T
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return nil, zero, err
	}
	if typ != websocket.MessageText {
		return nil, zero, errors.New("binary frames are not part of the protocol")
	}
	msg, err := protocol.DecodeHandshake(data)
	if err != nil {
		return nil, zero, err
	}
	v, ok := msg.(T)
	if !ok {
		return nil, zero, fmt.Errorf("unexpected %T", msg)
	}
	return data, v, nil
}

func writeMessage(ctx context.Context, conn *websocket.Conn, msg any) error {
	b, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}

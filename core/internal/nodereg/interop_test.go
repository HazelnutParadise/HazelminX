package nodereg

// Enrollment with the real hazelminx-agent binary against this registry
// and the real hub, over TLS:
//
//	cargo build -p hazelminx-agent   (in agent/)
//	HAZELMINX_AGENT_BIN=$CARGO_TARGET_DIR/debug/hazelminx-agent go test ./internal/nodereg -run Interop -v
//
// Without HAZELMINX_AGENT_BIN the tests are skipped.

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

type interop struct {
	t   *testing.T
	bin string
	reg *Registry
	hub *nodes.Hub
	pki *nodes.PKI
	url string
}

func newInterop(t *testing.T) *interop {
	t.Helper()
	bin := os.Getenv("HAZELMINX_AGENT_BIN")
	if bin == "" {
		t.Skip("HAZELMINX_AGENT_BIN not set; build hazelminx-agent to run the enrollment interop tests")
	}
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	pki, err := nodes.LoadOrCreatePKI(context.Background(), &memSecrets{vals: map[string][]byte{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg, err := New(Options{DB: st.DB, PKI: pki, IssuerKeyPEM: []byte("-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----\n"), Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	hub := nodes.NewHub(nodes.HubOptions{PKI: pki, CoreVersion: "test", Authorizer: reg, Logger: quiet, Heartbeat: time.Second,
		DuplicatePingTimeout: 2 * time.Second,
		OnReady:              func(s *nodes.Session, _ []string) { go reg.OnReady(s) }, OnClosed: reg.OnClosed, OnConflict: reg.OnConflict})
	reg.AttachChannel(hub)
	srv := httptest.NewUnstartedServer(reg.AgentHandler(hub))
	srv.TLS = pki.ServerTLSConfig()
	srv.StartTLS()
	t.Cleanup(func() { hub.Close(); srv.Close() })
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return &interop{t: t, bin: bin, reg: reg, hub: hub, pki: pki, url: "https://" + net.JoinHostPort("127.0.0.1", port)}
}

type proc struct {
	cmd  *exec.Cmd
	out  *lockedBuffer
	done chan struct{}
	err  error
}

func (x *interop) agent(stateDir string, args ...string) *proc {
	x.t.Helper()
	all := append([]string{"--config", "/nonexistent/agent.toml", "--state-dir", stateDir, "--log-format", "text"}, args...)
	cmd := exec.Command(x.bin, all...)
	cmd.Env = append(os.Environ(), "RUST_LOG=info")
	p := &proc{cmd: cmd, out: &lockedBuffer{}, done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = p.out, p.out
	if err := cmd.Start(); err != nil {
		x.t.Fatal(err)
	}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	x.t.Cleanup(func() { _ = cmd.Process.Signal(syscall.SIGKILL); <-p.done })
	return p
}

func (p *proc) wait(t *testing.T, within time.Duration) error {
	t.Helper()
	select {
	case <-p.done:
		return p.err
	case <-time.After(within):
		t.Fatalf("agent still running after %s; output:\n%s", within, p.out)
		return nil
	}
}

func until(t *testing.T, what string, within time.Duration, cond func() bool, logs ...*proc) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	var out strings.Builder
	for _, p := range logs {
		out.WriteString(p.out.String())
	}
	t.Fatalf("timed out waiting for %s; agent output:\n%s", what, out.String())
}

// enroll runs `hazelminx-agent enroll` in stateDir and approves it.
func (x *interop) enroll(stateDir string) Node {
	x.t.Helper()
	ctx := context.Background()
	en, code, err := x.reg.CreateEnrollment(ctx, CreateInput{Name: "interop", Actor: "usr_test"})
	if err != nil {
		x.t.Fatal(err)
	}
	p := x.agent(stateDir, "enroll", "--core-url", x.url, "--code", code.String(), "--wait-seconds", "60")
	var pending Enrollment
	until(x.t, "the host to wait for approval", 15*time.Second, func() bool {
		pending, _ = x.reg.GetEnrollment(ctx, en.ID)
		return pending.State == EnrollmentPendingApproval
	}, p)
	n, err := x.reg.Approve(ctx, en.ID, ApproveInput{Actor: "usr_test", AsNewNode: true})
	if err != nil {
		x.t.Fatal(err)
	}
	if err := p.wait(x.t, 20*time.Second); err != nil {
		x.t.Fatalf("enroll failed: %v\n%s", err, p.out)
	}
	if !strings.Contains(p.out.String(), n.ID) || !strings.Contains(p.out.String(), pending.Host.Fingerprint) {
		x.t.Fatalf("enroll output does not name the node and key:\n%s", p.out)
	}
	if n.Fingerprint != pending.Host.Fingerprint {
		x.t.Fatalf("node key %s, host key %s", n.Fingerprint, pending.Host.Fingerprint)
	}
	return n
}

func (x *interop) session(fp string) (nodes.ConnectionInfo, bool) {
	conns, _ := x.hub.Connections()
	for _, c := range conns {
		if c.NodeID == fp {
			return c, true
		}
	}
	return nodes.ConnectionInfo{}, false
}

// NOD-001, SEC-002, SEC-003, T-016, T-019, T-022 with the real agent.
func TestInteropEnrollConnectRotateRevoke(t *testing.T) {
	x := newInterop(t)
	ctx := context.Background()
	dir := t.TempDir()
	n := x.enroll(dir)
	for _, f := range []string{"identity/node.crt", "identity/core-ca.crt", "identity/privileged-issuer.pub", "enrollment.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("enroll did not write %s: %v", f, err)
		}
	}

	// The same code cannot enroll another host (T-016).
	en, code, _ := x.reg.CreateEnrollment(ctx, CreateInput{Actor: "usr_test"})
	first := x.agent(t.TempDir(), "enroll", "--core-url", x.url, "--code", code.String(), "--wait-seconds", "5")
	until(t, "first host to use the code", 15*time.Second, func() bool { e, _ := x.reg.GetEnrollment(ctx, en.ID); return e.Host != nil }, first)
	second := x.agent(t.TempDir(), "enroll", "--core-url", x.url, "--code", code.String(), "--wait-seconds", "5")
	if err := second.wait(t, 20*time.Second); err == nil || !strings.Contains(second.out.String(), "ENROLLMENT_CODE_USED") {
		t.Fatalf("second host with the same code: %v\n%s", err, second.out)
	}

	// The agent connects to the URL it enrolled with.
	run := x.agent(dir, "run")
	until(t, "agent session", 15*time.Second, func() bool { _, ok := x.session(n.Fingerprint); return ok }, run)
	until(t, "online status", 5*time.Second, func() bool {
		got, _ := x.reg.GetNode(ctx, n.ID)
		return got.Connection.State == ConnOnline && got.Components[0].Version != ""
	}, run)

	// Rotation: the new certificate is delivered, proven, then the only one.
	rotated, err := x.reg.RotateCertificate(ctx, n.ID, "usr_test")
	if err != nil || rotated.Certificate.Rotation == nil {
		t.Fatalf("rotate: %+v %v", rotated.Certificate, err)
	}
	next := rotated.Certificate.Rotation.NewSerial
	until(t, "agent to reconnect with the new certificate", 20*time.Second, func() bool {
		c, ok := x.session(n.Fingerprint)
		got, _ := x.reg.GetNode(ctx, n.ID)
		return ok && c.CertificateSerial == next && got.Certificate.Serial == next && got.Certificate.Rotation == nil
	}, run)
	if _, err := os.Stat(filepath.Join(dir, "identity/node.crt.prev")); err != nil {
		t.Fatalf("previous certificate not kept: %v", err)
	}
	if err := x.reg.AuthorizeNode(ctx, nodes.NodeIdentity{Fingerprint: n.Fingerprint, Serial: n.Certificate.Serial}); err == nil {
		t.Fatal("old certificate still accepted after the switch")
	}

	// Revocation closes the live session; the agent's reconnect is refused.
	revoked, err := x.reg.RevokeNode(ctx, n.ID, "interop", "usr_test")
	if err != nil || revoked.Revocation.SessionClosedAt == nil {
		t.Fatalf("revoke: %+v %v", revoked.Revocation, err)
	}
	if _, ok := x.session(n.Fingerprint); ok {
		t.Fatal("revoked node still connected")
	}
	until(t, "agent to be refused as revoked", 20*time.Second, func() bool { return strings.Contains(run.out.String(), "NODE_REVOKED") }, run)
}

// T-017 with the real agent: a copy of the node's state directory running
// at the same time is detected, both copies are refused, and data from the
// two never mixes.
func TestInteropClonedNodeIsAConflict(t *testing.T) {
	x := newInterop(t)
	ctx := context.Background()
	dir := t.TempDir()
	n := x.enroll(dir)
	clone := t.TempDir()
	if out, err := exec.Command("cp", "-Rp", dir+"/.", clone).CombinedOutput(); err != nil {
		t.Fatalf("copy state: %v %s", err, out)
	}
	a := x.agent(dir, "run")
	until(t, "first copy connected", 15*time.Second, func() bool { _, ok := x.session(n.Fingerprint); return ok }, a)
	b := x.agent(clone, "run")
	until(t, "conflict", 30*time.Second, func() bool { got, _ := x.reg.GetNode(ctx, n.ID); return got.State == StateIdentityConflict }, a, b)
	until(t, "both copies disconnected", 10*time.Second, func() bool { _, ok := x.session(n.Fingerprint); return !ok }, a, b)
	until(t, "the copies to be told", 30*time.Second, func() bool {
		return strings.Contains(a.out.String()+b.out.String(), "IDENTITY_CONFLICT")
	}, a, b)
}

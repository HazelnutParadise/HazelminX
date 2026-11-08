package hostledger

// End to end on a real Linux host: Core (hub, grant issuer, syncer, store)
// ← real hazelminx-agent ← real hazelminx-privileged with its root-only
// ledger. It restarts a real systemd unit through the executor so the host
// ledger has lines, then checks that Core's copy matches the host file byte
// for byte. It changes the host, so it needs root, systemd and an explicit
// opt-in:
//
//	cargo build -p hazelminx-agent -p hazelminx-privileged     (in agent/)
//	sudo HAZELMINX_DISPOSABLE_HOST=1 \
//	  HAZELMINX_AGENT_BIN=$CARGO_TARGET_DIR/debug/hazelminx-agent \
//	  HAZELMINX_PRIVILEGED_BIN=$CARGO_TARGET_DIR/debug/hazelminx-privileged \
//	  go test ./internal/hostledger -run Interop -v

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
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

	"github.com/HazelnutParadise/hazelminx/core/internal/grants"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

const interopUnit = "hzx-ledger-interop.service"

type interop struct {
	t         *testing.T
	agentBin  string
	privBin   string
	hub       *nodes.Hub
	pki       *nodes.PKI
	srv       *httptest.Server
	issuer    *grants.Issuer
	store     *Store
	syncer    *Syncer
	work      string // temp dir: sockets, keys, agent state
	ledgerDir string
	fp        string
	procs     []*exec.Cmd
	logs      *lockedBuffer
}

// lockedBuffer collects the output of both processes.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newInterop(t *testing.T) *interop {
	t.Helper()
	agentBin, privBin := os.Getenv("HAZELMINX_AGENT_BIN"), os.Getenv("HAZELMINX_PRIVILEGED_BIN")
	if agentBin == "" || privBin == "" {
		t.Skip("HAZELMINX_AGENT_BIN and HAZELMINX_PRIVILEGED_BIN not set")
	}
	if os.Getenv("HAZELMINX_DISPOSABLE_HOST") != "1" || os.Geteuid() != 0 {
		t.Skip("changes the host: needs root and HAZELMINX_DISPOSABLE_HOST=1")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Skip("host is not running systemd")
	}
	ctx := context.Background()
	e := &interop{t: t, agentBin: agentBin, privBin: privBin, work: t.TempDir(), logs: &lockedBuffer{}}
	st := &memSecrets{vals: map[string][]byte{}}
	var err error
	if e.pki, err = nodes.LoadOrCreatePKI(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	if e.issuer, err = grants.LoadOrCreate(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	e.store = openStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.hub = nodes.NewHub(nodes.HubOptions{PKI: e.pki, CoreVersion: "test", Heartbeat: time.Second, Logger: logger})
	e.srv = httptest.NewUnstartedServer(e.hub)
	e.srv.TLS = e.pki.ServerTLSConfig()
	e.srv.StartTLS()
	t.Cleanup(func() { e.hub.Close(); e.srv.Close() })
	e.syncer = &Syncer{Store: e.store, Grants: e.issuer, Pull: e.hub, PageLimit: 3, Logger: logger}

	// The ledger lives where the executor's ancestor check accepts it.
	base := filepath.Join("/var/lib", fmt.Sprintf("hzx-ledger-interop-%d", os.Getpid()))
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o755); err != nil { // umask may be 0 in containers
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	e.ledgerDir = filepath.Join(base, "ledger")

	unit := "/etc/systemd/system/" + interopUnit
	if err := os.WriteFile(unit, []byte("[Unit]\nDescription=hazelminx ledger interop\n[Service]\nExecStart=/bin/sleep infinity\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, "systemctl", "daemon-reload")
	t.Cleanup(func() {
		_ = exec.Command("systemctl", "stop", interopUnit).Run()
		_ = os.Remove(unit)
		_ = exec.Command("systemctl", "daemon-reload").Run()
	})
	return e
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func (e *interop) socket() string { return filepath.Join(e.work, "privileged.sock") }

func (e *interop) startExecutor() *exec.Cmd {
	t := e.t
	pub := filepath.Join(e.work, "issuer.pub")
	if err := os.WriteFile(pub, e.issuer.PublicKeyPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(e.privBin, "--config", "/nonexistent.toml", "--socket", e.socket(), "--issuer-key", pub,
		"--allowed-uid", fmt.Sprint(os.Getuid()), "--ledger-dir", e.ledgerDir, "--log-format", "text", "run")
	cmd.Stdout, cmd.Stderr = e.logs, e.logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e.procs = append(e.procs, cmd)
	t.Cleanup(func() { stop(cmd) })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(e.socket()); err == nil {
			return cmd
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("executor socket did not appear\n%s", e.logs.String())
	return nil
}

func stop(cmd *exec.Cmd) {
	if cmd.ProcessState != nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

// startAgent enrolls the agent (what ticket 019 will do) and runs it.
func (e *interop) startAgent() {
	t := e.t
	state := filepath.Join(e.work, "agent")
	out, err := exec.Command(e.agentBin, "--config", "/nonexistent.toml", "--state-dir", state, "identity").CombinedOutput()
	if err != nil {
		t.Fatalf("agent identity: %v\n%s", err, out)
	}
	pubPEM, err := os.ReadFile(filepath.Join(state, "identity", "node.pub"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pubPEM)
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pub := k.(ed25519.PublicKey)
	e.fp = protocol.Fingerprint(pub)
	cert, err := e.pki.IssueNodeCertificate(pub, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "identity", "node.crt"), cert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "identity", "core-ca.crt"), e.pki.CACertificatePEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(e.work, "agent.toml")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, "privileged_socket = %q\n", e.socket()), 0o644); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(e.srv.Listener.Addr().String())
	cmd := exec.Command(e.agentBin, "--config", cfg, "--state-dir", state, "--core-url", "https://127.0.0.1:"+port, "--log-format", "text", "run")
	cmd.Stdout, cmd.Stderr = e.logs, e.logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(cmd) })
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if s, ok := e.hub.Session(e.fp); ok && s.Supports(protocol.CapHostLedger, 1) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("agent did not connect\n%s", e.logs.String())
}

// restart asks the executor directly (as the agent would for a job) to
// restart the interop unit, which writes an intent and a result line.
func (e *interop) restart() {
	t := e.t
	g, err := e.issuer.Issue("user-interop", []string{"host.services.manage"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := json.Marshal(map[string]any{"type": "action", "request_id": "req-interop", "job_id": "job-interop",
		"grant": g, "action": map[string]any{"kind": "restart_service", "unit": interopUnit}})
	conn, err := net.Dial("unix", e.socket())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(append(req, '\n')); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var f struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(sc.Bytes(), &f)
		switch f.Type {
		case "result":
			return
		case "error":
			t.Fatalf("restart refused: %s\n%s", f.Error, e.logs.String())
		}
	}
	t.Fatalf("executor closed without a result: %v", sc.Err())
}

func (e *interop) hostLines() []string {
	b, err := os.ReadFile(filepath.Join(e.ledgerDir, "ledger.jsonl"))
	if err != nil {
		e.t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func (e *interop) requireCopyMatchesHost() {
	t := e.t
	host := e.hostLines()
	got, err := e.store.Lines(context.Background(), e.fp, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(host) {
		t.Fatalf("Core has %d lines, host has %d", len(got), len(host))
	}
	for i := range host {
		if got[i].Raw != host[i] {
			t.Fatalf("line %d differs:\nCore %s\nhost %s", i+1, got[i].Raw, host[i])
		}
	}
}

func TestInteropCoreCopiesTheRealHostLedger(t *testing.T) {
	e := newInterop(t)
	ctx := context.Background()
	executor := e.startExecutor()
	e.restart()
	e.restart()
	e.startAgent()

	head, err := e.syncer.SyncNode(ctx, e.fp)
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, e.logs.String())
	}
	if head.Seq != 4 {
		t.Fatalf("head %+v", head)
	}
	e.requireCopyMatchesHost()

	// More changes on the host; the next sync fetches only the new lines.
	e.restart()
	if head, err = e.syncer.SyncNode(ctx, e.fp); err != nil || head.Seq != 6 {
		t.Fatalf("second sync: %+v %v", head, err)
	}
	e.requireCopyMatchesHost()
	c, _, _ := e.store.Get(ctx, e.fp)
	if c.LastErrorCode != "" || c.LastSyncedAt == nil {
		t.Fatalf("copy %+v", c)
	}

	// Executor down: the agent says so and Core records it.
	stop(executor)
	_ = os.Remove(e.socket())
	if _, err := e.syncer.SyncNode(ctx, e.fp); err == nil {
		t.Fatal("sync with the executor stopped succeeded")
	}
	c, _, _ = e.store.Get(ctx, e.fp)
	if c.LastErrorCode != protocol.LedgerExecutorUnreachable {
		t.Fatalf("recorded %q (%s)", c.LastErrorCode, c.LastError)
	}

	// A grant from another issuer is refused by the executor.
	e.startExecutor()
	other, err := grants.LoadOrCreate(ctx, &memSecrets{vals: map[string][]byte{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	forged := &Syncer{Store: e.store, Grants: other, Pull: e.hub}
	if _, err := forged.SyncNode(ctx, e.fp); err == nil {
		t.Fatal("foreign grant accepted")
	}
	c, _, _ = e.store.Get(ctx, e.fp)
	if c.LastErrorCode != protocol.LedgerExecutorRefused {
		t.Fatalf("recorded %q (%s)", c.LastErrorCode, c.LastError)
	}
	// The genuine issuer still works, and nothing was lost.
	if head, err = e.syncer.SyncNode(ctx, e.fp); err != nil || head.Seq != 6 {
		t.Fatalf("recovery: %+v %v", head, err)
	}
	e.requireCopyMatchesHost()
}

// memSecrets is an in-memory secret store for the interop test.
type memSecrets struct {
	list []secrets.Secret
	vals map[string][]byte
}

func (m *memSecrets) List(context.Context) ([]secrets.Secret, error) { return m.list, nil }

func (m *memSecrets) Create(_ context.Context, in secrets.CreateInput) (secrets.Secret, error) {
	s := secrets.Secret{ID: fmt.Sprintf("sec_%d", len(m.list)+1), Kind: in.Kind, Name: in.Name, Scope: in.Scope}
	m.list = append(m.list, s)
	m.vals[s.ID] = append([]byte(nil), in.Value...)
	return s, nil
}

func (m *memSecrets) Reveal(_ context.Context, req secrets.RevealRequest) ([]byte, int, error) {
	for _, s := range m.list {
		if s.ID == req.ID {
			return append([]byte(nil), m.vals[s.ID]...), 1, nil
		}
	}
	return nil, 0, secrets.ErrNotFound
}

package hoststorage

// End to end with a real agent: Core (hub, Reporter, Prober, Store) ←
// real hazelminx-agent reading the storage of the host it runs on.
//
//	cargo build -p hazelminx-agent -p hazelminx-privileged     (in agent/)
//	HAZELMINX_AGENT_BIN=$CARGO_TARGET_DIR/debug/hazelminx-agent \
//	  go test ./internal/hoststorage -run Interop -v
//
// TestInteropAgentReportsStorage needs no root: the agent's privileged
// executor socket does not exist, so the usage request must come back as
// "could not measure" with nothing invented.
//
// TestInteropRootHelperMeasuresWhatAgentCannotRead runs the agent as
// nobody and the real hazelminx-privileged as root on a Linux host whose
// /var/lib/docker is its own local mount (a container with a volume there):
//
//	sudo HAZELMINX_DISPOSABLE_HOST=1 HAZELMINX_AGENT_BIN=... \
//	  HAZELMINX_PRIVILEGED_BIN=$CARGO_TARGET_DIR/debug/hazelminx-privileged \
//	  go test ./internal/hoststorage -run Interop -v

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/grants"
	"github.com/HazelnutParadise/hazelminx/core/internal/hostmetrics"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

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

type memSecrets struct {
	mu   sync.Mutex
	list []secrets.Secret
	vals map[string][]byte
}

func (m *memSecrets) List(context.Context) ([]secrets.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]secrets.Secret(nil), m.list...), nil
}

func (m *memSecrets) Create(_ context.Context, in secrets.CreateInput) (secrets.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := secrets.Secret{ID: fmt.Sprintf("sec_%d", len(m.list)+1), Kind: in.Kind, Name: in.Name, Scope: in.Scope}
	m.list = append(m.list, s)
	m.vals[s.ID] = append([]byte(nil), in.Value...)
	return s, nil
}

func (m *memSecrets) Reveal(_ context.Context, req secrets.RevealRequest) ([]byte, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.vals[req.ID]
	if !ok {
		return nil, 0, errors.New("missing")
	}
	return v, 1, nil
}

func stopProc(cmd *exec.Cmd) {
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

func waitFor(t *testing.T, logs *lockedBuffer, within time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not happen within %s\n%s", what, within, logs.String())
}

func interopDB(t *testing.T) *sql.DB {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.DB.Exec(`INSERT INTO nodes (id, name, fingerprint, public_key, state, cert_serial, cert_not_after, created_at, updated_at)
		VALUES ('node-1', 'node-1', 'fp-1', x'00', 'active', '1', '2027-01-01T00:00:00Z', '2026-11-01T00:00:00Z', '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	return st.DB
}

// core is a Core with hub, grant issuer, storage and metrics stores.
type core struct {
	hub    *nodes.Hub
	addr   string
	pki    *nodes.PKI
	issuer *grants.Issuer
	st     *Store
	hm     *hostmetrics.Store
}

func startCore(t *testing.T, fp *string) *core {
	t.Helper()
	ctx := context.Background()
	sec := &memSecrets{vals: map[string][]byte{}}
	pki, err := nodes.LoadOrCreatePKI(ctx, sec, nil)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := grants.LoadOrCreate(ctx, sec, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := interopDB(t)
	c := &core{pki: pki, issuer: issuer, st: NewStore(db, nil), hm: hostmetrics.NewStore(db, nil)}
	nodeID := func(_ context.Context, got string) (string, error) {
		if got != *fp {
			return "", errors.New("unknown fingerprint")
		}
		return "node-1", nil
	}
	c.hub = nodes.NewHub(nodes.HubOptions{PKI: pki, CoreVersion: "test", Heartbeat: time.Second,
		HostReports:    hostmetrics.Reporter{Store: c.hm, NodeID: nodeID},
		StorageReports: Reporter{Store: c.st, NodeID: nodeID},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv := httptest.NewUnstartedServer(c.hub)
	srv.TLS = pki.ServerTLSConfig()
	srv.StartTLS()
	t.Cleanup(func() { c.hub.Close(); srv.Close() })
	c.addr = srv.Listener.Addr().String()
	return c
}

// enroll makes an agent identity in state, signs it and returns its
// fingerprint; owner, when not -1, gets the state directory.
func (c *core) enroll(t *testing.T, agentBin, state string, owner int) string {
	t.Helper()
	if out, err := exec.Command(agentBin, "--config", "/nonexistent.toml", "--state-dir", state, "identity").CombinedOutput(); err != nil {
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
	cert, err := c.pki.IssueNodeCertificate(k.(ed25519.PublicKey), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "identity", "node.crt"), cert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "identity", "core-ca.crt"), c.pki.CACertificatePEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	if owner >= 0 {
		_ = filepath.Walk(state, func(p string, _ os.FileInfo, _ error) error { return os.Lchown(p, owner, owner) })
	}
	return protocol.Fingerprint(k.(ed25519.PublicKey))
}

func (c *core) prober(fp string) *Prober {
	return &Prober{Store: c.st, Grants: c.issuer, Hub: c.hub, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		NodeID: func(context.Context, string) (string, error) { return "node-1", nil }}
}

func startAgent(t *testing.T, logs *lockedBuffer, agentBin, cfg, state, coreAddr string, uid int) {
	t.Helper()
	_, port, _ := net.SplitHostPort(coreAddr)
	cmd := exec.Command(agentBin, "--config", cfg, "--state-dir", state, "--core-url", "https://127.0.0.1:"+port, "--log-format", "text", "run")
	if uid >= 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(uid)}}
	}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProc(cmd) })
}

func TestInteropAgentReportsStorage(t *testing.T) {
	agentBin := os.Getenv("HAZELMINX_AGENT_BIN")
	if agentBin == "" {
		t.Skip("HAZELMINX_AGENT_BIN not set")
	}
	ctx := context.Background()
	var fp string
	c := startCore(t, &fp)
	logs := &lockedBuffer{}
	work := t.TempDir()
	state := filepath.Join(work, "agent")
	fp = c.enroll(t, agentBin, state, -1)
	cfg := filepath.Join(work, "agent.toml")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, "privileged_socket = %q\n", filepath.Join(work, "absent.sock")), 0o644); err != nil {
		t.Fatal(err)
	}
	startAgent(t, logs, agentBin, cfg, state, c.addr, -1)

	waitFor(t, logs, 15*time.Second, "agent session with host.storage", func() bool {
		s, ok := c.hub.Session(fp)
		return ok && s.Supports(protocol.CapHostStorage, 1)
	})
	var inv *Inventory
	waitFor(t, logs, 20*time.Second, "storage inventory stored", func() bool {
		inv, _ = c.st.Inventory(ctx, "node-1")
		return inv != nil
	})
	if runtime.GOOS != "linux" {
		if len(inv.Storage.Mounts) != 0 || !slices.Contains(inv.Storage.Unreadable, "/proc/self/mountinfo") {
			t.Fatalf("no /proc here, yet mounts arrived or the gap is not named: %+v", inv.Storage)
		}
	} else if len(inv.Storage.Mounts) == 0 {
		t.Fatalf("no mounts on Linux: %+v", inv.Storage)
	}

	// No executor: the request comes back as "could not measure", with
	// nothing measured and no zeros.
	if err := c.prober(fp).Probe(ctx, fp); err == nil {
		t.Fatal("usage without the executor must fail")
	}
	u, err := c.st.Usage(ctx, "node-1")
	if err != nil || u == nil || u.Measured || u.Error == nil || u.Error.Code != protocol.StorageUsageExecutorUnreachable {
		t.Fatalf("usage: %+v %v\n%s", u, err, logs.String())
	}
	v, err := c.st.NodeView(ctx, c.hm, "node-1")
	if err != nil || !v.Reported || v.UsageError == nil || v.UsageMeasuredAt != nil {
		t.Fatalf("view: %+v %v", v, err)
	}
	for _, m := range v.Mounts {
		if m.Usage != nil {
			t.Fatalf("mount %s has usage without a measurement: %+v", m.MountPoint, m.Usage)
		}
	}
	for _, d := range v.Devices {
		if d.Kind == "disk" && (d.Health == nil || d.Health.State != "notCollected") {
			t.Fatalf("disk %s must not claim a health result: %+v", d.Name, d.Health)
		}
	}
}

func TestInteropRootHelperMeasuresWhatAgentCannotRead(t *testing.T) {
	agentBin, privBin := os.Getenv("HAZELMINX_AGENT_BIN"), os.Getenv("HAZELMINX_PRIVILEGED_BIN")
	if agentBin == "" || privBin == "" {
		t.Skip("HAZELMINX_AGENT_BIN and HAZELMINX_PRIVILEGED_BIN not set")
	}
	if runtime.GOOS != "linux" || os.Getenv("HAZELMINX_DISPOSABLE_HOST") != "1" || os.Geteuid() != 0 {
		t.Skip("writes under /var/lib/docker and runs the executor as root: Linux, root and HAZELMINX_DISPOSABLE_HOST=1")
	}
	const nobody = 65534
	ctx := context.Background()
	// /var/lib/docker must be on a local filesystem (an overlay root is not
	// measured): on a CI runner it is on the root disk, in a container a
	// volume is mounted there. The walk covers the whole host, so the CI
	// step allows 30 minutes.
	if out, err := exec.Command("findmnt", "-n", "-o", "FSTYPE", "-T", "/var/lib/docker").Output(); err != nil || len(out) == 0 || strings.TrimSpace(string(out)) == "overlay" {
		t.Skip("/var/lib/docker is on an overlay root, which is not measured")
	}
	// Root-only engine storage, like Docker's 0710 root directory.
	data := filepath.Join("/var/lib/docker", "overlay2", "hzx-interop-"+strconv.Itoa(os.Getpid()))
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(data) })
	if err := os.Chmod("/var/lib/docker", 0o710); err != nil {
		t.Fatal(err)
	}
	blob := make([]byte, 4<<20)
	if err := os.WriteFile(filepath.Join(data, "layer"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	var fp string
	c := startCore(t, &fp)
	logs := &lockedBuffer{}
	// A directory nobody can enter: state, config and the executor socket.
	work, err := os.MkdirTemp("/tmp", "hzx-storage-interop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	_ = os.Chmod(work, 0o755)
	// A copy of the agent nobody may run (the build tree may be private).
	agentCopy := filepath.Join(work, "hazelminx-agent")
	b, err := os.ReadFile(agentBin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentCopy, b, 0o755); err != nil {
		t.Fatal(err)
	}
	agentBin = agentCopy
	state := filepath.Join(work, "agent")
	fp = c.enroll(t, agentBin, state, nobody)

	// The executor, as root, accepts nobody's connections.
	base := filepath.Join("/var/lib", fmt.Sprintf("hzx-storage-interop-%d", os.Getpid()))
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(base, 0o755)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	sock := filepath.Join(work, "privileged.sock")
	pub := filepath.Join(work, "issuer.pub")
	if err := os.WriteFile(pub, c.issuer.PublicKeyPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := exec.Command(privBin, "--config", "/nonexistent.toml", "--socket", sock, "--issuer-key", pub,
		"--allowed-uid", strconv.Itoa(nobody), "--ledger-dir", filepath.Join(base, "ledger"), "--log-format", "text", "run")
	ex.Stdout, ex.Stderr = logs, logs
	if err := ex.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProc(ex) })
	waitFor(t, logs, 10*time.Second, "executor socket", func() bool { _, err := os.Stat(sock); return err == nil })
	// Deployed, the executor runs with the agent's group (Group=hazelminx-agent)
	// and its 0660 socket is the agent's to open; here that group is nobody's.
	if err := os.Chown(sock, 0, nobody); err != nil {
		t.Fatal(err)
	}

	cfg := filepath.Join(work, "agent.toml")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, "privileged_socket = %q\n", sock), 0o644); err != nil {
		t.Fatal(err)
	}
	startAgent(t, logs, agentBin, cfg, state, c.addr, nobody)
	waitFor(t, logs, 15*time.Second, "agent session with host.storage", func() bool {
		s, ok := c.hub.Session(fp)
		return ok && s.Supports(protocol.CapHostStorage, 1)
	})
	// The agent's own account cannot even list the directory.
	ls := exec.Command("ls", data)
	ls.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: nobody, Gid: nobody}}
	if err := ls.Run(); err == nil {
		t.Fatalf("%s is readable by the agent's account; the test proves nothing", data)
	}
	if err := c.prober(fp).Probe(ctx, fp); err != nil {
		t.Fatalf("probe: %v\n%s", err, logs.String())
	}
	u, err := c.st.Usage(ctx, "node-1")
	if err != nil || u == nil || !u.Measured || u.Error != nil {
		t.Fatalf("usage: %+v %v", u, err)
	}
	var found *protocol.FilesystemUsage
	for i, f := range u.Usage.Filesystems {
		for _, cat := range f.Categories {
			if cat.Category == protocol.CategoryContainerImages {
				found = &u.Usage.Filesystems[i]
			}
		}
	}
	if found == nil {
		t.Fatalf("no container images measured: %+v", u.Usage)
	}
	var sum, images uint64
	for _, cat := range found.Categories {
		sum += cat.Bytes
		if cat.Category == protocol.CategoryContainerImages {
			images = cat.Bytes
		}
	}
	if found.State != "complete" || found.OtherBytes == nil || sum+*found.OtherBytes != *found.UsedBytes || images < uint64(len(blob)) {
		t.Fatalf("a complete round of root-only data that adds up: %+v", found)
	}
	t.Logf("measured %s as root for an agent running as uid %d: %+v, other %d, used %d, %d entries in %d ms",
		found.MountPoint, nobody, found.Categories, *found.OtherBytes, *found.UsedBytes, u.Usage.Entries, u.Usage.DurationMs)
}

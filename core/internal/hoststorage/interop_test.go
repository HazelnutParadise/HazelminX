package hoststorage

// End to end with a real agent: Core (hub, Reporter, Store) ← real
// hazelminx-agent reading the storage of the host it runs on. The agent
// must be built with test-actions so the first usage round starts at once:
//
//	cargo build -p hazelminx-agent --features test-actions   (in agent/)
//	HAZELMINX_AGENT_BIN=$CARGO_TARGET_DIR/debug/hazelminx-agent \
//	  go test ./internal/hoststorage -run Interop -v
//
// On Linux it checks the host's real mounts and the usage breakdown;
// elsewhere the agent has no /proc and reports the sources unreadable.

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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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

func TestInteropAgentReportsStorage(t *testing.T) {
	agentBin := os.Getenv("HAZELMINX_AGENT_BIN")
	if agentBin == "" {
		t.Skip("HAZELMINX_AGENT_BIN not set")
	}
	ctx := context.Background()
	pki, err := nodes.LoadOrCreatePKI(ctx, &memSecrets{vals: map[string][]byte{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := interopDB(t)
	st, hm := NewStore(db, nil), hostmetrics.NewStore(db, nil)
	logs := &lockedBuffer{}
	state := filepath.Join(t.TempDir(), "agent")
	out, err := exec.Command(agentBin, "--config", "/nonexistent.toml", "--state-dir", state, "identity").CombinedOutput()
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
	fp := protocol.Fingerprint(k.(ed25519.PublicKey))
	nodeID := func(_ context.Context, got string) (string, error) {
		if got != fp {
			return "", errors.New("unknown fingerprint")
		}
		return "node-1", nil
	}
	hub := nodes.NewHub(nodes.HubOptions{PKI: pki, CoreVersion: "test", Heartbeat: time.Second,
		HostReports:    hostmetrics.Reporter{Store: hm, NodeID: nodeID},
		StorageReports: Reporter{Store: st, NodeID: nodeID},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv := httptest.NewUnstartedServer(hub)
	srv.TLS = pki.ServerTLSConfig()
	srv.StartTLS()
	t.Cleanup(func() { hub.Close(); srv.Close() })
	cert, err := pki.IssueNodeCertificate(k.(ed25519.PublicKey), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "identity", "node.crt"), cert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "identity", "core-ca.crt"), pki.CACertificatePEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	cmd := exec.Command(agentBin, "--config", "/nonexistent.toml", "--state-dir", state, "--core-url", "https://127.0.0.1:"+port, "--log-format", "text", "run")
	cmd.Env = append(os.Environ(), "HAZELMINX_TEST_USAGE_FIRST_SECONDS=0")
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProc(cmd) })

	waitFor(t, logs, 15*time.Second, "agent session with host.storage", func() bool {
		s, ok := hub.Session(fp)
		return ok && s.Supports(protocol.CapHostStorage, 1)
	})
	var inv *Inventory
	waitFor(t, logs, 20*time.Second, "storage inventory stored", func() bool {
		inv, _ = st.Inventory(ctx, "node-1")
		return inv != nil
	})
	if runtime.GOOS != "linux" {
		if len(inv.Storage.Mounts) != 0 || !slices.Contains(inv.Storage.Unreadable, "/proc/self/mountinfo") {
			t.Fatalf("no /proc here, yet mounts arrived or the gap is not named: %+v", inv.Storage)
		}
		return
	}
	var root *protocol.Mount
	for i, m := range inv.Storage.Mounts {
		if m.MountPoint == "/" {
			root = &inv.Storage.Mounts[i]
		}
	}
	if root == nil || root.Dev == "" || root.Reference == "" {
		t.Fatalf("root mount missing or incomplete: %+v", inv.Storage.Mounts)
	}
	var use *Usage
	waitFor(t, logs, 120*time.Second, "storage usage stored", func() bool {
		use, _ = st.Usage(ctx, "node-1")
		return use != nil
	})
	var fsu *protocol.FilesystemUsage
	for i, f := range use.Usage.Filesystems {
		if f.Dev == root.Dev {
			fsu = &use.Usage.Filesystems[i]
		}
	}
	if fsu == nil || fsu.UsedBytes == nil || use.Usage.Entries == 0 {
		t.Fatalf("no usage for the root filesystem %s: %+v", root.Dev, use.Usage)
	}
	var sum uint64
	cats := map[string]uint64{}
	for _, c := range fsu.Categories {
		sum += c.Bytes
		cats[c.Category] = c.Bytes
	}
	if root.Class == "local" && cats[protocol.CategoryOSAndLogs] == 0 {
		t.Fatalf("/usr is on the root filesystem, yet osAndLogs is empty: %+v", fsu)
	}
	if fsu.State == "complete" && (fsu.OtherBytes == nil || sum+*fsu.OtherBytes != *fsu.UsedBytes) {
		t.Fatalf("a complete measurement must add up to used space: %+v", fsu)
	}
	t.Logf("usage of /: state %s, %d entries in %d ms, categories %v, other %v, used %d",
		fsu.State, use.Usage.Entries, use.Usage.DurationMs, cats, fsu.OtherBytes, *fsu.UsedBytes)
	waitFor(t, logs, 30*time.Second, "a metrics sample", func() bool {
		l, _ := hm.Latest(ctx, "node-1")
		return l != nil
	})
	v, err := st.NodeView(ctx, hm, "node-1")
	if err != nil || !v.Reported {
		t.Fatalf("view: %+v %v", v, err)
	}
	for _, d := range v.Devices {
		if d.Kind == "disk" && (d.Health == nil || d.Health.State != "notCollected") {
			t.Fatalf("disk %s must not claim a health result: %+v", d.Name, d.Health)
		}
	}
}

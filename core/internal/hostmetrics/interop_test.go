package hostmetrics

// End to end with a real agent: Core (hub, Reporter, Store) ← real
// hazelminx-agent collecting from the host it runs on. Needs no root:
//
//	cargo build -p hazelminx-agent     (in agent/)
//	HAZELMINX_AGENT_BIN=$CARGO_TARGET_DIR/debug/hazelminx-agent \
//	  go test ./internal/hostmetrics -run Interop -v
//
// On Linux it also checks that the host's real values arrive; elsewhere
// the agent has no /proc and every value must arrive as not provided.

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
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

func TestInteropAgentReportsFactsAndSamples(t *testing.T) {
	agentBin := os.Getenv("HAZELMINX_AGENT_BIN")
	if agentBin == "" {
		t.Skip("HAZELMINX_AGENT_BIN not set")
	}
	ctx := context.Background()
	pki, err := nodes.LoadOrCreatePKI(ctx, &memSecrets{vals: map[string][]byte{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := openDB(t) // node-1
	st := NewStore(db, nil)
	logs := &lockedBuffer{}
	work := t.TempDir()
	state := filepath.Join(work, "agent")
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
	reporter := Reporter{Store: st, NodeID: func(_ context.Context, got string) (string, error) {
		if got != fp {
			return "", errors.New("unknown fingerprint")
		}
		return "node-1", nil
	}}
	hub := nodes.NewHub(nodes.HubOptions{PKI: pki, CoreVersion: "test", Heartbeat: time.Second, HostReports: reporter,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
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
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProc(cmd) })

	waitFor(t, logs, 15*time.Second, "agent session with host.metrics", func() bool {
		s, ok := hub.Session(fp)
		return ok && s.Supports(protocol.CapHostMetrics, 1)
	})
	var facts *Facts
	waitFor(t, logs, 15*time.Second, "host facts stored", func() bool {
		facts, _ = st.Facts(ctx, "node-1")
		return facts != nil
	})
	// The second sample (one interval later) has CPU shares and rates.
	var latest *Latest
	waitFor(t, logs, 25*time.Second, "two samples stored", func() bool {
		latest, _ = st.Latest(ctx, "node-1")
		return latest != nil && latest.Sample.Seq >= 2
	})
	if latest.IntervalSeconds != 10 || latest.ReceivedAt.Before(latest.CollectedAt.Add(-time.Minute)) {
		t.Fatalf("latest %+v", latest)
	}
	if facts.Facts.Kernel.Source != "/proc/sys/kernel/osrelease" || facts.Facts.Arch.Value == nil {
		t.Fatalf("facts %+v", facts.Facts)
	}
	if runtime.GOOS != "linux" {
		if facts.Facts.Kernel.Value != nil || latest.Sample.Memory.TotalBytes != nil || latest.Sample.CPU.Total != nil {
			t.Fatalf("no /proc here, yet values arrived: %+v %+v", facts.Facts, latest.Sample)
		}
		return
	}
	f, s := facts.Facts, latest.Sample
	if f.Kernel.Value == nil || f.BootTime.Value == nil || f.LogicalCPUs.Value == nil || *f.LogicalCPUs.Value < 1 || f.TimeSynchronized.Value == nil {
		t.Fatalf("linux facts %+v", f)
	}
	if s.CPU.Total == nil || len(s.CPU.Cores) != int(*f.LogicalCPUs.Value) {
		t.Fatalf("cpu %+v (logical %d)", s.CPU, *f.LogicalCPUs.Value)
	}
	if s.Memory.TotalBytes == nil || s.Memory.AvailableBytes == nil || *s.Memory.UsedBytes != *s.Memory.TotalBytes-*s.Memory.AvailableBytes {
		t.Fatalf("memory %+v", s.Memory)
	}
	if s.Limits.Tasks == nil || s.Limits.PidMax == nil || len(s.Filesystems) == 0 {
		t.Fatalf("limits %+v filesystems %d", s.Limits, len(s.Filesystems))
	}
	pts, err := st.History(ctx, HistoryQuery{NodeID: "node-1", Series: "cpu.usage", Resolution: ResolutionRaw, From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Minute)},
		paging.Query{Limit: 10, Sort: []paging.SortKey{{Field: "at"}}})
	if err != nil || len(pts) == 0 {
		t.Fatalf("history %+v %v", pts, err)
	}

	// Core goes away and comes back: the agent keeps collecting and sends
	// what it buffered; nothing between the two sessions is invented.
	before := latest.Sample.Seq
	addr := srv.Listener.Addr().String()
	hub.Close()
	srv.Close()
	down := time.Now()
	time.Sleep(12 * time.Second) // at least one sample is collected meanwhile
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen again on %s: %v", addr, err)
	}
	hub2 := nodes.NewHub(nodes.HubOptions{PKI: pki, CoreVersion: "test", Heartbeat: time.Second, HostReports: reporter,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv2 := httptest.NewUnstartedServer(hub2)
	srv2.Listener.Close()
	srv2.Listener = ln
	srv2.TLS = pki.ServerTLSConfig()
	srv2.StartTLS()
	t.Cleanup(func() { hub2.Close(); srv2.Close() })
	up := time.Now()
	waitFor(t, logs, 30*time.Second, "buffered samples delivered after reconnect", func() bool {
		pts, err := st.History(ctx, HistoryQuery{NodeID: "node-1", Series: "load.one", Resolution: ResolutionRaw, From: down.Add(time.Second), To: up},
			paging.Query{Limit: 10, Sort: []paging.SortKey{{Field: "at"}}})
		return err == nil && len(pts) > 0
	})
	if l, _ := st.Latest(ctx, "node-1"); l.Sample.Seq <= before {
		t.Fatalf("latest seq %d, was %d", l.Sample.Seq, before)
	}
}

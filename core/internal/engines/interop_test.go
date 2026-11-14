package engines

// End to end on a real Linux host: Core (hub, grant issuer, prober, store)
// ← real hazelminx-agent ← real hazelminx-privileged (engine_inspect, as
// root). Stand-in engine sockets answer the engine API the way Docker and
// Podman do: a rootful docker.sock served by Podman (podman-docker) and
// two users' rootless sockets, so the test checks ENV-002 (kind from the
// answer) and T-051 (environments and owners kept apart) through the real
// path. It creates sockets under /run and users' runtime directories, so
// it needs root and an explicit opt-in:
//
//	cargo build -p hazelminx-agent -p hazelminx-privileged     (in agent/)
//	sudo HAZELMINX_DISPOSABLE_HOST=1 \
//	  HAZELMINX_AGENT_BIN=$CARGO_TARGET_DIR/debug/hazelminx-agent \
//	  HAZELMINX_PRIVILEGED_BIN=$CARGO_TARGET_DIR/debug/hazelminx-privileged \
//	  go test ./internal/engines -run Interop -v

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
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

// fakeEngine serves /version and /info on a Unix socket at path, from a
// child process running as uid (the executor checks who answers).
func fakeEngine(t *testing.T, path string, uid int, version, info string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Skipf("%s exists on this host; not replacing it", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for d := filepath.Dir(path); strings.HasPrefix(d, "/run/user/"); d = filepath.Dir(d) {
		if err := os.Chown(d, uid, uid); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(helperBinary(t), "-test.run=^TestFakeEngineProcess$")
	cmd.Env = append(os.Environ(), "HZX_FAKE_ENGINE="+path, "HZX_FAKE_VERSION="+version, "HZX_FAKE_INFO="+info)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(uid)}}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = os.Remove(path) })
	for deadline := time.Now().Add(10 * time.Second); ; {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake engine at %s did not start", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

var helperOnce struct {
	sync.Once
	path string
	err  error
}

// helperBinary is a copy of this test binary another uid may run (the go
// build cache is root-only).
func helperBinary(t *testing.T) string {
	helperOnce.Do(func() {
		dir, err := os.MkdirTemp("", "hzx-engines-helper")
		if err != nil {
			helperOnce.err = err
			return
		}
		_ = os.Chmod(dir, 0o755)
		b, err := os.ReadFile(os.Args[0])
		if err != nil {
			helperOnce.err = err
			return
		}
		helperOnce.path = filepath.Join(dir, "engines.test")
		helperOnce.err = os.WriteFile(helperOnce.path, b, 0o755)
	})
	if helperOnce.err != nil {
		t.Fatal(helperOnce.err)
	}
	return helperOnce.path
}

// TestFakeEngineProcess is the child of fakeEngine; it does nothing in a
// normal run.
func TestFakeEngineProcess(t *testing.T) {
	path := os.Getenv("HZX_FAKE_ENGINE")
	if path == "" {
		t.Skip("helper process")
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	version, info := os.Getenv("HZX_FAKE_VERSION"), os.Getenv("HZX_FAKE_INFO")
	_ = http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/version" {
			_, _ = io.WriteString(w, version)
			return
		}
		_, _ = io.WriteString(w, info)
	}))
}

func TestInteropRealAgentReportsEnvironmentsApart(t *testing.T) {
	agentBin, privBin := os.Getenv("HAZELMINX_AGENT_BIN"), os.Getenv("HAZELMINX_PRIVILEGED_BIN")
	if agentBin == "" || privBin == "" {
		t.Skip("HAZELMINX_AGENT_BIN and HAZELMINX_PRIVILEGED_BIN not set")
	}
	if os.Getenv("HAZELMINX_DISPOSABLE_HOST") != "1" || os.Geteuid() != 0 {
		t.Skip("creates engine sockets on the host: needs root and HAZELMINX_DISPOSABLE_HOST=1")
	}
	ctx := context.Background()
	work := t.TempDir()
	logs := &lockedBuffer{}
	st := &memSecrets{vals: map[string][]byte{}}
	pki, err := nodes.LoadOrCreatePKI(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := grants.LoadOrCreate(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := nodes.NewHub(nodes.HubOptions{PKI: pki, CoreVersion: "test", Heartbeat: time.Second, Logger: logger})
	srv := httptest.NewUnstartedServer(hub)
	srv.TLS = pki.ServerTLSConfig()
	srv.StartTLS()
	t.Cleanup(func() { hub.Close(); srv.Close() })

	podmanV := `{"Platform":{"Name":"linux/arm64/debian-13"},"Components":[{"Name":"Podman Engine","Version":"5.4.2"}],"Version":"5.4.2","ApiVersion":"1.41"}`
	dockerV := `{"Platform":{"Name":"Docker Engine - Community"},"Version":"27.3.1","ApiVersion":"1.47"}`
	// A host that already runs Docker (CI runners do) keeps it: the real
	// engine is then the rootful environment to find.
	realDocker := false
	if _, err := os.Lstat("/run/docker.sock"); err == nil {
		realDocker = true
	} else {
		fakeEngine(t, "/run/docker.sock", 0, podmanV, `{"Containers":2,"ContainersRunning":1,"CgroupDriver":"systemd","CgroupVersion":"2"}`)
	}
	fakeEngine(t, "/run/user/1000/podman/podman.sock", 1000, podmanV, `{"Containers":3,"ContainersRunning":3}`)
	fakeEngine(t, "/run/user/1001/docker.sock", 1001, dockerV, `{"Containers":7,"ContainersRunning":0,"Swarm":{"LocalNodeState":"inactive"}}`)
	t.Cleanup(func() { _ = os.RemoveAll("/run/user/1000"); _ = os.RemoveAll("/run/user/1001") })

	// Executor: ledger where its ancestor check accepts it.
	base := filepath.Join("/var/lib", fmt.Sprintf("hzx-engines-interop-%d", os.Getpid()))
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(base, 0o755) // umask may be 0 in containers
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	sock := filepath.Join(work, "privileged.sock")
	pub := filepath.Join(work, "issuer.pub")
	if err := os.WriteFile(pub, issuer.PublicKeyPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := exec.Command(privBin, "--config", "/nonexistent.toml", "--socket", sock, "--issuer-key", pub,
		"--allowed-uid", fmt.Sprint(os.Getuid()), "--ledger-dir", filepath.Join(base, "ledger"), "--log-format", "text", "run")
	ex.Stdout, ex.Stderr = logs, logs
	if err := ex.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProc(ex) })
	for deadline := time.Now().Add(10 * time.Second); ; {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("executor socket did not appear\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Agent: enrolled by hand (ticket 019 does it for real).
	state := filepath.Join(work, "agent")
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
	fp := protocol.Fingerprint(k.(ed25519.PublicKey))
	cert, err := pki.IssueNodeCertificate(k.(ed25519.PublicKey), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(state, "identity", "node.crt"), cert, 0o644)
	_ = os.WriteFile(filepath.Join(state, "identity", "core-ca.crt"), pki.CACertificatePEM(), 0o644)
	cfg := filepath.Join(work, "agent.toml")
	_ = os.WriteFile(cfg, fmt.Appendf(nil, "privileged_socket = %q\n", sock), 0o644)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	ag := exec.Command(agentBin, "--config", cfg, "--state-dir", state, "--core-url", "https://127.0.0.1:"+port, "--log-format", "text", "run")
	ag.Stdout, ag.Stderr = logs, logs
	if err := ag.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProc(ag) })
	for deadline := time.Now().Add(15 * time.Second); ; {
		if s, ok := hub.Session(fp); ok && s.Supports(protocol.CapHostEngines, 1) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent did not connect with host.engines\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	db := openDB(t)
	addNode(t, db, "node-1", fp)
	store := NewStore(db, nil)
	p := &Prober{Store: store, Grants: issuer, Hub: hub, Logger: logger,
		NodeID: func(context.Context, string) (string, error) { return "node-1", nil }}
	if err := p.Probe(ctx, fp); err != nil {
		t.Fatalf("probe: %v\n%s", err, logs.String())
	}
	v, err := store.Get(ctx, "node-1")
	if err != nil || v == nil || v.Report == nil {
		t.Fatalf("no report stored: %v %+v", err, v)
	}
	byEndpoint := map[string]protocol.Environment{}
	for _, e := range v.Report.Environments {
		byEndpoint[e.EndpointResolved] = e
	}
	sys := byEndpoint["/run/docker.sock"]
	switch {
	case realDocker:
		if sys.Kind != protocol.EngineDocker || !sys.KindConfirmed || sys.Mode != "rootful" || sys.Version == nil {
			t.Fatalf("the host's Docker must be a rootful Docker environment: %+v", sys)
		}
	case sys.Kind != protocol.EnginePodman || !sys.KindConfirmed || sys.Mode != "rootful" || sys.OwnerUID != 0:
		t.Fatalf("ENV-002: docker.sock served by Podman must be a rootful Podman environment: %+v", sys)
	}
	u1, u2 := byEndpoint["/run/user/1000/podman/podman.sock"], byEndpoint["/run/user/1001/docker.sock"]
	if u1.Kind != protocol.EnginePodman || u1.Mode != "rootless" || u1.OwnerUID != 1000 || u1.Containers == nil || *u1.Containers != 3 {
		t.Fatalf("user 1000's Podman: %+v", u1)
	}
	if u2.Kind != protocol.EngineDocker || u2.Mode != "rootless" || u2.OwnerUID != 1001 || u2.Containers == nil || *u2.Containers != 7 || u2.Swarm == nil {
		t.Fatalf("user 1001's Docker: %+v", u2)
	}
	if len(v.Report.Engines) != 3 {
		t.Fatalf("engines: %+v", v.Report.Engines)
	}
	for _, e := range v.Report.Engines {
		if e.Kind == protocol.EngineK3s && e.State != protocol.EngineNotInstalled && e.State != protocol.EngineStateUnknown {
			t.Errorf("k3s on a host without it: %+v", e)
		}
	}
	t.Logf("environments: %d, uninspected: %+v", len(v.Report.Environments), v.Report.Uninspected)

	// A grant without host.engines.read is refused by the executor, and
	// the refusal is what Core records.
	bad := &Prober{Store: store, Grants: wrongGrants{issuer}, Hub: hub, Logger: logger,
		NodeID: func(context.Context, string) (string, error) { return "node-1", nil }}
	if err := bad.Probe(ctx, fp); err == nil {
		t.Fatal("a grant without host.engines.read must be refused")
	}
	v, _ = store.Get(ctx, "node-1")
	if v.LastError == nil || v.LastError.Code != protocol.EnginesExecutorRefused || v.Report == nil {
		t.Fatalf("refusal: %+v", v.LastError)
	}
}

type wrongGrants struct{ i *grants.Issuer }

func (w wrongGrants) Issue(subject string, _ []string, lifetime time.Duration) (protocol.ExecutorGrant, error) {
	return w.i.Issue(subject, []string{"host.info.read"}, lifetime)
}

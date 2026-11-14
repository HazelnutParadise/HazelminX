package composedeploy

// End to end on a disposable Linux host with a real Docker engine: Core's
// step runner and grant issuer ← real hazelminx-agent running as uid 65534
// ← real hazelminx-privileged as root ← dockerd on /var/run/docker.sock.
// It deploys under /var/lib/hazelminx/compose and starts containers, so it
// needs root and an explicit opt-in:
//
//	cargo build -p hazelminx-agent -p hazelminx-privileged     (in agent/)
//	sudo HAZELMINX_DISPOSABLE_HOST=1 \
//	  HAZELMINX_AGENT_BIN=$CARGO_TARGET_DIR/debug/hazelminx-agent \
//	  HAZELMINX_PRIVILEGED_BIN=$CARGO_TARGET_DIR/debug/hazelminx-privileged \
//	  go test ./internal/composedeploy -run Interop -v

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
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

const agentUID = 65534

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
	return append([]byte(nil), m.vals[req.ID]...), 1, nil
}

type logRep struct {
	mu    sync.Mutex
	lines []string
}

func (r *logRep) Log(l string)    { r.mu.Lock(); r.lines = append(r.lines, l); r.mu.Unlock() }
func (r *logRep) Progress(int)    {}
func (r *logRep) Heartbeat()      {}
func (r *logRep) Evidence(string) {}
func (r *logRep) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, "\n")
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

type interop struct {
	t      *testing.T
	d      *Deployer
	runner *nodes.StepRunner
	fp     string
	logs   *lockedBuffer
	ledger string
}

const project = "hzx-interop-shop"

var unitFiles = map[string][]byte{
	"compose.yaml": []byte(`services:
  app:
    build: ./app
    command: ["sh", "-c", "cat /conf/app.conf > /data/seen && cat /run/secrets/token > /data/token-len && sleep 3600"]
    volumes:
      - ./conf/app.conf:/conf/app.conf:ro
      - ./data:/data
    secrets: [token]
  extra:
    image: busybox:1.37
    profiles: [extra]
    command: ["sleep", "3600"]
secrets:
  token:
    file: ./token.txt
`),
	"deploy/prod.yaml": []byte("services:\n  app:\n    environment:\n      STAGE: prod\n"),
	"app/Dockerfile":   []byte("FROM busybox:1.37\nCOPY hello.txt /hello.txt\n"),
	"app/hello.txt":    []byte("hello\n"),
	"conf/app.conf":    []byte("version=1\n"),
	"token.txt":        []byte("tok-very-secret-123\n"),
}

func (x *interop) step(name string, m Manifest, staged []StagedFile, exec, perm string) (json.RawMessage, string, error) {
	g, err := x.d.Grants.IssueBound("usr_interop", []string{perm}, GrantLifetime, m.Binding(exec))
	if err != nil {
		x.t.Fatal(err)
	}
	job := &jobs.Job{ID: m.Revision, Node: x.fp, Resources: []jobs.ResourceRef{{Kind: ResourceKind, ID: m.Project, Node: x.fp}}}
	// As in a compose.deploy job: the check is step 0, the deploy step 1.
	idx := 0
	if name == ActionDeploy {
		idx = 1
	}
	step := &jobs.Step{Index: idx, Name: name}
	rep := &logRep{}
	res, err := x.runner.Execute(context.Background(), job, step, rep, nodes.Action{Name: name, Version: 1,
		Params: map[string]any{"grant": g, "manifest": m, "files": staged}})
	return res, rep.String(), err
}

func (x *interop) manifest(rev string, files map[string][]byte, profiles ...string) (Manifest, []StagedFile) {
	return NewManifest(project, rev, "env:/var/run/docker.sock", "compose.yaml", []string{"deploy/prod.yaml"}, profiles, "", files)
}

func TestInteropDeploysThroughTheRealAgentExecutorAndDocker(t *testing.T) {
	agentBin, privBin := os.Getenv("HAZELMINX_AGENT_BIN"), os.Getenv("HAZELMINX_PRIVILEGED_BIN")
	if agentBin == "" || privBin == "" {
		t.Skip("HAZELMINX_AGENT_BIN and HAZELMINX_PRIVILEGED_BIN not set")
	}
	if os.Getenv("HAZELMINX_DISPOSABLE_HOST") != "1" || os.Geteuid() != 0 {
		t.Skip("deploys containers on the host: needs root and HAZELMINX_DISPOSABLE_HOST=1")
	}
	if _, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skip("no Docker engine on /var/run/docker.sock")
	}
	x := setup(t, agentBin, privBin)
	t.Cleanup(func() {
		out, _ := exec.Command("docker", "compose", "-p", project, "down", "-v", "--rmi", "local").CombinedOutput()
		t.Logf("cleanup: %s", out)
		_ = os.RemoveAll("/var/lib/hazelminx/compose/" + project)
	})
	root := "/var/lib/hazelminx/compose/" + project

	// 1. Host check: read-only, ready.
	m1, s1 := x.manifest("job_interop1", unitFiles)
	res, _, err := x.step(ActionCheck, m1, s1, execCheck, PermCheck)
	if err != nil {
		t.Fatalf("check: %v\n%s", err, x.logs.String())
	}
	var chk struct {
		Ready    bool              `json:"ready"`
		Checks   []HostCheck       `json:"checks"`
		Services []json.RawMessage `json:"services"`
		DataDirs []string          `json:"dataDirs"`
	}
	_ = json.Unmarshal(res, &chk)
	if !chk.Ready || len(chk.DataDirs) != 1 || chk.DataDirs[0] != "data" {
		t.Fatalf("check result: %s", res)
	}
	if _, err := os.Stat(root); err == nil {
		t.Fatal("the check changed the host")
	}

	// 2. Deploy revision 1: multiple -f files, a build, a relative config
	// file, a data directory, a secret file; profile "extra" stays off.
	res, logs, err := x.step(ActionDeploy, m1, s1, execDeploy, PermDeploy)
	if err != nil {
		t.Fatalf("deploy: %v\nstep logs:\n%s\n%s", err, logs, x.logs.String())
	}
	if strings.Contains(logs, "tok-very-secret-123") {
		t.Fatal("a secret value reached the step log")
	}
	var dep struct {
		Containers []struct {
			Service, State, Name string
		} `json:"containers"`
	}
	_ = json.Unmarshal(res, &dep)
	if len(dep.Containers) != 1 || dep.Containers[0].Service != "app" || dep.Containers[0].State != "running" {
		t.Fatalf("containers after deploy: %s", res)
	}
	waitFile(t, root+"/data/data/seen", "version=1\n")
	waitFile(t, root+"/data/data/token-len", "tok-very-secret-123\n")
	out, _ := exec.Command("docker", "inspect", "-f", "{{range .Config.Env}}{{println .}}{{end}}", dep.Containers[0].Name).Output()
	if !strings.Contains(string(out), "STAGE=prod") {
		t.Fatalf("deploy/prod.yaml was not merged: %s", out)
	}
	if fi, err := os.Lstat(root + "/revisions/job_interop1/data"); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the revision's data path must be a link to the data directory: %v %v", fi, err)
	}
	if fi, _ := os.Stat(root + "/revisions/job_interop1"); fi.Mode().Perm() != 0o700 {
		t.Fatalf("revision directory mode %v", fi.Mode())
	}

	// 3. The host ledger recorded the compose project and the directories.
	led, _ := os.ReadFile(filepath.Join(x.ledger, "ledger.jsonl"))
	for _, want := range []string{`"type":"compose_project"`, root + "/revisions/job_interop1", root + "/data", `"phase":"result"`} {
		if !strings.Contains(string(led), want) {
			t.Fatalf("ledger lacks %s:\n%s", want, led)
		}
	}
	if strings.Contains(string(led), "tok-very-secret-123") {
		t.Fatal("a secret value is in the host ledger")
	}

	// 4. Revision 2 with a new config file: the data directory stays, the
	// old revision's files stay for anything still using them (CMP-012).
	files2 := map[string][]byte{}
	for k, v := range unitFiles {
		files2[k] = v
	}
	files2["conf/app.conf"] = []byte("version=2\n")
	m2, s2 := x.manifest("job_interop2", files2)
	if _, logs, err := x.step(ActionDeploy, m2, s2, execDeploy, PermDeploy); err != nil {
		t.Fatalf("deploy 2: %v\n%s", err, logs)
	}
	waitFile(t, root+"/data/data/seen", "version=2\n")
	if b, _ := os.ReadFile(root + "/revisions/job_interop1/conf/app.conf"); string(b) != "version=1\n" {
		t.Fatal("revision 1's files were changed")
	}

	// 5. A grant for the check cannot deploy, and content other than what
	// was signed is refused.
	g, _ := x.d.Grants.IssueBound("usr_interop", []string{PermDeploy}, GrantLifetime, m2.Binding(execCheck))
	job := &jobs.Job{ID: "job_interop3", Node: x.fp, Resources: []jobs.ResourceRef{{Kind: ResourceKind, ID: project, Node: x.fp}}}
	m3, s3 := x.manifest("job_interop3", files2)
	_, err = x.runner.Execute(context.Background(), job, &jobs.Step{Index: 1, Name: ActionDeploy}, &logRep{}, nodes.Action{Name: ActionDeploy, Version: 1,
		Params: map[string]any{"grant": g, "manifest": m3, "files": s3}})
	if err == nil || !strings.Contains(err.Error(), "EXECUTOR_PERMISSION_DENIED") {
		t.Fatalf("a check grant deployed: %v", err)
	}

	// 6. A host port in use is found before anything changes.
	l, err := net.Listen("tcp", "0.0.0.0:18081")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	files4 := map[string][]byte{"compose.yaml": []byte("services:\n  web:\n    image: busybox:1.37\n    ports: [\"18081:80\"]\n    volumes: [/nonexistent-hzx:/x]\n"),
		"deploy/prod.yaml": []byte("services: {}\n")}
	m4, s4 := x.manifest("job_interop4", files4)
	res, _, err = x.step(ActionCheck, m4, s4, execCheck, PermCheck)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res), "PORT_IN_USE") || !strings.Contains(string(res), "BIND_SOURCE_MISSING") || !strings.Contains(string(res), `"ready":false`) {
		t.Fatalf("check with a busy port and a missing bind source: %s", res)
	}
	if _, _, err := x.step(ActionDeploy, m4, s4, execDeploy, PermDeploy); err == nil || !strings.Contains(err.Error(), "host pre-checks failed") {
		t.Fatalf("deploy with a busy port: %v", err)
	}
	if _, err := os.Stat(root + "/revisions/job_interop4"); err == nil {
		t.Fatal("a refused deployment left its revision behind")
	}
	// 7. The executor interpolates as Core analysed: a .env and ${HOME} do
	// not turn a data path into a host path (review P1), and a high-risk
	// setting without the owner's approval is refused on the host.
	files7 := map[string][]byte{
		"compose.yaml": []byte("services:\n  web:\n    image: busybox:1.37\n    command: [\"sleep\", \"3600\"]\n" +
			"    volumes:\n      - ${SRC:-./d1}:/h1\n      - ${HOME:-./d2}/x:/h2\n"),
		".env":             []byte("SRC=/etc\n"),
		"deploy/prod.yaml": []byte("services: {}\n"),
	}
	m7, s7 := x.manifest("job_interop7", files7)
	res, _, err = x.step(ActionCheck, m7, s7, execCheck, PermCheck)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res), "/etc") || strings.Contains(string(res), "/root") || !strings.Contains(string(res), `"d1"`) {
		t.Fatalf(".env or HOME reached the executor's interpolation: %s", res)
	}
	files8 := map[string][]byte{"compose.yaml": []byte("services:\n  web:\n    image: busybox:1.37\n    privileged: true\n    volumes: [/etc:/h:ro]\n"),
		"deploy/prod.yaml": []byte("services: {}\n")}
	m8, s8 := x.manifest("job_interop8", files8)
	res, _, err = x.step(ActionCheck, m8, s8, execCheck, PermCheck)
	if err != nil || !strings.Contains(string(res), "HOST_RISK") || !strings.Contains(string(res), `"ready":true`) {
		t.Fatalf("check of an unapproved high-risk unit: %v %s", err, res)
	}
	if _, _, err := x.step(ActionDeploy, m8, s8, execDeploy, PermDeploy); err == nil || !strings.Contains(err.Error(), "without the owner's approval") {
		t.Fatalf("unapproved privileged deploy: %v", err)
	}

	// 9. An image that is not on the host and not in its registry.
	files9 := map[string][]byte{"compose.yaml": []byte("services:\n  web:\n    image: busybox:hzx-no-such-tag-0\n"),
		"deploy/prod.yaml": []byte("services: {}\n")}
	m9, s9 := x.manifest("job_interop9", files9)
	res, _, err = x.step(ActionCheck, m9, s9, execCheck, PermCheck)
	if err != nil || !strings.Contains(string(res), "IMAGE_UNAVAILABLE") || !strings.Contains(string(res), `"ready":false`) {
		t.Fatalf("check with a missing image: %v %s", err, res)
	}

	// 10. A data path inside another data path is refused (its link would
	// go through a directory a container writes).
	files10 := map[string][]byte{"compose.yaml": []byte("services:\n  web:\n    image: busybox:1.37\n    volumes: [./a:/a, ./a/b:/b]\n"),
		"deploy/prod.yaml": []byte("services: {}\n")}
	m10, s10 := x.manifest("job_interop10", files10)
	if _, _, err := x.step(ActionDeploy, m10, s10, execDeploy, PermDeploy); err == nil || !strings.Contains(err.Error(), "is inside data path") {
		t.Fatalf("nested data paths: %v", err)
	}

	// 11. Ledger: the first revision directory is platform-installed and
	// the Compose project's state names its removal.
	led, _ = os.ReadFile(filepath.Join(x.ledger, "ledger.jsonl"))
	var sawInstalled, sawRemoval bool
	for _, line := range strings.Split(strings.TrimSpace(string(led)), "\n") {
		sawInstalled = sawInstalled || (strings.Contains(line, root+"/revisions/job_interop1") && strings.Contains(line, `"ownership":"platform_installed"`))
		sawRemoval = sawRemoval || (strings.Contains(line, `"type":"compose_project"`) && strings.Contains(line, "down --volumes"))
	}
	if !sawInstalled || !sawRemoval {
		t.Fatalf("ledger ownership %v, removal %v", sawInstalled, sawRemoval)
	}

	// The spool is gone after each step.
	if ents, _ := os.ReadDir(filepath.Join(filepath.Dir(x.ledger), "agent", "compose-spool")); len(ents) != 0 {
		t.Fatalf("spool left behind: %v", ents)
	}
}

func waitFile(t *testing.T, p, want string) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); ; {
		if b, err := os.ReadFile(p); err == nil && string(b) == want {
			return
		}
		if time.Now().After(deadline) {
			b, err := os.ReadFile(p)
			t.Fatalf("%s: %q %v, want %q", p, b, err, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func setup(t *testing.T, agentBin, privBin string) *interop {
	ctx := context.Background()
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

	// A world-traversable work directory: the agent runs as another uid.
	work, err := os.MkdirTemp("/var/lib", "hzx-compose-interop-")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(work, 0o755)
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	state := filepath.Join(work, "agent")
	sock := filepath.Join(work, "privileged.sock")
	pub := filepath.Join(work, "issuer.pub")
	_ = os.WriteFile(pub, issuer.PublicKeyPEM(), 0o644)
	pcfg := filepath.Join(work, "privileged.toml")
	_ = os.WriteFile(pcfg, fmt.Appendf(nil, "compose_spool_dir = %q\n", filepath.Join(state, "compose-spool")), 0o644)
	ledger := filepath.Join(work, "ledger")
	ex := exec.Command(privBin, "--config", pcfg, "--socket", sock, "--issuer-key", pub,
		"--allowed-uid", fmt.Sprint(agentUID), "--ledger-dir", ledger, "--log-format", "text", "run")
	ex.Stdout, ex.Stderr = logs, logs
	if err := ex.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProc(ex) })
	for deadline := time.Now().Add(10 * time.Second); ; {
		if _, err := os.Stat(sock); err == nil {
			// The installer gives the agent's group the socket.
			_ = os.Chown(sock, 0, agentUID)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("executor socket did not appear\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	asAgent := func(args ...string) *exec.Cmd {
		c := exec.Command(agentBin, args...)
		c.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: agentUID, Gid: agentUID}}
		c.Env = []string{"HOME=/tmp", "PATH=/usr/bin:/bin"}
		return c
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.Chown(state, agentUID, agentUID)
	if out, err := asAgent("--config", "/nonexistent.toml", "--state-dir", state, "identity").CombinedOutput(); err != nil {
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
	for name, b := range map[string][]byte{"node.crt": cert, "core-ca.crt": pki.CACertificatePEM()} {
		p := filepath.Join(state, "identity", name)
		_ = os.WriteFile(p, b, 0o644)
		_ = os.Chown(p, agentUID, agentUID)
	}
	cfg := filepath.Join(work, "agent.toml")
	_ = os.WriteFile(cfg, fmt.Appendf(nil, "privileged_socket = %q\n", sock), 0o644)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	ag := asAgent("--config", cfg, "--state-dir", state, "--core-url", "https://127.0.0.1:"+port, "--log-format", "text", "run")
	ag.Stdout, ag.Stderr = logs, logs
	if err := ag.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProc(ag) })
	for deadline := time.Now().Add(15 * time.Second); ; {
		if s, ok := hub.Session(fp); ok && s.Supports(ActionDeploy, 1) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent did not connect with %s\n%s", ActionDeploy, logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	runner := nodes.NewStepRunner(hub)
	runner.StepTTL = 5 * time.Minute
	go func() {
		// Surface executor and agent output on failure.
		<-t.Context().Done()
		if t.Failed() {
			s := bufio.NewScanner(strings.NewReader(logs.String()))
			for s.Scan() {
				t.Log(s.Text())
			}
		}
	}()
	return &interop{t: t, d: &Deployer{Grants: issuer}, runner: runner, fp: fp, logs: logs, ledger: ledger}
}

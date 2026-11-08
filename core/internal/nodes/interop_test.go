package nodes

// Core↔Agent interop: the real hazelminx-agent binary against this hub.
//
// Build the agent with the test-only actions, then point the test at it:
//
//	cargo build -p hazelminx-agent --features test-actions   (in agent/)
//	HAZELMINX_AGENT_BIN=$CARGO_TARGET_DIR/debug/hazelminx-agent go test ./internal/nodes -run Interop -v
//
// Without HAZELMINX_AGENT_BIN the tests are skipped.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

const kindTestSleep = "test.sleep"

// sleepHandler is a job kind backed by the agent's test.sleep action. Each
// run of the action appends a line to counterFile, so the test can tell how
// often the step really ran.
type sleepHandler struct {
	runner      *StepRunner
	millis      int
	counterFile string
	retryable   bool
}

func (h *sleepHandler) Kind() string { return kindTestSleep }
func (h *sleepHandler) Plan(context.Context, *jobs.Job) ([]jobs.StepSpec, error) {
	return []jobs.StepSpec{{Name: "sleep", Retryable: h.retryable}}, nil
}
func (h *sleepHandler) Recheck(context.Context, *jobs.Job, *jobs.Step) error { return nil }
func (h *sleepHandler) Execute(ctx context.Context, j *jobs.Job, s *jobs.Step, rep jobs.Reporter) error {
	_, err := h.runner.Execute(ctx, j, s, rep, Action{Name: kindTestSleep, Version: 1, Params: map[string]any{"millis": h.millis, "counterFile": h.counterFile}})
	return err
}
func (h *sleepHandler) Rollback(context.Context, *jobs.Job, *jobs.Step, jobs.Reporter) error {
	return nil
}
func (h *sleepHandler) Reconcile(ctx context.Context, j *jobs.Job, s *jobs.Step) (jobs.StepReport, error) {
	return h.runner.Reconcile(ctx, j, s)
}
func (h *sleepHandler) Verify(context.Context, *jobs.Job) (jobs.CompletionReport, error) {
	return jobs.CompletionReport{Checks: []jobs.Check{{Name: "slept", Required: true, Passed: true}}}, nil
}

func (h *sleepHandler) runs(t *testing.T) int {
	b, err := os.ReadFile(h.counterFile)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(b, []byte("ran\n"))
}

// interopEnv is one Core (hub + engine) and one agent state directory.
type interopEnv struct {
	t        *testing.T
	bin      string
	core     *testCore
	engine   *jobs.Engine
	sleep    *sleepHandler
	stateDir string
	fp       string
	logs     *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func newInteropEnv(t *testing.T, hub HubOptions) *interopEnv {
	t.Helper()
	bin := os.Getenv("HAZELMINX_AGENT_BIN")
	if bin == "" {
		t.Skip("HAZELMINX_AGENT_BIN not set; build the agent with --features test-actions to run interop tests")
	}
	e := &interopEnv{t: t, bin: bin, engine: newEngine(t), stateDir: t.TempDir(), logs: &syncBuffer{}}
	var reconciler *Reconciler
	hub.OnReady = func(s *Session, inFlight []string) { reconciler.OnReady(s, inFlight) }
	hub.Heartbeat = time.Second
	hub.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	e.core = startCore(t, hub)
	runner := NewStepRunner(e.core.hub)
	runner.ReconcileTimeout = 500 * time.Millisecond
	reconciler = &Reconciler{Engine: e.engine, Runner: runner, Logger: hub.Logger, Interval: 500 * time.Millisecond}
	e.sleep = &sleepHandler{runner: runner, millis: 1500, counterFile: filepath.Join(t.TempDir(), "runs")}
	for _, h := range []jobs.Handler{&InspectHandler{Runner: runner}, e.sleep} {
		if err := e.engine.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.engine.Start(ctx)
	t.Cleanup(e.engine.Stop)
	e.enroll()
	return e
}

// enroll does what ticket 019 will: the agent makes its key, the Core
// certifies it, and the agent gets the Core CA to pin.
func (e *interopEnv) enroll() {
	t := e.t
	out, err := exec.Command(e.bin, "--config", "/nonexistent/agent.toml", "--state-dir", e.stateDir, "identity").CombinedOutput()
	if err != nil {
		t.Fatalf("agent identity: %v\n%s", err, out)
	}
	idDir := filepath.Join(e.stateDir, "identity")
	pubPEM, err := os.ReadFile(filepath.Join(idDir, "node.pub"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pubPEM)
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pub := pubAny.(ed25519.PublicKey)
	e.fp = protocol.Fingerprint(pub)
	if !strings.Contains(string(out), e.fp) {
		t.Fatalf("agent fingerprint %q differs from core's %s", out, e.fp)
	}
	cert, err := e.core.pki.IssueNodeCertificate(pub, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(idDir, "node.crt"), cert, 0o644))
	must(os.WriteFile(filepath.Join(idDir, "core-ca.crt"), e.core.pki.CACertificatePEM(), 0o644))
}

type agentProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// start runs `hazelminx-agent run` against url with an optional config.
func (e *interopEnv) start(coreURL, configTOML string) *agentProc {
	t := e.t
	cfg := "/nonexistent/agent.toml"
	if configTOML != "" {
		cfg = filepath.Join(e.stateDir, "agent.toml")
		if err := os.WriteFile(cfg, []byte(configTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(e.bin, "--config", cfg, "--state-dir", e.stateDir, "--core-url", coreURL, "--log-format", "text", "run")
	cmd.Env = append(os.Environ(), "RUST_LOG=info")
	cmd.Stdout, cmd.Stderr = e.logs, e.logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &agentProc{cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	t.Cleanup(func() { p.kill() })
	return p
}

func (p *agentProc) kill() {
	_ = p.cmd.Process.Signal(syscall.SIGKILL)
	<-p.done
}

func (p *agentProc) stop() {
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.kill()
	}
}

func (e *interopEnv) url(host string) string {
	_, port, _ := net.SplitHostPort(e.core.srv.Listener.Addr().String())
	return "https://" + net.JoinHostPort(host, port)
}

func (e *interopEnv) waitSession(within time.Duration) *Session {
	e.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if s, ok := e.core.hub.Session(e.fp); ok {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("agent did not connect; agent log:\n%s", e.logs.String())
	return nil
}

func (e *interopEnv) submit(kind, reqID string) *jobs.Job {
	e.t.Helper()
	j, _, err := e.engine.Submit(context.Background(), jobs.SubmitRequest{RequestID: reqID, Kind: kind, Node: e.fp})
	if err != nil {
		e.t.Fatal(err)
	}
	return j
}

func (e *interopEnv) waitJob(id string, within time.Duration, states ...jobs.State) *jobs.Job {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	j, err := e.engine.Wait(ctx, id, func(j *jobs.Job) bool {
		for _, s := range states {
			if j.State == s {
				return true
			}
		}
		return false
	})
	if err != nil {
		e.t.Fatalf("job %s stuck in %s (%s), want %v; agent log:\n%s", id, j.State, j.StateReason, states, e.logs.String())
	}
	return j
}

func (e *interopEnv) waitRuns(n int) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for e.sleep.runs(e.t) < n {
		if time.Now().After(deadline) {
			e.t.Fatalf("test.sleep did not start %d time(s); agent log:\n%s", n, e.logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// NOD-002, NOD-011, T-018: the agent dials out, both sides prove identity
// over mTLS, capabilities are exchanged, and a read-only job runs and is
// verified. The Core's address changes (localhost → 127.0.0.1) and the
// agent reconnects on the same trust material.
func TestInteropConnectInspectAndAddressChange(t *testing.T) {
	e := newInteropEnv(t, HubOptions{})
	a := e.start(e.url("localhost"), "")
	s := e.waitSession(15 * time.Second)
	info := s.info()
	if info.Transport != TransportMTLS || info.ProtocolVersion != 1 || !s.Supports(protocol.CapAgentInfo, 1) || !s.Supports(kindTestSleep, 1) {
		t.Fatalf("session %+v", info)
	}
	j := e.waitJob(e.submit(KindNodeInspect, "inspect-1").ID, 15*time.Second, jobs.StateSucceeded, jobs.StateFailed, jobs.StateNeedsAttention)
	if j.State != jobs.StateSucceeded || !j.Completion.RequiredPassed() {
		t.Fatalf("node.inspect: %s %s %+v", j.State, j.StateReason, j.Completion)
	}
	a.stop()
	waitFor(t, "session to end", func() bool { _, ok := e.core.hub.Session(e.fp); return !ok })
	e.start(e.url("127.0.0.1"), "")
	e.waitSession(15 * time.Second)
}

// ENG-003 (agent side): the agent is killed mid-step. After it restarts,
// its journal says the step began and never finished; the core reconciles
// first and, because the step is not safe to repeat, asks a person instead
// of re-running it or calling it done.
func TestInteropAgentCrashMidStepIsNotRerunOrMarkedDone(t *testing.T) {
	e := newInteropEnv(t, HubOptions{})
	a := e.start(e.url("127.0.0.1"), "")
	e.waitSession(15 * time.Second)
	job := e.submit(kindTestSleep, "sleep-crash")
	e.waitRuns(1)
	a.kill()
	j := e.waitJob(job.ID, 10*time.Second, jobs.StateUnknown)
	e.start(e.url("127.0.0.1"), "")
	j = e.waitJob(job.ID, 20*time.Second, jobs.StateNeedsAttention, jobs.StateSucceeded, jobs.StateFailed)
	if j.State != jobs.StateNeedsAttention {
		t.Fatalf("want needs_attention, got %s (%s)", j.State, j.StateReason)
	}
	time.Sleep(2 * time.Second) // longer than the step: a re-run would have counted by now
	if got := e.sleep.runs(t); got != 1 {
		t.Fatalf("step ran %d times; an interrupted non-retryable step must not re-run", got)
	}
	mustContain(t, e.logs.String(), "interrupted")
}

// A retryable step interrupted by an agent crash is reconciled and run
// again (it is declared safe to repeat), and the job then succeeds.
func TestInteropAgentCrashMidRetryableStepRunsAgainAfterReconcile(t *testing.T) {
	e := newInteropEnv(t, HubOptions{})
	e.sleep.retryable = true
	a := e.start(e.url("127.0.0.1"), "")
	e.waitSession(15 * time.Second)
	job := e.submit(kindTestSleep, "sleep-retry")
	e.waitRuns(1)
	a.kill()
	e.waitJob(job.ID, 10*time.Second, jobs.StateUnknown)
	e.start(e.url("127.0.0.1"), "")
	j := e.waitJob(job.ID, 20*time.Second, jobs.StateSucceeded, jobs.StateNeedsAttention, jobs.StateFailed)
	if j.State != jobs.StateSucceeded || e.sleep.runs(t) != 2 {
		t.Fatalf("got %s (%s) after %d runs", j.State, j.StateReason, e.sleep.runs(t))
	}
}

// T-028 / SEC-008: the connection drops while the step runs. The agent
// finishes and journals the step anyway; after reconnecting it re-sends the
// unacknowledged events, the core reconciles, accepts the completion and
// the step is not run a second time.
func TestInteropConnectionLossDuringStepReconcilesWithoutRerun(t *testing.T) {
	e := newInteropEnv(t, HubOptions{})
	e.start(e.url("127.0.0.1"), "")
	s := e.waitSession(15 * time.Second)
	job := e.submit(kindTestSleep, "sleep-drop")
	e.waitRuns(1)
	s.close(1001, "test: network drop")
	e.waitJob(job.ID, 10*time.Second, jobs.StateUnknown)
	j := e.waitJob(job.ID, 30*time.Second, jobs.StateSucceeded, jobs.StateNeedsAttention, jobs.StateFailed)
	if j.State != jobs.StateSucceeded {
		t.Fatalf("got %s (%s)", j.State, j.StateReason)
	}
	if got := e.sleep.runs(t); got != 1 {
		t.Fatalf("step ran %d times after reconnect", got)
	}
}

// T-021 / SEC-011: a Core that only speaks protocol 2 refuses this agent
// (protocol 1). The agent logs both ranges, runs nothing and backs off; the
// refusal with both ranges is visible on the Core.
func TestInteropIncompatibleProtocolIsRefusedAndShown(t *testing.T) {
	e := newInteropEnv(t, HubOptions{Range: protocol.Range{Min: 2, Max: 2}})
	e.start(e.url("127.0.0.1"), "")
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, rej := e.core.hub.Connections()
		if len(rej) > 0 {
			r := rej[0]
			if r.Code != protocol.RejectProtocolIncompatible || r.AgentRange == nil || *r.AgentRange != (protocol.Range{Min: 1, Max: 1}) || r.CoreRange != (protocol.Range{Min: 2, Max: 2}) {
				t.Fatalf("rejection %+v", r)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no rejection recorded; agent log:\n%s", e.logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitFor(t, "agent to log the refusal", func() bool {
		l := e.logs.String()
		return strings.Contains(l, "PROTOCOL_INCOMPATIBLE") && strings.Contains(l, "1..1") && strings.Contains(l, "2..2")
	})
	// The agent waits for an operator, not a reconnect storm.
	time.Sleep(3 * time.Second)
	if _, rej := e.core.hub.Connections(); len(rej) != 1 {
		t.Fatalf("agent retried %d times within 3 s after an incompatibility", len(rej))
	}
	if conns, _ := e.core.hub.Connections(); len(conns) != 0 {
		t.Fatal("incompatible agent has a session")
	}
}

// T-024: behind a TLS-terminating proxy that adds headers claiming another
// node, the agent (core_transport = "proxy") is identified by its own
// certificate and proof; the forged header changes nothing.
func TestInteropThroughTLSTerminatingProxy(t *testing.T) {
	e := newInteropEnv(t, HubOptions{})
	target, _ := url.Parse(e.url("127.0.0.1"))
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = &http.Transport{TLSClientConfig: tlsToCore(t, e.core)}
	director := rp.Director
	rp.Director = func(r *http.Request) {
		director(r)
		r.Header.Set("X-Hazelminx-Node", strings.Repeat("0", 64))
		r.Header.Set("X-Forwarded-Client-Cert", "Hash=forged")
	}
	// The proxy has its own CA and a server certificate for 127.0.0.1,
	// unrelated to the Core's CA (like a public certificate on a CDN edge).
	proxyCA, leaf := proxyCertificates(t)
	proxy := httptest.NewUnstartedServer(rp)
	proxy.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}}
	proxy.StartTLS()
	t.Cleanup(proxy.Close)
	caFile := filepath.Join(t.TempDir(), "proxy-ca.pem")
	if err := os.WriteFile(caFile, proxyCA, 0o644); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(proxy.Listener.Addr().String())
	e.start("https://127.0.0.1:"+port, fmt.Sprintf("core_transport = \"proxy\"\nproxy_ca_file = %q\n", caFile))
	s := e.waitSession(15 * time.Second)
	if s.info().Transport != TransportProxied || s.NodeID() != e.fp {
		t.Fatalf("session %+v", s.info())
	}
	j := e.waitJob(e.submit(KindNodeInspect, "inspect-proxy").ID, 15*time.Second, jobs.StateSucceeded, jobs.StateFailed, jobs.StateNeedsAttention)
	if j.State != jobs.StateSucceeded {
		t.Fatalf("node.inspect through proxy: %s %s", j.State, j.StateReason)
	}
}

// proxyCertificates returns a CA (PEM) and a serverAuth certificate for
// 127.0.0.1 signed by it.
func proxyCertificates(t *testing.T) ([]byte, tls.Certificate) {
	t.Helper()
	caPub, caKey, _ := ed25519.GenerateKey(nil)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test proxy CA"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(nil, caTpl, caTpl, caPub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	pub, key, _ := ed25519.GenerateKey(nil)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "127.0.0.1"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(nil, tpl, ca, pub, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// The connection drops while a 4-second step runs and comes back before
// the step ends. The agent answers the reconcile only when the step
// finishes; the core keeps asking while the session lives, so the job
// settles without another reconnect, and the step ran once.
func TestInteropStepStillRunningAtReconnectSettlesInSession(t *testing.T) {
	e := newInteropEnv(t, HubOptions{})
	e.sleep.millis = 4000
	e.start(e.url("127.0.0.1"), "")
	s := e.waitSession(15 * time.Second)
	job := e.submit(kindTestSleep, "sleep-long")
	e.waitRuns(1)
	s.close(1001, "test: network drop")
	e.waitJob(job.ID, 10*time.Second, jobs.StateUnknown)
	j := e.waitJob(job.ID, 30*time.Second, jobs.StateSucceeded, jobs.StateNeedsAttention, jobs.StateFailed)
	if j.State != jobs.StateSucceeded || e.sleep.runs(t) != 1 {
		t.Fatalf("got %s (%s) after %d runs", j.State, j.StateReason, e.sleep.runs(t))
	}
}

// The agent cannot write its journal (here: the file is read-only). It
// refuses the step with JOURNAL_UNAVAILABLE instead of staying silent, so
// the job ends as a known failure and nothing ran.
func TestInteropUnwritableJournalRefusesTheStep(t *testing.T) {
	e := newInteropEnv(t, HubOptions{})
	e.start(e.url("127.0.0.1"), "")
	e.waitSession(15 * time.Second)
	// Make sure the journal exists, then take write access away.
	j := e.waitJob(e.submit(KindNodeInspect, "inspect-before").ID, 15*time.Second, jobs.StateSucceeded, jobs.StateFailed)
	if j.State != jobs.StateSucceeded {
		t.Fatalf("setup job: %s", j.State)
	}
	jf := filepath.Join(e.stateDir, "jobs", "journal.jsonl")
	if err := os.Chmod(jf, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(jf, 0o600) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	j = e.waitJob(e.submit(kindTestSleep, "sleep-ro").ID, 15*time.Second, jobs.StateFailed, jobs.StateSucceeded, jobs.StateNeedsAttention, jobs.StateUnknown)
	if j.State != jobs.StateFailed || j.ErrorCode != protocol.StepRejectJournalUnavailable || e.sleep.runs(t) != 0 {
		t.Fatalf("got %s %s (%s) after %d runs", j.State, j.ErrorCode, j.StateReason, e.sleep.runs(t))
	}
}

// The journal becomes unwritable while a step runs, so its outcome cannot
// be recorded. The agent ends the session instead of going quiet; after it
// reconnects the core reconciles, finds the step began without a recorded
// end and, the step not being safe to repeat, asks a person.
func TestInteropJournalFailingMidStepEndsSessionForReconcile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	e := newInteropEnv(t, HubOptions{})
	e.start(e.url("127.0.0.1"), "")
	first := e.waitSession(15 * time.Second)
	job := e.submit(kindTestSleep, "sleep-journal-dies")
	e.waitRuns(1)
	jf := filepath.Join(e.stateDir, "jobs", "journal.jsonl")
	if err := os.Chmod(jf, 0o400); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the agent to end its session", func() bool { s, ok := e.core.hub.Session(e.fp); return !ok || s != first })
	if err := os.Chmod(jf, 0o600); err != nil {
		t.Fatal(err)
	}
	j := e.waitJob(job.ID, 30*time.Second, jobs.StateNeedsAttention, jobs.StateSucceeded, jobs.StateFailed)
	if j.State != jobs.StateNeedsAttention || e.sleep.runs(t) != 1 {
		t.Fatalf("got %s (%s) after %d runs", j.State, j.StateReason, e.sleep.runs(t))
	}
	mustContain(t, e.logs.String(), "could not be journaled")
}

func tlsToCore(t *testing.T, core *testCore) *tls.Config {
	return &tls.Config{RootCAs: coreCAPool(t, core.pki), ServerName: protocol.CoreServerName}
}

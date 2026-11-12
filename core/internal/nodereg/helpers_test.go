package nodereg

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// memSecrets is enough of the secrets service for the node PKI.
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
	s := secrets.Secret{ID: in.Name, Name: in.Name}
	m.list = append(m.list, s)
	m.vals[s.ID] = append([]byte(nil), in.Value...)
	return s, nil
}

func (m *memSecrets) Reveal(_ context.Context, req secrets.RevealRequest) ([]byte, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.vals[req.ID]
	if !ok {
		return nil, 0, errors.New("no such secret")
	}
	return append([]byte(nil), v...), 1, nil
}

// fakeChannel stands in for the hub.
type fakeChannel struct {
	mu           sync.Mutex
	conns        map[string]nodes.ConnectionInfo
	disconnected []string

	renewed    map[string][]byte
	renewCount map[string]int
	renewErr   error
}

func newFakeChannel() *fakeChannel {
	return &fakeChannel{conns: map[string]nodes.ConnectionInfo{}, renewed: map[string][]byte{}, renewCount: map[string]int{}}
}

func (f *fakeChannel) Connections() ([]nodes.ConnectionInfo, []nodes.Rejection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []nodes.ConnectionInfo
	for _, c := range f.conns {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeChannel) Disconnect(fp, _ string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.conns[fp]
	delete(f.conns, fp)

	if ok {
		f.disconnected = append(f.disconnected, fp)
	}
	return ok
}

func (f *fakeChannel) RenewCertificate(_ context.Context, fp string, pem []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.conns[fp]; !ok {
		return nodes.ErrNotConnected
	}
	if f.renewErr != nil {
		return f.renewErr
	}
	f.renewed[fp] = pem
	f.renewCount[fp]++
	return nil
}

func (f *fakeChannel) connect(fp string, seen time.Time, serial string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conns[fp] = nodes.ConnectionInfo{NodeID: fp, SessionID: "ses_" + fp[:6], LastSeenAt: seen, ConnectedAt: seen, CertificateSerial: serial,
		AgentVersion: "0.1.0", ProtocolVersion: 1, AgentRange: protocol.Range{Min: 1, Max: 1}, Transport: nodes.TransportMTLS, RemoteAddr: "203.0.113.61:40000"}
}

type env struct {
	t     *testing.T
	reg   *Registry
	pki   *nodes.PKI
	ch    *fakeChannel
	clock time.Time
	mu    sync.Mutex
}

func (e *env) now() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.clock }
func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.clock = e.clock.Add(d)
	e.mu.Unlock()
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{t: t, ch: newFakeChannel(), clock: time.Date(2026, 11, 12, 3, 40, 0, 0, time.UTC)}
	e.pki, err = nodes.LoadOrCreatePKI(context.Background(), &memSecrets{vals: map[string][]byte{}}, e.now)
	if err != nil {
		t.Fatal(err)
	}
	e.reg, err = New(Options{DB: st.DB, PKI: e.pki, IssuerKeyPEM: []byte("ISSUER-PUB"), Now: e.now, Jobs: fakeJobs{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e.reg.AttachChannel(e.ch)
	return e
}

type fakeJobs struct{}

func (fakeJobs) Get(_ context.Context, id string) (*jobs.Job, error) {
	return &jobs.Job{ID: id, State: jobs.StateRunning}, nil
}

func (fakeJobs) List(_ context.Context, o jobs.ListOptions) ([]*jobs.Job, error) {
	return []*jobs.Job{{ID: "job_running", Kind: "host.package.install", Node: o.Node, State: jobs.StateRunning}}, nil
}

// host is a node key and the facts its agent reports.
type host struct {
	pub       ed25519.PublicKey
	fp        string
	hostname  string
	machineID string
}

func newHost(hostname, machineID string) host {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	return host{pub: pub, fp: protocol.Fingerprint(pub), hostname: hostname, machineID: machineID}
}

func (h host) request(code string) protocol.EnrollRequest {
	return protocol.EnrollRequest{Code: code, NodePublicKey: base64.RawURLEncoding.EncodeToString(h.pub), Hostname: h.hostname,
		MachineID: h.machineID, OS: "Debian 13", Arch: "arm64", AgentVersion: "0.1.0"}
}

func (e *env) newCode(in CreateInput) (Enrollment, string) {
	e.t.Helper()
	if in.Actor == "" {
		in.Actor = "usr_owner"
	}
	en, code, err := e.reg.CreateEnrollment(context.Background(), in)
	if err != nil {
		e.t.Fatal(err)
	}
	return en, code.String()
}

func (e *env) claim(h host, code string) (int, protocol.EnrollResponse) {
	e.t.Helper()
	return e.reg.Claim(context.Background(), h.request(code), "203.0.113.61")
}

func (f *fakeChannel) renewals(fp string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renewCount[fp]
}

// enroll runs the whole flow for h and returns the approved node.
func (e *env) enroll(h host, in CreateInput, approve ApproveInput) Node {
	e.t.Helper()
	en, code := e.newCode(in)
	if st, r := e.claim(h, code); st != 202 || r.Status != protocol.EnrollPending {
		e.t.Fatalf("claim: %d %+v", st, r)
	}
	if approve.Actor == "" {
		approve.Actor = "usr_owner"
	}
	n, err := e.reg.Approve(context.Background(), en.ID, approve)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

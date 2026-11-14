package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/engines"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

type stubProber struct {
	mu    sync.Mutex
	fps   []string
	probe func(fp string) error
}

func (p *stubProber) Probe(_ context.Context, fp string) error {
	p.mu.Lock()
	p.fps = append(p.fps, fp)
	p.mu.Unlock()
	return p.probe(fp)
}

func TestHostEnginesOverTheAPI(t *testing.T) {
	hs := newHarness(t)
	pki, err := nodes.LoadOrCreatePKI(context.Background(), &pkiSecrets{vals: map[string][]byte{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), t.TempDir()+"/nodes.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reg, err := nodereg.New(nodereg.Options{DB: st.DB, PKI: pki, IssuerKeyPEM: []byte("pub"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	hs.srv.RegisterNodes(reg)
	es := engines.NewStore(st.DB, nil)
	prober := &stubProber{}
	hs.srv.RegisterHostEngines(reg, es, prober)
	owner := hs.bootstrap()
	claim := func(code string) { claimAs(t, reg, code, "h", "") }
	a := approveNode(t, owner, claim, "a")
	node, err := reg.GetNode(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}

	// Never probed: reported false, nothing made up.
	r := owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/engines", nil, nil)
	if r.Code != 200 || r.Body["reported"] != false || r.Body["report"] != nil || r.Body["lastError"] != nil {
		t.Fatalf("before any report: %d %s", r.Code, r.Raw)
	}

	// Refresh while the node is offline: 200, previous view, NODE_OFFLINE.
	prober.probe = func(string) error { return nodes.ErrNodeOffline }
	r = owner.do(http.MethodPost, "/management/v1/nodes/"+a+"/engines/refresh", nil, nil)
	if r.Code != 200 || r.Body["reported"] != false || obj(r.Body["lastError"])["code"] != "NODE_OFFLINE" {
		t.Fatalf("refresh offline: %d %s", r.Code, r.Raw)
	}
	if len(prober.fps) != 1 || prober.fps[0] != node.Fingerprint {
		t.Fatalf("probed %v, want the node's fingerprint", prober.fps)
	}

	// Refresh that stores a report.
	b, err := os.ReadFile("../../../engineering/contracts/fixtures/messages/engines_report.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := protocol.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	rep := m.(*protocol.EnginesReport)
	rep.CollectedAt = time.Now().UTC().Truncate(time.Second)
	prober.probe = func(string) error { return es.RecordReport(context.Background(), a, rep) }
	r = owner.do(http.MethodPost, "/management/v1/nodes/"+a+"/engines/refresh", nil, nil)
	if r.Code != 200 || r.Body["reported"] != true || r.Body["lastError"] != nil {
		t.Fatalf("refresh: %d %s", r.Code, r.Raw)
	}
	report := obj(r.Body["report"])
	envs, _ := report["environments"].([]any)
	if len(envs) != 4 {
		t.Fatalf("environments: %s", r.Raw)
	}
	tim, other := obj(envs[2]), obj(envs[3])
	if tim["ownerName"] != "tim" || tim["mode"] != "rootless" || other["ownerUid"] != float64(1001) || other["ownerName"] != nil || other["kindConfirmed"] != false {
		t.Fatalf("rootless environments must stay apart with their owners: %v / %v", tim, other)
	}
	engs, _ := report["engines"].([]any)
	if len(engs) != 3 || obj(engs[2])["state"] != protocol.EngineNotInstalled || obj(engs[2])["binary"] != nil {
		t.Fatalf("engines: %v", engs)
	}

	// A failed refresh keeps the report and says why.
	prober.probe = func(string) error {
		_ = es.RecordError(context.Background(), a, protocol.EnginesExecutorUnreachable, "cannot reach the privileged executor")
		return errors.New("engines: EXECUTOR_UNREACHABLE")
	}
	r = owner.do(http.MethodPost, "/management/v1/nodes/"+a+"/engines/refresh", nil, nil)
	if r.Code != 200 || r.Body["reported"] != true || obj(r.Body["lastError"])["code"] != protocol.EnginesExecutorUnreachable {
		t.Fatalf("failed refresh: %d %s", r.Code, r.Raw)
	}

	// Unknown node: 404 for both routes.
	if r := owner.do(http.MethodGet, "/management/v1/nodes/nope/engines", nil, nil); r.Code != 404 {
		t.Fatalf("unknown node: %d", r.Code)
	}
	if r := owner.do(http.MethodPost, "/management/v1/nodes/nope/engines/refresh", nil, nil); r.Code != 404 {
		t.Fatalf("unknown node refresh: %d", r.Code)
	}

	// An auditor reads but cannot make the node probe.
	auditor := hs.createUser(owner, "aud", iam.RoleAuditor, iam.Scope{Kind: iam.ScopeGlobal, ID: "*"}, "10.0.0.9")
	if r := auditor.do(http.MethodGet, "/management/v1/nodes/"+a+"/engines", nil, nil); r.Code != 200 {
		t.Fatalf("auditor read: %d %s", r.Code, r.Raw)
	}
	before := len(prober.fps)
	if r := auditor.do(http.MethodPost, "/management/v1/nodes/"+a+"/engines/refresh", nil, nil); r.Code != 403 {
		t.Fatalf("auditor refresh: %d %s", r.Code, r.Raw)
	}
	if len(prober.fps) != before {
		t.Fatal("a refused refresh must not probe")
	}
}

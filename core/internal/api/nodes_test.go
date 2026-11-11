package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

type pkiSecrets struct {
	mu   sync.Mutex
	list []secrets.Secret
	vals map[string][]byte
}

func (m *pkiSecrets) List(context.Context) ([]secrets.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]secrets.Secret(nil), m.list...), nil
}
func (m *pkiSecrets) Create(_ context.Context, in secrets.CreateInput) (secrets.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := secrets.Secret{ID: in.Name, Name: in.Name}
	m.list = append(m.list, s)
	m.vals[s.ID] = in.Value
	return s, nil
}
func (m *pkiSecrets) Reveal(_ context.Context, r secrets.RevealRequest) ([]byte, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.vals[r.ID]
	if !ok {
		return nil, 0, errors.New("missing")
	}
	return v, 1, nil
}

func nodesHarness(t *testing.T) (*harness, *nodereg.Registry) {
	t.Helper()
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
	return hs, reg
}

// claimAs uses a code the way the agent would, with a fresh key.
func claimAs(t *testing.T, reg *nodereg.Registry, code, hostname, machineID string) string {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	st, r := reg.Claim(context.Background(), protocol.EnrollRequest{Code: code, NodePublicKey: base64.RawURLEncoding.EncodeToString(pub),
		Hostname: hostname, MachineID: machineID, OS: "Debian 13", Arch: "amd64", AgentVersion: "0.1.0"}, "203.0.113.5")
	if st != 202 {
		t.Fatalf("claim: %d %+v", st, r)
	}
	return r.EnrollmentID
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

func TestNodeEnrollmentAndLifecycleOverTheAPI(t *testing.T) {
	hs, reg := nodesHarness(t)
	if r := hs.client("192.0.2.1").do(http.MethodPost, "/management/v1/node-enrollments", map[string]any{}, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", r.Code)
	}
	owner := hs.bootstrap()

	r := owner.do(http.MethodPost, "/management/v1/node-enrollments", map[string]any{"name": "lab-pi-02", "tags": []string{"lab"}, "ttlSeconds": 600}, nil)
	if r.Code != http.StatusCreated {
		t.Fatalf("create code: %d %s", r.Code, r.Raw)
	}
	code, _ := r.Body["code"].(string)
	en := obj(r.Body["enrollment"])
	if !strings.HasPrefix(code, "HZX-") || en["state"] != nodereg.EnrollmentWaiting {
		t.Fatalf("created %s", r.Raw)
	}
	enID := en["id"].(string)
	// The code is shown once: reading the enrollment does not reveal it.
	if g := owner.do(http.MethodGet, "/management/v1/node-enrollments/"+enID, nil, nil); g.Code != 200 || strings.Contains(g.Raw, code) {
		t.Fatalf("get enrollment leaks the code or fails: %d %s", g.Code, g.Raw)
	}
	claimAs(t, reg, code, "lab-pi-02", "5c1e")
	r = owner.do(http.MethodGet, "/management/v1/node-enrollments?state=pending_approval", nil, nil)
	items, _ := r.Body["items"].([]any)
	if r.Code != 200 || len(items) != 1 || obj(obj(items[0])["host"])["hostname"] != "lab-pi-02" {
		t.Fatalf("pending list: %d %s", r.Code, r.Raw)
	}
	r = owner.do(http.MethodPost, "/management/v1/node-enrollments/"+enID+"/approve", map[string]any{}, nil)
	if r.Code != 200 {
		t.Fatalf("approve: %d %s", r.Code, r.Raw)
	}
	node := obj(r.Body["node"])
	id := node["id"].(string)
	if node["name"] != "lab-pi-02" || obj(node["connection"])["state"] != nodereg.ConnNeverConnected {
		t.Fatalf("node %s", r.Raw)
	}

	g := owner.do(http.MethodGet, "/management/v1/nodes/"+id, nil, nil)
	etag := g.Hdr.Get("ETag")
	if g.Code != 200 || etag == "" {
		t.Fatalf("get node: %d %s", g.Code, g.Raw)
	}
	if p := owner.do(http.MethodPatch, "/management/v1/nodes/"+id, map[string]any{"name": "pi"}, nil); p.Code != http.StatusPreconditionRequired {
		t.Fatalf("patch without If-Match: %d", p.Code)
	}
	p := owner.do(http.MethodPatch, "/management/v1/nodes/"+id, map[string]any{"name": "pi", "tags": []string{"lab", "arm"}}, map[string]string{"If-Match": etag})
	if p.Code != 200 || obj(p.Body["node"])["name"] != "pi" {
		t.Fatalf("patch: %d %s", p.Code, p.Raw)
	}
	if p := owner.do(http.MethodPatch, "/management/v1/nodes/"+id, map[string]any{"name": "x"}, map[string]string{"If-Match": etag}); p.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale patch: %d", p.Code)
	}
	if l := owner.do(http.MethodGet, "/management/v1/nodes?tag=arm", nil, nil); l.Code != 200 || len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("search: %d %s", l.Code, l.Raw)
	}

	m := owner.do(http.MethodPut, "/management/v1/nodes/"+id+"/maintenance", map[string]any{"enabled": true, "reason": "disk swap"}, nil)
	if m.Code != 200 || obj(obj(m.Body["node"])["maintenance"])["enabled"] != true {
		t.Fatalf("maintenance: %d %s", m.Code, m.Raw)
	}
	if x := owner.do(http.MethodPost, "/management/v1/nodes/"+id+"/remove", map[string]any{"mode": nodereg.RemoveAgent}, nil); x.Code != http.StatusConflict || x.Body["code"] != CodeNotAvailable {
		t.Fatalf("remove-agent: %d %s", x.Code, x.Raw)
	}
	v := owner.do(http.MethodPost, "/management/v1/nodes/"+id+"/revoke", map[string]any{"reason": "test"}, nil)
	rev := obj(obj(v.Body["node"])["revocation"])
	if v.Code != 200 || rev["pending"] != true || obj(v.Body["node"])["state"] != nodereg.StateRevoked {
		t.Fatalf("revoke: %d %s", v.Code, v.Raw)
	}
	if v := owner.do(http.MethodPost, "/management/v1/nodes/"+id+"/revoke", map[string]any{}, nil); v.Code != http.StatusConflict || v.Body["code"] != CodePrecheckFailed {
		t.Fatalf("revoke twice: %d %s", v.Code, v.Raw)
	}
	if x := owner.do(http.MethodPost, "/management/v1/nodes/"+id+"/remove", map[string]any{"mode": nodereg.Unmanage}, nil); x.Code != 200 {
		t.Fatalf("unmanage: %d %s", x.Code, x.Raw)
	}
}

func TestEnrollmentChoiceAndRejectOverTheAPI(t *testing.T) {
	hs, reg := nodesHarness(t)
	owner := hs.bootstrap()
	create := func() (string, string) {
		r := owner.do(http.MethodPost, "/management/v1/node-enrollments", map[string]any{}, nil)
		return obj(r.Body["enrollment"])["id"].(string), r.Body["code"].(string)
	}
	id1, c1 := create()
	claimAs(t, reg, c1, "web", "same-machine")
	owner.do(http.MethodPost, "/management/v1/node-enrollments/"+id1+"/approve", map[string]any{}, nil)
	id2, c2 := create()
	claimAs(t, reg, c2, "web", "same-machine")
	r := owner.do(http.MethodPost, "/management/v1/node-enrollments/"+id2+"/approve", map[string]any{}, nil)
	if r.Code != http.StatusConflict || r.Body["code"] != CodePrecheckFailed || obj(r.Body["details"])["matches"] == nil {
		t.Fatalf("approve needing a choice: %d %s", r.Code, r.Raw)
	}
	r = owner.do(http.MethodPost, "/management/v1/node-enrollments/"+id2+"/reject", map[string]any{}, nil)
	if r.Code != 200 || obj(r.Body["enrollment"])["state"] != nodereg.EnrollmentRejected {
		t.Fatalf("reject: %d %s", r.Code, r.Raw)
	}
	id3, _ := create()
	if r := owner.do(http.MethodDelete, "/management/v1/node-enrollments/"+id3, nil, nil); r.Code != 200 || obj(r.Body["enrollment"])["state"] != nodereg.EnrollmentRevoked {
		t.Fatalf("withdraw: %d %s", r.Code, r.Raw)
	}
	if r := owner.do(http.MethodPost, "/management/v1/node-enrollments", map[string]any{"ttlSeconds": 10}, nil); r.Code != http.StatusBadRequest {
		t.Fatalf("ttl too short: %d", r.Code)
	}
}

// T-014 for nodes: an account scoped to one node cannot see another, and
// read-only roles cannot change anything.
func TestNodeScopesOverTheAPI(t *testing.T) {
	hs, reg := nodesHarness(t)
	owner := hs.bootstrap()
	ids := []string{}
	for _, name := range []string{"a", "b"} {
		r := owner.do(http.MethodPost, "/management/v1/node-enrollments", map[string]any{"name": name}, nil)
		enID, code := obj(r.Body["enrollment"])["id"].(string), r.Body["code"].(string)
		claimAs(t, reg, code, name, "")
		r = owner.do(http.MethodPost, "/management/v1/node-enrollments/"+enID+"/approve", map[string]any{}, nil)
		ids = append(ids, obj(r.Body["node"])["id"].(string))
	}
	op := hs.createUser(owner, "opa", iam.RoleHostOperator, iam.Scope{Kind: iam.ScopeNode, ID: ids[0]}, "10.0.0.20")
	if r := op.do(http.MethodGet, "/management/v1/nodes/"+ids[0], nil, nil); r.Code != 200 {
		t.Fatalf("own node: %d", r.Code)
	}
	if r := op.do(http.MethodGet, "/management/v1/nodes/"+ids[1], nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("other node: %d", r.Code)
	}
	if r := op.do(http.MethodGet, "/management/v1/nodes", nil, nil); r.Code != 200 || len(r.Body["items"].([]any)) != 1 {
		t.Fatalf("list: %d %s", r.Code, r.Raw)
	}
	if r := op.do(http.MethodPost, "/management/v1/nodes/"+ids[0]+"/revoke", map[string]any{}, nil); r.Code != http.StatusForbidden {
		t.Fatalf("host operator revoking: %d", r.Code)
	}
	if r := op.do(http.MethodPost, "/management/v1/node-enrollments", map[string]any{}, nil); r.Code != http.StatusForbidden {
		t.Fatalf("host operator creating a code: %d", r.Code)
	}
	if r := op.do(http.MethodGet, "/management/v1/node-enrollments", nil, nil); r.Code != http.StatusForbidden {
		t.Fatalf("host operator listing codes: %d", r.Code)
	}
}

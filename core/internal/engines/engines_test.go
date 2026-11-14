package engines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st.DB
}

func addNode(t *testing.T, db *sql.DB, id, fp string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO nodes (id, name, fingerprint, public_key, state, cert_serial, cert_not_after, created_at, updated_at)
		VALUES (?, ?, ?, x'00', 'active', '1', '2027-01-01T00:00:00Z', '2026-11-01T00:00:00Z', '2026-11-01T00:00:00Z')`, id, id, fp); err != nil {
		t.Fatal(err)
	}
}

// fixture is the contract sample, decoded by the strict channel decoder.
func fixture(t *testing.T) *protocol.EnginesReport {
	t.Helper()
	b, err := os.ReadFile("../../../engineering/contracts/fixtures/messages/engines_report.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := protocol.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return m.(*protocol.EnginesReport)
}

func TestValidateRefusesWhatTheDecoderLetsThrough(t *testing.T) {
	ok := fixture(t)
	if err := Validate(&ok.Report); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	cases := map[string]func(r *protocol.EngineReport){
		"no lists":          func(r *protocol.EngineReport) { r.Environments = nil },
		"repeated kind":     func(r *protocol.EngineReport) { r.Engines[1].Kind = protocol.EngineDocker },
		"unknown kind":      func(r *protocol.EngineReport) { r.Engines[0].Kind = "lxc" },
		"unknown state":     func(r *protocol.EngineReport) { r.Engines[0].State = "fine" },
		"repeated env id":   func(r *protocol.EngineReport) { r.Environments[1].ID = r.Environments[0].ID },
		"env not installed": func(r *protocol.EngineReport) { r.Environments[0].State = protocol.EngineNotInstalled },
		"env mode":          func(r *protocol.EngineReport) { r.Environments[0].Mode = "root" },
		"env kind k8s":      func(r *protocol.EngineReport) { r.Environments[0].Kind = "kubernetes" },
	}
	for name, mutate := range cases {
		r := fixture(t).Report
		mutate(&r)
		if err := Validate(&r); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStoreKeepsLatestReportAndLastError(t *testing.T) {
	db := openDB(t)
	addNode(t, db, "n1", "fp1")
	now := time.Date(2026, 11, 14, 8, 1, 0, 0, time.UTC)
	s := NewStore(db, func() time.Time { return now })
	ctx := context.Background()
	if v, err := s.Get(ctx, "n1"); err != nil || v != nil {
		t.Fatalf("never probed: %v %v", v, err)
	}
	// A failure before any report: no report, the error is kept.
	if err := s.RecordError(ctx, "n1", protocol.EnginesExecutorUnreachable, strings.Repeat("x", 2000)); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get(ctx, "n1")
	if v.Report != nil || v.LastError == nil || v.LastError.Code != protocol.EnginesExecutorUnreachable || len(v.LastError.Message) != maxErrorMessage {
		t.Fatalf("error only: %+v", v)
	}
	m := fixture(t)
	if err := s.RecordReport(ctx, "n1", m); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Get(ctx, "n1")
	if v.Report == nil || v.LastError != nil || !v.CollectedAt.Equal(m.CollectedAt) || !v.ReceivedAt.Equal(now) {
		t.Fatalf("after report: %+v", v)
	}
	if len(v.Report.Environments) != 4 || v.Report.Environments[0].Kind != protocol.EngineDocker || v.Report.Environments[3].OwnerName != nil {
		t.Fatalf("report round trip: %+v", v.Report.Environments)
	}
	// A failure keeps the last report.
	_ = s.RecordError(ctx, "n1", CodeNoAnswer, "late")
	v, _ = s.Get(ctx, "n1")
	if v.Report == nil || v.LastError.Code != CodeNoAnswer {
		t.Fatalf("error after report: %+v", v)
	}
	// An older report (late answer) does not replace a newer one.
	old := fixture(t)
	old.CollectedAt = m.CollectedAt.Add(-time.Hour)
	old.Report.Environments = old.Report.Environments[:1]
	_ = s.RecordReport(ctx, "n1", old)
	v, _ = s.Get(ctx, "n1")
	if len(v.Report.Environments) != 4 {
		t.Fatal("older report replaced the newer one")
	}
	// A report from a clock far ahead is refused.
	ahead := fixture(t)
	ahead.CollectedAt = now.Add(time.Hour)
	if err := s.RecordReport(ctx, "n1", ahead); !errors.Is(err, ErrValidation) {
		t.Fatalf("ahead: %v", err)
	}
	b, _ := json.Marshal(v.Report)
	if strings.Contains(string(b), `"ownerName":""`) {
		t.Fatal("a missing owner name must stay null")
	}
}

type fakeHub struct {
	mu    sync.Mutex
	calls int
	grant protocol.ExecutorGrant
	reply func() (*protocol.EnginesReport, error)
	inFly atomic.Int32
	max   atomic.Int32
}

func (h *fakeHub) ProbeEngines(ctx context.Context, fp string, g protocol.ExecutorGrant) (*protocol.EnginesReport, error) {
	n := h.inFly.Add(1)
	defer h.inFly.Add(-1)
	if n > h.max.Load() {
		h.max.Store(n)
	}
	h.mu.Lock()
	h.calls++
	h.grant = g
	h.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	return h.reply()
}

type fakeGrants struct{ perms []string }

func (g *fakeGrants) Issue(subject string, perms []string, lifetime time.Duration) (protocol.ExecutorGrant, error) {
	g.perms = perms
	return protocol.ExecutorGrant{ID: "g1", Subject: subject, Permissions: perms}, nil
}

func TestProberRecordsEachOutcome(t *testing.T) {
	db := openDB(t)
	addNode(t, db, "n1", "fp1")
	s := NewStore(db, nil)
	hub := &fakeHub{}
	g := &fakeGrants{}
	p := &Prober{Store: s, Grants: g, Hub: hub, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		NodeID: func(_ context.Context, fp string) (string, error) {
			if fp != "fp1" {
				return "", errors.New("unknown")
			}
			return "n1", nil
		}}
	ctx := context.Background()
	lastCode := func() string {
		v, _ := s.Get(ctx, "n1")
		if v == nil || v.LastError == nil {
			return ""
		}
		return v.LastError.Code
	}

	hub.reply = func() (*protocol.EnginesReport, error) { return nil, nodes.ErrNodeOffline }
	if err := p.Probe(ctx, "fp1"); !errors.Is(err, nodes.ErrNodeOffline) || lastCode() != "" {
		t.Fatalf("offline: %v %q", err, lastCode())
	}
	hub.reply = func() (*protocol.EnginesReport, error) { return nil, nodes.ErrEnginesUnsupported }
	if _ = p.Probe(ctx, "fp1"); lastCode() != CodeAgentUnsupported {
		t.Fatal(lastCode())
	}
	hub.reply = func() (*protocol.EnginesReport, error) {
		return nil, &nodes.EnginesUnavailableError{Code: protocol.EnginesExecutorRefused, Message: "GrantInvalid"}
	}
	if _ = p.Probe(ctx, "fp1"); lastCode() != protocol.EnginesExecutorRefused {
		t.Fatal(lastCode())
	}
	hub.reply = func() (*protocol.EnginesReport, error) { return nil, context.DeadlineExceeded }
	if _ = p.Probe(ctx, "fp1"); lastCode() != CodeNoAnswer {
		t.Fatal(lastCode())
	}
	hub.reply = func() (*protocol.EnginesReport, error) {
		r := fixture(t)
		r.CollectedAt = time.Now().UTC().Truncate(time.Second)
		r.Report.Engines = append(r.Report.Engines, r.Report.Engines[0])
		return r, nil
	}
	if _ = p.Probe(ctx, "fp1"); lastCode() != CodeInvalidReport {
		t.Fatal(lastCode())
	}
	hub.reply = func() (*protocol.EnginesReport, error) {
		r := fixture(t)
		r.CollectedAt = time.Now().UTC().Truncate(time.Second)
		return r, nil
	}
	if err := p.Probe(ctx, "fp1"); err != nil || lastCode() != "" {
		t.Fatalf("report: %v %q", err, lastCode())
	}
	if len(g.perms) != 1 || g.perms[0] != GrantPermission || hub.grant.Subject != GrantSubject {
		t.Fatalf("grant: %v %+v", g.perms, hub.grant)
	}
	if err := p.Probe(ctx, "nobody"); err == nil {
		t.Fatal("unknown fingerprint")
	}

	// Concurrent probes of one node run one at a time.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = p.Probe(ctx, "fp1") }()
	}
	wg.Wait()
	if hub.max.Load() != 1 {
		t.Fatalf("%d probes of one node ran at once", hub.max.Load())
	}
}

package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/hostmetrics"
	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

func approveNode(t *testing.T, owner *client, reg func(code string), name string) string {
	t.Helper()
	r := owner.do(http.MethodPost, "/management/v1/node-enrollments", map[string]any{"name": name}, nil)
	enID, code := obj(r.Body["enrollment"])["id"].(string), r.Body["code"].(string)
	reg(code)
	r = owner.do(http.MethodPost, "/management/v1/node-enrollments/"+enID+"/approve", map[string]any{}, nil)
	if r.Code != 200 {
		t.Fatalf("approve: %d %s", r.Code, r.Raw)
	}
	return obj(r.Body["node"])["id"].(string)
}

func f64(v float64) *float64 { return &v }
func u64(v uint64) *uint64   { return &v }

func TestHostMetricsOverTheAPI(t *testing.T) {
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
	hm := hostmetrics.NewStore(st.DB, nil)
	hs.srv.RegisterHostMetrics(reg, hm)
	owner := hs.bootstrap()
	claim := func(code string) { claimAs(t, reg, code, "h", "") }
	a, b := approveNode(t, owner, claim, "a"), approveNode(t, owner, claim, "b")

	// Nothing reported yet: 200 with reported false, not zeros.
	r := owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/host", nil, nil)
	if r.Code != 200 || r.Body["reported"] != false || r.Body["facts"] != nil {
		t.Fatalf("facts before any report: %d %s", r.Code, r.Raw)
	}
	r = owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/metrics/latest", nil, nil)
	if r.Code != 200 || r.Body["reported"] != false || r.Body["sample"] != nil {
		t.Fatalf("latest before any report: %d %s", r.Code, r.Raw)
	}

	now := time.Now().UTC().Truncate(time.Second)
	ctx := context.Background()
	if err := hm.RecordFacts(ctx, a, &protocol.HostFacts{CollectedAt: now, Facts: protocol.HostFactsData{
		Kernel:   protocol.Fact[string]{Value: func() *string { s := "6.12.48"; return &s }(), Source: "/proc/sys/kernel/osrelease"},
		Timezone: protocol.Fact[string]{Source: "/etc/localtime"}}}); err != nil {
		t.Fatal(err)
	}
	rep := &protocol.MetricsReport{}
	for i, off := range []int{-300, -290, -280, -60, -50} {
		s := protocol.MetricsSample{Seq: uint64(i + 1), CollectedAt: now.Add(time.Duration(off) * time.Second), IntervalSeconds: 10,
			CPU:         protocol.CPU{Total: &protocol.CPUUsage{Usage: float64(10 + i)}, Cores: []protocol.CoreUsage{}},
			Pressure:    protocol.Pressure{State: protocol.PressureDisabled},
			Limits:      protocol.Limits{Tasks: u64(92), PidMax: u64(100)},
			Filesystems: []protocol.Filesystem{{MountPoint: "/", State: "ok", Inodes: "ok", TotalBytes: u64(100), FreeBytes: u64(40), InodesTotal: u64(10), InodesFree: u64(0)}},
			Disks:       []protocol.Disk{}, Networks: []protocol.Network{}, Temperatures: []protocol.Temperature{}}
		rep.Samples = append(rep.Samples, s)
	}
	if _, err := hm.RecordReport(ctx, a, rep); err != nil {
		t.Fatal(err)
	}

	r = owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/host", nil, nil)
	facts := obj(r.Body["facts"])
	if r.Code != 200 || r.Body["reported"] != true || obj(facts["kernel"])["value"] != "6.12.48" || obj(facts["timezone"])["value"] != nil || obj(facts["timezone"])["source"] != "/etc/localtime" {
		t.Fatalf("facts: %d %s", r.Code, r.Raw)
	}
	r = owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/metrics/latest", nil, nil)
	x := obj(r.Body["exhaustion"])
	mp := obj(x["memoryPressure"])
	fs := obj(x["filesystems"].([]any)[0])
	if r.Code != 200 || r.Body["stale"] != false || obj(x["pids"])["exceeded"] != true || mp["state"] != "disabled" || mp["someAvg60"] != nil || fs["inodesExhausted"] != true {
		t.Fatalf("latest: %d %s", r.Code, r.Raw)
	}

	// History: raw by default, oldest first, a gap where samples stop.
	r = owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/metrics/history?series=cpu.usage&limit=3", nil, nil)
	items, _ := r.Body["items"].([]any)
	if r.Code != 200 || len(items) != 3 || obj(items[0])["gapBefore"] != true || obj(items[1])["gapBefore"] != false || obj(items[0])["resolutionSeconds"] != float64(10) {
		t.Fatalf("history page 1: %d %s", r.Code, r.Raw)
	}
	cursor := obj(r.Body["page"])["nextCursor"].(string)
	r = owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/metrics/history?cursor="+cursor, nil, nil)
	items, _ = r.Body["items"].([]any)
	if r.Code != 200 || len(items) != 2 || obj(items[0])["gapBefore"] != true || obj(items[0])["value"] != float64(13) {
		t.Fatalf("history page 2: %d %s", r.Code, r.Raw)
	}
	r = owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/metrics/history?series=cpu.usage&resolution=5m", nil, nil)
	if items, _ = r.Body["items"].([]any); r.Code != 200 || len(items) == 0 || obj(items[0])["resolutionSeconds"] != float64(300) {
		t.Fatalf("5m history: %d %s", r.Code, r.Raw)
	}

	for _, bad := range []string{
		"",                                 // series is required
		"?series=nope",                     // not a series
		"?series=cpu.usage&from=yesterday", // not RFC 3339
		"?series=cpu.usage&from=2026-01-01T00:00:00Z&to=2026-01-05T00:00:00Z", // longer than raw keeps
		"?series=cpu.usage&from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z", // from after to
		"?series=cpu.usage&resolution=1h",
	} {
		if r := owner.do(http.MethodGet, "/management/v1/nodes/"+a+"/metrics/history"+bad, nil, nil); r.Code != http.StatusBadRequest || r.Body["code"] != CodeValidationFailed {
			t.Fatalf("%q: %d %s", bad, r.Code, r.Raw)
		}
	}

	// Scoped to node a: node b and unknown nodes are not found.
	op := hs.createUser(owner, "opa", iam.RoleHostOperator, iam.Scope{Kind: iam.ScopeNode, ID: a}, "10.0.0.20")
	if r := op.do(http.MethodGet, "/management/v1/nodes/"+a+"/metrics/latest", nil, nil); r.Code != 200 {
		t.Fatalf("own node: %d", r.Code)
	}
	for _, p := range []string{"/host", "/metrics/latest", "/metrics/history?series=cpu.usage"} {
		if r := op.do(http.MethodGet, "/management/v1/nodes/"+b+p, nil, nil); r.Code != http.StatusNotFound {
			t.Fatalf("other node %s: %d", p, r.Code)
		}
		if r := owner.do(http.MethodGet, "/management/v1/nodes/nope"+p, nil, nil); r.Code != http.StatusNotFound {
			t.Fatalf("unknown node %s: %d", p, r.Code)
		}
	}
	if r := hs.client("192.0.2.9").do(http.MethodGet, "/management/v1/nodes/"+a+"/host", nil, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", r.Code)
	}
}

// Staleness follows Core's clock (receivedAt), not the agent's
// collectedAt: a host whose clock runs behind is fresh while it reports,
// and one whose clock runs ahead is stale once it stops.
func TestHostMetricsStaleUsesCoreClock(t *testing.T) {
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
	hs.srv.RegisterHostMetrics(reg, hostmetrics.NewStore(st.DB, nil))
	owner := hs.bootstrap()
	claim := func(code string) { claimAs(t, reg, code, "h", "") }
	behind, ahead := approveNode(t, owner, claim, "behind"), approveNode(t, owner, claim, "ahead")

	now := time.Now().UTC().Truncate(time.Second)
	one := func(at time.Time) *protocol.MetricsReport {
		return &protocol.MetricsReport{Samples: []protocol.MetricsSample{{Seq: 1, CollectedAt: at, IntervalSeconds: 10,
			CPU: protocol.CPU{Cores: []protocol.CoreUsage{}}, Pressure: protocol.Pressure{State: protocol.PressureUnsupported},
			Filesystems: []protocol.Filesystem{}, Disks: []protocol.Disk{}, Networks: []protocol.Network{}, Temperatures: []protocol.Temperature{}}}}
	}
	ctx := context.Background()
	// Received now, collected by a clock 40 s behind.
	if _, err := hostmetrics.NewStore(st.DB, func() time.Time { return now }).RecordReport(ctx, behind, one(now.Add(-40*time.Second))); err != nil {
		t.Fatal(err)
	}
	// Received a minute ago, collected by a clock 4 minutes ahead.
	minuteAgo := now.Add(-time.Minute)
	if _, err := hostmetrics.NewStore(st.DB, func() time.Time { return minuteAgo }).RecordReport(ctx, ahead, one(minuteAgo.Add(4*time.Minute))); err != nil {
		t.Fatal(err)
	}
	if r := owner.do(http.MethodGet, "/management/v1/nodes/"+behind+"/metrics/latest", nil, nil); r.Code != 200 || r.Body["stale"] != false {
		t.Fatalf("clock behind, reporting: %d %s", r.Code, r.Raw)
	}
	if r := owner.do(http.MethodGet, "/management/v1/nodes/"+ahead+"/metrics/latest", nil, nil); r.Code != 200 || r.Body["stale"] != true {
		t.Fatalf("clock ahead, stopped a minute ago: %d %s", r.Code, r.Raw)
	}
}

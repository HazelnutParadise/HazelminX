package hostmetrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

var t0 = time.Date(2026, 11, 14, 3, 0, 0, 0, time.UTC)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// openDB opens a migrated store with one node row, "node-1".
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	addNode(t, st.DB, "node-1", "fp-1")
	return st.DB
}

func addNode(t *testing.T, db *sql.DB, id, fp string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO nodes (id, name, fingerprint, public_key, state, cert_serial, cert_not_after, created_at, updated_at)
		VALUES (?, ?, ?, x'00', 'active', '1', '2027-01-01T00:00:00Z', '2026-11-01T00:00:00Z', '2026-11-01T00:00:00Z')`, id, id, fp); err != nil {
		t.Fatal(err)
	}
}

func fixtureReport(t *testing.T) *protocol.MetricsReport {
	t.Helper()
	b, err := os.ReadFile("../../../engineering/contracts/fixtures/messages/metrics_report.json")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := protocol.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return msg.(*protocol.MetricsReport)
}

func ptr[T any](v T) *T { return &v }

// sample is a minimal sample at t with cpu usage u (nil: first sample).
func sample(seq uint64, at time.Time, u *float64) protocol.MetricsSample {
	s := protocol.MetricsSample{Seq: seq, CollectedAt: at, IntervalSeconds: 10, Pressure: protocol.Pressure{State: protocol.PressureUnsupported},
		Filesystems: []protocol.Filesystem{}, Disks: []protocol.Disk{}, Networks: []protocol.Network{}, Temperatures: []protocol.Temperature{}}
	if u != nil {
		s.CPU.Total = &protocol.CPUUsage{Usage: *u}
	}
	return s
}

func history(t *testing.T, st *Store, q HistoryQuery) []Point {
	t.Helper()
	pts, err := st.History(context.Background(), q, paging.Query{Limit: 100, Sort: []paging.SortKey{{Field: "at"}}})
	if err != nil {
		t.Fatal(err)
	}
	return pts
}

func TestRecordReportStoresLatestPointsAndRollups(t *testing.T) {
	db := openDB(t)
	c := &clock{now: t0.Add(time.Minute)}
	st := NewStore(db, c.Now)
	ctx := context.Background()
	rep := fixtureReport(t)
	seq, err := st.RecordReport(ctx, "node-1", rep)
	if err != nil || seq != 42 {
		t.Fatalf("record: %d %v", seq, err)
	}
	l, err := st.Latest(ctx, "node-1")
	if err != nil || l == nil {
		t.Fatalf("latest: %v %v", l, err)
	}
	if l.Sample.Seq != 42 || l.DroppedSamples != 3 || l.IntervalSeconds != 10 || !l.CollectedAt.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("latest %+v", l)
	}
	// The first sample had no CPU value and no available memory: no point
	// for them (not a zero).
	cpu := history(t, st, HistoryQuery{NodeID: "node-1", Series: "cpu.usage", Resolution: ResolutionRaw, From: t0, To: t0.Add(time.Hour)})
	if len(cpu) != 1 || cpu[0].Value != 12.5 || !cpu[0].At.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("cpu points %+v", cpu)
	}
	mem := history(t, st, HistoryQuery{NodeID: "node-1", Series: "memory.availableBytes", Resolution: ResolutionRaw, From: t0, To: t0.Add(time.Hour)})
	if len(mem) != 1 {
		t.Fatalf("memory points %+v", mem)
	}
	fs := history(t, st, HistoryQuery{NodeID: "node-1", Series: "filesystem.inodesUsed", Key: "/", Resolution: ResolutionRaw, From: t0, To: t0.Add(time.Hour)})
	if len(fs) != 1 || fs[0].Value != 6553600 {
		t.Fatalf("inodes %+v", fs)
	}
	// Unresponsive NFS and btrfs inodes have no points; loopback has none.
	for _, q := range []HistoryQuery{
		{Series: "filesystem.usedBytes", Key: "/mnt/nas"},
		{Series: "filesystem.inodesUsed", Key: "/srv/pool"},
		{Series: "network.rxBytesPerSecond", Key: "lo"},
		{Series: "disk.readLatencyMs", Key: "sda"},
	} {
		q.NodeID, q.Resolution, q.From, q.To = "node-1", ResolutionRaw, t0, t0.Add(time.Hour)
		if p := history(t, st, q); len(p) != 0 {
			t.Fatalf("%s %s: %+v", q.Series, q.Key, p)
		}
	}
	load := history(t, st, HistoryQuery{NodeID: "node-1", Series: "load.one", Resolution: Resolution5m, From: t0, To: t0.Add(time.Hour)})
	if len(load) != 1 || load[0].Samples != 2 || load[0].Value != 0.25 || load[0].ResolutionSeconds != 300 {
		t.Fatalf("rollup %+v", load)
	}

	// The agent resends the same report (the acknowledgement was lost):
	// nothing is counted twice.
	if _, err := st.RecordReport(ctx, "node-1", rep); err != nil {
		t.Fatal(err)
	}
	load = history(t, st, HistoryQuery{NodeID: "node-1", Series: "load.one", Resolution: Resolution5m, From: t0, To: t0.Add(time.Hour)})
	if load[0].Samples != 2 {
		t.Fatalf("resend counted again: %+v", load)
	}
}

func TestHistoryMarksGapsInsteadOfFilling(t *testing.T) {
	db := openDB(t)
	st := NewStore(db, (&clock{now: t0}).Now)
	ctx := context.Background()
	// Samples at 0, 10, 20 s, then nothing until 120 s (agent offline).
	rep := &protocol.MetricsReport{}
	for i, off := range []int{0, 10, 20, 120, 130} {
		rep.Samples = append(rep.Samples, sample(uint64(i+1), t0.Add(time.Duration(off)*time.Second), ptr(float64(10*(i+1)))))
	}
	if _, err := st.RecordReport(ctx, "node-1", rep); err != nil {
		t.Fatal(err)
	}
	pts := history(t, st, HistoryQuery{NodeID: "node-1", Series: "cpu.usage", Resolution: ResolutionRaw, From: t0, To: t0.Add(time.Hour)})
	if len(pts) != 5 {
		t.Fatalf("points %+v", pts)
	}
	var gaps []bool
	for _, p := range pts {
		gaps = append(gaps, p.GapBefore)
		if p.Value == 0 {
			t.Fatalf("a zero was filled in: %+v", pts)
		}
	}
	want := []bool{true, false, false, true, false}
	for i := range want {
		if gaps[i] != want[i] {
			t.Fatalf("gapBefore %v, want %v", gaps, want)
		}
	}
	// A window that starts after the first points still knows the point
	// before it, and the second page continues after the first.
	q := HistoryQuery{NodeID: "node-1", Series: "cpu.usage", Resolution: ResolutionRaw, From: t0.Add(5 * time.Second), To: t0.Add(time.Hour)}
	p1, _ := st.History(ctx, q, paging.Query{Limit: 2, Sort: []paging.SortKey{{Field: "at"}}})
	if len(p1) != 2 || p1[0].GapBefore {
		t.Fatalf("page 1 %+v", p1)
	}
	p2, _ := st.History(ctx, q, paging.Query{Limit: 2, Sort: []paging.SortKey{{Field: "at"}}, After: []string{p1[1].SortValue()}})
	if len(p2) != 2 || !p2[0].GapBefore || !p2[0].At.Equal(t0.Add(120*time.Second)) {
		t.Fatalf("page 2 %+v", p2)
	}
	if n, err := st.CountHistory(ctx, q); err != nil || n != 4 {
		t.Fatalf("count %d %v", n, err)
	}
	// Rollups: the bucket [0, 300) holds all five, average of 10..50.
	r := history(t, st, HistoryQuery{NodeID: "node-1", Series: "cpu.usage", Resolution: Resolution5m, From: t0, To: t0.Add(time.Hour)})
	if len(r) != 1 || r[0].Value != 30 || r[0].Min != 10 || r[0].Max != 50 || r[0].Samples != 5 {
		t.Fatalf("rollup %+v", r)
	}
}

func TestRecordReportRejectsBadReports(t *testing.T) {
	st := NewStore(openDB(t), nil)
	ctx := context.Background()
	if _, err := st.RecordReport(ctx, "node-1", &protocol.MetricsReport{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty report: %v", err)
	}
	s := sample(1, t0, nil)
	s.IntervalSeconds = 0
	if _, err := st.RecordReport(ctx, "node-1", &protocol.MetricsReport{Samples: []protocol.MetricsSample{s}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("interval 0: %v", err)
	}
	// An unknown node is refused by the foreign key; nothing is stored.
	if _, err := st.RecordReport(ctx, "node-x", &protocol.MetricsReport{Samples: []protocol.MetricsSample{sample(1, t0, ptr(1.0))}}); err == nil {
		t.Fatal("report of an unknown node stored")
	}
}

func TestOlderLatestAndFactsDoNotReplaceNewer(t *testing.T) {
	st := NewStore(openDB(t), (&clock{now: t0}).Now)
	ctx := context.Background()
	newer := &protocol.MetricsReport{Samples: []protocol.MetricsSample{sample(9, t0.Add(time.Minute), ptr(9.0))}}
	older := &protocol.MetricsReport{Samples: []protocol.MetricsSample{sample(1, t0, ptr(1.0))}}
	if _, err := st.RecordReport(ctx, "node-1", newer); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordReport(ctx, "node-1", older); err != nil {
		t.Fatal(err)
	}
	if l, _ := st.Latest(ctx, "node-1"); l.Sample.Seq != 9 {
		t.Fatalf("latest went back to %d", l.Sample.Seq)
	}
	b, _ := os.ReadFile("../../../engineering/contracts/fixtures/messages/host_facts.json")
	msg, err := protocol.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	f := msg.(*protocol.HostFacts)
	if err := st.RecordFacts(ctx, "node-1", f); err != nil {
		t.Fatal(err)
	}
	old := *f
	old.CollectedAt = f.CollectedAt.Add(-time.Hour)
	old.Facts.Hostname.Value = ptr("old-name")
	if err := st.RecordFacts(ctx, "node-1", &old); err != nil {
		t.Fatal(err)
	}
	got, err := st.Facts(ctx, "node-1")
	if err != nil || *got.Facts.Hostname.Value != "edge-01" || got.Facts.MemoryTotalBytes.Value != nil || got.Facts.Kernel.Source != "/proc/sys/kernel/osrelease" {
		t.Fatalf("facts %+v %v", got, err)
	}
	// Not provided stays null through storage.
	raw, _ := json.Marshal(got.Facts.MemoryTotalBytes)
	if string(raw) != `{"value":null,"source":"/proc/meminfo"}` {
		t.Fatalf("memoryTotalBytes %s", raw)
	}
	if f, err := st.Facts(ctx, "nobody"); f != nil || err != nil {
		t.Fatalf("facts of a node that never reported: %+v %v", f, err)
	}
}

func TestPruneKeepsRetention(t *testing.T) {
	c := &clock{now: t0}
	st := NewStore(openDB(t), c.Now)
	ctx := context.Background()
	rep := &protocol.MetricsReport{Samples: []protocol.MetricsSample{sample(1, t0, ptr(5.0)), sample(2, t0.Add(47*time.Hour), ptr(6.0))}}
	if _, err := st.RecordReport(ctx, "node-1", rep); err != nil {
		t.Fatal(err)
	}
	c.now = t0.Add(49 * time.Hour)
	if err := st.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	all := HistoryQuery{NodeID: "node-1", Series: "cpu.usage", Resolution: ResolutionRaw, From: t0.Add(-time.Hour), To: t0.Add(100 * time.Hour)}
	if p := history(t, st, all); len(p) != 1 || p[0].Value != 6 {
		t.Fatalf("raw after prune %+v", p)
	}
	all.Resolution = Resolution5m
	if p := history(t, st, all); len(p) != 2 {
		t.Fatalf("rollups are kept longer: %+v", p)
	}
	c.now = t0.Add(93 * 24 * time.Hour)
	if err := st.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	all.To = c.now
	if p := history(t, st, all); len(p) != 0 {
		t.Fatalf("rollups after 90 days %+v", p)
	}
}

package hoststorage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

func u64(v uint64) *uint64 { return &v }

func openStore(t *testing.T, now time.Time) (*Store, string) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir()+"/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB.Exec(`INSERT INTO nodes (id, name, fingerprint, public_key, state, cert_serial, cert_not_after, created_at, updated_at)
		VALUES ('n1', 'n1', 'fp', x'00', 'active', 's', '2027-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	return NewStore(st.DB, func() time.Time { return now }), "n1"
}

func TestEvaluateThresholds(t *testing.T) {
	gib := uint64(1 << 30)
	d := DefaultRule()
	for _, tc := range []struct {
		name  string
		c     Capacity
		g     *float64
		want  map[string]string
		empty bool
	}{
		{"plenty", Capacity{TotalBytes: u64(100 * gib), AvailableBytes: u64(50 * gib), InodesTotal: u64(100), InodesFree: u64(50)}, nil, nil, true},
		{"warn space", Capacity{TotalBytes: u64(100 * gib), AvailableBytes: u64(10 * gib)}, nil, map[string]string{"availableLow": "warning"}, false},
		{"critical percent", Capacity{TotalBytes: u64(100 * gib), AvailableBytes: u64(4 * gib)}, nil, map[string]string{"availableLow": "critical"}, false},
		// 1.5 GiB of 30 GiB is 5%: not under 5%, but under the 2 GiB floor.
		{"critical floor", Capacity{TotalBytes: u64(30 * gib), AvailableBytes: u64(3 * gib / 2)}, nil, map[string]string{"availableLow": "critical"}, false},
		// A 512 MiB /boot/efi with 40% free is fine: the floor does not apply.
		{"small fs ignores floor", Capacity{TotalBytes: u64(gib / 2), AvailableBytes: u64(gib / 5)}, nil, nil, true},
		// T-165: space left, inodes gone.
		{"inodes exhausted", Capacity{TotalBytes: u64(100 * gib), AvailableBytes: u64(40 * gib), InodesTotal: u64(1000), InodesFree: u64(0)}, nil, map[string]string{"inodesLow": "critical"}, false},
		{"full soon", Capacity{TotalBytes: u64(100 * gib), AvailableBytes: u64(30 * gib)}, ptr(float64(3 * gib)), map[string]string{"fullSoon": "warning"}, false},
		{"shrinking is not full soon", Capacity{TotalBytes: u64(100 * gib), AvailableBytes: u64(30 * gib)}, ptr(-float64(gib)), nil, true},
		// Values the host did not give raise nothing; they are not zero.
		{"not provided", Capacity{}, ptr(float64(gib)), nil, true},
	} {
		got := map[string]string{}
		for _, a := range Evaluate(d, tc.c, tc.g) {
			got[a.Kind] = a.Level
		}
		if tc.empty && len(got) != 0 {
			t.Errorf("%s: want no alerts, got %v", tc.name, got)
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%s: %s = %q, want %q (all %v)", tc.name, k, got[k], v, got)
			}
		}
	}
	off := Rule{}
	if a := Evaluate(off, Capacity{TotalBytes: u64(100), AvailableBytes: u64(0)}, nil); len(a) != 0 {
		t.Errorf("a rule with every threshold off raises nothing, got %v", a)
	}
}

func ptr[T any](v T) *T { return &v }

func TestGrowth(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if Growth([]time.Time{t0, t0.Add(time.Hour)}, []float64{0, 10}) != nil {
		t.Error("less than a day of history gives no trend")
	}
	var at []time.Time
	var used []float64
	for i := 0; i <= 48; i++ {
		at = append(at, t0.Add(time.Duration(i)*time.Hour))
		used = append(used, 1000+float64(i)*50) // 1200 per day
	}
	if g := Growth(at, used); g == nil || *g < 1199.9 || *g > 1200.1 {
		t.Errorf("growth = %v, want 1200/day", g)
	}
}

func TestRulePrecedenceAndValidation(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	s, node := openStore(t, now)
	ctx := context.Background()
	r, err := s.RuleFor(ctx, node, "/data")
	if err != nil || !r.BuiltIn {
		t.Fatalf("no rules: built-in default, got %+v %v", r, err)
	}
	put := func(n, m string, warn float64) {
		if _, err := s.PutRule(ctx, Rule{NodeID: n, MountPoint: m, WarnAvailablePercent: &warn}, "u"); err != nil {
			t.Fatal(err)
		}
	}
	put("", "", 20)
	put("", "/data", 30)
	put(node, "", 40)
	put(node, "/data", 50)
	for _, tc := range []struct {
		node, mount string
		want        float64
	}{{node, "/data", 50}, {node, "/other", 40}, {"x", "/data", 30}, {"x", "/other", 20}} {
		r, err := s.RuleFor(ctx, tc.node, tc.mount)
		if err != nil || r.WarnAvailablePercent == nil || *r.WarnAvailablePercent != tc.want {
			t.Errorf("%s %s: %+v %v, want %v", tc.node, tc.mount, r, err, tc.want)
		}
	}
	rules, _ := s.Rules(ctx)
	if len(rules) != 4 || rules[0].BuiltIn || rules[0].NodeID != "" || rules[0].MountPoint != "" {
		t.Errorf("stored default comes first and replaces the built-in: %+v", rules)
	}
	if err := s.DeleteRule(ctx, "x", "/none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing: %v", err)
	}
	bad := 101.0
	if _, err := s.PutRule(ctx, Rule{WarnAvailablePercent: &bad}, "u"); !errors.Is(err, ErrValidation) {
		t.Errorf("101%%: %v", err)
	}
}

func TestRecordRejectsBadReports(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	s, node := openStore(t, now)
	ctx := context.Background()
	inv := &protocol.HostStorage{CollectedAt: now, Storage: protocol.StorageInventory{
		Mounts: []protocol.Mount{{MountPoint: "/", Configs: []uint32{3}}}}}
	if err := s.RecordInventory(ctx, node, inv); !errors.Is(err, ErrValidation) {
		t.Errorf("config index out of range: %v", err)
	}
	inv.Storage.Mounts[0].Configs = nil
	inv.CollectedAt = now.Add(time.Hour)
	if err := s.RecordInventory(ctx, node, inv); !errors.Is(err, ErrValidation) {
		t.Errorf("clock ahead: %v", err)
	}
	// A later inventory wins; an older resend changes nothing.
	inv.CollectedAt = now
	inv.Storage.Unreadable = []string{"new"}
	if err := s.RecordInventory(ctx, node, inv); err != nil {
		t.Fatal(err)
	}
	old := &protocol.HostStorage{CollectedAt: now.Add(-time.Minute), Storage: protocol.StorageInventory{Unreadable: []string{"old"}}}
	if err := s.RecordInventory(ctx, node, old); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Inventory(ctx, node)
	if got == nil || len(got.Storage.Unreadable) != 1 || got.Storage.Unreadable[0] != "new" {
		t.Errorf("late resend replaced the inventory: %+v", got)
	}
	for _, cats := range [][]protocol.CategoryUsage{
		{{Category: "media", Bytes: 1}},
		{{Category: protocol.CategoryAppData, Bytes: 1}, {Category: protocol.CategoryAppData, Bytes: 2}},
		{{Category: protocol.CategoryOther, Bytes: 1}},
	} {
		u := &protocol.StorageUsage{Usage: protocol.StorageUsageData{StartedAt: now, Filesystems: []protocol.FilesystemUsage{{MountPoint: "/", Categories: cats}}}}
		if err := s.RecordUsage(ctx, node, u); !errors.Is(err, ErrValidation) {
			t.Errorf("categories %v: %v", cats, err)
		}
	}
}

func TestFSUsageOrderAndOther(t *testing.T) {
	f := protocol.FilesystemUsage{State: "complete", UsedBytes: u64(100), OtherBytes: u64(10), Categories: []protocol.CategoryUsage{
		{Category: protocol.CategoryOSAndLogs, Bytes: 50}, {Category: protocol.CategoryAppData, Bytes: 40}, {Category: protocol.CategoryDatabase, Bytes: 0}}}
	got := fsUsage(f, time.Time{})
	var names []string
	var sum uint64
	for _, c := range got.Categories {
		names = append(names, c.Category)
		sum += c.Bytes
	}
	if len(names) != 3 || names[0] != "appData" || names[1] != "osAndLogs" || names[2] != "other" || sum != 100 {
		t.Errorf("order and sum: %v %d", names, sum)
	}
	f.State, f.OtherBytes = "partial", nil
	if got := fsUsage(f, time.Time{}); len(got.Categories) != 2 {
		t.Errorf("partial lists no other: %+v", got.Categories)
	}
}

package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/hostmetrics"
	"github.com/HazelnutParadise/hazelminx/core/internal/hoststorage"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodereg"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

func TestHostStorageOverTheAPI(t *testing.T) {
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
	hst := hoststorage.NewStore(st.DB, nil)
	hs.srv.RegisterHostStorage(reg, hst, hm)
	owner := hs.bootstrap()
	a := approveNode(t, owner, func(code string) { claimAs(t, reg, code, "h", "") }, "a")
	path := "/management/v1/nodes/" + a + "/storage"

	// Nothing reported: reported false and empty lists, not zeros.
	r := owner.do(http.MethodGet, path, nil, nil)
	if r.Code != 200 || r.Body["reported"] != false || len(r.Body["mounts"].([]any)) != 0 || r.Body["backupStaging"] != "notApplicable" {
		t.Fatalf("before any report: %d %s", r.Code, r.Raw)
	}

	ctx := context.Background()
	raw, err := os.ReadFile("../../../engineering/contracts/fixtures/messages/host_storage.json")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := protocol.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	inv := msg.(*protocol.HostStorage)
	now := time.Now().UTC().Truncate(time.Second)
	inv.CollectedAt = now
	if err := hst.RecordInventory(ctx, a, inv); err != nil {
		t.Fatal(err)
	}
	sample := protocol.MetricsSample{Seq: 1, CollectedAt: now, IntervalSeconds: 10, CPU: protocol.CPU{Cores: []protocol.CoreUsage{}},
		Pressure: protocol.Pressure{State: protocol.PressureDisabled},
		Filesystems: []protocol.Filesystem{
			{MountPoint: "/mnt/data", State: "ok", Inodes: "ok", TotalBytes: u64(1000), FreeBytes: u64(30), AvailableBytes: u64(30), UsedBytes: u64(970), InodesTotal: u64(100), InodesFree: u64(5)},
			{MountPoint: "/mnt/scratch", State: "ok", Inodes: "notApplicable", TotalBytes: u64(1000), FreeBytes: u64(900), AvailableBytes: u64(900), UsedBytes: u64(100)},
		},
		Disks: []protocol.Disk{}, Networks: []protocol.Network{}, Temperatures: []protocol.Temperature{}}
	if _, err := hm.RecordReport(ctx, a, &protocol.MetricsReport{Samples: []protocol.MetricsSample{sample}}); err != nil {
		t.Fatal(err)
	}
	if err := hst.RecordUsage(ctx, a, &protocol.StorageUsage{Usage: protocol.StorageUsageData{StartedAt: now, Filesystems: []protocol.FilesystemUsage{
		{MountPoint: "/mnt/data", Dev: "8:1", State: "complete", UsedBytes: u64(970), OtherBytes: u64(370), Categories: []protocol.CategoryUsage{
			{Category: protocol.CategoryContainerImages, Bytes: 100, Paths: []string{"/var/lib/docker"}},
			{Category: protocol.CategoryAppData, Bytes: 500, Paths: []string{"/var/lib/docker/volumes"}},
		}},
		{MountPoint: "/mnt/scratch", Dev: "8:17", State: "partial", UsedBytes: u64(100), Categories: []protocol.CategoryUsage{}, UnreadableDirs: 1},
	}}}); err != nil {
		t.Fatal(err)
	}

	r = owner.do(http.MethodGet, path, nil, nil)
	if r.Code != 200 || r.Body["reported"] != true {
		t.Fatalf("view: %d %s", r.Code, r.Raw)
	}
	devs := r.Body["devices"].([]any)
	if h := obj(obj(devs[0])["health"]); h["state"] != "notCollected" || obj(devs[1])["health"] != nil {
		t.Fatalf("disk health must say not collected, partitions carry none: %s", r.Raw)
	}
	mounts := r.Body["mounts"].([]any)
	data, scratch := obj(mounts[0]), obj(mounts[1])
	if data["mountPoint"] != "/mnt/data" || data["reference"] != "uuid" || scratch["reference"] != "device" {
		t.Fatalf("mounts: %s", r.Raw)
	}
	// Categories in the fixed order, other last, adding up to used space.
	u := obj(data["usage"])
	cats := u["categories"].([]any)
	var sum float64
	var names []string
	for _, c := range cats {
		names = append(names, obj(c)["category"].(string))
		sum += obj(c)["bytes"].(float64)
	}
	if u["state"] != "complete" || len(names) != 3 || names[0] != "appData" || names[1] != "containerImages" || names[2] != "other" || sum != u["usedBytes"].(float64) {
		t.Fatalf("usage of /mnt/data: %v %s", names, r.Raw)
	}
	// A partial measurement has no "other": it would not be honest.
	if su := obj(scratch["usage"]); su["state"] != "partial" || len(su["categories"].([]any)) != 0 {
		t.Fatalf("partial usage: %s", r.Raw)
	}
	alerts := func(m map[string]any) map[string]string {
		out := map[string]string{}
		for _, a := range m["alerts"].([]any) {
			out[obj(a)["kind"].(string)] = obj(a)["level"].(string)
		}
		return out
	}
	if got := alerts(data); got["availableLow"] != "critical" || got["inodesLow"] != "warning" {
		t.Fatalf("alerts of /mnt/data: %v", got)
	}
	if got := alerts(scratch); len(got) != 0 {
		t.Fatalf("no inode table and plenty of space: no alerts, got %v", got)
	}
	var problems []string
	for _, p := range r.Body["problems"].([]any) {
		problems = append(problems, obj(p)["kind"].(string)+" "+obj(p)["mountPoint"].(string))
	}
	if !slices.Contains(problems, "deviceName /mnt/scratch") || !slices.Contains(problems, "missingDevice /mnt/old") {
		t.Fatalf("problems: %v", problems)
	}

	// Rules: the built-in default, then a node-specific override.
	r = owner.do(http.MethodGet, "/management/v1/storage/alert-rules", nil, nil)
	items := r.Body["items"].([]any)
	if r.Code != 200 || len(items) != 1 || obj(items[0])["builtIn"] != true {
		t.Fatalf("rules: %d %s", r.Code, r.Raw)
	}
	r = owner.do(http.MethodPut, "/management/v1/storage/alert-rules", map[string]any{"nodeId": a, "mountPoint": "/mnt/data", "warnAvailablePercent": 1}, nil)
	if r.Code != 200 || r.Body["builtIn"] != false || r.Body["criticalAvailablePercent"] != nil {
		t.Fatalf("put rule: %d %s", r.Code, r.Raw)
	}
	r = owner.do(http.MethodGet, path, nil, nil)
	data = obj(r.Body["mounts"].([]any)[0])
	if got := alerts(data); len(got) != 0 || obj(data["rule"])["nodeId"] != a {
		t.Fatalf("the specific rule applies and turned the thresholds off: %v %s", got, r.Raw)
	}
	for _, bad := range []map[string]any{
		{"warnAvailablePercent": 150},
		{"mountPoint": "relative"},
		{"warnFullWithinDays": 0},
		{"unknown": 1},
	} {
		if r = owner.do(http.MethodPut, "/management/v1/storage/alert-rules", bad, nil); r.Code != 400 {
			t.Fatalf("bad rule %v: %d %s", bad, r.Code, r.Raw)
		}
	}
	if r = owner.do(http.MethodPut, "/management/v1/storage/alert-rules", map[string]any{"nodeId": "nope"}, nil); r.Code != 404 {
		t.Fatalf("unknown node: %d %s", r.Code, r.Raw)
	}
	if r = owner.do(http.MethodDelete, "/management/v1/storage/alert-rules?nodeId="+a+"&mountPoint=/mnt/data", nil, nil); r.Code != 204 {
		t.Fatalf("delete: %d %s", r.Code, r.Raw)
	}
	if r = owner.do(http.MethodDelete, "/management/v1/storage/alert-rules?nodeId="+a+"&mountPoint=/mnt/data", nil, nil); r.Code != 404 {
		t.Fatalf("delete again: %d %s", r.Code, r.Raw)
	}
	if r = owner.do(http.MethodGet, "/management/v1/nodes/nope/storage", nil, nil); r.Code != 404 {
		t.Fatalf("unknown node view: %d", r.Code)
	}
}

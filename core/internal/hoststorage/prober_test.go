package hoststorage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/hostmetrics"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

type fakeHub struct {
	u    *protocol.StorageUsage
	err  error
	seen []protocol.ExecutorGrant
}

func (h *fakeHub) RequestStorageUsage(_ context.Context, _ string, g protocol.ExecutorGrant) (*protocol.StorageUsage, error) {
	h.seen = append(h.seen, g)
	return h.u, h.err
}

type fakeIssuer struct{ perms []string }

func (f *fakeIssuer) Issue(subject string, perms []string, _ time.Duration) (protocol.ExecutorGrant, error) {
	f.perms = perms
	return protocol.ExecutorGrant{ID: "g", Subject: subject, Permissions: perms}, nil
}

func TestProberRecordsMeasurementOrWhyNot(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	s, node := openStore(t, now)
	ctx := context.Background()
	iss := &fakeIssuer{}
	hub := &fakeHub{err: &nodes.StorageUsageUnavailableError{Code: protocol.StorageUsageExecutorUnreachable, Message: "no socket"}}
	p := &Prober{Store: s, Grants: iss, Hub: hub, Now: func() time.Time { return now },
		NodeID: func(context.Context, string) (string, error) { return node, nil }}

	if due, _ := p.Due(ctx, node); !due {
		t.Fatal("a node never measured is due")
	}
	if err := p.Probe(ctx, "fp"); err == nil {
		t.Fatal("unavailable must be an error")
	}
	if len(iss.perms) != 1 || iss.perms[0] != GrantPermission {
		t.Fatalf("grant permissions %v", iss.perms)
	}
	u, err := s.Usage(ctx, node)
	if err != nil || u == nil || u.Measured || u.Error == nil || u.Error.Code != protocol.StorageUsageExecutorUnreachable {
		t.Fatalf("the failure is recorded and nothing is measured: %+v %v", u, err)
	}
	v, err := s.NodeView(ctx, nilMetrics{}, node)
	if err != nil || v.UsageError == nil || v.UsageMeasuredAt != nil {
		t.Fatalf("the view says why there is no usage: %+v %v", v, err)
	}
	if due, _ := p.Due(ctx, node); due {
		t.Fatal("a fresh failure is not retried before the interval")
	}

	hub.err = nil
	hub.u = &protocol.StorageUsage{Usage: protocol.StorageUsageData{StartedAt: now, Filesystems: []protocol.FilesystemUsage{
		{MountPoint: "/", Dev: "8:1", State: "complete", UsedBytes: u64(10), OtherBytes: u64(4),
			Categories: []protocol.CategoryUsage{{Category: protocol.CategoryOSAndLogs, Bytes: 6}}}}}}
	if err := p.Probe(ctx, "fp"); err != nil {
		t.Fatal(err)
	}
	if u, _ = s.Usage(ctx, node); !u.Measured || u.Error != nil {
		t.Fatalf("a measurement clears the failure: %+v", u)
	}

	hub.u.Usage.Filesystems[0].OtherBytes = u64(5) // does not add up
	hub.u.Usage.StartedAt = now.Add(time.Minute)
	if err := p.Probe(ctx, "fp"); err == nil {
		t.Fatal("an invalid measurement is refused")
	}
	if u, _ = s.Usage(ctx, node); !u.Measured || u.Error == nil || u.Error.Code != CodeInvalidUsage {
		t.Fatalf("the last good measurement stays, the failure is recorded: %+v", u)
	}

	hub.err = nodes.ErrNodeOffline
	if err := p.Probe(ctx, "fp"); !errors.Is(err, nodes.ErrNodeOffline) {
		t.Fatalf("offline: %v", err)
	}
	p.Now = func() time.Time { return now.Add(7 * time.Hour) }
	if due, _ := p.Due(ctx, node); !due {
		t.Fatal("due again after the interval")
	}
}

type nilMetrics struct{}

func (nilMetrics) Latest(context.Context, string) (*hostmetrics.Latest, error) { return nil, nil }
func (nilMetrics) History(context.Context, hostmetrics.HistoryQuery, paging.Query) ([]hostmetrics.Point, error) {
	return nil, nil
}

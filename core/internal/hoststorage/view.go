package hoststorage

import (
	"context"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/hostmetrics"
	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Health of a disk. SMART and NVMe health need root and are read by the
// privileged executor in ticket 020.3; until then every disk says so, and
// nothing is reported healthy.
type Health struct {
	// State: notCollected (nothing reads it yet). 020.3 adds ok, warning,
	// failing and unsupported.
	State  string `json:"state"`
	Reason string `json:"reason"`
}

var healthNotCollected = Health{State: "notCollected", Reason: "SMART and NVMe health are not read yet (ticket 020.3); this is not a healthy result"}

type Device struct {
	protocol.BlockDevice
	// Health is null for partitions; a disk's health covers them.
	Health *Health `json:"health"`
}

// CategoryBytes is one category's share of a filesystem's used space.
type CategoryBytes struct {
	Category string   `json:"category"`
	Bytes    uint64   `json:"bytes"`
	Paths    []string `json:"paths"`
}

// FSUsage is the usage breakdown of one filesystem. When State is
// complete the categories (other included) add up to UsedBytes and no
// path counts twice; when partial the bytes are lower bounds and other is
// absent. Only categories with bytes are listed, in the fixed order.
type FSUsage struct {
	State      string          `json:"state"`
	MeasuredAt time.Time       `json:"measuredAt"`
	UsedBytes  *uint64         `json:"usedBytes"`
	Categories []CategoryBytes `json:"categories"`
	// UnreadableDirs could not be read, so their bytes are missing.
	UnreadableDirs uint64 `json:"unreadableDirs"`
}

// CategoryOrder is the order every disk lists its categories in.
var CategoryOrder = []string{protocol.CategoryAppData, protocol.CategoryDatabase, protocol.CategoryBackupStaging,
	protocol.CategoryContainerImages, protocol.CategoryOSAndLogs, protocol.CategoryOther}

type MountView struct {
	protocol.Mount
	// Capacity is null until a metrics sample lists this mount.
	Capacity *Capacity `json:"capacity"`
	// GrowthBytesPerDay is the 7-day trend of used bytes; null with less
	// than a day of history.
	GrowthBytesPerDay *float64 `json:"growthBytesPerDay"`
	// Usage is null until a measurement covered this filesystem.
	Usage  *FSUsage `json:"usage"`
	Rule   Rule     `json:"rule"`
	Alerts []Alert  `json:"alerts"`
}

// View is a node's storage.
type View struct {
	Reported    bool       `json:"reported"`
	CollectedAt *time.Time `json:"collectedAt"`
	ReceivedAt  *time.Time `json:"receivedAt"`
	// UsageMeasuredAt is when the latest usage round started; null when
	// none finished yet.
	UsageMeasuredAt *time.Time `json:"usageMeasuredAt"`
	// UsageError is why the latest request got no measurement (the
	// privileged executor is not reachable, refused, or failed); the
	// previous measurement, if any, stays. Null when the latest succeeded.
	UsageError *UsageError             `json:"usageError"`
	Devices    []Device                `json:"devices"`
	Mounts     []MountView             `json:"mounts"`
	Configs    []protocol.MountConfig  `json:"configs"`
	Swaps      []protocol.SwapArea     `json:"swaps"`
	Problems   []protocol.MountProblem `json:"problems"`
	Unreadable []string                `json:"unreadable"`
	// BackupStaging: whether alerts include the space backups need to
	// stage. notApplicable until backup jobs exist (ticket 078).
	BackupStaging string `json:"backupStaging"`
}

// Metrics is what the view reads from hostmetrics.
type Metrics interface {
	Latest(ctx context.Context, nodeID string) (*hostmetrics.Latest, error)
	History(ctx context.Context, q hostmetrics.HistoryQuery, page paging.Query) ([]hostmetrics.Point, error)
}

// trendWindow is how far back the growth trend looks.
const trendWindow = 7 * 24 * time.Hour

// NodeView assembles a node's storage view.
func (s *Store) NodeView(ctx context.Context, m Metrics, nodeID string) (View, error) {
	v := View{Devices: []Device{}, Mounts: []MountView{}, Configs: []protocol.MountConfig{}, Swaps: []protocol.SwapArea{},
		Problems: []protocol.MountProblem{}, Unreadable: []string{}, BackupStaging: "notApplicable"}
	use, err := s.Usage(ctx, nodeID)
	if err != nil {
		return v, err
	}
	if use != nil {
		v.UsageError = use.Error
	}
	inv, err := s.Inventory(ctx, nodeID)
	if err != nil || inv == nil {
		return v, err
	}
	latest, err := m.Latest(ctx, nodeID)
	if err != nil {
		return v, err
	}
	v.Reported, v.CollectedAt, v.ReceivedAt = true, &inv.CollectedAt, &inv.ReceivedAt
	st := inv.Storage
	for _, d := range st.Devices {
		dv := Device{BlockDevice: d}
		if d.Kind == "disk" {
			h := healthNotCollected
			dv.Health = &h
		}
		v.Devices = append(v.Devices, dv)
	}
	caps := map[string]Capacity{}
	if latest != nil {
		for _, f := range latest.Sample.Filesystems {
			if f.State == protocol.FilesystemOK {
				c := Capacity{TotalBytes: f.TotalBytes, AvailableBytes: f.AvailableBytes, UsedBytes: f.UsedBytes, ReadOnly: f.ReadOnly}
				if f.Inodes == protocol.InodesOK {
					c.InodesTotal, c.InodesFree = f.InodesTotal, f.InodesFree
				}
				caps[f.MountPoint] = c
			}
		}
	}
	usage := map[string]protocol.FilesystemUsage{}
	if use != nil && use.Measured {
		at := use.Usage.StartedAt
		v.UsageMeasuredAt = &at
		for _, f := range use.Usage.Filesystems {
			usage[f.Dev] = f
		}
	}
	for _, mt := range st.Mounts {
		mv := MountView{Mount: mt, Alerts: []Alert{}}
		if mv.Configs == nil {
			mv.Configs = []uint32{}
		}
		if c, ok := caps[mt.MountPoint]; ok {
			mv.Capacity = &c
		}
		if f, ok := usage[mt.Dev]; ok {
			mv.Usage = fsUsage(f, use.Usage.StartedAt)
		}
		if mv.Capacity != nil {
			mv.GrowthBytesPerDay, err = s.growth(ctx, m, nodeID, mt.MountPoint)
			if err != nil {
				return v, err
			}
		}
		if mv.Rule, err = s.RuleFor(ctx, nodeID, mt.MountPoint); err != nil {
			return v, err
		}
		if mv.Capacity != nil && mt.Class != "virtual" {
			mv.Alerts = Evaluate(mv.Rule, *mv.Capacity, mv.GrowthBytesPerDay)
		}
		v.Mounts = append(v.Mounts, mv)
	}
	v.Configs = nonNil(st.Configs)
	v.Swaps = nonNil(st.Swaps)
	v.Problems = nonNil(st.Problems)
	v.Unreadable = nonNil(st.Unreadable)
	return v, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// fsUsage puts a measured filesystem into the fixed category order, with
// other last; other is listed only for a complete measurement.
func fsUsage(f protocol.FilesystemUsage, at time.Time) *FSUsage {
	out := &FSUsage{State: f.State, MeasuredAt: at, UsedBytes: f.UsedBytes, Categories: []CategoryBytes{}, UnreadableDirs: f.UnreadableDirs}
	by := map[string]protocol.CategoryUsage{}
	for _, c := range f.Categories {
		by[c.Category] = c
	}
	for _, name := range CategoryOrder {
		if name == protocol.CategoryOther {
			if f.State == "complete" && f.OtherBytes != nil && *f.OtherBytes > 0 {
				out.Categories = append(out.Categories, CategoryBytes{Category: name, Bytes: *f.OtherBytes, Paths: []string{}})
			}
			continue
		}
		if c, ok := by[name]; ok && c.Bytes > 0 {
			out.Categories = append(out.Categories, CategoryBytes{Category: name, Bytes: c.Bytes, Paths: nonNil(c.Paths)})
		}
	}
	return out
}

// growth reads the 5-minute averages of used bytes over trendWindow.
func (s *Store) growth(ctx context.Context, m Metrics, nodeID, mountPoint string) (*float64, error) {
	to := s.now()
	pts, err := m.History(ctx, hostmetrics.HistoryQuery{NodeID: nodeID, Series: "filesystem.usedBytes", Key: mountPoint,
		Resolution: hostmetrics.Resolution5m, From: to.Add(-trendWindow), To: to},
		paging.Query{Limit: int(trendWindow / (5 * time.Minute)), Sort: []paging.SortKey{{Field: "at"}}})
	if err != nil {
		return nil, err
	}
	at := make([]time.Time, len(pts))
	used := make([]float64, len(pts))
	for i, p := range pts {
		at[i], used[i] = p.At, p.Value
	}
	return Growth(at, used), nil
}

package hostmetrics

import (
	"sort"
	"strconv"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// SeriesInfo describes one history series.
type SeriesInfo struct {
	Name string
	Unit string
	// Key says what the series key names; "" for host-wide series.
	Key string
}

// Catalog lists every series with history. Units: percent (0–100),
// bytes, bytesPerSecond, perSecond, milliseconds, celsius, count, load
// (run-queue average).
var Catalog = []SeriesInfo{
	{"cpu.usage", "percent", ""},
	{"cpu.user", "percent", ""},
	{"cpu.system", "percent", ""},
	{"cpu.iowait", "percent", ""},
	{"cpu.steal", "percent", ""},
	{"cpu.core.usage", "percent", "CPU number"},
	{"memory.usedBytes", "bytes", ""},
	{"memory.availableBytes", "bytes", ""},
	{"memory.cachedBytes", "bytes", ""},
	{"swap.usedBytes", "bytes", ""},
	{"load.one", "load", ""},
	{"load.five", "load", ""},
	{"load.fifteen", "load", ""},
	{"pressure.cpu.someAvg60", "percent", ""},
	{"pressure.memory.someAvg60", "percent", ""},
	{"pressure.io.someAvg60", "percent", ""},
	{"limits.tasks", "count", ""},
	{"limits.fileHandles", "count", ""},
	{"filesystem.usedBytes", "bytes", "mount point"},
	{"filesystem.availableBytes", "bytes", "mount point"},
	{"filesystem.inodesUsed", "count", "mount point"},
	{"disk.readBytesPerSecond", "bytesPerSecond", "block device name"},
	{"disk.writeBytesPerSecond", "bytesPerSecond", "block device name"},
	{"disk.readsPerSecond", "perSecond", "block device name"},
	{"disk.writesPerSecond", "perSecond", "block device name"},
	{"disk.readLatencyMs", "milliseconds", "block device name"},
	{"disk.writeLatencyMs", "milliseconds", "block device name"},
	{"disk.utilization", "percent", "block device name"},
	{"network.rxBytesPerSecond", "bytesPerSecond", "interface name"},
	{"network.txBytesPerSecond", "bytesPerSecond", "interface name"},
	{"temperature.celsius", "celsius", "chip/label"},
}

// SeriesNames is the series of Catalog, in order.
func SeriesNames() []string {
	out := make([]string, len(Catalog))
	for i, s := range Catalog {
		out[i] = s.Name
	}
	return out
}

// PointValue is one value of a sample.
type PointValue struct {
	Series string
	Key    string
	Value  float64
}

// Points flattens the history series of a sample. Values the agent did
// not provide produce no point. Only physical interfaces, filesystems that
// answered and disks other than loop devices get history.
func Points(s *protocol.MetricsSample) []PointValue {
	var out []PointValue
	add := func(series, key string, v *float64) {
		if v != nil {
			out = append(out, PointValue{series, key, *v})
		}
	}
	u := func(v *uint64) *float64 {
		if v == nil {
			return nil
		}
		f := float64(*v)
		return &f
	}
	f := func(v float64) *float64 { return &v }
	if t := s.CPU.Total; t != nil {
		add("cpu.usage", "", f(t.Usage))
		add("cpu.user", "", f(t.User))
		add("cpu.system", "", f(t.System))
		add("cpu.iowait", "", f(t.Iowait))
		add("cpu.steal", "", f(t.Steal))
	}
	for i, c := range s.CPU.Cores {
		if i >= maxKeysPerFamily {
			break
		}
		add("cpu.core.usage", strconv.FormatUint(uint64(c.ID), 10), f(c.Usage.Usage))
	}
	add("memory.usedBytes", "", u(s.Memory.UsedBytes))
	add("memory.availableBytes", "", u(s.Memory.AvailableBytes))
	add("memory.cachedBytes", "", u(s.Memory.CachedBytes))
	add("swap.usedBytes", "", u(s.Swap.UsedBytes))
	add("load.one", "", s.Load.One)
	add("load.five", "", s.Load.Five)
	add("load.fifteen", "", s.Load.Fifteen)
	for name, line := range map[string]*protocol.PressureLine{"cpu": s.Pressure.CPU, "memory": s.Pressure.Memory, "io": s.Pressure.IO} {
		if s.Pressure.State == protocol.PressureOK && line != nil {
			add("pressure."+name+".someAvg60", "", f(line.SomeAvg60))
		}
	}
	add("limits.tasks", "", u(s.Limits.Tasks))
	add("limits.fileHandles", "", u(s.Limits.FileHandles))
	n := 0
	for _, fs := range s.Filesystems {
		if fs.State != protocol.FilesystemOK || n >= maxKeysPerFamily {
			continue
		}
		n++
		add("filesystem.usedBytes", fs.MountPoint, u(fs.UsedBytes))
		add("filesystem.availableBytes", fs.MountPoint, u(fs.AvailableBytes))
		if fs.Inodes == protocol.InodesOK && fs.InodesTotal != nil && fs.InodesFree != nil && *fs.InodesTotal >= *fs.InodesFree {
			add("filesystem.inodesUsed", fs.MountPoint, f(float64(*fs.InodesTotal-*fs.InodesFree)))
		}
	}
	n = 0
	for _, d := range s.Disks {
		if d.Class == "loop" || n >= maxKeysPerFamily {
			continue
		}
		n++
		add("disk.readBytesPerSecond", d.Name, d.ReadBytesPerSecond)
		add("disk.writeBytesPerSecond", d.Name, d.WriteBytesPerSecond)
		add("disk.readsPerSecond", d.Name, d.ReadsPerSecond)
		add("disk.writesPerSecond", d.Name, d.WritesPerSecond)
		add("disk.readLatencyMs", d.Name, d.ReadLatencyMs)
		add("disk.writeLatencyMs", d.Name, d.WriteLatencyMs)
		add("disk.utilization", d.Name, d.Utilization)
	}
	n = 0
	for _, nw := range s.Networks {
		if nw.Class != "physical" || n >= maxKeysPerFamily {
			continue
		}
		n++
		add("network.rxBytesPerSecond", nw.Name, nw.RxBytesPerSecond)
		add("network.txBytesPerSecond", nw.Name, nw.TxBytesPerSecond)
	}
	for i, t := range s.Temperatures {
		if i >= maxKeysPerFamily {
			break
		}
		add("temperature.celsius", t.Chip+"/"+t.Label, f(t.Celsius))
	}
	// Map iteration above: keep the order stable for tests and inserts.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Series != out[j].Series {
			return out[i].Series < out[j].Series
		}
		return out[i].Key < out[j].Key
	})
	return dedupe(out)
}

// dedupe keeps the first of points with the same series and key (two
// sensors can share a chip and label).
func dedupe(in []PointValue) []PointValue {
	out := in[:0]
	for i, p := range in {
		if i > 0 && p.Series == in[i-1].Series && p.Key == in[i-1].Key {
			continue
		}
		out = append(out, p)
	}
	return out
}

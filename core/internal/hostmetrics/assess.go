package hostmetrics

import (
	"math"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Default thresholds of addendum 03 51.3 "ROB-009 指標".
const (
	PIDThresholdPercent          = 90
	FileHandleThresholdPercent   = 90
	MemoryPressureThresholdAvg60 = 10
)

// Usage is a count against its limit. Percent and Exceeded are nil when
// either value was not provided.
type Usage struct {
	Used             *uint64  `json:"used"`
	Limit            *uint64  `json:"limit"`
	Percent          *float64 `json:"percent"`
	ThresholdPercent float64  `json:"thresholdPercent"`
	Exceeded         *bool    `json:"exceeded"`
}

// MemoryPressure is PSI some avg60 of memory. State is ok, unsupported,
// disabled or unreadable; SomeAvg60 and Exceeded are nil unless ok.
type MemoryPressure struct {
	State            string   `json:"state"`
	SomeAvg60        *float64 `json:"someAvg60"`
	ThresholdPercent float64  `json:"thresholdPercent"`
	Exceeded         *bool    `json:"exceeded"`
}

// FilesystemState flags a filesystem that looks free but cannot take new
// files. Inodes is ok or notApplicable (no fixed inode count: never
// exhausted); InodeUsedPercent and InodesExhausted are nil when not
// applicable or not provided.
type FilesystemState struct {
	MountPoint       string   `json:"mountPoint"`
	State            string   `json:"state"`
	ReadOnly         bool     `json:"readOnly"`
	Inodes           string   `json:"inodes"`
	InodeUsedPercent *float64 `json:"inodeUsedPercent"`
	InodesExhausted  *bool    `json:"inodesExhausted"`
}

// Exhaustion is the ROB-009 view of a sample: limits that run out before
// space does.
type Exhaustion struct {
	PIDs           Usage             `json:"pids"`
	FileHandles    Usage             `json:"fileHandles"`
	MemoryPressure MemoryPressure    `json:"memoryPressure"`
	Filesystems    []FilesystemState `json:"filesystems"`
}

func usage(used, limit *uint64, threshold float64) Usage {
	u := Usage{Used: used, Limit: limit, ThresholdPercent: threshold}
	if used != nil && limit != nil && *limit > 0 {
		p := math.Round(float64(*used)*10000/float64(*limit)) / 100
		e := p > threshold
		u.Percent, u.Exceeded = &p, &e
	}
	return u
}

// Assess derives the exhaustion view of s.
func Assess(s *protocol.MetricsSample) Exhaustion {
	x := Exhaustion{
		PIDs:           usage(s.Limits.Tasks, s.Limits.PidMax, PIDThresholdPercent),
		FileHandles:    usage(s.Limits.FileHandles, s.Limits.FileMax, FileHandleThresholdPercent),
		MemoryPressure: MemoryPressure{State: s.Pressure.State, ThresholdPercent: MemoryPressureThresholdAvg60},
		Filesystems:    []FilesystemState{},
	}
	if s.Pressure.State == protocol.PressureOK && s.Pressure.Memory != nil {
		v := s.Pressure.Memory.SomeAvg60
		e := v > MemoryPressureThresholdAvg60
		x.MemoryPressure.SomeAvg60, x.MemoryPressure.Exceeded = &v, &e
	}
	for _, fs := range s.Filesystems {
		st := FilesystemState{MountPoint: fs.MountPoint, State: fs.State, ReadOnly: fs.ReadOnly, Inodes: fs.Inodes}
		if fs.Inodes == protocol.InodesOK && fs.InodesTotal != nil && fs.InodesFree != nil && *fs.InodesTotal > 0 {
			used := float64(*fs.InodesTotal - min(*fs.InodesFree, *fs.InodesTotal))
			p := math.Round(used*10000/float64(*fs.InodesTotal)) / 100
			e := *fs.InodesFree == 0
			st.InodeUsedPercent, st.InodesExhausted = &p, &e
		}
		x.Filesystems = append(x.Filesystems, st)
	}
	return x
}

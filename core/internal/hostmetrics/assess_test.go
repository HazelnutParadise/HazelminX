package hostmetrics

import (
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// T-165 (display): 40% space left but no inodes; btrfs has no inode table.
func TestAssessInodesExhaustedWithSpaceLeft(t *testing.T) {
	s := sample(1, t0, nil)
	s.Filesystems = []protocol.Filesystem{
		{MountPoint: "/", State: "ok", Inodes: "ok", TotalBytes: ptr[uint64](100), FreeBytes: ptr[uint64](40), InodesTotal: ptr[uint64](1000), InodesFree: ptr[uint64](0)},
		{MountPoint: "/pool", State: "ok", Inodes: "notApplicable"},
		{MountPoint: "/ro", State: "ok", Inodes: "ok", ReadOnly: true, InodesTotal: ptr[uint64](1000), InodesFree: ptr[uint64](900)},
	}
	x := Assess(&s)
	root := x.Filesystems[0]
	if root.InodesExhausted == nil || !*root.InodesExhausted || *root.InodeUsedPercent != 100 {
		t.Fatalf("root %+v", root)
	}
	pool := x.Filesystems[1]
	if pool.Inodes != "notApplicable" || pool.InodesExhausted != nil || pool.InodeUsedPercent != nil {
		t.Fatalf("btrfs is never exhausted: %+v", pool)
	}
	ro := x.Filesystems[2]
	if !ro.ReadOnly || *ro.InodesExhausted || *ro.InodeUsedPercent != 10 {
		t.Fatalf("read-only %+v", ro)
	}
}

// T-172: PSI turned off reads "disabled" with no value, never 0%.
func TestAssessPressureDisabledIsNotZero(t *testing.T) {
	s := sample(1, t0, nil)
	s.Pressure = protocol.Pressure{State: protocol.PressureDisabled}
	x := Assess(&s)
	if x.MemoryPressure.State != "disabled" || x.MemoryPressure.SomeAvg60 != nil || x.MemoryPressure.Exceeded != nil {
		t.Fatalf("pressure %+v", x.MemoryPressure)
	}
	s.Pressure = protocol.Pressure{State: protocol.PressureOK, Memory: &protocol.PressureLine{SomeAvg60: 11.5}}
	x = Assess(&s)
	if !*x.MemoryPressure.Exceeded || *x.MemoryPressure.SomeAvg60 != 11.5 {
		t.Fatalf("pressure %+v", x.MemoryPressure)
	}
}

// T-183 (flag): 92% of pid_max in use is over the 90% threshold; missing
// limits give no percentage.
func TestAssessPIDUsage(t *testing.T) {
	s := sample(1, t0, nil)
	s.Limits = protocol.Limits{Tasks: ptr[uint64](92), PidMax: ptr[uint64](100), FileHandles: ptr[uint64](5)}
	x := Assess(&s)
	if *x.PIDs.Percent != 92 || !*x.PIDs.Exceeded || x.PIDs.ThresholdPercent != 90 {
		t.Fatalf("pids %+v", x.PIDs)
	}
	if x.FileHandles.Percent != nil || x.FileHandles.Exceeded != nil || *x.FileHandles.Used != 5 {
		t.Fatalf("file handles without file-max %+v", x.FileHandles)
	}
}

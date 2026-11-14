package protocol

import "time"

// host.metrics v1 (ticket 020.1): host facts and resource samples, mirror of
// agent/crates/protocol/src/metrics.rs. A nil pointer is a value the agent
// could not read ("not provided"); it is never a zero.

// Fact is a host fact and where it was read from.
type Fact[T any] struct {
	Value  *T     `json:"value"`
	Source string `json:"source"`
}

// HostFactsData is what a host is.
type HostFactsData struct {
	Hostname           Fact[string] `json:"hostname"`
	OSName             Fact[string] `json:"osName"`
	OSVersion          Fact[string] `json:"osVersion"`
	OSPrettyName       Fact[string] `json:"osPrettyName"`
	Kernel             Fact[string] `json:"kernel"`
	Arch               Fact[string] `json:"arch"`
	BootTime           Fact[string] `json:"bootTime"`
	Timezone           Fact[string] `json:"timezone"`
	TimeSynchronized   Fact[bool]   `json:"timeSynchronized"`
	TimeMaxErrorMicros Fact[uint64] `json:"timeMaxErrorMicros"`
	CPUModel           Fact[string] `json:"cpuModel"`
	LogicalCPUs        Fact[uint32] `json:"logicalCpus"`
	MemoryTotalBytes   Fact[uint64] `json:"memoryTotalBytes"`
}

// HostFacts is sent after ready and when a value changes.
type HostFacts struct {
	Type        string        `json:"type"`
	CollectedAt time.Time     `json:"collectedAt"`
	Facts       HostFactsData `json:"facts"`
}

// MetricsReport carries the samples Core has not acknowledged, oldest
// first. DroppedSamples counts samples the agent discarded (buffer full)
// since its previous report.
type MetricsReport struct {
	Type           string          `json:"type"`
	Samples        []MetricsSample `json:"samples"`
	DroppedSamples uint64          `json:"droppedSamples"`
}

// MetricsAck: Core stored every sample up to Seq.
type MetricsAck struct {
	Type string `json:"type"`
	Seq  uint64 `json:"seq"`
}

// CPUUsage is percent of the interval (0–100).
type CPUUsage struct {
	Usage  float64 `json:"usage"`
	User   float64 `json:"user"`
	System float64 `json:"system"`
	Iowait float64 `json:"iowait"`
	Steal  float64 `json:"steal"`
}

type CoreUsage struct {
	ID    uint32   `json:"id"`
	Usage CPUUsage `json:"usage"`
}

// CPU: Total is nil and Cores empty on the first sample after start.
type CPU struct {
	Total *CPUUsage   `json:"total"`
	Cores []CoreUsage `json:"cores"`
}

// Memory in bytes; Used is total minus available (cache counts as
// available).
type Memory struct {
	TotalBytes           *uint64 `json:"totalBytes"`
	AvailableBytes       *uint64 `json:"availableBytes"`
	UsedBytes            *uint64 `json:"usedBytes"`
	FreeBytes            *uint64 `json:"freeBytes"`
	BuffersBytes         *uint64 `json:"buffersBytes"`
	CachedBytes          *uint64 `json:"cachedBytes"`
	ShmemBytes           *uint64 `json:"shmemBytes"`
	ReclaimableSlabBytes *uint64 `json:"reclaimableSlabBytes"`
}

type Swap struct {
	TotalBytes *uint64 `json:"totalBytes"`
	FreeBytes  *uint64 `json:"freeBytes"`
	UsedBytes  *uint64 `json:"usedBytes"`
}

type Load struct {
	One     *float64 `json:"one"`
	Five    *float64 `json:"five"`
	Fifteen *float64 `json:"fifteen"`
}

type PressureLine struct {
	SomeAvg10  float64  `json:"someAvg10"`
	SomeAvg60  float64  `json:"someAvg60"`
	SomeAvg300 float64  `json:"someAvg300"`
	FullAvg10  *float64 `json:"fullAvg10"`
	FullAvg60  *float64 `json:"fullAvg60"`
	FullAvg300 *float64 `json:"fullAvg300"`
}

// Pressure states.
const (
	PressureOK          = "ok"
	PressureUnsupported = "unsupported"
	PressureDisabled    = "disabled"
	PressureUnreadable  = "unreadable"
)

type Pressure struct {
	State  string        `json:"state"`
	CPU    *PressureLine `json:"cpu"`
	Memory *PressureLine `json:"memory"`
	IO     *PressureLine `json:"io"`
}

type Limits struct {
	Tasks       *uint64 `json:"tasks"`
	PidMax      *uint64 `json:"pidMax"`
	FileHandles *uint64 `json:"fileHandles"`
	FileMax     *uint64 `json:"fileMax"`
}

// Filesystem states and inode states.
const (
	FilesystemOK           = "ok"
	FilesystemUnresponsive = "unresponsive"
	FilesystemUnreadable   = "unreadable"
	InodesOK               = "ok"
	InodesNotApplicable    = "notApplicable"
)

type Filesystem struct {
	MountPoint     string  `json:"mountPoint"`
	Device         string  `json:"device"`
	FSType         string  `json:"fsType"`
	Class          string  `json:"class"`
	ReadOnly       bool    `json:"readOnly"`
	State          string  `json:"state"`
	TotalBytes     *uint64 `json:"totalBytes"`
	FreeBytes      *uint64 `json:"freeBytes"`
	AvailableBytes *uint64 `json:"availableBytes"`
	UsedBytes      *uint64 `json:"usedBytes"`
	Inodes         string  `json:"inodes"`
	InodesTotal    *uint64 `json:"inodesTotal"`
	InodesFree     *uint64 `json:"inodesFree"`
}

type Disk struct {
	Name                string   `json:"name"`
	Class               string   `json:"class"`
	Model               *string  `json:"model"`
	Transport           *string  `json:"transport"`
	Rotational          *bool    `json:"rotational"`
	Removable           *bool    `json:"removable"`
	SizeBytes           *uint64  `json:"sizeBytes"`
	ReadBytesPerSecond  *float64 `json:"readBytesPerSecond"`
	WriteBytesPerSecond *float64 `json:"writeBytesPerSecond"`
	ReadsPerSecond      *float64 `json:"readsPerSecond"`
	WritesPerSecond     *float64 `json:"writesPerSecond"`
	ReadLatencyMs       *float64 `json:"readLatencyMs"`
	WriteLatencyMs      *float64 `json:"writeLatencyMs"`
	Utilization         *float64 `json:"utilization"`
}

type Network struct {
	Name               string   `json:"name"`
	Class              string   `json:"class"`
	OperState          *string  `json:"operState"`
	SpeedMbps          *uint64  `json:"speedMbps"`
	RxBytesPerSecond   *float64 `json:"rxBytesPerSecond"`
	TxBytesPerSecond   *float64 `json:"txBytesPerSecond"`
	RxPacketsPerSecond *float64 `json:"rxPacketsPerSecond"`
	TxPacketsPerSecond *float64 `json:"txPacketsPerSecond"`
	RxErrors           *uint64  `json:"rxErrors"`
	TxErrors           *uint64  `json:"txErrors"`
	RxDrops            *uint64  `json:"rxDrops"`
	TxDrops            *uint64  `json:"txDrops"`
}

type Temperature struct {
	Source          string   `json:"source"`
	Chip            string   `json:"chip"`
	Label           string   `json:"label"`
	Celsius         float64  `json:"celsius"`
	HighCelsius     *float64 `json:"highCelsius"`
	CriticalCelsius *float64 `json:"criticalCelsius"`
}

// MetricsSample is one collection round.
type MetricsSample struct {
	Seq             uint64        `json:"seq"`
	CollectedAt     time.Time     `json:"collectedAt"`
	IntervalSeconds uint32        `json:"intervalSeconds"`
	CPU             CPU           `json:"cpu"`
	Memory          Memory        `json:"memory"`
	Swap            Swap          `json:"swap"`
	Load            Load          `json:"load"`
	Pressure        Pressure      `json:"pressure"`
	Limits          Limits        `json:"limits"`
	Filesystems     []Filesystem  `json:"filesystems"`
	Disks           []Disk        `json:"disks"`
	Networks        []Network     `json:"networks"`
	Temperatures    []Temperature `json:"temperatures"`
}

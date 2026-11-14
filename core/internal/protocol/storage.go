package protocol

import "time"

// host.storage v1 (ticket 030.1): block devices, mounts, mount
// configuration and per-category filesystem usage, mirror of
// agent/crates/protocol/src/storage.rs. A nil pointer is "not provided".

// Usage categories. Every disk uses the same set; a path counts in one
// category only, and other is the rest of the used space.
const (
	CategoryAppData         = "appData"
	CategoryDatabase        = "database"
	CategoryBackupStaging   = "backupStaging"
	CategoryContainerImages = "containerImages"
	CategoryOSAndLogs       = "osAndLogs"
	CategoryOther           = "other"
)

type BlockDevice struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Parent     *string  `json:"parent"`
	Dev        string   `json:"dev"`
	Class      string   `json:"class"`
	SizeBytes  *uint64  `json:"sizeBytes"`
	Model      *string  `json:"model"`
	Serial     *string  `json:"serial"`
	Transport  *string  `json:"transport"`
	Rotational *bool    `json:"rotational"`
	Removable  *bool    `json:"removable"`
	ReadOnly   *bool    `json:"readOnly"`
	FSType     *string  `json:"fsType"`
	FSUUID     *string  `json:"fsUuid"`
	FSLabel    *string  `json:"fsLabel"`
	PartUUID   *string  `json:"partUuid"`
	PartLabel  *string  `json:"partLabel"`
	Holders    []string `json:"holders"`
}

type MountConfig struct {
	Origin     string  `json:"origin"`
	File       string  `json:"file"`
	Line       *uint32 `json:"line"`
	Spec       string  `json:"spec"`
	Reference  string  `json:"reference"`
	MountPoint string  `json:"mountPoint"`
	FSType     string  `json:"fsType"`
	Options    string  `json:"options"`
	Boot       string  `json:"boot"`
	ResolvesTo *string `json:"resolvesTo"`
}

type Mount struct {
	MountPoint string   `json:"mountPoint"`
	Source     string   `json:"source"`
	Device     *string  `json:"device"`
	Dev        string   `json:"dev"`
	FSType     string   `json:"fsType"`
	Class      string   `json:"class"`
	Options    string   `json:"options"`
	ReadOnly   bool     `json:"readOnly"`
	Reference  string   `json:"reference"`
	Configs    []uint32 `json:"configs"`
}

type SwapArea struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	SizeBytes uint64   `json:"sizeBytes"`
	Priority  int32    `json:"priority"`
	Configs   []uint32 `json:"configs"`
}

type MountProblem struct {
	Kind       string   `json:"kind"`
	MountPoint string   `json:"mountPoint"`
	Configs    []uint32 `json:"configs"`
}

type StorageInventory struct {
	Devices    []BlockDevice  `json:"devices"`
	Mounts     []Mount        `json:"mounts"`
	Configs    []MountConfig  `json:"configs"`
	Swaps      []SwapArea     `json:"swaps"`
	Problems   []MountProblem `json:"problems"`
	Unreadable []string       `json:"unreadable"`
}

// HostStorage is sent after ready and when the inventory changes.
type HostStorage struct {
	Type        string           `json:"type"`
	CollectedAt time.Time        `json:"collectedAt"`
	Storage     StorageInventory `json:"storage"`
}

type CategoryUsage struct {
	Category string   `json:"category"`
	Bytes    uint64   `json:"bytes"`
	Paths    []string `json:"paths"`
}

type FilesystemUsage struct {
	MountPoint     string          `json:"mountPoint"`
	Dev            string          `json:"dev"`
	State          string          `json:"state"`
	UsedBytes      *uint64         `json:"usedBytes"`
	Categories     []CategoryUsage `json:"categories"`
	OtherBytes     *uint64         `json:"otherBytes"`
	UnreadableDirs uint64          `json:"unreadableDirs"`
}

type StorageUsageData struct {
	StartedAt   time.Time         `json:"startedAt"`
	DurationMs  uint64            `json:"durationMs"`
	Entries     uint64            `json:"entries"`
	Filesystems []FilesystemUsage `json:"filesystems"`
}

// StorageUsageRequest asks the agent for a usage measurement; the agent
// passes Grant (host.storage.read) to the privileged executor's
// storage_usage, which walks as root.
type StorageUsageRequest struct {
	Type      string        `json:"type"`
	RequestID string        `json:"requestId"`
	Grant     ExecutorGrant `json:"grant"`
}

// StorageUsage is the executor's measurement, relayed unchanged.
type StorageUsage struct {
	Type      string           `json:"type"`
	RequestID string           `json:"requestId"`
	Usage     StorageUsageData `json:"usage"`
}

// StorageUsageUnavailable: the agent could not get a measurement.
type StorageUsageUnavailable struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// storage_usage_unavailable codes.
const (
	StorageUsageExecutorUnreachable = "EXECUTOR_UNREACHABLE"
	StorageUsageExecutorRefused     = "EXECUTOR_REFUSED"
	StorageUsageMeasureFailed       = "MEASURE_FAILED"
)

package protocol

import "time"

// host.engines v1 (ticket 039.1): the container engines on a host, mirror
// of agent/crates/protocol/src/engines.rs. The privileged executor's
// read-only engine_inspect produces the report; the agent relays it. A nil
// pointer is a value the host did not provide; it is never guessed.

// Engine kinds.
const (
	EngineDocker  = "docker"
	EnginePodman  = "podman"
	EngineK3s     = "k3s"
	EngineUnknown = "unknown"
)

// Engine and environment states.
const (
	EngineRunning          = "running"
	EngineStopped          = "stopped"
	EngineNotInstalled     = "not_installed"
	EngineIncompatible     = "incompatible"
	EnginePermissionDenied = "permission_denied"
	EngineNotResponding    = "not_responding"
	EngineStateUnknown     = "unknown"
)

// engines_unavailable codes.
const (
	EnginesExecutorUnreachable = "EXECUTOR_UNREACHABLE"
	EnginesExecutorRefused     = "EXECUTOR_REFUSED"
	EnginesInspectFailed       = "INSPECT_FAILED"
)

// EnginesProbe asks the agent for an engine report; Grant must carry
// host.engines.read.
type EnginesProbe struct {
	Type      string        `json:"type"`
	RequestID string        `json:"requestId"`
	Grant     ExecutorGrant `json:"grant"`
}

// EnginesReport is the executor's report, relayed unchanged.
type EnginesReport struct {
	Type        string       `json:"type"`
	RequestID   string       `json:"requestId"`
	CollectedAt time.Time    `json:"collectedAt"`
	Report      EngineReport `json:"report"`
}

// EnginesUnavailable: the agent could not produce a report.
type EnginesUnavailable struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// EngineReport lists every engine kind looked for, every endpoint found
// (several of one kind stay separate) and what could not be looked at.
type EngineReport struct {
	Engines      []Engine      `json:"engines"`
	Environments []Environment `json:"environments"`
	Uninspected  []Uninspected `json:"uninspected"`
}

// Engine is an engine as installed on the host.
type Engine struct {
	Kind          string   `json:"kind"`
	State         string   `json:"state"`
	Binary        *string  `json:"binary"`
	BinaryVersion *string  `json:"binaryVersion"`
	Units         []Unit   `json:"units"`
	Plugins       []Plugin `json:"plugins"`
}

// Unit is a systemd unit as systemctl show reports it.
type Unit struct {
	Name          string  `json:"name"`
	LoadState     *string `json:"loadState"`
	ActiveState   *string `json:"activeState"`
	SubState      *string `json:"subState"`
	UnitFileState *string `json:"unitFileState"`
}

// Plugin is a Docker CLI plugin (compose, buildx).
type Plugin struct {
	Name    string  `json:"name"`
	Path    string  `json:"path"`
	Version *string `json:"version"`
}

// Environment is one engine endpoint and its owner. Kind is what the
// endpoint answered (ENV-002) when KindConfirmed, else the kind its path
// usually belongs to.
type Environment struct {
	ID                string  `json:"id"`
	Kind              string  `json:"kind"`
	KindConfirmed     bool    `json:"kindConfirmed"`
	Endpoint          string  `json:"endpoint"`
	EndpointResolved  string  `json:"endpointResolved"`
	Mode              string  `json:"mode"`
	OwnerUID          uint32  `json:"ownerUid"`
	OwnerName         *string `json:"ownerName"`
	State             string  `json:"state"`
	Version           *string `json:"version"`
	APIVersion        *string `json:"apiVersion"`
	CgroupDriver      *string `json:"cgroupDriver"`
	CgroupVersion     *string `json:"cgroupVersion"`
	Swarm             *string `json:"swarm"`
	Containers        *uint64 `json:"containers"`
	ContainersRunning *uint64 `json:"containersRunning"`
	Error             *string `json:"error"`
}

// Uninspected is something the executor could not look at.
type Uninspected struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

package jobs

import (
	"encoding/json"
	"errors"
	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
	"time"
)

// Errors returned by the engine. The API maps them to HTTP codes.
var (
	ErrNotFound       = errors.New("jobs: no such job")
	ErrConflict       = errors.New("jobs: request id already used with different content")
	ErrUnknownKind    = errors.New("jobs: no handler registered for this kind")
	ErrNotCancellable = errors.New("jobs: job is already finished")
	ErrInvalidRequest = errors.New("jobs: invalid request")
)

// Outcome sentinels a Handler returns from Execute/Rollback/Reconcile so the
// engine can tell "known failure" from "outcome unknown".
var (
	// ErrDisconnected: the executor (Agent) went away before it reported the
	// result. The step may or may not have happened; the job becomes unknown
	// and is never re-run automatically if the step is irreversible.
	ErrDisconnected = errors.New("jobs: executor disconnected before reporting a result")
	// ErrRecheckBlocked: the pre-execution re-check found the environment no
	// longer matches the precheck (port taken, disk swapped). Nothing was
	// changed; the job fails without touching the foreign resource.
	ErrRecheckBlocked = errors.New("jobs: environment changed since precheck")
)

// ResourceRef identifies something a job reserves. Locks compare identity,
// not strings: the same device, the same mount and parent/child paths all
// conflict (spec 33.2).
type ResourceRef struct {
	// Kind: "disk", "mount", "path", "database", "port", "service", … Free-form
	// for kinds whose identity is just Kind+ID.
	Kind string `json:"kind"`
	// ID is the stable identity for non-path kinds (database id, service id,
	// "tcp/8080"). Empty for disk/mount/path.
	ID string `json:"id,omitempty"`
	// Device is the block device identity (by-id or UUID) for disk, mount and
	// path refs. Two paths on the same device conflict with a disk-level
	// operation on that device.
	Device string `json:"device,omitempty"`
	// Mount is the mount point for mount and path refs.
	Mount string `json:"mount,omitempty"`
	// Path is the path as the user gave it; ResolvedPath is what the probe
	// resolved it to (symlinks followed). The lock key prefers ResolvedPath.
	Path         string `json:"path,omitempty"`
	ResolvedPath string `json:"resolvedPath,omitempty"`
	// Node scopes the resource; refs on different nodes never conflict.
	Node string `json:"node,omitempty"`
}

// Budget is what a job may consume without starving the management process
// (ENG-011). The engine admits a job only while the running count of its kind
// is below MaxConcurrent, and refuses jobs whose declared space need would eat
// into ReserveBytes of the data directory.
type Budget struct {
	MaxConcurrent int   `json:"maxConcurrent"`
	ReserveBytes  int64 `json:"reserveBytes"`
}

// SubmitRequest is what the API hands to Engine.Submit.
type SubmitRequest struct {
	RequestID string          `json:"requestId"`
	Kind      string          `json:"kind"`
	Params    json.RawMessage `json:"params,omitempty"`
	Resources []ResourceRef   `json:"resources,omitempty"`
	Node      string          `json:"node,omitempty"`
	Requester string          `json:"requester,omitempty"`
	// RequiredBytes the job will write under the data directory (0 = unknown).
	RequiredBytes int64 `json:"requiredBytes,omitempty"`
}

// StepStatus is the state of a single step.
type StepStatus string

const (
	StepPending        StepStatus = "pending"
	StepRunning        StepStatus = "running"
	StepDone           StepStatus = "done"
	StepFailed         StepStatus = "failed"
	StepSkipped        StepStatus = "skipped"
	StepUnknown        StepStatus = "unknown"
	StepRolledBack     StepStatus = "rolled_back"
	StepRollbackFailed StepStatus = "rollback_failed"
)

// Step is one unit of a job's plan (spec 33.3 "每個步驟另存").
type Step struct {
	Index         int        `json:"index"`
	Name          string     `json:"name"`
	Status        StepStatus `json:"status"`
	InputRevision string     `json:"inputRevision,omitempty"`
	Preconditions []string   `json:"preconditions,omitempty"`
	// Recheck records the outcome of the re-check right before execution.
	Recheck string `json:"recheck,omitempty"`
	// SideEffects confirmed after execution.
	SideEffects []string `json:"sideEffects,omitempty"`
	// RollbackMethod is how the step can be undone; empty = not reversible.
	RollbackMethod string `json:"rollbackMethod,omitempty"`
	// Irreversible marks a step that crosses a point of no return. It is never
	// re-run without reconciliation and cannot be interrupted mid-way.
	Irreversible bool `json:"irreversible"`
	// Cancellable: the step can be stopped at any time without leaving the
	// system half-changed. Irreversible steps are never cancellable.
	Cancellable bool `json:"cancellable"`
	// Retryable: running the step twice has no additional side effect, so it
	// may be re-run after an unknown outcome without reconciliation.
	Retryable bool `json:"retryable"`
	// WaitReason explains why a requested cancel is still waiting on this step.
	WaitReason string `json:"waitReason,omitempty"`
	// Attempts counts how many times the step started executing, including
	// attempts whose outcome reconciliation later reset.
	Attempts   int        `json:"attempts,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Evidence   []string   `json:"evidence,omitempty"`
	Error      *StepError `json:"error,omitempty"`
	// LogTail is the retained tail of the step's output; LogsDropped counts
	// lines that did not fit (T-034).
	LogTail     []string `json:"logTail,omitempty"`
	LogsDropped int      `json:"logsDropped,omitempty"`
}

// StepSpec is what a Handler's Plan returns; the engine turns it into Steps.
type StepSpec struct {
	Name           string
	InputRevision  string
	Preconditions  []string
	RollbackMethod string
	Irreversible   bool
	Cancellable    bool
	Retryable      bool
}

// StepError is the controlled diagnostic for a failed step (ENG-009). Cause
// is only set when Evidence backs it; the engine never invents one.
type StepError struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	// Cause is the established root cause, or empty when the evidence does not
	// support one.
	Cause            string   `json:"cause,omitempty"`
	Evidence         []string `json:"evidence,omitempty"`
	SuggestedActions []string `json:"suggestedActions,omitempty"`
	Retryable        bool     `json:"retryable"`
	// NothingChanged: the executor refused or never received this attempt,
	// so this attempt left nothing behind. The step still counts as executed
	// when an earlier attempt started (Step.Attempts > 1).
	NothingChanged bool `json:"nothingChanged,omitempty"`
}

func (e *StepError) Error() string { return e.Code + ": " + e.Summary }

// Check is one completion check of a job kind (ENG-012).
type Check struct {
	Name     string   `json:"name"`
	Required bool     `json:"required"`
	Passed   bool     `json:"passed"`
	Detail   string   `json:"detail,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

// CompletionReport is what Verify returns. The job succeeds only when every
// required check passed.
type CompletionReport struct {
	Checks []Check `json:"checks"`
}

// RequiredPassed reports whether no required check failed.
func (r CompletionReport) RequiredPassed() bool {
	for _, c := range r.Checks {
		if c.Required && !c.Passed {
			return false
		}
	}
	return true
}

// Diagnostics is the controlled view a user sees on failure, unknown or
// needs-attention (ENG-009). Logs are the retained tail; LogsTruncated and
// LogsDropped say what was lost (T-034).
type Diagnostics struct {
	ErrorSummary     string            `json:"errorSummary,omitempty"`
	Cause            string            `json:"cause,omitempty"`
	Logs             []string          `json:"logs,omitempty"`
	LogsTruncated    bool              `json:"logsTruncated"`
	LogsDropped      int               `json:"logsDropped,omitempty"`
	SystemState      map[string]string `json:"systemState,omitempty"`
	Evidence         []string          `json:"evidence,omitempty"`
	SuggestedActions []string          `json:"suggestedActions,omitempty"`
	// Residue lists what remains changed and not reverted (needs_attention,
	// cancelled).
	Residue []string `json:"residue,omitempty"`
}

// Job is the persisted record (spec 33.3 "每個工作至少保存").
type Job struct {
	ID          string          `json:"id"`
	RequestID   string          `json:"requestId"`
	RequestHash string          `json:"requestHash"`
	Kind        string          `json:"kind"`
	Params      json.RawMessage `json:"params,omitempty"`
	Requester   string          `json:"requester,omitempty"`
	Approver    string          `json:"approver,omitempty"`
	Node        string          `json:"node,omitempty"`
	Resources   []ResourceRef   `json:"resources,omitempty"`
	PlanVersion int             `json:"planVersion"`
	PlanSummary string          `json:"planSummary,omitempty"`
	// RequiredPermissions is recorded for ticket 009/016; not enforced here.
	RequiredPermissions []string `json:"requiredPermissions,omitempty"`

	State State `json:"state"`
	// StateReason is the plain-language reason for the current state
	// (why it is queued, why cancel is waiting, why it is unknown).
	StateReason string `json:"stateReason,omitempty"`
	// PriorState is the state the job was in when it became unknown, so
	// reconciliation resumes the right phase (steps, verification, rollback).
	PriorState State `json:"priorState,omitempty"`

	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`

	// ControlGeneration is recorded now and enforced by ticket 090 (ENG-010).
	ControlGeneration int64 `json:"controlGeneration"`

	Steps       []Step `json:"steps"`
	CurrentStep int    `json:"currentStep"`
	RetryCount  int    `json:"retryCount"`
	// Progress is 0–100 as reported by the handler.
	Progress int `json:"progress"`
	// Cancellable: a cancel request will be honoured now (not merely queued
	// behind an uninterruptible step).
	Cancellable bool `json:"cancellable"`
	// Reversible: every executed step so far has a rollback method.
	Reversible bool `json:"reversible"`
	// CancelRequested is set once a user asked for cancellation.
	CancelRequested bool `json:"cancelRequested"`

	Budget        Budget `json:"budget"`
	RequiredBytes int64  `json:"requiredBytes,omitempty"`

	Result      json.RawMessage   `json:"result,omitempty"`
	ErrorCode   string            `json:"errorCode,omitempty"`
	Completion  *CompletionReport `json:"completion,omitempty"`
	Diagnostics *Diagnostics      `json:"diagnostics,omitempty"`
	// Evidence links (event ids, log refs) collected over the job's life.
	Evidence []string `json:"evidence,omitempty"`
	// LastEventSeq is the sequence number of the newest event; a client that
	// reconnects asks for events after the last one it saw.
	LastEventSeq int64 `json:"lastEventSeq"`
}

// Event is one entry of the per-job event log. Seq is contiguous per job so a
// reconnecting UI resumes from the last seq it saw (spec 33.3).
type Event struct {
	JobID   string          `json:"jobId"`
	Seq     int64           `json:"seq"`
	At      time.Time       `json:"at"`
	Type    string          `json:"type"`
	Step    int             `json:"step,omitempty"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Event types.
const (
	EventStateChanged = "state_changed"
	EventStepStarted  = "step_started"
	EventStepFinished = "step_finished"
	EventLog          = "log"
	EventProgress     = "progress"
	EventHeartbeat    = "heartbeat"
	EventCheck        = "check"
	EventCancel       = "cancel_requested"
	EventLogTruncated = "log_truncated"
)

// StepReport is what a reconnecting executor says about a step whose outcome
// the Core lost (ENG-003, T-028).
type StepReport struct {
	Index int `json:"index"`
	// Completed: the executor's persistent record shows the step finished.
	Completed bool     `json:"completed"`
	Evidence  []string `json:"evidence,omitempty"`
	// Started: the executor's record shows the step began (and did not finish).
	Started bool `json:"started"`
}

// ListOptions filters Engine.List. Results are newest first unless
// Ascending is set; Page, when it has a Sort, replaces Limit and Ascending
// with a keyset page (sort fields: createdAt, updatedAt, kind, state, id).
type ListOptions struct {
	States    []State
	Kind      string
	Node      string
	Limit     int
	Ascending bool
	Page      paging.Query
}

// JobSortColumns maps list sort fields to job columns.
var JobSortColumns = map[string]paging.Column{
	"createdAt": {Expr: "created_at"},
	"updatedAt": {Expr: "updated_at"},
	"kind":      {Expr: "kind"},
	"state":     {Expr: "state"},
	"id":        {Expr: "id"},
}

// SortValue returns the value of a sort field of j, as JobSortColumns
// compares it.
func (j *Job) SortValue(field string) string {
	switch field {
	case "createdAt":
		return j.CreatedAt.UTC().Format(time.RFC3339Nano)
	case "updatedAt":
		return j.UpdatedAt.UTC().Format(time.RFC3339Nano)
	case "kind":
		return j.Kind
	case "state":
		return string(j.State)
	default:
		return j.ID
	}
}

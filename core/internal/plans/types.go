// Package plans is the change-plan engine (spec 33.1 "等待批准", 36.2,
// IAM-005, ENG-005; ticket 016.1).
//
// A plan is what a high-risk change will do, computed before anything is
// changed: its targets with the names people recognise and the versions they
// had, the diff, the services it affects, the prechecks and the approval it
// needs. Its digest covers all of that. An approval is bound to the digest
// and to the plan's deadline, and the plan is computed again before it is
// approved and again before it runs: when the content or a target's version
// moved, the approval no longer counts and the caller gets the difference.
//
// What a plan contains is decided by its Kind, registered by the ticket
// that owns the resource (compose projects, disks, engines…). The package
// itself knows nothing about any resource.
package plans

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// State is where a plan is in its life.
type State string

const (
	StateAwaitingApproval State = "awaiting_approval" // 等待批准
	StateApproved         State = "approved"          // 已批准，尚未執行
	StateRejected         State = "rejected"          // 被拒絕
	StateWithdrawn        State = "withdrawn"         // 提出者撤回
	StateExpired          State = "expired"           // 過了有效期限
	StateInvalidated      State = "invalidated"       // 內容或資源版本已改變，原批准失效
	StateExecuted         State = "executed"          // 已建立工作
)

// States lists every state.
var States = []State{StateAwaitingApproval, StateApproved, StateRejected, StateWithdrawn, StateExpired, StateInvalidated, StateExecuted}

// Errors. The API maps each to one error code.
var (
	ErrNotFound    = errors.New("plans: no such plan") // also: not visible to the caller
	ErrForbidden   = errors.New("plans: not allowed")
	ErrInvalid     = errors.New("plans: invalid request")
	ErrUnknownKind = errors.New("plans: no such plan kind")
	ErrState       = errors.New("plans: the plan is not in a state that allows this")
	ErrExpired     = errors.New("plans: the plan expired")
	ErrPrecheck    = errors.New("plans: a precheck failed")
	// ErrUnavailable: the job the plan runs as cannot be created here (no
	// handler for its kind on this Core).
	ErrUnavailable = errors.New("plans: the plan's job cannot run on this core")
	// ErrChanged comes wrapped in a *ChangedError.
	ErrChanged = errors.New("plans: the plan's content or a target's version changed")
	// ErrRequestIDTaken: execute was called again with a different request
	// id, or the request id is already used by another plan.
	ErrRequestIDTaken = errors.New("plans: the plan was executed with another request id")
)

// ForbiddenError says why an approval or decision was refused.
type ForbiddenError struct {
	// Reason: "missing-permission", "owner-required", "not-requester".
	Reason string
	Action iam.Action
	Scope  iam.Scope
}

func (e *ForbiddenError) Error() string   { return "plans: not allowed (" + e.Reason + ")" }
func (e *ForbiddenError) Is(t error) bool { return t == ErrForbidden }

// ChangedError is the conflict a caller sees when the plan it reviewed is
// no longer what would run (T-030): which targets moved from which version
// to which, and the changes as planned next to the changes as they would be
// computed now.
type ChangedError struct {
	PlanDigest string `json:"planDigest"`
	// CurrentDigest is the digest of the plan computed now; empty when it
	// could not be computed (a target is gone).
	CurrentDigest string `json:"currentDigest,omitempty"`
	// SentDigest is the digest the caller said it reviewed, when it differs
	// from PlanDigest.
	SentDigest string       `json:"sentDigest,omitempty"`
	Targets    []TargetMove `json:"targets"`
	Planned    []Change     `json:"planned"`
	Current    []Change     `json:"current"`
	// Invalidated: the plan is ended by this conflict; a new plan is needed.
	Invalidated bool `json:"invalidated"`
}

func (e *ChangedError) Error() string   { return "plans: the plan changed since it was reviewed" }
func (e *ChangedError) Is(t error) bool { return t == ErrChanged }

// TargetMove is a target whose version or presence differs.
type TargetMove struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	PlannedVersion int64  `json:"plannedVersion"`
	// CurrentVersion is 0 when the target no longer exists.
	CurrentVersion int64 `json:"currentVersion"`
}

// Kind is one sort of change that goes through a plan. Draft must be a pure
// function of params and the current state of the targets: computing it
// twice without anything changing yields the same content and digest.
type Kind interface {
	// Name is the plan kind, e.g. "compose.update".
	Name() string
	// Action is the permission needed on every target scope to create,
	// approve, reject or execute a plan of this kind.
	Action() iam.Action
	// JobKind is the job kind the plan runs as. Jobs of this kind can only be
	// created through a plan; POST /jobs refuses them.
	JobKind() string
	// Draft computes the plan for params from the current state. Wrap
	// ErrInvalid for bad params and ErrNotFound for a target that does not
	// exist. Draft runs before the caller's permissions are known (they come
	// from the targets it returns), so its error messages must not describe
	// a target: say "no such setting", not what the setting holds. Params are stored with the plan and shown to approvers, so they
	// name secrets by reference, never by value; a secret's value enters the
	// plan only as a Change marked Secret.
	Draft(ctx context.Context, params json.RawMessage) (*Draft, error)
	// Job is the job that carries the plan out. Its params should include the
	// target versions of the plan so the handler writes only if they are
	// still current (the plan is checked again right before Job is called,
	// but the job runs later). RequestID, Requester and the plan fields are
	// filled in by the service.
	Job(p *Plan) (jobs.SubmitRequest, error)
}

// Draft is what a Kind computes.
type Draft struct {
	// Summary is one line naming what changes where, with real names
	// ("update shop on hazel-tw-01 · production").
	Summary  string
	Targets  []Target
	Changes  []Change
	Affected []Affected
	Checks   []Check
	// RequireApproval: approval is needed even when no check is high risk.
	RequireApproval bool
	// TTL is how long the plan stays valid; 0 means DefaultTTL. It is
	// clamped to MaxTTL.
	TTL time.Duration
}

// Default and maximum plan validity.
const (
	DefaultTTL = time.Hour
	MaxTTL     = 24 * time.Hour
)

// Target is one resource the plan changes, with the names a person checks
// before approving (UI-006: real node, disk and environment names).
type Target struct {
	// Key is stable within the plan: "<kind>:<id>".
	Key  string `json:"key"`
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Node and NodeName, when the target lives on a node.
	Node     string `json:"node,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
	// Environment is the project environment name ("production"), when any.
	Environment string `json:"environment,omitempty"`
	// Device names the disk by its identity and as people see it.
	Device string `json:"device,omitempty"`
	// Scope owns the target for permissions and visibility.
	Scope iam.Scope `json:"scope"`
	// Version is the version of the target the plan was computed from;
	// 0 means the target does not exist yet (it is being created).
	Version int64 `json:"version"`
}

// Change is one line of the diff. A secret value is never stored or shown:
// Secret marks it, Before/After are dropped and the digest still covers it.
type Change struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
	Secret bool   `json:"secret,omitempty"`
	// Op: "add", "remove" or "change".
	Op string `json:"op"`
}

// Affected is a service or resource the change reaches.
type Affected struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Effect in plain words ("recreated, seamless replacement").
	Effect string `json:"effect"`
	// Relation: "direct", "indirect" or "unaffected".
	Relation string `json:"relation"`
}

// CheckStatus is the outcome of one precheck.
type CheckStatus string

const (
	CheckPass     CheckStatus = "pass"
	CheckWarning  CheckStatus = "warning"   // may be approved
	CheckHighRisk CheckStatus = "high_risk" // needs the owner's approval
	CheckFailed   CheckStatus = "failed"    // the plan cannot be approved or run
)

// Check is one precheck result.
type Check struct {
	ID     string      `json:"id"`
	Status CheckStatus `json:"status"`
	Title  string      `json:"title"`
	Detail string      `json:"detail,omitempty"`
	// Target is the key of the target the check is about, when any.
	Target   string   `json:"target,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

// Risk is the highest check status that is not "failed".
type Risk string

const (
	RiskLow     Risk = "low"
	RiskWarning Risk = "warning"
	RiskHigh    Risk = "high"
)

// Approval is what the plan needs and, once given, who gave it.
type Approval struct {
	Required bool       `json:"required"`
	Action   iam.Action `json:"action"`
	// OwnerRequired: only an owner may approve (a high-risk check).
	OwnerRequired bool       `json:"ownerRequired"`
	AutoApproved  bool       `json:"autoApproved"`
	ApprovedBy    *Actor     `json:"approvedBy,omitempty"`
	ApprovedAt    *time.Time `json:"approvedAt,omitempty"`
	// Digest the approval is bound to.
	Digest string `json:"digest,omitempty"`
	// ExpiresAt the approval is bound to (the plan's deadline).
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// AutoApprover is the approver a job records when its plan was approved on
// creation (no warning or high-risk check, the kind does not demand it).
const AutoApprover = "auto-approved"

// Actor names an account.
type Actor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Decision is how a plan ended without running.
type Decision struct {
	By     *Actor    `json:"by,omitempty"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// Execution is the job a plan became.
type Execution struct {
	RequestID string    `json:"requestId"`
	JobID     string    `json:"jobId,omitempty"`
	By        Actor     `json:"by"`
	At        time.Time `json:"at"`
}

// Plan is the stored plan.
type Plan struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	State       State           `json:"state"`
	Summary     string          `json:"summary"`
	Params      json.RawMessage `json:"params"`
	Digest      string          `json:"digest"`
	RequestedBy Actor           `json:"requestedBy"`
	CreatedAt   time.Time       `json:"createdAt"`
	ExpiresAt   time.Time       `json:"expiresAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	Targets     []Target        `json:"targets"`
	Changes     []Change        `json:"changes"`
	Affected    []Affected      `json:"affected"`
	Checks      []Check         `json:"checks"`
	Risk        Risk            `json:"risk"`
	// Executable is false when a check failed.
	Executable bool       `json:"executable"`
	Approval   Approval   `json:"approval"`
	Decision   *Decision  `json:"decision,omitempty"`
	Execution  *Execution `json:"execution,omitempty"`
	// Invalidation is the conflict that ended the plan, when it was
	// invalidated.
	Invalidation *ChangedError `json:"invalidation,omitempty"`

	salt []byte
	// storedState is the state column as read; State differs from it when
	// the plan is past its deadline but not yet written as expired.
	storedState State
}

// SortValue returns the value of a list sort field.
func (p *Plan) SortValue(field string) string {
	switch field {
	case "expiresAt":
		return stamp(p.ExpiresAt)
	case "kind":
		return p.Kind
	default:
		return stamp(p.CreatedAt)
	}
}

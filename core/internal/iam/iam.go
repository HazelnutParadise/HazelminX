// Package iam is the identity and access layer of hazelminx-core (spec
// chapter 5, ticket 009): local accounts, roles with resource scopes,
// login sessions, default-deny authorization, the audit ledger and login
// attempt limiting.
//
// Second factors (TOTP, passkeys, recovery codes), session lists and the
// per-user idle timeout are ticket 014 (mfa.go, passkey.go, sessions.go).
// API Tokens are ticket 015 and only the TokenAuthenticator seam exists here.
//
// Rules the rest of core relies on:
//   - Authorize answers deny unless a role assignment explicitly grants the
//     action on a scope covering the resource (IAM-001).
//   - Visible returns false when the principal has no read access at all, so
//     handlers answer 404 for guessed IDs instead of 403 (T-014).
//   - Every mutation and every login outcome is written to audit_events with
//     secrets masked (IAM-006).
//   - Login is throttled per account and per source, with a cap on the
//     cool-down and a trusted-source bypass, so nobody can lock out all
//     administrators for good (IAM-012).
package iam

import (
	"context"
	"errors"
	"time"
)

// Role is one of the 5.1 role presets. Automation and proxy-service users are
// token principals (ticket 015), not assignable roles.
type Role string

const (
	RoleOwner         Role = "owner"
	RolePlatformAdmin Role = "platform_admin"
	RoleHostOperator  Role = "host_operator"
	RoleProjectAdmin  Role = "project_admin"
	RoleDBOperator    Role = "db_operator"
	RoleAuditor       Role = "auditor"
)

// Roles lists the assignable roles in privilege order.
var Roles = []Role{RoleOwner, RolePlatformAdmin, RoleHostOperator, RoleProjectAdmin, RoleDBOperator, RoleAuditor}

// Action is a "<area>:<verb>" permission string checked by Authorize.
type Action string

const (
	ActionUsersRead         Action = "users:read"
	ActionUsersWrite        Action = "users:write"
	ActionRolesAssign       Action = "roles:assign"
	ActionAuditRead         Action = "audit:read"
	ActionNodesRead         Action = "nodes:read"
	ActionNodesWrite        Action = "nodes:write"
	ActionPlatformRead      Action = "platform:read"
	ActionPlatformWrite     Action = "platform:write"
	ActionAppsRead          Action = "apps:read"
	ActionAppsWrite         Action = "apps:write"
	ActionHostRead          Action = "host:read"
	ActionHostWrite         Action = "host:write"
	ActionProjectsRead      Action = "projects:read"
	ActionProjectsWrite     Action = "projects:write"
	ActionProjectsDeploy    Action = "projects:deploy"
	ActionDatabasesRead     Action = "databases:read"
	ActionDatabasesWrite    Action = "databases:write"
	ActionTerminalUse       Action = "terminal:use"
	ActionSecretsRead       Action = "secrets:read"
	ActionSecretsExport     Action = "secrets:export"
	ActionOwnershipTransfer Action = "ownership:transfer"
)

// ScopeKind names what a role assignment or a resource belongs to.
type ScopeKind string

const (
	ScopeGlobal   ScopeKind = "global"
	ScopeNode     ScopeKind = "node"
	ScopeProject  ScopeKind = "project"
	ScopeDatabase ScopeKind = "database"
)

// Scope is the owning unit of a resource. Global has ID "*".
type Scope struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id"`
}

// Global is the scope that covers everything.
var Global = Scope{Kind: ScopeGlobal, ID: "*"}

// Covers reports whether an assignment scope s grants access to resource scope r.
func (s Scope) Covers(r Scope) bool {
	if s.Kind == ScopeGlobal {
		return true
	}
	return s.Kind == r.Kind && s.ID == r.ID
}

// Assignment binds a role to a user within a scope.
type Assignment struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Role      Role      `json:"role"`
	Scope     Scope     `json:"scope"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy,omitempty"`
}

// User is a local account. The password hash never leaves the package.
type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// UserStatus values.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Session is a browser login. Token is only populated by Login; the store
// keeps a hash.
type Session struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	Token      string    `json:"-"`
	CSRFToken  string    `json:"-"`
	SourceIP   string    `json:"sourceIp"`
	UserAgent  string    `json:"userAgent"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	// SecondFactor is the method that completed the login ("" for none).
	SecondFactor string `json:"secondFactor"`
	// Renewed is set by Authenticate when it moved the idle expiry, so the
	// HTTP layer re-sends the cookie with the new lifetime.
	Renewed bool `json:"-"`
}

// Principal is who is acting: a user with their assignments, resolved once
// per request. Kind distinguishes browser sessions from API tokens.
type Principal struct {
	Kind        string       `json:"kind"` // "session" | "token"
	User        User         `json:"user"`
	Assignments []Assignment `json:"assignments"`
	SessionID   string       `json:"sessionId,omitempty"`
	// EnrollmentRequired is set for a session of an account that must use a
	// second factor (RequiresSecondFactor) but has none yet; the API layer
	// then only lets it reach the enrolment routes.
	EnrollmentRequired bool `json:"secondFactorEnrollmentRequired"`
}

// TokenAuthenticator is the seam for ticket 015. Nil means tokens are not
// accepted; the session middleware then only looks at cookies.
type TokenAuthenticator interface {
	Authenticate(ctx context.Context, token string) (*Principal, error)
}

// Source identifies where a request came from, for throttling, sessions and audit.
type Source struct {
	IP        string
	UserAgent string
	// TraceID identifies one HTTP call (X-Trace-ID). It is not the
	// operation request identifier that deduplicates jobs.
	TraceID string
	// RPID is the domain the browser is using, so a login challenge only
	// offers passkeys that can work there. Empty means "unknown".
	RPID string
}

// Errors returned by the service. Handlers map them to status codes; the
// messages never reveal whether an account exists.
var (
	ErrInvalidCredentials = errors.New("iam: invalid credentials")
	ErrThrottled          = errors.New("iam: too many attempts")
	ErrSessionInvalid     = errors.New("iam: session invalid")
	ErrForbidden          = errors.New("iam: forbidden")
	ErrNotFound           = errors.New("iam: not found")
	ErrConflict           = errors.New("iam: conflict")
	ErrSetupDone          = errors.New("iam: setup already completed")
	ErrSetupCode          = errors.New("iam: setup code invalid or expired")
	ErrValidation         = errors.New("iam: validation failed")
)

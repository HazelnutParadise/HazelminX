// Package nodereg is the node registry (ticket 019): enrollment codes and
// the hosts that used them, nodes with their persistent IDs, names, tags and
// groups, certificates and their rotation, revocation, maintenance, removal,
// and the connection status shown to administrators.
//
// The agent channel (package nodes) asks the registry whether a node may
// connect (AuthorizeNode), tells it about sessions (OnReady, OnClosed,
// OnConflict), and lets it refuse job steps (GateJob).
package nodereg

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Errors. API handlers map them to the management error codes.
var (
	ErrNotFound        = errors.New("nodereg: not found")
	ErrValidation      = errors.New("nodereg: validation failed")
	ErrState           = errors.New("nodereg: not allowed in the current state")
	ErrVersionMismatch = errors.New("nodereg: version mismatch")
	// ErrRemovalUnavailable: the removal mode exists but this platform
	// cannot carry it out yet.
	ErrRemovalUnavailable = errors.New("nodereg: removal mode not available")
	// ErrCodeNotFound: no usable enrollment code with that ID.
	ErrCodeNotFound = errors.New("nodereg: no usable enrollment code")
)

// ChoiceRequiredError: the host that used the code has a new key but the
// same machine-id as existing nodes; approving needs ReplaceNodeID or
// AsNewNode.
type ChoiceRequiredError struct{ Matches []NodeMatch }

func (e *ChoiceRequiredError) Error() string {
	return "nodereg: this host matches an existing node; choose to replace it or to create a new node"
}

func (e *ChoiceRequiredError) Unwrap() error { return ErrState }

// Node states.
const (
	StateActive           = "active"
	StateRevoked          = "revoked"
	StateIdentityConflict = "identity_conflict"
	StateRemoved          = "removed"
)

// Connection states. Offline only means the channel is down; it says
// nothing about whether the host or its applications are running.
const (
	ConnOnline         = "online"
	ConnStale          = "stale"
	ConnOffline        = "offline"
	ConnNeverConnected = "never_connected"
)

// Certificate states.
const (
	CertValid    = "valid"
	CertExpiring = "expiring"
	CertExpired  = "expired"
)

// What has not reached the agent yet.
const (
	SyncRevocation = "revocation"
	SyncRemoval    = "removal"
	SyncRotation   = "certificate-rotation"
	SyncRecheck    = "maintenance-recheck"
)

// Removal modes (NOD-009).
const (
	Unmanage          = "unmanage"
	RemoveAgent       = "remove-agent"
	RemoveClusterNode = "remove-cluster-node"
	RemoveWorkloads   = "delete-workloads"
)

// Component kinds on the node page (addendum 03 51.6; see ticket 019 Notes).
const (
	ComponentAgent       = "agent"
	ComponentCertificate = "node-certificate"
)

// Step gate codes.
const (
	CodeNodeRevoked       = "NODE_REVOKED"
	CodeNodeConflict      = "NODE_IDENTITY_CONFLICT"
	CodeNodeInMaintenance = "NODE_IN_MAINTENANCE"
	CodeNodeNotEnrolled   = "NODE_NOT_ENROLLED"
)

// Defaults.
const (
	DefaultCodeTTL      = 10 * time.Minute
	MinCodeTTL          = time.Minute
	MaxCodeTTL          = 60 * time.Minute
	DefaultApprovalTTL  = 24 * time.Hour
	DefaultCertValidity = 90 * 24 * time.Hour
	DefaultRenewBefore  = 30 * 24 * time.Hour
	// DefaultStaleAfter: no message for this long and the status is stale.
	// Agents send a heartbeat every 10 s and the channel drops a silent
	// session after 30 s, so the status changes within 40 s either way.
	DefaultStaleAfter = 30 * time.Second
	// ClockSkewWarning is the agent clock difference shown as a warning.
	ClockSkewWarning = 60 * time.Second
	deliverTimeout   = 15 * time.Second
)

// Channel is what the registry needs from the agent hub.
type Channel interface {
	Connections() ([]nodes.ConnectionInfo, []nodes.Rejection)
	Disconnect(fingerprint, reason string) bool
	RenewCertificate(ctx context.Context, fingerprint string, certPEM []byte) error
}

// JobLister lists a node's jobs (maintenance shows the affected work).
type JobLister interface {
	List(ctx context.Context, opts jobs.ListOptions) ([]*jobs.Job, error)
}

// Options configures a Registry. DB and PKI are required.
type Options struct {
	DB  *sql.DB
	PKI *nodes.PKI
	// IssuerKeyPEM is the privileged executor's grant issuer public key,
	// handed to each enrolled host.
	IssuerKeyPEM []byte
	Jobs         JobLister
	Now          func() time.Time
	Logger       *slog.Logger
	CodeTTL      time.Duration
	ApprovalTTL  time.Duration
	CertValidity time.Duration
	RenewBefore  time.Duration
	StaleAfter   time.Duration
}

// Registry is the node registry.
type Registry struct {
	opts    Options
	db      *sql.DB
	chMu    sync.RWMutex
	ch      Channel
	limiter *failureLimiter
	// rotating serializes certificate deliveries per node.
	rotMu    sync.Mutex
	rotating map[string]bool
}

// New builds a registry.
func New(opts Options) (*Registry, error) {
	if opts.DB == nil || opts.PKI == nil {
		return nil, errors.New("nodereg: DB and PKI are required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.CodeTTL <= 0 {
		opts.CodeTTL = DefaultCodeTTL
	}
	if opts.ApprovalTTL <= 0 {
		opts.ApprovalTTL = DefaultApprovalTTL
	}
	if opts.CertValidity <= 0 {
		opts.CertValidity = DefaultCertValidity
	}
	if opts.RenewBefore <= 0 {
		opts.RenewBefore = DefaultRenewBefore
	}
	if opts.StaleAfter <= 0 {
		opts.StaleAfter = DefaultStaleAfter
	}
	return &Registry{opts: opts, db: opts.DB, limiter: newFailureLimiter(20, 10*time.Minute), rotating: map[string]bool{}}, nil
}

// AttachChannel connects the registry to the hub (the hub is built with the
// registry as its Authorizer, so this happens after both exist).
func (r *Registry) AttachChannel(ch Channel) {
	r.chMu.Lock()
	r.ch = ch
	r.chMu.Unlock()
}

func (r *Registry) channel() Channel {
	r.chMu.RLock()
	defer r.chMu.RUnlock()
	return r.ch
}

func (r *Registry) now() time.Time { return r.opts.Now().UTC() }

// ---- shared helpers ---------------------------------------------------------

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func nullStamp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return stamp(*t)
}

func parseStamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t.UTC()
}

func parseNull(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseStamp(s.String)
	return &t
}

func newID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("nodereg: crypto/rand unavailable: " + err.Error())
	}
	return prefix + hex.EncodeToString(b)
}

func encodeTags(tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	b, _ := json.Marshal(tags)
	return string(b)
}

func decodeTags(s string) []string {
	var t []string
	_ = json.Unmarshal([]byte(s), &t)
	if t == nil {
		t = []string{}
	}
	return t
}

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

func statef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrState, fmt.Sprintf(format, args...))
}

// checkText accepts printable single-line text up to max characters.
func checkText(field, v string, max int) error {
	if len([]rune(v)) > max {
		return validationf("%s is longer than %d characters", field, max)
	}
	for _, c := range v {
		if !unicode.IsPrint(c) {
			return validationf("%s contains a control character", field)
		}
	}
	return nil
}

const (
	maxName  = 100
	maxGroup = 64
	maxTags  = 20
	maxTag   = 40
)

func checkTag(t string) error {
	if t == "" || len(t) > maxTag {
		return validationf("tag %q must be 1 to %d characters", t, maxTag)
	}
	for _, c := range t {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:", c)) {
			return validationf("tag %q may only contain letters, digits and - _ . :", t)
		}
	}
	return nil
}

func normalizeTags(tags []string) ([]string, error) {
	if len(tags) > maxTags {
		return nil, validationf("at most %d tags", maxTags)
	}
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if err := checkTag(t); err != nil {
			return nil, err
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out, nil
}

// capsJSON stores declared capabilities.
func capsJSON(caps []protocol.Capability) string {
	if caps == nil {
		caps = []protocol.Capability{}
	}
	b, _ := json.Marshal(caps)
	return string(b)
}

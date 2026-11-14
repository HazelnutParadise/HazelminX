// Package engines keeps what each node's host has of Docker, Podman and
// K3s (ticket 039.1). The report comes from the privileged executor's
// read-only engine_inspect, asked through the agent (host.engines v1) when
// a node connects, every Interval while it stays connected, and when an
// administrator asks for a refresh. Only the latest report is kept.
package engines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Grant the Core issues for each probe: read-only, short-lived.
const (
	GrantSubject    = "system:engine-probe"
	GrantPermission = "host.engines.read"
	GrantLifetime   = 5 * time.Minute
)

// ErrNoNode: the fingerprint belongs to no active node (pending,
// maintenance or revoked); nothing was asked or recorded.
var ErrNoNode = errors.New("engines: no active node for this session")

// Codes RecordError stores, besides the agent's engines_unavailable codes.
const (
	CodeAgentUnsupported = "AGENT_UNSUPPORTED"
	CodeNoAnswer         = "NO_ANSWER"
	CodeInvalidReport    = "INVALID_REPORT"
	CodeGrantFailed      = "GRANT_FAILED"
)

// maxClockAhead is how far ahead of Core's clock a report may be dated.
const maxClockAhead = 5 * time.Minute

const timeLayout = "2006-01-02T15:04:05Z"

func ts(t time.Time) string { return t.UTC().Truncate(time.Second).Format(timeLayout) }

// Store reads and writes host_engines (migration 0014).
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

// View is what Core knows about a node's engines.
type View struct {
	// Report is nil until the first report arrived.
	Report      *protocol.EngineReport
	CollectedAt *time.Time
	ReceivedAt  *time.Time
	// LastError is the latest failed probe, cleared by the next report.
	LastError *Failure
}

// Failure is a probe that produced no report.
type Failure struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// Get returns nil when Core never probed the node.
func (s *Store) Get(ctx context.Context, nodeID string) (*View, error) {
	var report, col, rec, code, msg, at sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT report, collected_at, received_at, error_code, error_message, error_at FROM host_engines WHERE node_id = ?`, nodeID).
		Scan(&report, &col, &rec, &code, &msg, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := &View{}
	if report.Valid {
		v.Report = &protocol.EngineReport{}
		if err := json.Unmarshal([]byte(report.String), v.Report); err != nil {
			return nil, fmt.Errorf("engines: stored report of %s: %w", nodeID, err)
		}
		c, _ := time.Parse(time.RFC3339, col.String)
		r, _ := time.Parse(time.RFC3339, rec.String)
		v.CollectedAt, v.ReceivedAt = &c, &r
	}
	if code.Valid {
		t, _ := time.Parse(time.RFC3339, at.String)
		v.LastError = &Failure{Code: code.String, Message: msg.String, At: t}
	}
	return v, nil
}

// ErrValidation: the report cannot be stored as it is.
var ErrValidation = errors.New("engines: invalid report")

// Validate checks what the decoder cannot: lists present, known kinds and
// states, one engine per kind, unique environment ids.
func Validate(r *protocol.EngineReport) error {
	if r.Engines == nil || r.Environments == nil || r.Uninspected == nil {
		return fmt.Errorf("%w: engines, environments and uninspected are required", ErrValidation)
	}
	kinds := map[string]bool{protocol.EngineDocker: true, protocol.EnginePodman: true, protocol.EngineK3s: true}
	states := map[string]bool{protocol.EngineRunning: true, protocol.EngineStopped: true, protocol.EngineNotInstalled: true,
		protocol.EngineIncompatible: true, protocol.EnginePermissionDenied: true, protocol.EngineNotResponding: true, protocol.EngineStateUnknown: true}
	seen := map[string]bool{}
	for _, e := range r.Engines {
		if !kinds[e.Kind] || seen[e.Kind] {
			return fmt.Errorf("%w: engine kind %q unknown or repeated", ErrValidation, e.Kind)
		}
		seen[e.Kind] = true
		if !states[e.State] {
			return fmt.Errorf("%w: engine %s state %q", ErrValidation, e.Kind, e.State)
		}
	}
	if len(r.Environments) > maxListItems || len(r.Uninspected) > maxListItems {
		return fmt.Errorf("%w: more than %d environments or uninspected entries", ErrValidation, maxListItems)
	}
	if err := checkStrings(reflect.ValueOf(r)); err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, e := range r.Environments {
		if e.ID == "" || ids[e.ID] {
			return fmt.Errorf("%w: environment id %q empty or repeated", ErrValidation, e.ID)
		}
		ids[e.ID] = true
		if !kinds[e.Kind] && e.Kind != protocol.EngineUnknown {
			return fmt.Errorf("%w: environment %s kind %q", ErrValidation, e.ID, e.Kind)
		}
		if !states[e.State] || e.State == protocol.EngineNotInstalled {
			return fmt.Errorf("%w: environment %s state %q", ErrValidation, e.ID, e.State)
		}
		if e.Mode != "rootful" && e.Mode != "rootless" {
			return fmt.Errorf("%w: environment %s mode %q", ErrValidation, e.ID, e.Mode)
		}
	}
	return nil
}

// Limits on agent-supplied data (the executor cuts strings to 256
// characters; Core does not rely on it).
const (
	maxListItems  = 1024
	maxStringRune = 512
)

// checkStrings refuses over-long strings and control or invisible
// formatting characters anywhere in the report.
func checkStrings(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return checkStrings(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := checkStrings(v.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if v.Len() > maxListItems {
			return fmt.Errorf("%w: list of %d items", ErrValidation, v.Len())
		}
		for i := 0; i < v.Len(); i++ {
			if err := checkStrings(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.String:
		s := v.String()
		if utf8.RuneCountInString(s) > maxStringRune {
			return fmt.Errorf("%w: string longer than %d characters", ErrValidation, maxStringRune)
		}
		for _, c := range s {
			if unicode.Is(unicode.Cc, c) || unicode.Is(unicode.Cf, c) {
				return fmt.Errorf("%w: control or formatting character U+%04X", ErrValidation, c)
			}
		}
	}
	return nil
}

// RecordReport stores a report, replacing the previous one and clearing
// the last error. A report dated before the stored one is ignored (a late
// answer); one dated too far ahead of Core's clock is ErrValidation.
func (s *Store) RecordReport(ctx context.Context, nodeID string, m *protocol.EnginesReport) error {
	if m.CollectedAt.After(s.now().Add(maxClockAhead)) {
		return fmt.Errorf("%w: collected at %s, ahead of Core's clock", ErrValidation, ts(m.CollectedAt))
	}
	if err := Validate(&m.Report); err != nil {
		return err
	}
	b, err := json.Marshal(m.Report)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO host_engines (node_id, report, collected_at, received_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (node_id) DO UPDATE SET report = excluded.report, collected_at = excluded.collected_at, received_at = excluded.received_at,
			error_code = NULL, error_message = NULL, error_at = NULL
		WHERE host_engines.collected_at IS NULL OR excluded.collected_at >= host_engines.collected_at`,
		nodeID, string(b), ts(m.CollectedAt), ts(s.now()))
	return err
}

// maxErrorMessage bounds the stored message (agent-supplied text).
const maxErrorMessage = 512

// RecordError stores a failed probe; the last report stays.
func (s *Store) RecordError(ctx context.Context, nodeID, code, message string) error {
	if r := []rune(message); len(r) > maxErrorMessage {
		message = string(r[:maxErrorMessage])
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO host_engines (node_id, error_code, error_message, error_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (node_id) DO UPDATE SET error_code = excluded.error_code, error_message = excluded.error_message, error_at = excluded.error_at`,
		nodeID, code, message, ts(s.now()))
	return err
}

// Hub asks a node's agent for a report (nodes.Hub implements it).
type Hub interface {
	ProbeEngines(ctx context.Context, fingerprint string, grant protocol.ExecutorGrant) (*protocol.EnginesReport, error)
}

// GrantIssuer signs executor grants (grants.Issuer implements it).
type GrantIssuer interface {
	Issue(subject string, perms []string, lifetime time.Duration) (protocol.ExecutorGrant, error)
}

// Prober asks agents for reports and stores them.
type Prober struct {
	Store  *Store
	Grants GrantIssuer
	Hub    Hub
	// NodeID maps a session fingerprint to its node id.
	NodeID func(ctx context.Context, fingerprint string) (string, error)
	// Timeout bounds one probe. Default 60 s (the executor looks at up to
	// a few hundred sockets, each bounded to 5 s, in parallel with nothing).
	Timeout time.Duration
	Logger  *slog.Logger

	once sync.Once
	mu   sync.Mutex
	busy map[string]*sync.Mutex
}

func (p *Prober) defaults() {
	if p.Timeout <= 0 {
		p.Timeout = 60 * time.Second
	}
	if p.Logger == nil {
		p.Logger = slog.Default()
	}
}

func (p *Prober) lock(fp string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy == nil {
		p.busy = map[string]*sync.Mutex{}
	}
	m, ok := p.busy[fp]
	if !ok {
		m = &sync.Mutex{}
		p.busy[fp] = m
	}
	return m
}

// Probe asks the node behind fingerprint for a report. One probe per node
// runs at a time; a second caller waits for the first and probes again.
//
// Expected results:
//   - node offline or session ended: that error, nothing recorded (the
//     next session probes again);
//   - agent without host.engines: AGENT_UNSUPPORTED recorded;
//   - the agent's engines_unavailable: its code and message recorded;
//   - no answer within Timeout: NO_ANSWER recorded;
//   - a report Validate refuses: INVALID_REPORT recorded;
//   - otherwise the report is stored and the error cleared.
func (p *Prober) Probe(ctx context.Context, fingerprint string) error {
	p.once.Do(p.defaults)
	m := p.lock(fingerprint)
	m.Lock()
	defer m.Unlock()
	nodeID, err := p.NodeID(ctx, fingerprint)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrNoNode, fingerprint, err)
	}
	grant, err := p.Grants.Issue(GrantSubject, []string{GrantPermission}, GrantLifetime)
	if err != nil {
		return p.fail(ctx, nodeID, CodeGrantFailed, err)
	}
	pctx, cancel := context.WithTimeout(ctx, p.Timeout)
	r, err := p.Hub.ProbeEngines(pctx, fingerprint, grant)
	cancel()
	var unavailable *nodes.EnginesUnavailableError
	switch {
	case err == nil:
	case errors.Is(err, nodes.ErrNodeOffline), errors.Is(err, nodes.ErrSessionClosed), ctx.Err() != nil:
		return err
	case errors.Is(err, nodes.ErrEnginesUnsupported):
		return p.fail(ctx, nodeID, CodeAgentUnsupported, err)
	case errors.As(err, &unavailable):
		return p.fail(ctx, nodeID, unavailable.Code, errors.New(unavailable.Message))
	default: // no answer within Timeout, or the session failed to send
		return p.fail(ctx, nodeID, CodeNoAnswer, err)
	}
	if err := p.Store.RecordReport(ctx, nodeID, r); err != nil {
		if errors.Is(err, ErrValidation) {
			return p.fail(ctx, nodeID, CodeInvalidReport, err)
		}
		return err
	}
	return nil
}

func (p *Prober) fail(ctx context.Context, nodeID, code string, err error) error {
	p.Logger.Warn("container engine probe failed", "node", nodeID, "code", code, "error", err.Error())
	if rerr := p.Store.RecordError(ctx, nodeID, code, err.Error()); rerr != nil {
		p.Logger.Error("cannot record container engine probe failure", "node", nodeID, "error", rerr.Error())
	}
	return fmt.Errorf("engines: %s: %w", code, err)
}

// Run probes every connected node fingerprints returns, every interval,
// until ctx ends. Sessions starting call Probe themselves (OnReady).
func (p *Prober) Run(ctx context.Context, fingerprints func() []string, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Each node on its own: a silent node must not delay the
			// others (Probe serializes probes of one node).
			var wg sync.WaitGroup
			for _, fp := range fingerprints() {
				wg.Add(1)
				go func() { defer wg.Done(); _ = p.Probe(ctx, fp) }()
			}
			wg.Wait()
		}
	}
}

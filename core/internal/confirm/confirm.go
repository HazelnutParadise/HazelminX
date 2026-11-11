// Package confirm keeps ROB-010 confirmations (addendum 03): a change that
// may cut the management path is applied, then must be confirmed by its
// operator from a connection opened after the change, or it is reverted.
//
// This package records confirmations and decides whether an attempt counts.
// Starting the countdown, reverting on expiry and resuming after a restart
// belong to ticket 024, which creates confirmations through Create.
//
// Limitation: "a new connection" is judged from the TCP connection that
// reaches Core. When a reverse proxy sits in front of the management entry,
// Core cannot see the browser's connections and the rule cannot be proven;
// ROB-010 assumes the browser reaches Core directly (engineering/architecture.md).
package confirm

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// State of a confirmation.
type State string

const (
	StatePending    State = "pending"
	StateConfirmed  State = "confirmed"
	StateExpired    State = "expired"
	StateRolledBack State = "rolled_back"
)

// Errors.
var (
	// ErrNotFound: no such confirmation, or it belongs to another user.
	ErrNotFound = errors.New("confirm: not found")
	// ErrExpired: the confirmation window is over (expired or already
	// rolled back); the attempt changes nothing.
	ErrExpired = errors.New("confirm: expired")
	// ErrNewConnectionRequired: the attempt did not come over a connection
	// opened after the change, or not through the required entry.
	ErrNewConnectionRequired = errors.New("confirm: a new connection is required")
	// ErrInvalid: Create input is incomplete.
	ErrInvalid = errors.New("confirm: invalid input")
)

// Confirmation is one record.
type Confirmation struct {
	ID        string             `json:"id"`
	UserID    string             `json:"userId"`
	Summary   string             `json:"summary,omitempty"`
	Resources []jobs.ResourceRef `json:"resources"`
	JobID     string             `json:"jobId,omitempty"`
	// RequireHost: the confirmation must arrive with this Host (a domain
	// change). Empty means any host.
	RequireHost string `json:"requireHost,omitempty"`
	// RequireLocalAddr: the confirmation must arrive on this local
	// address host:port (a listen address or port change); a wildcard host
	// such as 0.0.0.0 matches any interface on that port. Empty means any.
	RequireLocalAddr string     `json:"requireLocalAddr,omitempty"`
	State            State      `json:"state"`
	AppliedAt        time.Time  `json:"appliedAt"`
	ExpiresAt        time.Time  `json:"expiresAt"`
	ConfirmedAt      *time.Time `json:"confirmedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

// CreateInput is what ticket 024 passes when it applies a change.
type CreateInput struct {
	UserID           string
	Summary          string
	Resources        []jobs.ResourceRef
	JobID            string
	RequireHost      string
	RequireLocalAddr string
	AppliedAt        time.Time
	// Window is how long the operator has to confirm (60 s, 10 min for a
	// domain change).
	Window time.Duration
}

// Attempt describes one confirmation request.
type Attempt struct {
	UserID string
	// ConnOpenedAt is when the TCP connection carrying the request was
	// accepted.
	ConnOpenedAt time.Time
	Host         string // request Host, without changing case
	LocalAddr    string // local address host:port the connection arrived on
}

// Service stores confirmations in the platform database.
type Service struct {
	db  *sql.DB
	now func() time.Time
}

// New returns a Service. now may be nil (time.Now).
func New(db *sql.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

// Create records a pending confirmation and returns it.
//
// Expected results:
//   - ID is "cfm_" + 24 lowercase hex characters from crypto/rand.
//   - State pending; ExpiresAt = AppliedAt + Window; CreatedAt = UpdatedAt
//     = now(); all times UTC, stored as RFC 3339 (nanoseconds kept).
//   - empty UserID, no Resources, zero AppliedAt or Window <= 0 →
//     ErrInvalid (wrapped, message names the field); nothing stored.
//   - a UserID that is not in users → the foreign key error is returned
//     (wrapped), nothing stored.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Confirmation, error) {
	switch {
	case in.UserID == "":
		return nil, fmt.Errorf("%w: userId is required", ErrInvalid)
	case len(in.Resources) == 0:
		return nil, fmt.Errorf("%w: at least one resource is required", ErrInvalid)
	case in.AppliedAt.IsZero():
		return nil, fmt.Errorf("%w: appliedAt is required", ErrInvalid)
	case in.Window <= 0:
		return nil, fmt.Errorf("%w: window must be positive", ErrInvalid)
	}
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	c := &Confirmation{ID: "cfm_" + hex.EncodeToString(b[:]), UserID: in.UserID, Summary: in.Summary, Resources: in.Resources, JobID: in.JobID,
		RequireHost: in.RequireHost, RequireLocalAddr: in.RequireLocalAddr, State: StatePending,
		AppliedAt: in.AppliedAt.UTC(), ExpiresAt: in.AppliedAt.UTC().Add(in.Window), CreatedAt: now, UpdatedAt: now}
	res, err := json.Marshal(c.Resources)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO confirmations(id,user_id,summary,resources,job_id,require_host,require_local_addr,state,applied_at,expires_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.UserID, c.Summary, string(res), c.JobID, c.RequireHost, c.RequireLocalAddr, string(c.State),
		ts(c.AppliedAt), ts(c.ExpiresAt), ts(now), ts(now))
	if err != nil {
		return nil, fmt.Errorf("confirm: create: %w", err)
	}
	return c, nil
}

// Get returns the confirmation as userID may see it.
//
// Expected results:
//   - unknown id, or a confirmation of another user → ErrNotFound (the same
//     error, so ids of other users cannot be probed).
//   - a pending confirmation whose ExpiresAt is before now() is returned
//     with State expired, and that state is persisted (UpdatedAt = now()).
//   - other states are returned as stored.
func (s *Service) Get(ctx context.Context, id, userID string) (*Confirmation, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.UserID != userID {
		return nil, ErrNotFound
	}
	return s.expireIfDue(ctx, c)
}

// Confirm applies an attempt.
//
// Expected results, checked in this order:
//   - not visible to a.UserID → ErrNotFound.
//   - State confirmed → the record unchanged and nil error (idempotent: a
//     retried confirmation gets the same answer).
//   - State expired or rolled_back, or pending but now() after ExpiresAt
//     (then persisted as expired) → ErrExpired.
//   - a.ConnOpenedAt not strictly after AppliedAt → ErrNewConnectionRequired
//     wrapped with "connection opened before the change".
//   - RequireHost set and a.Host not equal ignoring case (and ignoring a
//     ":port" suffix on either side) → ErrNewConnectionRequired wrapped
//     with "confirm through <RequireHost>".
//   - RequireLocalAddr set and a.LocalAddr != RequireLocalAddr →
//     ErrNewConnectionRequired wrapped with "confirm through
//     <RequireLocalAddr>".
//   - otherwise State confirmed, ConfirmedAt = UpdatedAt = now(), persisted
//     with a conditional update (WHERE state = 'pending') so two concurrent
//     attempts confirm once; the loser re-reads and returns the confirmed
//     record with nil error.
//   - a rejected attempt changes nothing in the database.
func (s *Service) Confirm(ctx context.Context, id string, a Attempt) (*Confirmation, error) {
	c, err := s.Get(ctx, id, a.UserID)
	if err != nil {
		return nil, err
	}
	switch c.State {
	case StateConfirmed:
		return c, nil
	case StateExpired, StateRolledBack:
		return nil, ErrExpired
	}
	if !a.ConnOpenedAt.After(c.AppliedAt) {
		return nil, fmt.Errorf("%w: connection opened before the change; open a new connection and confirm again", ErrNewConnectionRequired)
	}
	if c.RequireHost != "" && !strings.EqualFold(hostOnly(a.Host), hostOnly(c.RequireHost)) {
		return nil, fmt.Errorf("%w: confirm through %s", ErrNewConnectionRequired, c.RequireHost)
	}
	if c.RequireLocalAddr != "" && !sameLocalAddr(c.RequireLocalAddr, a.LocalAddr) {
		return nil, fmt.Errorf("%w: confirm through %s", ErrNewConnectionRequired, c.RequireLocalAddr)
	}
	now := s.now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE confirmations SET state='confirmed', confirmed_at=?, updated_at=? WHERE id=? AND state='pending'`,
		ts(now), ts(now), id); err != nil {
		return nil, err
	}
	// Whoever won the conditional update, the record is now confirmed.
	c, err = s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.State != StateConfirmed {
		return nil, ErrExpired
	}
	return c, nil
}

// sameLocalAddr compares the address a change requires with the one a
// connection arrived on. A wildcard host (0.0.0.0, ::, empty) in the
// requirement means "any interface", so only the port is compared.
func sameLocalAddr(want, got string) bool {
	if want == got {
		return true
	}
	wh, wp, err := net.SplitHostPort(want)
	if err != nil {
		return false
	}
	gh, gp, err := net.SplitHostPort(got)
	if err != nil || wp != gp {
		return false
	}
	if ip := net.ParseIP(wh); wh == "" || (ip != nil && ip.IsUnspecified()) {
		return true
	}
	return wh == gh
}

// hostOnly drops a ":port" suffix.
func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

// MarkRolledBack records that ticket 024 reverted the change.
//
// Expected results:
//   - pending or expired → rolled_back, UpdatedAt = now(), nil.
//   - confirmed → error wrapping ErrInvalid (a confirmed change is kept).
//   - rolled_back → nil (idempotent).
//   - unknown id → ErrNotFound.
func (s *Service) MarkRolledBack(ctx context.Context, id string) error {
	c, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	switch c.State {
	case StateRolledBack:
		return nil
	case StateConfirmed:
		return fmt.Errorf("%w: confirmation %s was confirmed; the change is kept", ErrInvalid, id)
	}
	now := ts(s.now().UTC())
	_, err = s.db.ExecContext(ctx, `UPDATE confirmations SET state='rolled_back', updated_at=? WHERE id=? AND state IN ('pending','expired')`, now, id)
	return err
}

// PendingFor returns the pending (not yet expired) confirmation whose
// Resources overlap any of res, using the job lock identity rules
// (jobs.ResourcesOverlap), or nil when none. Writes to those resources are
// refused with CONFIRMATION_REQUIRED meanwhile.
//
// Expected results:
//   - none pending → nil, nil.
//   - pending on {kind:"node-network", id:"n1"} and res {kind:"node-network",
//     id:"n1"} → that confirmation.
//   - res on another node or another id → nil.
//   - a pending one past ExpiresAt does not count (and is persisted as
//     expired).
//   - several match → the one with the earliest ExpiresAt.
func (s *Service) PendingFor(ctx context.Context, res []jobs.ResourceRef) (*Confirmation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM confirmations WHERE state='pending' ORDER BY expires_at, id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		c, err := s.load(ctx, id)
		if err != nil {
			return nil, err
		}
		if c, err = s.expireIfDue(ctx, c); err != nil {
			return nil, err
		}
		if c.State == StatePending && jobs.ResourcesOverlap(c.Resources, res) {
			return c, nil
		}
	}
	return nil, nil
}

// load reads one record regardless of owner.
func (s *Service) load(ctx context.Context, id string) (*Confirmation, error) {
	var c Confirmation
	var res, state, applied, expires, created, updated string
	var confirmed sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id,summary,resources,job_id,require_host,require_local_addr,state,applied_at,expires_at,confirmed_at,created_at,updated_at
		FROM confirmations WHERE id=?`, id).Scan(&c.ID, &c.UserID, &c.Summary, &res, &c.JobID, &c.RequireHost, &c.RequireLocalAddr, &state,
		&applied, &expires, &confirmed, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(res), &c.Resources); err != nil {
		return nil, fmt.Errorf("confirm: resources of %s: %w", id, err)
	}
	c.State = State(state)
	c.AppliedAt, c.ExpiresAt, c.CreatedAt, c.UpdatedAt = pt(applied), pt(expires), pt(created), pt(updated)
	if confirmed.Valid {
		t := pt(confirmed.String)
		c.ConfirmedAt = &t
	}
	return &c, nil
}

// expireIfDue records that a pending confirmation's window is over.
func (s *Service) expireIfDue(ctx context.Context, c *Confirmation) (*Confirmation, error) {
	now := s.now().UTC()
	if c.State != StatePending || !now.After(c.ExpiresAt) {
		return c, nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE confirmations SET state='expired', updated_at=? WHERE id=? AND state='pending'`, ts(now), c.ID); err != nil {
		return nil, err
	}
	return s.load(ctx, c.ID)
}

// ts writes times with a fixed nine-digit fraction so that their text order
// is their time order (RFC3339Nano drops trailing zeros).
func ts(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") }

func pt(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

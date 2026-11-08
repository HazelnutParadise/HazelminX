package hostledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// ErrLedgerReplaced: the host now reports a different ledger id than the
// one Core's copy follows (the host ledger was recreated or swapped). Core
// keeps the lines it has and stores nothing from the new ledger until a
// person decides.
var ErrLedgerReplaced = errors.New("hostledger: host reports a different ledger than Core's copy")

// Copy is the state of Core's copy for one node.
type Copy struct {
	NodeID   string
	LedgerID string // "" until the first page is stored
	Head     protocol.LedgerHead
	// LastSyncedAt is when a sync last completed without error.
	LastSyncedAt *time.Time
	// LastErrorCode and LastError describe the latest failed attempt;
	// empty after a success.
	LastErrorCode string
	LastError     string
	LastErrorAt   *time.Time
}

// Store is the SQL side of the copy (migration 0008).
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore uses db (the platform database); now may be nil.
func NewStore(db *sql.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

func (s *Store) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func get(ctx context.Context, q queryer, nodeID string) (Copy, bool, error) {
	c := Copy{NodeID: nodeID}
	var synced, errAt sql.NullString
	err := q.QueryRowContext(ctx, `SELECT ledger_id, head_seq, head_hash, last_synced_at, last_error_code, last_error, last_error_at
		FROM host_ledgers WHERE node_id = ?`, nodeID).
		Scan(&c.LedgerID, &c.Head.Seq, &c.Head.Hash, &synced, &c.LastErrorCode, &c.LastError, &errAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, fmt.Errorf("hostledger: read copy of %s: %w", nodeID, err)
	}
	c.LastSyncedAt = parseTime(synced)
	c.LastErrorAt = parseTime(errAt)
	return c, true, nil
}

func parseTime(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, v.String)
	if err != nil {
		return nil
	}
	return &t
}

// Get returns the copy of nodeID; ok is false when Core holds nothing.
func (s *Store) Get(ctx context.Context, nodeID string) (Copy, bool, error) {
	return get(ctx, s.db, nodeID)
}

// Append verifies page against the copy of nodeID and stores its lines in
// one transaction. A page that does not continue the copy stores nothing.
//
// Expected results: the first page creates the copy (ledger id from the
// page); later pages must carry the same ledger id (else ErrLedgerReplaced)
// and continue the head (else ErrChain); the error columns are cleared and
// last_synced_at is set when the page was the last one (More false).
func (s *Store) Append(ctx context.Context, nodeID string, page *protocol.LedgerPage) (protocol.LedgerHead, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.LedgerHead{}, fmt.Errorf("hostledger: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	cur, ok, err := get(ctx, tx, nodeID)
	if err != nil {
		return protocol.LedgerHead{}, err
	}
	var prev *protocol.LedgerHead
	if ok && cur.LedgerID != "" {
		if cur.LedgerID != page.LedgerID {
			return cur.Head, fmt.Errorf("%w: copy follows %s, host reports %s", ErrLedgerReplaced, cur.LedgerID, page.LedgerID)
		}
		prev = &cur.Head
	}
	lines, head, err := VerifyPage(prev, page)
	if err != nil {
		return cur.Head, err
	}
	now := s.stamp()
	if !ok {
		if _, err := tx.ExecContext(ctx, `INSERT INTO host_ledgers (node_id, created_at, updated_at) VALUES (?, ?, ?)`, nodeID, now, now); err != nil {
			return cur.Head, fmt.Errorf("hostledger: create copy of %s: %w", nodeID, err)
		}
	}
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx, `INSERT INTO host_ledger_lines (node_id, ledger_id, seq, hash, line, received_at) VALUES (?, ?, ?, ?, ?, ?)`,
			nodeID, page.LedgerID, l.Seq, l.Hash, l.Raw, now); err != nil {
			return cur.Head, fmt.Errorf("hostledger: store line %d of %s: %w", l.Seq, nodeID, err)
		}
	}
	synced := sql.NullString{}
	if !page.More {
		synced = sql.NullString{String: now, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE host_ledgers SET ledger_id = ?, head_seq = ?, head_hash = ?, updated_at = ?,
		last_synced_at = COALESCE(?, last_synced_at), last_error_code = '', last_error = '', last_error_at = NULL
		WHERE node_id = ?`, page.LedgerID, head.Seq, head.Hash, now, synced, nodeID); err != nil {
		return cur.Head, fmt.Errorf("hostledger: update head of %s: %w", nodeID, err)
	}
	if err := tx.Commit(); err != nil {
		return cur.Head, fmt.Errorf("hostledger: commit: %w", err)
	}
	return head, nil
}

// RecordError notes why the latest sync of nodeID failed. message must not
// contain secrets (it comes from the agent or this package).
func (s *Store) RecordError(ctx context.Context, nodeID, code, message string) error {
	now := s.stamp()
	if len(message) > 1024 {
		message = message[:1024]
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO host_ledgers (node_id, created_at, updated_at, last_error_code, last_error, last_error_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (node_id) DO UPDATE SET updated_at = excluded.updated_at, last_error_code = excluded.last_error_code,
			last_error = excluded.last_error, last_error_at = excluded.last_error_at`,
		nodeID, now, now, code, message, now)
	if err != nil {
		return fmt.Errorf("hostledger: record error for %s: %w", nodeID, err)
	}
	return nil
}

// Lines returns up to limit lines of nodeID's current ledger after afterSeq,
// in order.
func (s *Store) Lines(ctx context.Context, nodeID string, afterSeq uint64, limit int) ([]Line, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.seq, l.hash, l.line FROM host_ledger_lines l
		JOIN host_ledgers h ON h.node_id = l.node_id AND h.ledger_id = l.ledger_id
		WHERE l.node_id = ? AND l.seq > ? ORDER BY l.seq LIMIT ?`, nodeID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("hostledger: read lines of %s: %w", nodeID, err)
	}
	defer rows.Close()
	var out []Line
	for rows.Next() {
		var l Line
		if err := rows.Scan(&l.Seq, &l.Hash, &l.Raw); err != nil {
			return nil, fmt.Errorf("hostledger: read lines of %s: %w", nodeID, err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

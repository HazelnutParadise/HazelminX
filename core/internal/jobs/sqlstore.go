package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SQLStore persists jobs in the platform SQLite database (migration 0002).
// Queryable columns mirror the JSON document; the document is the record.
type SQLStore struct {
	db *sql.DB
}

// NewSQLStore wraps an opened platform database.
func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

const jobColumns = `id, request_id, request_hash, kind, node, requester, state, state_reason, error_code, progress, last_event_seq,
	created_at, updated_at, started_at, finished_at, last_heartbeat_at, doc`

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func rfcPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return rfc(*t)
}

func (s *SQLStore) CreateJob(ctx context.Context, j *Job) error {
	doc, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO jobs (`+jobColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.RequestID, j.RequestHash, j.Kind, j.Node, j.Requester, string(j.State), j.StateReason, j.ErrorCode, j.Progress, j.LastEventSeq,
		rfc(j.CreatedAt), rfc(j.UpdatedAt), rfcPtr(j.StartedAt), rfcPtr(j.FinishedAt), rfcPtr(j.LastHeartbeatAt), string(doc))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

func (s *SQLStore) UpdateJob(ctx context.Context, j *Job) error {
	doc, err := json.Marshal(j)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET state=?, state_reason=?, error_code=?, progress=?, last_event_seq=?, updated_at=?,
		started_at=?, finished_at=?, last_heartbeat_at=?, doc=? WHERE id=?`,
		string(j.State), j.StateReason, j.ErrorCode, j.Progress, j.LastEventSeq, rfc(j.UpdatedAt),
		rfcPtr(j.StartedAt), rfcPtr(j.FinishedAt), rfcPtr(j.LastHeartbeatAt), string(doc), j.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLStore) Touch(ctx context.Context, id string, heartbeatAt time.Time, progress int) error {
	// The document is patched in SQL so the whole record is not rewritten
	// on every heartbeat.
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET last_heartbeat_at=?, progress=?, updated_at=?,
		doc=json_set(doc, '$.lastHeartbeatAt', ?, '$.progress', ?, '$.updatedAt', ?) WHERE id=?`,
		rfc(heartbeatAt), progress, rfc(heartbeatAt), rfc(heartbeatAt), progress, rfc(heartbeatAt), id)
	return err
}

func (s *SQLStore) GetJob(ctx context.Context, id string) (*Job, error) {
	return s.one(ctx, `SELECT doc FROM jobs WHERE id=?`, id)
}

func (s *SQLStore) GetJobByRequestID(ctx context.Context, requestID string) (*Job, error) {
	return s.one(ctx, `SELECT doc FROM jobs WHERE request_id=?`, requestID)
}

func (s *SQLStore) one(ctx context.Context, q string, arg any) (*Job, error) {
	var doc string
	if err := s.db.QueryRowContext(ctx, q, arg).Scan(&doc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return decodeJob(doc)
}

func decodeJob(doc string) (*Job, error) {
	var j Job
	if err := json.Unmarshal([]byte(doc), &j); err != nil {
		return nil, fmt.Errorf("jobs: decode record: %w", err)
	}
	return &j, nil
}

func (s *SQLStore) ListJobs(ctx context.Context, opts ListOptions) ([]*Job, error) {
	q := `SELECT doc FROM jobs`
	var where []string
	var args []any
	if len(opts.States) > 0 {
		ph := make([]string, len(opts.States))
		for i, st := range opts.States {
			ph[i] = "?"
			args = append(args, string(st))
		}
		where = append(where, "state IN ("+strings.Join(ph, ",")+")")
	}
	if opts.Kind != "" {
		where = append(where, "kind=?")
		args = append(args, opts.Kind)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if opts.Ascending {
		q += " ORDER BY created_at ASC, id ASC"
	} else {
		q += " ORDER BY created_at DESC, id DESC"
	}
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	q += " LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		var doc string
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		j, err := decodeJob(doc)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *SQLStore) AppendEvent(ctx context.Context, e *Event) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(seq),0)+1 FROM job_events WHERE job_id=?`, e.JobID).Scan(&seq); err != nil {
		return 0, err
	}
	var data any
	if len(e.Data) > 0 {
		data = string(e.Data)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO job_events (job_id, seq, at, type, step, message, data) VALUES (?,?,?,?,?,?,?)`,
		e.JobID, seq, rfc(e.At), e.Type, e.Step, e.Message, data); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET last_event_seq=?, doc=json_set(doc,'$.lastEventSeq',?) WHERE id=?`, seq, seq, e.JobID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	e.Seq = seq
	return seq, nil
}

func (s *SQLStore) ListEvents(ctx context.Context, jobID string, afterSeq int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, at, type, step, message, data FROM job_events WHERE job_id=? AND seq>? ORDER BY seq LIMIT ?`, jobID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var at string
		var data sql.NullString
		if err := rows.Scan(&e.Seq, &at, &e.Type, &e.Step, &e.Message, &data); err != nil {
			return nil, err
		}
		e.JobID = jobID
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		if data.Valid {
			e.Data = json.RawMessage(data.String)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Package hostmetrics keeps what agents report about their hosts (ticket
// 020.1): the latest facts, the latest full sample, and the history of a
// fixed set of series at two resolutions.
//
//   - Raw points: one per sample at the agent's interval (10 s by
//     default), kept RawRetention (48 h).
//   - 5-minute rollups: average, minimum, maximum and sample count, kept
//     RollupRetention (90 days).
//
// Nothing is filled in: a value the agent did not provide has no point,
// and a time without samples has no point either. History marks a point
// whose predecessor is more than two intervals earlier with GapBefore, so
// readers draw a gap instead of a line (MON-005).
package hostmetrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

const (
	RawRetention    = 48 * time.Hour
	RollupRetention = 90 * 24 * time.Hour
	// RollupSeconds is the rollup resolution.
	RollupSeconds = 300
	// maxKeysPerFamily bounds how many disks, interfaces, filesystems,
	// cores or sensors of one sample get history; the latest sample still
	// carries all of them.
	maxKeysPerFamily = 64
	// maxClockAhead is how far in the future (by Core's clock) a sample or
	// facts may be; later ones come from a wrong agent clock and would
	// freeze the latest view and history until real time caught up.
	maxClockAhead = 5 * time.Minute
	// maxIntervalSeconds bounds the sampling interval an agent may claim.
	maxIntervalSeconds = 3600
)

// Resolutions of History.
const (
	ResolutionRaw    = "raw"
	Resolution5m     = "5m"
	timeLayout       = "2006-01-02T15:04:05Z"
	maxSamplesPerMsg = 1000
)

var ErrValidation = errors.New("hostmetrics: validation failed")

// Store reads and writes the tables of migration 0011.
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

func ts(t time.Time) string { return t.UTC().Truncate(time.Second).Format(timeLayout) }

// usable reports whether a sample time is one Core keeps: not ahead of
// Core's clock by more than maxClockAhead and not older than the raw
// retention (a point already pruned would be counted into its rollup
// again).
func (s *Store) usable(at time.Time) bool {
	now := s.now()
	return !at.After(now.Add(maxClockAhead)) && !at.Before(now.Add(-RawRetention))
}

// RecordFacts stores a node's facts, replacing older ones. Facts collected
// before the stored ones are ignored (a late resend), and so are facts
// from a clock too far ahead (ErrValidation).
func (s *Store) RecordFacts(ctx context.Context, nodeID string, m *protocol.HostFacts) error {
	if m.CollectedAt.After(s.now().Add(maxClockAhead)) {
		return fmt.Errorf("%w: facts collected at %s, ahead of Core's clock", ErrValidation, ts(m.CollectedAt))
	}
	b, err := json.Marshal(m.Facts)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO host_facts (node_id, facts, collected_at, received_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (node_id) DO UPDATE SET facts = excluded.facts, collected_at = excluded.collected_at, received_at = excluded.received_at
		WHERE excluded.collected_at >= host_facts.collected_at`,
		nodeID, string(b), ts(m.CollectedAt), ts(s.now()))
	return err
}

// RecordReport stores the samples of one report in one transaction and
// returns the highest seq it holds, which the agent may forget. A sample
// Core already has (same node and collection time, resent after a lost
// acknowledgement) changes nothing. Samples Core cannot use (interval
// outside 1..3600 s, a time ahead of Core's clock or past the raw
// retention) are left out and counted in Skipped; they are acknowledged
// too, since sending them again would not change them. Only a database
// failure is an error, and then nothing is stored.
func (s *Store) RecordReport(ctx context.Context, nodeID string, m *protocol.MetricsReport) (uint64, error) {
	r, err := s.Record(ctx, nodeID, m)
	return r.Seq, err
}

// Recorded is what RecordReport did with a report.
type Recorded struct {
	// Seq is the highest seq of the report.
	Seq uint64
	// Skipped counts the samples left out as unusable.
	Skipped int
}

// Record is RecordReport with the count of skipped samples.
func (s *Store) Record(ctx context.Context, nodeID string, m *protocol.MetricsReport) (Recorded, error) {
	if len(m.Samples) == 0 || len(m.Samples) > maxSamplesPerMsg {
		return Recorded{}, fmt.Errorf("%w: a report carries 1 to %d samples", ErrValidation, maxSamplesPerMsg)
	}
	var out Recorded
	keep := make([]*protocol.MetricsSample, 0, len(m.Samples))
	for i := range m.Samples {
		smp := &m.Samples[i]
		out.Seq = max(out.Seq, smp.Seq)
		if smp.IntervalSeconds == 0 || smp.IntervalSeconds > maxIntervalSeconds || !s.usable(smp.CollectedAt) {
			out.Skipped++
			continue
		}
		keep = append(keep, smp)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Recorded{}, err
	}
	defer tx.Rollback()
	received := ts(s.now())
	for _, smp := range keep {
		at := ts(smp.CollectedAt)
		bucket := ts(smp.CollectedAt.UTC().Truncate(RollupSeconds * time.Second))
		for _, p := range Points(smp) {
			res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO host_metric_points (node_id, series, series_key, at, value, interval_seconds) VALUES (?, ?, ?, ?, ?, ?)`,
				nodeID, p.Series, p.Key, at, p.Value, smp.IntervalSeconds)
			if err != nil {
				return Recorded{}, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue // already stored: already in its rollup
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO host_metric_rollups (node_id, series, series_key, bucket_start, value_sum, value_min, value_max, samples) VALUES (?, ?, ?, ?, ?, ?, ?, 1)
				ON CONFLICT (node_id, series, series_key, bucket_start) DO UPDATE SET value_sum = value_sum + excluded.value_sum,
					value_min = min(value_min, excluded.value_min), value_max = max(value_max, excluded.value_max), samples = samples + 1`,
				nodeID, p.Series, p.Key, bucket, p.Value, p.Value, p.Value); err != nil {
				return Recorded{}, err
			}
		}
		raw, err := json.Marshal(smp)
		if err != nil {
			return Recorded{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO host_metric_latest (node_id, sample, collected_at, received_at, interval_seconds) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (node_id) DO UPDATE SET sample = excluded.sample, collected_at = excluded.collected_at, received_at = excluded.received_at, interval_seconds = excluded.interval_seconds
			WHERE excluded.collected_at > host_metric_latest.collected_at`,
			nodeID, string(raw), at, received, smp.IntervalSeconds); err != nil {
			return Recorded{}, err
		}
	}
	if m.DroppedSamples > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE host_metric_latest SET dropped_samples = dropped_samples + ? WHERE node_id = ?`, m.DroppedSamples, nodeID); err != nil {
			return Recorded{}, err
		}
	}
	return out, tx.Commit()
}

// Facts is a node's latest facts; nil when it never reported any.
type Facts struct {
	CollectedAt time.Time              `json:"collectedAt"`
	ReceivedAt  time.Time              `json:"receivedAt"`
	Facts       protocol.HostFactsData `json:"facts"`
}

func (s *Store) Facts(ctx context.Context, nodeID string) (*Facts, error) {
	var raw, col, rec string
	err := s.db.QueryRowContext(ctx, `SELECT facts, collected_at, received_at FROM host_facts WHERE node_id = ?`, nodeID).Scan(&raw, &col, &rec)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f := &Facts{}
	if err := json.Unmarshal([]byte(raw), &f.Facts); err != nil {
		return nil, err
	}
	f.CollectedAt, _ = time.Parse(time.RFC3339, col)
	f.ReceivedAt, _ = time.Parse(time.RFC3339, rec)
	return f, nil
}

// Latest is a node's latest full sample; nil when it never sent one.
type Latest struct {
	CollectedAt     time.Time              `json:"collectedAt"`
	ReceivedAt      time.Time              `json:"receivedAt"`
	IntervalSeconds int                    `json:"intervalSeconds"`
	DroppedSamples  int64                  `json:"droppedSamples"`
	Sample          protocol.MetricsSample `json:"sample"`
}

func (s *Store) Latest(ctx context.Context, nodeID string) (*Latest, error) {
	var raw, col, rec string
	l := &Latest{}
	err := s.db.QueryRowContext(ctx, `SELECT sample, collected_at, received_at, interval_seconds, dropped_samples FROM host_metric_latest WHERE node_id = ?`, nodeID).
		Scan(&raw, &col, &rec, &l.IntervalSeconds, &l.DroppedSamples)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &l.Sample); err != nil {
		return nil, err
	}
	l.CollectedAt, _ = time.Parse(time.RFC3339, col)
	l.ReceivedAt, _ = time.Parse(time.RFC3339, rec)
	return l, nil
}

// HistoryQuery selects one series of one node in [From, To).
type HistoryQuery struct {
	NodeID     string
	Series     string
	Key        string
	Resolution string
	From, To   time.Time
}

// Point is one history point. For raw points Value, Min and Max are the
// sample's value and Samples is 1; for rollups Value is the average.
// GapBefore: the previous point of this series is more than two
// resolutions earlier (or there is none and the window starts earlier),
// so the line must break before this point.
type Point struct {
	At                time.Time `json:"at"`
	ResolutionSeconds int       `json:"resolutionSeconds"`
	Value             float64   `json:"value"`
	Min               float64   `json:"min"`
	Max               float64   `json:"max"`
	Samples           int       `json:"samples"`
	GapBefore         bool      `json:"gapBefore"`
}

// SortValue is the value of the only sort field, at.
func (p Point) SortValue() string { return ts(p.At) }

func (q HistoryQuery) table() (sel, from string) {
	if q.Resolution == Resolution5m {
		return `bucket_start, ` + strconv.Itoa(RollupSeconds) + `, value_sum / samples, value_min, value_max, samples,
			COALESCE((SELECT CAST(strftime('%s', p.bucket_start) AS INTEGER) - CAST(strftime('%s', MAX(o.bucket_start)) AS INTEGER) > 2 * ` + strconv.Itoa(RollupSeconds) + `
				FROM host_metric_rollups o WHERE o.node_id = p.node_id AND o.series = p.series AND o.series_key = p.series_key AND o.bucket_start < p.bucket_start), 1)`,
			`host_metric_rollups p`
	}
	return `at, interval_seconds, value, value, value, 1,
		COALESCE((SELECT CAST(strftime('%s', p.at) AS INTEGER) - CAST(strftime('%s', MAX(o.at)) AS INTEGER) > 2 * p.interval_seconds
			FROM host_metric_points o WHERE o.node_id = p.node_id AND o.series = p.series AND o.series_key = p.series_key AND o.at < p.at), 1)`,
		`host_metric_points p`
}

func (q HistoryQuery) col() string {
	if q.Resolution == Resolution5m {
		return "p.bucket_start"
	}
	return "p.at"
}

// History reads one page of points in page order.
func (s *Store) History(ctx context.Context, q HistoryQuery, page paging.Query) ([]Point, error) {
	order, where, args, err := page.SQL(map[string]paging.Column{"at": {Expr: q.col()}})
	if err != nil {
		return nil, err
	}
	sel, from := q.table()
	stmt := `SELECT ` + sel + ` FROM ` + from + ` WHERE p.node_id = ? AND p.series = ? AND p.series_key = ? AND ` + q.col() + ` >= ? AND ` + q.col() + ` < ?`
	all := []any{q.NodeID, q.Series, q.Key, ts(q.From), ts(q.To)}
	if where != "" {
		stmt += ` AND (` + where + `)`
		all = append(all, args...)
	}
	stmt += ` ORDER BY ` + order + ` LIMIT ?`
	all = append(all, page.Limit)
	rows, err := s.db.QueryContext(ctx, stmt, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var at string
		var gap int
		if err := rows.Scan(&at, &p.ResolutionSeconds, &p.Value, &p.Min, &p.Max, &p.Samples, &gap); err != nil {
			return out, err
		}
		p.At, _ = time.Parse(time.RFC3339, at)
		p.GapBefore = gap == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountHistory counts the points History pages through.
func (s *Store) CountHistory(ctx context.Context, q HistoryQuery) (int, error) {
	_, from := q.table()
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+from+` WHERE p.node_id = ? AND p.series = ? AND p.series_key = ? AND `+q.col()+` >= ? AND `+q.col()+` < ?`,
		q.NodeID, q.Series, q.Key, ts(q.From), ts(q.To)).Scan(&n)
	return n, err
}

// Prune deletes raw points and rollups past their retention.
func (s *Store) Prune(ctx context.Context) error {
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM host_metric_points WHERE at < ?`, ts(now.Add(-RawRetention))); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM host_metric_rollups WHERE bucket_start < ?`, ts(now.Add(-RollupRetention)))
	return err
}

// RunPruner prunes every interval until ctx ends.
func (s *Store) RunPruner(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		_ = s.Prune(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

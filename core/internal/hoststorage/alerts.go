package hoststorage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// Rule is a set of capacity alert thresholds. NodeID "" applies to every
// node and MountPoint "" to every filesystem. A nil threshold is off.
type Rule struct {
	NodeID                    string    `json:"nodeId"`
	MountPoint                string    `json:"mountPoint"`
	WarnAvailablePercent      *float64  `json:"warnAvailablePercent"`
	CriticalAvailablePercent  *float64  `json:"criticalAvailablePercent"`
	CriticalAvailableBytes    *int64    `json:"criticalAvailableBytes"`
	WarnInodesFreePercent     *float64  `json:"warnInodesFreePercent"`
	CriticalInodesFreePercent *float64  `json:"criticalInodesFreePercent"`
	WarnFullWithinDays        *int64    `json:"warnFullWithinDays"`
	UpdatedAt                 time.Time `json:"updatedAt"`
	UpdatedBy                 string    `json:"updatedBy"`
	// BuiltIn: no rule was stored for this scope; these are the defaults.
	BuiltIn bool `json:"builtIn"`
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }

// DefaultRule applies when no stored rule matches: warn under 15% free
// space or 10% free inodes or when the trend fills the filesystem within
// 14 days; critical under 5% or 2 GiB free space or 3% free inodes. The
// 2 GiB floor matches the platform's own low-disk threshold (addendum 03,
// ROB-004). A byte floor only applies to filesystems at least ten times its
// size, so a small /boot is judged by percentages alone.
func DefaultRule() Rule {
	return Rule{WarnAvailablePercent: f64(15), CriticalAvailablePercent: f64(5), CriticalAvailableBytes: i64(2 << 30),
		WarnInodesFreePercent: f64(10), CriticalInodesFreePercent: f64(3), WarnFullWithinDays: i64(14), BuiltIn: true}
}

// Validate checks the thresholds of a rule a caller sent.
func (r Rule) Validate() error {
	for name, v := range map[string]*float64{"warnAvailablePercent": r.WarnAvailablePercent, "criticalAvailablePercent": r.CriticalAvailablePercent,
		"warnInodesFreePercent": r.WarnInodesFreePercent, "criticalInodesFreePercent": r.CriticalInodesFreePercent} {
		if v != nil && (math.IsNaN(*v) || *v < 0 || *v > 100) {
			return fmt.Errorf("%w: %s must be between 0 and 100", ErrValidation, name)
		}
	}
	if r.CriticalAvailableBytes != nil && *r.CriticalAvailableBytes < 0 {
		return fmt.Errorf("%w: criticalAvailableBytes must not be negative", ErrValidation)
	}
	if r.WarnFullWithinDays != nil && (*r.WarnFullWithinDays < 1 || *r.WarnFullWithinDays > 3650) {
		return fmt.Errorf("%w: warnFullWithinDays must be between 1 and 3650", ErrValidation)
	}
	if r.MountPoint != "" && r.MountPoint[0] != '/' {
		return fmt.Errorf("%w: mountPoint must be an absolute path or empty", ErrValidation)
	}
	return nil
}

const ruleCols = `node_id, mount_point, warn_available_percent, critical_available_percent, critical_available_bytes,
	warn_inodes_free_percent, critical_inodes_free_percent, warn_full_within_days, updated_at, updated_by`

func scanRule(row interface{ Scan(...any) error }) (Rule, error) {
	var r Rule
	var at string
	var wa, ca, wi, ci sql.NullFloat64
	var cb, wd sql.NullInt64
	if err := row.Scan(&r.NodeID, &r.MountPoint, &wa, &ca, &cb, &wi, &ci, &wd, &at, &r.UpdatedBy); err != nil {
		return r, err
	}
	pf := func(v sql.NullFloat64) *float64 {
		if !v.Valid {
			return nil
		}
		return &v.Float64
	}
	pi := func(v sql.NullInt64) *int64 {
		if !v.Valid {
			return nil
		}
		return &v.Int64
	}
	r.WarnAvailablePercent, r.CriticalAvailablePercent, r.CriticalAvailableBytes = pf(wa), pf(ca), pi(cb)
	r.WarnInodesFreePercent, r.CriticalInodesFreePercent, r.WarnFullWithinDays = pf(wi), pf(ci), pi(wd)
	r.UpdatedAt = parseTS(at)
	return r, nil
}

// Rules lists the stored rules, default row first. The built-in default
// is listed when no default row is stored.
func (s *Store) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ruleCols+` FROM storage_alert_rules ORDER BY node_id, mount_point`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 || out[0].NodeID != "" || out[0].MountPoint != "" {
		out = append([]Rule{DefaultRule()}, out...)
	}
	return out, nil
}

// PutRule stores a rule for its scope, replacing the one there.
func (s *Store) PutRule(ctx context.Context, r Rule, actor string) (Rule, error) {
	if err := r.Validate(); err != nil {
		return Rule{}, err
	}
	r.UpdatedAt, r.UpdatedBy, r.BuiltIn = s.now().UTC().Truncate(time.Second), actor, false
	_, err := s.db.ExecContext(ctx, `INSERT INTO storage_alert_rules (`+ruleCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (node_id, mount_point) DO UPDATE SET warn_available_percent = excluded.warn_available_percent,
		critical_available_percent = excluded.critical_available_percent, critical_available_bytes = excluded.critical_available_bytes,
		warn_inodes_free_percent = excluded.warn_inodes_free_percent, critical_inodes_free_percent = excluded.critical_inodes_free_percent,
		warn_full_within_days = excluded.warn_full_within_days, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		r.NodeID, r.MountPoint, r.WarnAvailablePercent, r.CriticalAvailablePercent, r.CriticalAvailableBytes,
		r.WarnInodesFreePercent, r.CriticalInodesFreePercent, r.WarnFullWithinDays, ts(r.UpdatedAt), actor)
	return r, err
}

// DeleteRule removes the rule of a scope; ErrNotFound when there is none.
func (s *Store) DeleteRule(ctx context.Context, nodeID, mountPoint string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM storage_alert_rules WHERE node_id = ? AND mount_point = ?`, nodeID, mountPoint)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RuleFor is the rule that applies to a node's mount point.
func (s *Store) RuleFor(ctx context.Context, nodeID, mountPoint string) (Rule, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+ruleCols+` FROM storage_alert_rules
		WHERE node_id IN (?, '') AND mount_point IN (?, '')
		ORDER BY node_id = '' , mount_point = '' LIMIT 1`, nodeID, mountPoint)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultRule(), nil
	}
	return r, err
}

// Alert is one capacity condition of one filesystem.
type Alert struct {
	// Kind: availableLow, inodesLow or fullSoon. Read-only filesystems are
	// flagged by the metrics exhaustion view (ROB-009), not here.
	Kind string `json:"kind"`
	// Level: warning or critical.
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Capacity is what the latest sample says about a filesystem.
type Capacity struct {
	TotalBytes     *uint64 `json:"totalBytes"`
	AvailableBytes *uint64 `json:"availableBytes"`
	UsedBytes      *uint64 `json:"usedBytes"`
	InodesTotal    *uint64 `json:"inodesTotal"`
	InodesFree     *uint64 `json:"inodesFree"`
	ReadOnly       bool    `json:"readOnly"`
}

// Evaluate returns the alerts of one filesystem. growthPerDay is the
// trend of used bytes (nil when there is not enough history). Values the
// host did not provide raise no alert; they are not taken as zero.
func Evaluate(r Rule, c Capacity, growthPerDay *float64) []Alert {
	out := []Alert{}
	if c.TotalBytes != nil && c.AvailableBytes != nil && *c.TotalBytes > 0 {
		pct := float64(*c.AvailableBytes) / float64(*c.TotalBytes) * 100
		switch {
		case r.CriticalAvailablePercent != nil && pct < *r.CriticalAvailablePercent,
			r.CriticalAvailableBytes != nil && *c.TotalBytes/10 >= uint64(*r.CriticalAvailableBytes) && int64(*c.AvailableBytes) < *r.CriticalAvailableBytes:
			out = append(out, Alert{Kind: "availableLow", Level: "critical", Message: fmt.Sprintf("%.1f%% (%d bytes) available", pct, *c.AvailableBytes)})
		case r.WarnAvailablePercent != nil && pct < *r.WarnAvailablePercent:
			out = append(out, Alert{Kind: "availableLow", Level: "warning", Message: fmt.Sprintf("%.1f%% (%d bytes) available", pct, *c.AvailableBytes)})
		}
		if r.WarnFullWithinDays != nil && growthPerDay != nil && *growthPerDay > 0 {
			days := float64(*c.AvailableBytes) / *growthPerDay
			if days < float64(*r.WarnFullWithinDays) {
				out = append(out, Alert{Kind: "fullSoon", Level: "warning", Message: fmt.Sprintf("full in about %.0f days at the 7-day trend", math.Floor(days))})
			}
		}
	}
	if c.InodesTotal != nil && c.InodesFree != nil && *c.InodesTotal > 0 {
		pct := float64(*c.InodesFree) / float64(*c.InodesTotal) * 100
		switch {
		case r.CriticalInodesFreePercent != nil && pct < *r.CriticalInodesFreePercent:
			out = append(out, Alert{Kind: "inodesLow", Level: "critical", Message: fmt.Sprintf("%.1f%% of inodes free", pct)})
		case r.WarnInodesFreePercent != nil && pct < *r.WarnInodesFreePercent:
			out = append(out, Alert{Kind: "inodesLow", Level: "warning", Message: fmt.Sprintf("%.1f%% of inodes free", pct)})
		}
	}
	return out
}

// Growth is the least-squares slope of (time, used bytes) points in bytes
// per day; nil with fewer than 2 points or less than a day between the
// first and the last.
func Growth(at []time.Time, used []float64) *float64 {
	if len(at) < 2 || len(at) != len(used) || at[len(at)-1].Sub(at[0]) < 24*time.Hour {
		return nil
	}
	t0 := at[0]
	var n, sx, sy, sxx, sxy float64
	for i := range at {
		x := at[i].Sub(t0).Hours() / 24
		y := used[i]
		n++
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return nil
	}
	g := (n*sxy - sx*sy) / den
	return &g
}

package iam

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Session controls of ticket 014 (IAM-003, SET-004): a user lists and
// revokes their own browser sessions and chooses their idle timeout.
// Revocation is immediate because Authenticate reads revoked_at on every
// request; nothing caches a session.

// Bounds of the per-user idle timeout. The absolute lifetime (SessionMax)
// is not user-adjustable.
const (
	MinSessionIdle = 15 * time.Minute
	MaxSessionIdle = 24 * time.Hour
)

// SessionInfo is one row of the user's session list. It never carries the
// token or the CSRF value.
type SessionInfo struct {
	ID           string    `json:"id"`
	SourceIP     string    `json:"sourceIp"`
	UserAgent    string    `json:"userAgent"`
	SecondFactor string    `json:"secondFactor"` // "" | totp | passkey | recovery-code
	CreatedAt    time.Time `json:"createdAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Current      bool      `json:"current"`
}

// SessionSettings is what the user may tune about their own sessions.
type SessionSettings struct {
	IdleTimeoutSeconds int  `json:"idleTimeoutSeconds"`
	Default            bool `json:"default"` // true when the platform default applies
	MinSeconds         int  `json:"minSeconds"`
	MaxSeconds         int  `json:"maxSeconds"`
	AbsoluteMaxSeconds int  `json:"absoluteMaxSeconds"`
}

// ListSessions returns the caller's live sessions, newest first, with the
// one making the request marked Current. Token principals have no session
// and get ErrForbidden: session management is a browser concern.
func (s *Service) ListSessions(ctx context.Context, p *Principal) ([]SessionInfo, error) {
	if p == nil || p.Kind != "session" {
		return nil, ErrForbidden
	}
	now := s.now()
	rows, err := s.db.QueryContext(ctx, `SELECT id,source_ip,user_agent,second_factor,created_at,last_seen_at,expires_at
		FROM sessions WHERE user_id=? AND revoked_at IS NULL ORDER BY created_at DESC`, p.User.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionInfo{}
	for rows.Next() {
		var si SessionInfo
		var c, l, e string
		if err := rows.Scan(&si.ID, &si.SourceIP, &si.UserAgent, &si.SecondFactor, &c, &l, &e); err != nil {
			return nil, err
		}
		si.CreatedAt, si.LastSeenAt, si.ExpiresAt = pt(c), pt(l), pt(e)
		if !now.Before(si.ExpiresAt) || !now.Before(si.CreatedAt.Add(s.cfg.SessionMax)) {
			continue
		}
		si.Current = si.ID == p.SessionID
		out = append(out, si)
	}
	return out, rows.Err()
}

// RevokeSession ends one of the caller's sessions. Another user's session
// ID and an unknown ID both answer ErrNotFound (T-014). Revoking the current
// session is allowed and behaves like logout.
func (s *Service) RevokeSession(ctx context.Context, p *Principal, id string, src Source) error {
	if p == nil || p.Kind != "session" {
		return ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND user_id=? AND revoked_at IS NULL`, ts(s.now()), id, p.User.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "session.revoke", Scope: Global,
		ResourceKind: "session", ResourceID: id, Result: ResultOK, After: map[string]any{"current": id == p.SessionID},
		TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeOtherSessions ends every session of the caller except the current
// one and returns how many were ended.
func (s *Service) RevokeOtherSessions(ctx context.Context, p *Principal, src Source) (int, error) {
	if p == nil || p.Kind != "session" {
		return 0, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND id<>? AND revoked_at IS NULL`, ts(s.now()), p.User.ID, p.SessionID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "session.revoke-others", Scope: Global,
		ResourceKind: "user", ResourceID: p.User.ID, Result: ResultOK, After: map[string]any{"revoked": n},
		TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}

// SessionSettings returns the caller's idle timeout and its bounds.
func (s *Service) SessionSettings(ctx context.Context, p *Principal) (*SessionSettings, error) {
	if p == nil {
		return nil, ErrForbidden
	}
	idle, isDefault, err := s.userIdle(ctx, s.db, p.User.ID)
	if err != nil {
		return nil, err
	}
	return &SessionSettings{IdleTimeoutSeconds: int(idle / time.Second), Default: isDefault,
		MinSeconds: int(MinSessionIdle / time.Second), MaxSeconds: int(MaxSessionIdle / time.Second),
		AbsoluteMaxSeconds: int(s.cfg.SessionMax / time.Second)}, nil
}

// SetSessionIdle sets the caller's idle timeout; 0 restores the platform
// default. It applies to the current session at once: the current session's
// expiry is recomputed from now, and every other live session of the user
// picks it up on its next request.
func (s *Service) SetSessionIdle(ctx context.Context, p *Principal, idle time.Duration, src Source) (*SessionSettings, error) {
	if p == nil || p.Kind != "session" {
		return nil, ErrForbidden
	}
	if idle != 0 && (idle < MinSessionIdle || idle > MaxSessionIdle) {
		return nil, fmt.Errorf("%w: idle timeout must be between %d and %d seconds", ErrValidation, int(MinSessionIdle/time.Second), int(MaxSessionIdle/time.Second))
	}
	before, _, err := s.userIdle(ctx, s.db, p.User.ID)
	if err != nil {
		return nil, err
	}
	var v any
	if idle != 0 {
		v = int64(idle / time.Second)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET session_idle_seconds=?, updated_at=? WHERE id=?`, v, ts(s.now()), p.User.ID); err != nil {
		return nil, err
	}
	after, _, err := s.userIdle(ctx, tx, p.User.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_seen_at=?, expires_at=? WHERE id=?`, ts(now), ts(now.Add(after)), p.SessionID); err != nil {
		return nil, err
	}
	if after < before {
		if err := capOtherSessions(ctx, tx, p, after); err != nil {
			return nil, err
		}
	}
	if err := s.record(ctx, tx, AuditEvent{ActorID: p.User.ID, ActorName: p.User.Username, Action: "session.settings", Scope: Global,
		ResourceKind: "user", ResourceID: p.User.ID, Result: ResultOK,
		Before: map[string]any{"idleTimeoutSeconds": int(before / time.Second)}, After: map[string]any{"idleTimeoutSeconds": int(after / time.Second)},
		TraceID: src.TraceID, SourceIP: src.IP}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.SessionSettings(ctx, p)
}

// capOtherSessions shortens the user's other live sessions to the new idle
// window counted from their last activity, so an idle cookie elsewhere does
// not outlive a shorter setting.
func capOtherSessions(ctx context.Context, tx *sql.Tx, p *Principal, idle time.Duration) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,last_seen_at,expires_at FROM sessions WHERE user_id=? AND id<>? AND revoked_at IS NULL`, p.User.ID, p.SessionID)
	if err != nil {
		return err
	}
	type cap struct{ id, expires string }
	var caps []cap
	for rows.Next() {
		var id, last, exp string
		if err := rows.Scan(&id, &last, &exp); err != nil {
			rows.Close()
			return err
		}
		if limit := pt(last).Add(idle); limit.Before(pt(exp)) {
			caps = append(caps, cap{id, ts(limit)})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range caps {
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET expires_at=? WHERE id=?`, c.expires, c.id); err != nil {
			return err
		}
	}
	return nil
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// userIdle is the idle window for a user's sessions: their own setting, or
// the platform default (Config.SessionIdle).
func (s *Service) userIdle(ctx context.Context, q queryRower, userID string) (time.Duration, bool, error) {
	var secs sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT session_idle_seconds FROM users WHERE id=?`, userID).Scan(&secs); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, ErrNotFound
		}
		return 0, false, err
	}
	if !secs.Valid || secs.Int64 <= 0 {
		return s.cfg.SessionIdle, true, nil
	}
	return time.Duration(secs.Int64) * time.Second, false, nil
}

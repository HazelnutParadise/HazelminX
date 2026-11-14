package github

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Repository inventory and browsing for the source picker (ticket 048.1,
// spec 7.4 and 15). Everything here is tied to one connection the actor
// owns: a repository reached through two connections is two entries, and
// a failure of one connection never makes another one answer instead.

// Source sync states.
const (
	SyncPending     = "pending"
	SyncSyncing     = "syncing"
	SyncComplete    = "complete"
	SyncFailed      = "failed"
	SyncRateLimited = "rate-limited"
)

// Overall sync states of a connection.
const (
	InventoryNever       = "never"        // never synced
	InventorySyncing     = "syncing"      // more pages to read, nothing failed
	InventoryComplete    = "complete"     // every source read to its end
	InventoryPartial     = "partial"      // a source failed; the rest is shown
	InventoryRateLimited = "rate-limited" // waiting for GitHub's limit to reset
	// InventoryStale: read under an earlier authorization (the scopes,
	// permissions, repositories or expiry changed since); shown, never
	// complete, re-read by the next sync step.
	InventoryStale = "stale"
)

// generationOf fingerprints what a connection is allowed to see. Rows read
// under another generation are stale (spec 15.9: the cache includes the
// authorization and the permissions observed).
func generationOf(c Connection) string {
	exp := ""
	if c.AccessExpiresAt != nil {
		exp = c.AccessExpiresAt.UTC().Format(time.RFC3339)
	}
	b, _ := json.Marshal([]any{c.Host, c.Kind, c.Account.ID, c.InstallationID, c.TokenType, c.Scopes, c.Permissions, c.RepositoryIDs, c.RepositorySelection, exp})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// SyncPageBudget is how many GitHub pages one sync step reads at most, so a
// step stays a short request; the caller runs further steps.
const SyncPageBudget = 10

// minRateWait is the wait used when GitHub rate limits without saying how
// long.
const minRateWait = time.Minute

// ErrEmptyRepository: the repository has no commits yet.
var ErrEmptyRepository = errors.New("github: the repository is empty")

// RateLimitedError: GitHub limited this connection; no call is made for it
// before RetryAt.
type RateLimitedError struct{ RetryAt time.Time }

func (e *RateLimitedError) Error() string {
	return "github: rate limited by GitHub until " + e.RetryAt.UTC().Format(time.RFC3339)
}
func (e *RateLimitedError) Unwrap() error { return ErrUnavailable }

// SourceStatus is the sync state of one source.
type SourceStatus struct {
	Key          string     `json:"key"`
	Kind         string     `json:"kind"`
	Login        string     `json:"login,omitempty"`
	State        string     `json:"state"`
	NextPage     int        `json:"nextPage,omitempty"`
	Repositories int        `json:"repositories"`
	Error        string     `json:"error,omitempty"`
	RetryAt      *time.Time `json:"retryAt,omitempty"`
	SyncedAt     *time.Time `json:"syncedAt,omitempty"`
	// Stale: read under an earlier authorization of the connection.
	Stale bool `json:"stale,omitempty"`

	gen string
}

// SyncStatus is the inventory state of one connection. Complete is true
// only when every source was read to its end.
type SyncStatus struct {
	ConnectionID string         `json:"connectionId"`
	State        string         `json:"state"`
	Complete     bool           `json:"complete"`
	Repositories int            `json:"repositories"`
	RetryAt      *time.Time     `json:"retryAt,omitempty"`
	Sources      []SourceStatus `json:"sources"`
}

// RepoItem is a repository as one connection sees it.
type RepoItem struct {
	Repository
	ConnectionID    string `json:"connectionId"`
	ConnectionName  string `json:"connectionName"`
	ConnectionKind  string `json:"connectionKind"`
	ConnectionState string `json:"connectionState"`
	// SeenAt is when a sync last saw the repository through this
	// connection.
	SeenAt time.Time `json:"seenAt"`
	// Stale: seen under an earlier authorization of the connection; the
	// next sync step confirms or removes it.
	Stale bool `json:"stale,omitempty"`
}

// RepoFilter narrows ListRepositories.
type RepoFilter struct {
	ConnectionID string
	RepoID       int64
	// Query matches owner/name, case-insensitively.
	Query      string
	Owner      string
	Visibility string
	Archived   *bool
}

func (s *Service) sourcesOf(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, connID string) ([]SourceStatus, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.source_key, s.kind, s.login, s.state, s.next_page, s.error, s.retry_at, s.synced_at, s.generation,
		(SELECT COUNT(*) FROM github_repositories r WHERE r.connection_id=s.connection_id AND r.source_key=s.source_key)
		FROM github_repo_sources s WHERE s.connection_id=? ORDER BY CASE s.kind WHEN 'org' THEN 1 ELSE 0 END, s.source_key`, connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceStatus
	for rows.Next() {
		var st SourceStatus
		var retry, synced sql.NullString
		if err := rows.Scan(&st.Key, &st.Kind, &st.Login, &st.State, &st.NextPage, &st.Error, &retry, &synced, &st.gen, &st.Repositories); err != nil {
			return nil, err
		}
		st.RetryAt, st.SyncedAt = parseTSPtr(retry), parseTSPtr(synced)
		if st.State == SyncComplete || st.State == SyncFailed {
			st.NextPage = 0
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// RepositorySync returns the inventory state of a connection.
func (s *Service) RepositorySync(ctx context.Context, connID string, actor Actor) (SyncStatus, error) {
	c, err := s.GetConnection(ctx, connID, actor)
	if err != nil {
		return SyncStatus{}, err
	}
	return s.syncStatus(ctx, c)
}

func (s *Service) syncStatus(ctx context.Context, c Connection) (SyncStatus, error) {
	connID := c.ID
	gen := generationOf(c)
	srcs, err := s.sourcesOf(ctx, s.db, connID)
	if err != nil {
		return SyncStatus{}, err
	}
	st := SyncStatus{ConnectionID: connID, Sources: srcs, State: InventoryNever}
	if st.Sources == nil {
		st.Sources = []SourceStatus{}
		return st, nil
	}
	var limited, failed, pending, stale bool
	for i, src := range srcs {
		if src.gen != gen {
			srcs[i].Stale, stale = true, true
		}
		switch src.State {
		case SyncRateLimited:
			limited = true
			if src.RetryAt != nil && (st.RetryAt == nil || src.RetryAt.After(*st.RetryAt)) {
				st.RetryAt = src.RetryAt
			}
		case SyncFailed:
			failed = true
		case SyncPending, SyncSyncing:
			pending = true
		}
	}
	switch {
	case limited:
		st.State = InventoryRateLimited
	case stale:
		st.State = InventoryStale
	case failed:
		st.State = InventoryPartial
	case pending:
		st.State = InventorySyncing
	default:
		st.State, st.Complete = InventoryComplete, true
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT repo_id) FROM github_repositories WHERE connection_id=?`, connID).Scan(&st.Repositories); err != nil {
		return SyncStatus{}, err
	}
	return st, nil
}

// rateGate refuses before GitHub is asked when the connection is waiting
// for its rate limit to reset.
func (s *Service) rateGate(ctx context.Context, connID string) error {
	var v sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(retry_at) FROM github_repo_sources WHERE connection_id=?`, connID).Scan(&v); err != nil {
		return err
	}
	if t := parseTSPtr(v); t != nil && s.now().Before(*t) {
		return &RateLimitedError{RetryAt: *t}
	}
	return nil
}

// rateWait returns how long GitHub asked to wait, or 0 when err is not a
// rate limit.
func rateWait(err error) (time.Duration, bool) {
	var ae *APIError
	if !errors.As(err, &ae) || !errors.Is(err, ErrUnavailable) || (ae.Reason != "rate limited" && ae.RetryAfter == 0) {
		return 0, false
	}
	if ae.RetryAfter < minRateWait {
		return minRateWait, true
	}
	return ae.RetryAfter, true
}

// noteRateLimit records a rate limit on the connection, so neither a sync
// nor a browse call asks GitHub again before it resets.
func (s *Service) noteRateLimit(ctx context.Context, connID string, until time.Time) {
	if _, err := s.db.ExecContext(ctx, `UPDATE github_repo_sources SET retry_at=?, updated_at=? WHERE connection_id=?`,
		until.UTC().Format(time.RFC3339), s.ts(), connID); err != nil {
		s.log.Warn("github: recording a rate limit failed", "connection", connID, "error", err)
	}
}

// dropInventory forgets a connection's repositories; the next sync step
// reads them again.
func (s *Service) dropInventory(ctx context.Context, connID string) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM github_repo_sources WHERE connection_id=?`, connID); err != nil {
		s.log.Warn("github: dropping a repository inventory failed", "connection", connID, "error", err)
	}
}

// sourcesFor returns the sources a connection's repositories come from.
func (s *Service) sourcesFor(ctx context.Context, c Connection, token string) ([]RepoSource, error) {
	if c.Kind == KindAppInstallation {
		return []RepoSource{{Kind: SourceInstallation}}, nil
	}
	orgs, err := s.client.ListUserOrgs(ctx, token)
	if err != nil {
		return nil, err
	}
	out := []RepoSource{{Kind: SourceUser}}
	for _, o := range orgs {
		out = append(out, RepoSource{Kind: SourceOrg, Login: o.Login})
	}
	return out, nil
}

// SyncRepositories runs one sync step for a connection: it reads at most
// SyncPageBudget pages from the sources that are not finished, each from
// its own cursor, and returns the state. When every source is finished
// (complete or failed) a step starts the next round from page 1, sending
// the stored ETags. A rate limit stops the step and records when GitHub may
// be asked again; nothing is retried before that and no other connection is
// used instead.
func (s *Service) SyncRepositories(ctx context.Context, connID string, actor Actor) (SyncStatus, error) {
	c, err := s.GetConnection(ctx, connID, actor)
	if err != nil {
		return SyncStatus{}, err
	}
	unlock := s.lockConn("repos:" + c.ID)
	defer unlock()
	if err := s.rateGate(ctx, c.ID); err != nil {
		var rl *RateLimitedError
		if errors.As(err, &rl) {
			return s.syncStatus(ctx, c)
		}
		return SyncStatus{}, err
	}
	cred, err := s.Credential(ctx, c.ID, Scope{}, actor)
	if err != nil {
		return SyncStatus{}, err
	}
	srcs, err := s.sourcesOf(ctx, s.db, c.ID)
	if err != nil {
		return SyncStatus{}, err
	}
	open, stale := 0, false
	gen := generationOf(c)
	for _, src := range srcs {
		if src.State == SyncPending || src.State == SyncSyncing || src.State == SyncRateLimited {
			open++
		}
		stale = stale || src.gen != gen
	}
	if open == 0 || stale {
		if err := s.startRound(ctx, c, cred.Value); err != nil {
			if wait, ok := rateWait(err); ok {
				s.noteRateLimit(ctx, c.ID, s.now().Add(wait))
				return s.syncStatus(ctx, c)
			}
			return SyncStatus{}, err
		}
		if srcs, err = s.sourcesOf(ctx, s.db, c.ID); err != nil {
			return SyncStatus{}, err
		}
	}
	budget := SyncPageBudget
	for _, src := range srcs {
		if src.State == SyncComplete || src.State == SyncFailed {
			continue
		}
		page := max(src.NextPage, 1)
		for budget > 0 {
			budget--
			next, stop, err := s.syncPage(ctx, c, cred.Value, src, page)
			if err != nil {
				return SyncStatus{}, err
			}
			if stop {
				return s.syncStatus(ctx, c)
			}
			if next == 0 {
				break
			}
			page = next
		}
		if budget == 0 {
			break
		}
	}
	return s.syncStatus(ctx, c)
}

// startRound makes the source list current and sets every source back to
// page 1. Sources that went away (left an organization) are removed with
// their rows.
func (s *Service) startRound(ctx context.Context, c Connection, token string) error {
	gen := generationOf(c)
	want, err := s.sourcesFor(ctx, c, token)
	userFailed := ""
	if err != nil {
		if _, limited := rateWait(err); limited || !errors.Is(err, ErrRejected) && !errors.Is(err, ErrUnavailable) {
			return err
		}
		// Organizations could not be listed: sync the user's own
		// repositories and show the result as partial, keeping the
		// organizations seen before.
		userFailed = "organizations could not be listed: " + err.Error()
		want = []RepoSource{{Kind: SourceUser}}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Pages read under an earlier authorization are read again in full: a
	// 304 would keep rows the connection may no longer see.
	if _, err := tx.ExecContext(ctx, `DELETE FROM github_repo_pages WHERE connection_id=? AND source_key IN
		(SELECT source_key FROM github_repo_sources WHERE connection_id=? AND generation<>?)`, c.ID, c.ID, gen); err != nil {
		return err
	}
	keep := map[string]bool{}
	now := s.ts()
	for _, src := range want {
		keep[src.key()] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO github_repo_sources(connection_id, source_key, kind, login, state, next_page, generation, updated_at)
			VALUES(?, ?, ?, ?, 'pending', 1, ?, ?)
			ON CONFLICT(connection_id, source_key) DO UPDATE SET state='pending', next_page=1, error='', retry_at=NULL, generation=excluded.generation, updated_at=excluded.updated_at`,
			c.ID, src.key(), src.Kind, src.Login, gen, now); err != nil {
			return err
		}
	}
	if userFailed != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE github_repo_sources SET state='failed', error=?, generation=?, updated_at=? WHERE connection_id=? AND kind='org'`,
			userFailed, gen, now, c.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO github_repo_sources(connection_id, source_key, kind, login, state, error, generation, updated_at)
			VALUES(?, 'org:*', 'org', '*', 'failed', ?, ?, ?) ON CONFLICT(connection_id, source_key) DO UPDATE SET state='failed', error=excluded.error, generation=excluded.generation, updated_at=excluded.updated_at`,
			c.ID, userFailed, gen, now); err != nil {
			return err
		}
	} else {
		rows, err := tx.QueryContext(ctx, `SELECT source_key FROM github_repo_sources WHERE connection_id=?`, c.ID)
		if err != nil {
			return err
		}
		var drop []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return err
			}
			if !keep[k] {
				drop = append(drop, k)
			}
		}
		rows.Close()
		for _, k := range drop {
			if _, err := tx.ExecContext(ctx, `DELETE FROM github_repo_sources WHERE connection_id=? AND source_key=?`, c.ID, k); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// syncPage reads one page. It returns the next page to read (0 when the
// source is finished) and stop=true when the whole step must end (rate
// limit). A failure of the source is recorded on it, not returned.
func (s *Service) syncPage(ctx context.Context, c Connection, token string, src SourceStatus, page int) (next int, stop bool, err error) {
	var etag string
	var hadNext int
	_ = s.db.QueryRowContext(ctx, `SELECT etag, has_next FROM github_repo_pages WHERE connection_id=? AND source_key=? AND page=?`,
		c.ID, src.Key, page).Scan(&etag, &hadNext)
	res, gerr := s.client.ListRepositories(ctx, token, RepoSource{Kind: src.Kind, Login: src.Login}, page, etag)
	now := s.ts()
	if gerr != nil {
		if wait, ok := rateWait(gerr); ok {
			until := s.now().Add(wait)
			if _, err := s.db.ExecContext(ctx, `UPDATE github_repo_sources SET state='rate-limited', next_page=?, error='rate limited by GitHub', updated_at=? WHERE connection_id=? AND source_key=?`,
				page, now, c.ID, src.Key); err != nil {
				return 0, true, err
			}
			s.noteRateLimit(ctx, c.ID, until)
			return 0, true, nil
		}
		if errors.Is(gerr, ErrRejected) || errors.Is(gerr, ErrUnavailable) {
			_, err := s.db.ExecContext(ctx, `UPDATE github_repo_sources SET state='failed', next_page=?, error=?, updated_at=? WHERE connection_id=? AND source_key=?`,
				page, truncate(gerr.Error()), now, c.ID, src.Key)
			return 0, false, err
		}
		return 0, true, gerr
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, true, err
	}
	defer tx.Rollback()
	more := res.Next
	if res.NotModified {
		more = hadNext == 1
		if _, err := tx.ExecContext(ctx, `UPDATE github_repositories SET seen_at=? WHERE connection_id=? AND source_key=? AND page=?`, now, c.ID, src.Key, page); err != nil {
			return 0, true, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM github_repositories WHERE connection_id=? AND source_key=? AND page=?`, c.ID, src.Key, page); err != nil {
			return 0, true, err
		}
		for _, r := range res.Repositories {
			if !c.coversRepository(r.ID) {
				continue
			}
			if err := insertRepo(ctx, tx, c.ID, src.Key, page, r, now); err != nil {
				return 0, true, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO github_repo_pages(connection_id, source_key, page, etag, has_next) VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(connection_id, source_key, page) DO UPDATE SET etag=excluded.etag, has_next=excluded.has_next`,
			c.ID, src.Key, page, res.ETag, boolInt(more)); err != nil {
			return 0, true, err
		}
	}
	if more {
		next = page + 1
		_, err = tx.ExecContext(ctx, `UPDATE github_repo_sources SET state='syncing', next_page=?, error='', updated_at=? WHERE connection_id=? AND source_key=?`, next, now, c.ID, src.Key)
	} else {
		// The source ended here: rows and ETags of later pages are gone
		// on GitHub too.
		if _, err = tx.ExecContext(ctx, `DELETE FROM github_repositories WHERE connection_id=? AND source_key=? AND page>?`, c.ID, src.Key, page); err == nil {
			if _, err = tx.ExecContext(ctx, `DELETE FROM github_repo_pages WHERE connection_id=? AND source_key=? AND page>?`, c.ID, src.Key, page); err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE github_repo_sources SET state='complete', next_page=1, error='', synced_at=?, updated_at=? WHERE connection_id=? AND source_key=?`, now, now, c.ID, src.Key)
			}
		}
	}
	if err != nil {
		return 0, true, err
	}
	if err := tx.Commit(); err != nil {
		return 0, true, err
	}
	// GitHub says the budget is spent: stop before it refuses.
	if res.RateRemaining == 0 && !res.RateReset.IsZero() {
		if _, err := s.db.ExecContext(ctx, `UPDATE github_repo_sources SET state='rate-limited', next_page=?, updated_at=? WHERE connection_id=? AND source_key=? AND state='syncing'`,
			next, now, c.ID, src.Key); err != nil {
			return 0, true, err
		}
		s.noteRateLimit(ctx, c.ID, res.RateReset)
		return 0, true, nil
	}
	return next, false, nil
}

// coversRepository: an installation connection never lists more than the
// repositories its maker could reach (047.1).
func (c Connection) coversRepository(id int64) bool {
	if c.Kind != KindAppInstallation {
		return true
	}
	for _, v := range c.RepositoryIDs {
		if v == id {
			return true
		}
	}
	return false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func insertRepo(ctx context.Context, tx *sql.Tx, connID, key string, page int, r Repository, now string) error {
	var pushed any
	if r.PushedAt != nil {
		pushed = r.PushedAt.UTC().Format(time.RFC3339)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO github_repositories(connection_id, source_key, page, repo_id, full_name, name, owner_id, owner_login, owner_type,
		private, visibility, archived, disabled, fork, default_branch, html_url, pushed_at, size_kb, can_push, can_admin, seen_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(connection_id, source_key, repo_id) DO UPDATE SET page=excluded.page, full_name=excluded.full_name, name=excluded.name,
		owner_id=excluded.owner_id, owner_login=excluded.owner_login, owner_type=excluded.owner_type, private=excluded.private,
		visibility=excluded.visibility, archived=excluded.archived, disabled=excluded.disabled, fork=excluded.fork,
		default_branch=excluded.default_branch, html_url=excluded.html_url, pushed_at=excluded.pushed_at, size_kb=excluded.size_kb,
		can_push=excluded.can_push, can_admin=excluded.can_admin, seen_at=excluded.seen_at`,
		connID, key, page, r.ID, r.FullName, r.Name, r.Owner.ID, r.Owner.Login, r.Owner.Type, boolInt(r.Private), r.Visibility,
		boolInt(r.Archived), boolInt(r.Disabled), boolInt(r.Fork), r.DefaultBranch, r.HTMLURL, pushed, r.SizeKB, boolInt(r.Push), boolInt(r.Admin), now)
	return err
}

const repoColumns = `r.repo_id, r.full_name, r.name, r.owner_id, r.owner_login, r.owner_type, r.private, r.visibility, r.archived, r.disabled,
	r.fork, r.default_branch, r.html_url, r.pushed_at, r.size_kb, r.can_push, r.can_admin`

func scanRepo(sc scanner, extra ...any) (Repository, error) {
	var r Repository
	var pushed sql.NullString
	dst := append([]any{&r.ID, &r.FullName, &r.Name, &r.Owner.ID, &r.Owner.Login, &r.Owner.Type, &r.Private, &r.Visibility, &r.Archived,
		&r.Disabled, &r.Fork, &r.DefaultBranch, &r.HTMLURL, &pushed, &r.SizeKB, &r.Push, &r.Admin}, extra...)
	if err := sc.Scan(dst...); err != nil {
		return Repository{}, err
	}
	r.PushedAt = parseTSPtr(pushed)
	return r, nil
}

// ListRepositories returns the repositories of the actor's connections
// that match f, sorted by owner/name then connection. Each entry names its
// connection; nothing from a connection the actor does not own appears.
// Results of a sync still running or partly failed are included; the sync
// state says whether the list is complete.
func (s *Service) ListRepositories(ctx context.Context, actor Actor, f RepoFilter) ([]RepoItem, error) {
	conns, err := s.ListConnections(ctx, actor)
	if err != nil {
		return nil, err
	}
	gens := map[string]string{}
	byID := map[string]Connection{}
	for _, c := range conns {
		gens[c.ID], byID[c.ID] = generationOf(c), c
	}
	q := `SELECT ` + repoColumns + `, c.id, c.name, c.kind, c.state, r.seen_at, s.generation FROM github_repositories r
		JOIN github_connections c ON c.id = r.connection_id
		JOIN github_repo_sources s ON s.connection_id = r.connection_id AND s.source_key = r.source_key
		WHERE c.owner_user_id=?`
	args := []any{actor.UserID}
	if f.ConnectionID != "" {
		q += ` AND c.id=?`
		args = append(args, f.ConnectionID)
	}
	if f.RepoID != 0 {
		q += ` AND r.repo_id=?`
		args = append(args, f.RepoID)
	}
	if f.Query != "" {
		q += ` AND instr(lower(r.full_name), ?) > 0`
		args = append(args, strings.ToLower(f.Query))
	}
	if f.Owner != "" {
		q += ` AND lower(r.owner_login)=?`
		args = append(args, strings.ToLower(f.Owner))
	}
	if f.Visibility != "" {
		q += ` AND r.visibility=?`
		args = append(args, f.Visibility)
	}
	if f.Archived != nil {
		q += ` AND r.archived=?`
		args = append(args, boolInt(*f.Archived))
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []RepoItem
	for rows.Next() {
		var it RepoItem
		var seenAt, gen string
		r, err := scanRepo(rows, &it.ConnectionID, &it.ConnectionName, &it.ConnectionKind, &it.ConnectionState, &seenAt, &gen)
		if err != nil {
			return nil, err
		}
		// Apply what the connection allows now, not what it allowed when
		// the row was read.
		if c, ok := byID[it.ConnectionID]; !ok || !c.coversRepository(r.ID) {
			continue
		}
		it.SeenAt, it.Stale = parseTS(seenAt), gen != gens[it.ConnectionID]
		key := fmt.Sprintf("%s#%d", it.ConnectionID, r.ID)
		if seen[key] {
			continue // the same repository from two sources of one connection
		}
		seen[key] = true
		it.Repository = r
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].FullName), strings.ToLower(out[j].FullName)
		if a != b {
			return a < b
		}
		return out[i].ConnectionID < out[j].ConnectionID
	})
	return out, nil
}

// repoTarget is a repository a connection may browse: it must be in that
// connection's inventory, so only what GitHub listed for the connection
// (and, for installations, what its maker could reach) is reachable.
type repoTarget struct {
	conn  Connection
	repo  Repository
	token string
}

func (s *Service) target(ctx context.Context, connID string, repoID int64, actor Actor) (repoTarget, error) {
	c, err := s.GetConnection(ctx, connID, actor)
	if err != nil {
		return repoTarget{}, err
	}
	r, err := scanRepo(s.db.QueryRowContext(ctx, `SELECT `+repoColumns+` FROM github_repositories r WHERE r.connection_id=? AND r.repo_id=? LIMIT 1`, c.ID, repoID))
	if errors.Is(err, sql.ErrNoRows) {
		return repoTarget{}, ErrNotFound
	}
	if err != nil {
		return repoTarget{}, err
	}
	if !c.coversRepository(repoID) {
		return repoTarget{}, ErrNotFound
	}
	if err := s.rateGate(ctx, c.ID); err != nil {
		return repoTarget{}, err
	}
	cred, err := s.Credential(ctx, c.ID, Scope{RepositoryIDs: installScope(c, repoID), Permissions: installPerms(c)}, actor)
	if err != nil {
		return repoTarget{}, err
	}
	return repoTarget{conn: c, repo: r, token: cred.Value}, nil
}

func installScope(c Connection, repoID int64) []int64 {
	if c.Kind == KindAppInstallation {
		return []int64{repoID}
	}
	return nil
}

func installPerms(c Connection) map[string]string {
	if c.Kind == KindAppInstallation {
		return map[string]string{"contents": "read", "metadata": "read"}
	}
	return nil
}

// gh runs one browse call; a rate limit is recorded on the connection, and a
// renamed or moved repository (GitHub answers 301 to the old name) is read
// again by its id and the call retried once.
func (s *Service) gh(ctx context.Context, t *repoTarget, call func(fullName string) error) error {
	err := call(t.repo.FullName)
	if isStatus(err, 301) || isStatus(err, 307) {
		r, gerr := s.client.GetRepositoryByID(ctx, t.token, t.repo.ID)
		if gerr != nil {
			return s.ghErr(ctx, t, gerr)
		}
		s.updateRepo(ctx, t.conn.ID, r)
		t.repo = r
		err = call(r.FullName)
	}
	return s.ghErr(ctx, t, err)
}

func (s *Service) ghErr(ctx context.Context, t *repoTarget, err error) error {
	if err == nil {
		return nil
	}
	if wait, ok := rateWait(err); ok {
		until := s.now().Add(wait)
		s.noteRateLimit(ctx, t.conn.ID, until)
		return &RateLimitedError{RetryAt: until}
	}
	if isStatus(err, 409) {
		return ErrEmptyRepository
	}
	return err
}

func (s *Service) updateRepo(ctx context.Context, connID string, r Repository) {
	var pushed any
	if r.PushedAt != nil {
		pushed = r.PushedAt.UTC().Format(time.RFC3339)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE github_repositories SET full_name=?, name=?, owner_id=?, owner_login=?, owner_type=?, private=?, visibility=?,
		archived=?, disabled=?, default_branch=?, html_url=?, pushed_at=?, size_kb=?, can_push=?, can_admin=?, seen_at=? WHERE connection_id=? AND repo_id=?`,
		r.FullName, r.Name, r.Owner.ID, r.Owner.Login, r.Owner.Type, boolInt(r.Private), r.Visibility, boolInt(r.Archived), boolInt(r.Disabled),
		r.DefaultBranch, r.HTMLURL, pushed, r.SizeKB, boolInt(r.Push), boolInt(r.Admin), s.ts(), connID, r.ID); err != nil {
		s.log.Warn("github: updating a repository failed", "connection", connID, "error", err)
	}
}

// GetRepository reads a repository through the connection now (its current
// name, default branch and archive state) and updates the inventory.
func (s *Service) GetRepository(ctx context.Context, connID string, repoID int64, actor Actor) (RepoItem, error) {
	t, err := s.target(ctx, connID, repoID, actor)
	if err != nil {
		return RepoItem{}, err
	}
	r, err := s.client.GetRepositoryByID(ctx, t.token, repoID)
	if err = s.ghErr(ctx, &t, err); err != nil {
		return RepoItem{}, err
	}
	s.updateRepo(ctx, t.conn.ID, r)
	return RepoItem{Repository: r, ConnectionID: t.conn.ID, ConnectionName: t.conn.Name, ConnectionKind: t.conn.Kind, ConnectionState: t.conn.State}, nil
}

// RefPage is one page of branches, tags or commits.
type RefPage[T any] struct {
	Items []T
	Next  bool
}

// ListRefs lists branches or tags of a repository, one GitHub page at a time.
func (s *Service) ListRefs(ctx context.Context, connID string, repoID int64, kind string, page int, actor Actor) (RefPage[Ref], error) {
	if kind != "branches" && kind != "tags" {
		return RefPage[Ref]{}, invalid("kind", "must be branches or tags")
	}
	t, err := s.target(ctx, connID, repoID, actor)
	if err != nil {
		return RefPage[Ref]{}, err
	}
	var out RefPage[Ref]
	err = s.gh(ctx, &t, func(name string) error {
		var e error
		out.Items, out.Next, e = s.client.ListRefs(ctx, t.token, name, kind, max(page, 1))
		return e
	})
	if errors.Is(err, ErrEmptyRepository) {
		return RefPage[Ref]{Items: []Ref{}}, nil
	}
	if out.Items == nil {
		out.Items = []Ref{}
	}
	return out, err
}

// ListCommits lists commits reachable from ref (default branch when empty).
func (s *Service) ListCommits(ctx context.Context, connID string, repoID int64, ref string, page int, actor Actor) (RefPage[Commit], error) {
	t, err := s.target(ctx, connID, repoID, actor)
	if err != nil {
		return RefPage[Commit]{}, err
	}
	var out RefPage[Commit]
	err = s.gh(ctx, &t, func(name string) error {
		var e error
		out.Items, out.Next, e = s.client.ListCommits(ctx, t.token, name, ref, max(page, 1))
		return e
	})
	if err != nil {
		return RefPage[Commit]{}, refErr(err)
	}
	if out.Items == nil {
		out.Items = []Commit{}
	}
	return out, nil
}

// refErr turns GitHub's answer to an unknown ref into a validation error.
func refErr(err error) error {
	if isStatus(err, 404) || isStatus(err, 422) {
		return invalid("ref", "no branch, tag or commit with this name in the repository")
	}
	return err
}

// Resolved is a ref fixed to the commit it points to now. A deployment
// keeps SHA, never the branch name, so a branch that moves later does not
// change what was planned.
type Resolved struct {
	Ref           string `json:"ref"`
	DefaultBranch string `json:"defaultBranch"`
	Commit        Commit `json:"commit"`
}

// ResolveRef fixes a branch, tag or SHA to its commit (default branch when
// ref is empty). An empty repository is ErrEmptyRepository.
func (s *Service) ResolveRef(ctx context.Context, connID string, repoID int64, ref string, actor Actor) (Resolved, error) {
	t, err := s.target(ctx, connID, repoID, actor)
	if err != nil {
		return Resolved{}, err
	}
	return s.resolve(ctx, &t, ref)
}

func (s *Service) resolve(ctx context.Context, t *repoTarget, ref string) (Resolved, error) {
	if ref == "" {
		// The default branch may have changed since the inventory was read.
		r, err := s.client.GetRepositoryByID(ctx, t.token, t.repo.ID)
		if err := s.ghErr(ctx, t, err); err != nil {
			return Resolved{}, err
		}
		s.updateRepo(ctx, t.conn.ID, r)
		t.repo = r
		ref = r.DefaultBranch
		if ref == "" {
			return Resolved{}, ErrEmptyRepository
		}
	}
	var cm Commit
	err := s.gh(ctx, t, func(name string) error {
		var e error
		cm, e = s.client.GetCommit(ctx, t.token, name, ref)
		return e
	})
	if err != nil {
		return Resolved{}, refErr(err)
	}
	return Resolved{Ref: ref, DefaultBranch: t.repo.DefaultBranch, Commit: cm}, nil
}

// Directory is one directory of a commit.
type Directory struct {
	Resolved
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

// ListDirectory lists a directory at ref, fixing ref to a commit first so
// the listing and later reads agree.
func (s *Service) ListDirectory(ctx context.Context, connID string, repoID int64, ref, path string, actor Actor) (Directory, error) {
	clean, err := cleanRepoPath(path)
	if err != nil {
		return Directory{}, err
	}
	t, err := s.target(ctx, connID, repoID, actor)
	if err != nil {
		return Directory{}, err
	}
	res, err := s.resolve(ctx, &t, ref)
	if err != nil {
		return Directory{}, err
	}
	var entries []Entry
	err = s.gh(ctx, &t, func(name string) error {
		var e error
		entries, e = s.client.ListDirectory(ctx, t.token, name, clean, res.Commit.SHA)
		return e
	})
	if isStatus(err, 404) {
		return Directory{}, invalid("path", "no such directory at this commit")
	}
	if err != nil {
		return Directory{}, err
	}
	if entries == nil {
		entries = []Entry{}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if (entries[i].Type == "dir") != (entries[j].Type == "dir") {
			return entries[i].Type == "dir"
		}
		return entries[i].Name < entries[j].Name
	})
	return Directory{Resolved: res, Path: clean, Entries: entries}, nil
}

// cleanRepoPath accepts a relative path inside the repository.
func cleanRepoPath(p string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\\x00") {
			return "", invalid("path", "must be a relative path inside the repository")
		}
	}
	return p, nil
}

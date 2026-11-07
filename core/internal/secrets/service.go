package secrets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/store"
)

// Service stores and reveals secrets in the platform database using a Keyring.
//
// Rotation of the master key happens in two durable steps with the database
// transaction between them:
//  1. keyring: add generation N as pending            (file write)
//  2. database: re-wrap every version to N, verify    (one transaction)
//  3. keyring: activate N, retire the previous active (file write)
//
// Open reconciles an interruption: a pending generation that no row refers to
// is dropped (crash before 2 committed); one that rows refer to is activated
// (crash after 2 committed, before 3).
type Service struct {
	st  *store.Store
	kr  *Keyring
	now func() time.Time
	// mu serializes master-key rotation against writes: a value sealed while
	// a rotation is in flight would otherwise miss the re-wrap.
	mu sync.RWMutex

	// rotateHook lets tests inject failures between rotation steps:
	// "rewrapped" (inside the transaction, before commit) and
	// "committed" (after commit, before keyring activation).
	rotateHook func(step string) error
}

var errActorRequired = errors.New("secrets: actor is required")

// Open binds a Service to the store and keyring, reconciles an interrupted
// rotation, records the active generation in master_keys, and refuses
// (ErrKeyNotFound) when any stored version refers to a generation the keyring
// lacks — the check that catches an unreadable backup before it is needed.
func Open(ctx context.Context, st *store.Store, kr *Keyring) (*Service, error) {
	s := &Service{st: st, kr: kr, now: time.Now}
	if p := kr.PendingKeyID(); p != "" {
		var n int
		if err := st.DB.QueryRowContext(ctx, `SELECT count(*) FROM secret_versions WHERE key_id=?`, p).Scan(&n); err != nil {
			return nil, fmt.Errorf("secrets: inspect pending generation: %w", err)
		}
		if n > 0 {
			if err := kr.activatePending(); err != nil {
				return nil, err
			}
		} else if err := kr.dropPending(); err != nil {
			return nil, err
		}
	}
	if err := s.verifyKeyCoverage(ctx); err != nil {
		return nil, err
	}
	if err := s.recordActiveGeneration(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// verifyKeyCoverage checks every generation the data refers to is in the keyring.
func (s *Service) verifyKeyCoverage(ctx context.Context) error {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT DISTINCT key_id FROM secret_versions ORDER BY key_id`)
	if err != nil {
		return fmt.Errorf("secrets: list generations: %w", err)
	}
	defer rows.Close()
	var missing []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if _, ok := s.kr.key(id); !ok {
			missing = append(missing, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s (restore the master key file that holds them; the database was not changed)", ErrKeyNotFound, strings.Join(missing, ", "))
	}
	return nil
}

// recordActiveGeneration makes master_keys agree with the keyring: the active
// generation has an active row, every other row is retired.
func (s *Service) recordActiveGeneration(ctx context.Context) error {
	active := s.kr.ActiveKeyID()
	now := s.timestamp()
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE master_keys SET status='retired', retired_at=? WHERE status='active' AND key_id<>?`, now, active); err != nil {
		return fmt.Errorf("secrets: retire generations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO master_keys(key_id, status, created_at, activated_at) VALUES(?, 'active', ?, ?)
		ON CONFLICT(key_id) DO UPDATE SET status='active', retired_at=NULL`, active, now, now); err != nil {
		return fmt.Errorf("secrets: record active generation: %w", err)
	}
	return tx.Commit()
}

// ActiveKeyID returns the generation new values are sealed under.
func (s *Service) ActiveKeyID() string { return s.kr.ActiveKeyID() }

func (s *Service) timestamp() string { return s.now().UTC().Format(time.RFC3339) }

// Create stores a new secret at version 1.
// Errors: ErrInvalidKind, ErrEmptyValue, ErrNameTaken.
func (s *Service) Create(ctx context.Context, in CreateInput) (Secret, error) {
	if !kinds[in.Kind] {
		return Secret{}, fmt.Errorf("%w: %q", ErrInvalidKind, in.Kind)
	}
	if len(in.Value) == 0 {
		return Secret{}, ErrEmptyValue
	}
	if strings.TrimSpace(in.Name) == "" {
		return Secret{}, errors.New("secrets: name is required")
	}
	if in.Actor == "" {
		return Secret{}, errActorRequired
	}
	scopeJSON, err := encodeScope(in.Scope)
	if err != nil {
		return Secret{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyID := s.kr.ActiveKeyID()
	key, ok := s.kr.key(keyID)
	if !ok {
		return Secret{}, fmt.Errorf("%w: active generation %s", ErrKeyNotFound, keyID)
	}
	id := "sec_" + newID()
	now := s.timestamp()
	var expires any
	if in.ExpiresAt != nil {
		expires = in.ExpiresAt.UTC().Format(time.RFC3339)
	}
	ct, err := seal(key, in.Value, aad(id, 1))
	if err != nil {
		return Secret{}, fmt.Errorf("secrets: seal: %w", err)
	}

	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Secret{}, err
	}
	defer tx.Rollback()
	var taken int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM secrets WHERE name=? AND deleted_at IS NULL`, in.Name).Scan(&taken); err != nil {
		return Secret{}, err
	}
	if taken > 0 {
		return Secret{}, fmt.Errorf("%w: %q", ErrNameTaken, in.Name)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO secrets(id, kind, name, current_version, scope, expires_at, created_at, updated_at)
		VALUES(?, ?, ?, 1, ?, ?, ?, ?)`, id, string(in.Kind), in.Name, scopeJSON, expires, now, now); err != nil {
		return Secret{}, fmt.Errorf("secrets: insert: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO secret_versions(secret_id, version, key_id, ciphertext, created_at) VALUES(?, 1, ?, ?, ?)`,
		id, keyID, ct, now); err != nil {
		return Secret{}, fmt.Errorf("secrets: insert version: %w", err)
	}
	one := 1
	if err := recordChange(ctx, tx, Change{SecretID: id, SecretName: in.Name, Action: "created", ToVersion: &one, KeyID: keyID, Actor: in.Actor}, now); err != nil {
		return Secret{}, err
	}
	if err := tx.Commit(); err != nil {
		return Secret{}, err
	}
	return s.Get(ctx, id)
}

// Get returns metadata (never the value). Deleted secrets are ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (Secret, error) {
	return scanSecret(s.st.DB.QueryRowContext(ctx, selectSecret+` WHERE s.id=? AND s.deleted_at IS NULL`, id))
}

// List returns metadata of every live secret, by name.
func (s *Service) List(ctx context.Context) ([]Secret, error) {
	rows, err := s.st.DB.QueryContext(ctx, selectSecret+` WHERE s.deleted_at IS NULL ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Secret
	for rows.Next() {
		sec, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

const selectSecret = `SELECT s.id, s.kind, s.name, s.current_version, coalesce(v.key_id, ''), s.scope, s.expires_at, s.created_at, s.updated_at
	FROM secrets s LEFT JOIN secret_versions v ON v.secret_id = s.id AND v.version = s.current_version`

type scanner interface{ Scan(dest ...any) error }

func scanSecret(row scanner) (Secret, error) {
	var sec Secret
	var kind, scope, created, updated string
	var expires sql.NullString
	err := row.Scan(&sec.ID, &kind, &sec.Name, &sec.Version, &sec.KeyID, &scope, &expires, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Secret{}, ErrNotFound
	}
	if err != nil {
		return Secret{}, err
	}
	sec.Kind = Kind(kind)
	if err := json.Unmarshal([]byte(scope), &sec.Scope); err != nil {
		return Secret{}, fmt.Errorf("secrets: scope of %s is unreadable: %w", sec.ID, err)
	}
	if sec.Scope == nil {
		sec.Scope = []string{}
	}
	if expires.Valid {
		t, err := time.Parse(time.RFC3339, expires.String)
		if err != nil {
			return Secret{}, fmt.Errorf("secrets: expiry of %s is unreadable: %w", sec.ID, err)
		}
		sec.ExpiresAt = &t
	}
	sec.CreatedAt, _ = time.Parse(time.RFC3339, created)
	sec.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return sec, nil
}

func encodeScope(scope []string) (string, error) {
	if scope == nil {
		scope = []string{}
	}
	for _, c := range scope {
		if strings.TrimSpace(c) == "" {
			return "", errors.New("secrets: scope entries must not be empty")
		}
	}
	b, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Reveal returns the plaintext of a version for a consumer in the scope.
// Errors: ErrNotFound (secret or version), ErrOutOfScope, ErrExpired,
// ErrKeyNotFound, ErrDecrypt. The returned slice is the caller's to zero.
func (s *Service) Reveal(ctx context.Context, req RevealRequest) ([]byte, int, error) {
	sec, err := s.Get(ctx, req.ID)
	if err != nil {
		return nil, 0, err
	}
	if req.Consumer == "" || !contains(sec.Scope, req.Consumer) {
		return nil, 0, fmt.Errorf("%w: %s for %s", ErrOutOfScope, req.Consumer, sec.ID)
	}
	if sec.ExpiresAt != nil && !s.now().Before(*sec.ExpiresAt) {
		return nil, 0, fmt.Errorf("%w: %s expired at %s", ErrExpired, sec.ID, sec.ExpiresAt.Format(time.RFC3339))
	}
	version := req.Version
	if version == 0 {
		version = sec.Version
	}
	var keyID string
	var ct []byte
	err = s.st.DB.QueryRowContext(ctx, `SELECT key_id, ciphertext FROM secret_versions WHERE secret_id=? AND version=?`, sec.ID, version).Scan(&keyID, &ct)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("%w: %s has no version %d", ErrNotFound, sec.ID, version)
	}
	if err != nil {
		return nil, 0, err
	}
	key, ok := s.kr.key(keyID)
	if !ok {
		return nil, 0, fmt.Errorf("%w: %s", ErrKeyNotFound, keyID)
	}
	pt, err := open(key, ct, aad(sec.ID, version))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %s@%d", err, sec.ID, version)
	}
	return pt, version, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Rotate stores value as the next version and makes it current. Older
// versions stay readable so a deployment pinned to one keeps working.
func (s *Service) Rotate(ctx context.Context, id string, value []byte, actor string) (Secret, error) {
	if len(value) == 0 {
		return Secret{}, ErrEmptyValue
	}
	if actor == "" {
		return Secret{}, errActorRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyID := s.kr.ActiveKeyID()
	key, ok := s.kr.key(keyID)
	if !ok {
		return Secret{}, fmt.Errorf("%w: active generation %s", ErrKeyNotFound, keyID)
	}
	now := s.timestamp()
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Secret{}, err
	}
	defer tx.Rollback()
	sec, err := scanSecret(tx.QueryRowContext(ctx, selectSecret+` WHERE s.id=? AND s.deleted_at IS NULL`, id))
	if err != nil {
		return Secret{}, err
	}
	next := sec.Version + 1
	ct, err := seal(key, value, aad(sec.ID, next))
	if err != nil {
		return Secret{}, fmt.Errorf("secrets: seal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO secret_versions(secret_id, version, key_id, ciphertext, created_at) VALUES(?, ?, ?, ?, ?)`,
		sec.ID, next, keyID, ct, now); err != nil {
		return Secret{}, fmt.Errorf("secrets: insert version: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE secrets SET current_version=?, updated_at=? WHERE id=?`, next, now, sec.ID); err != nil {
		return Secret{}, err
	}
	from := sec.Version
	if err := recordChange(ctx, tx, Change{SecretID: sec.ID, SecretName: sec.Name, Action: "rotated", FromVersion: &from, ToVersion: &next, KeyID: keyID, Actor: actor}, now); err != nil {
		return Secret{}, err
	}
	if err := tx.Commit(); err != nil {
		return Secret{}, err
	}
	return s.Get(ctx, sec.ID)
}

// SetScope replaces the use scope.
func (s *Service) SetScope(ctx context.Context, id string, scope []string, actor string) error {
	scopeJSON, err := encodeScope(scope)
	if err != nil {
		return err
	}
	return s.update(ctx, id, actor, "scope-changed", `UPDATE secrets SET scope=?, updated_at=? WHERE id=?`, scopeJSON)
}

// SetExpiry replaces the expiry; nil clears it.
func (s *Service) SetExpiry(ctx context.Context, id string, at *time.Time, actor string) error {
	var v any
	if at != nil {
		v = at.UTC().Format(time.RFC3339)
	}
	return s.update(ctx, id, actor, "expiry-changed", `UPDATE secrets SET expires_at=?, updated_at=? WHERE id=?`, v)
}

// Delete soft-deletes a secret. Its versions and change record remain.
func (s *Service) Delete(ctx context.Context, id, actor string) error {
	return s.update(ctx, id, actor, "deleted", `UPDATE secrets SET deleted_at=?, updated_at=? WHERE id=?`, nil)
}

// update runs one metadata statement of the form (value, now, id) and records
// the change. For Delete the value slot is the timestamp itself.
func (s *Service) update(ctx context.Context, id, actor, action, stmt string, value any) error {
	if actor == "" {
		return errActorRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.timestamp()
	if action == "deleted" {
		value = now
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sec, err := scanSecret(tx.QueryRowContext(ctx, selectSecret+` WHERE s.id=? AND s.deleted_at IS NULL`, id))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, stmt, value, now, sec.ID); err != nil {
		return err
	}
	if err := recordChange(ctx, tx, Change{SecretID: sec.ID, SecretName: sec.Name, Action: action, KeyID: sec.KeyID, Actor: actor}, now); err != nil {
		return err
	}
	return tx.Commit()
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func recordChange(ctx context.Context, db execer, c Change, now string) error {
	var from, to any
	if c.FromVersion != nil {
		from = *c.FromVersion
	}
	if c.ToVersion != nil {
		to = *c.ToVersion
	}
	var keyID any
	if c.KeyID != "" {
		keyID = c.KeyID
	}
	_, err := db.ExecContext(ctx, `INSERT INTO secret_changes(secret_id, secret_name, action, from_version, to_version, key_id, actor, occurred_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, c.SecretID, c.SecretName, c.Action, from, to, keyID, c.Actor, now)
	if err != nil {
		return fmt.Errorf("secrets: record change: %w", err)
	}
	return nil
}

// Changes returns the change record of a secret, oldest first.
func (s *Service) Changes(ctx context.Context, id string) ([]Change, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT id, secret_id, secret_name, action, from_version, to_version, key_id, actor, occurred_at
		FROM secret_changes WHERE secret_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		var from, to sql.NullInt64
		var keyID sql.NullString
		var at string
		if err := rows.Scan(&c.ID, &c.SecretID, &c.SecretName, &c.Action, &from, &to, &keyID, &c.Actor, &at); err != nil {
			return nil, err
		}
		if from.Valid {
			v := int(from.Int64)
			c.FromVersion = &v
		}
		if to.Valid {
			v := int(to.Int64)
			c.ToVersion = &v
		}
		c.KeyID = keyID.String
		c.OccurredAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, c)
	}
	return out, rows.Err()
}

type versionRow struct {
	secretID   string
	version    int
	keyID      string
	ciphertext []byte
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadVersions(ctx context.Context, db querier) ([]versionRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT secret_id, version, key_id, ciphertext FROM secret_versions ORDER BY secret_id, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []versionRow
	for rows.Next() {
		var r versionRow
		if err := rows.Scan(&r.secretID, &r.version, &r.keyID, &r.ciphertext); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// VerifyAll decrypts every stored version and reports failures by location.
func (s *Service) VerifyAll(ctx context.Context) (VerifyReport, error) {
	versions, err := loadVersions(ctx, s.st.DB)
	if err != nil {
		return VerifyReport{}, err
	}
	rep := VerifyReport{Checked: len(versions)}
	for _, r := range versions {
		key, ok := s.kr.key(r.keyID)
		if !ok {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s@%d: %v (%s)", r.secretID, r.version, ErrKeyNotFound, r.keyID))
			continue
		}
		pt, err := open(key, r.ciphertext, aad(r.secretID, r.version))
		if err != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s@%d: %v", r.secretID, r.version, err))
			continue
		}
		zero(pt)
	}
	return rep, nil
}

// RotateMasterKey creates a new generation and re-wraps every version under
// it. Every re-wrapped ciphertext is decrypted again under the new key and
// compared with the plaintext read under the old key before commit. On any
// failure the transaction rolls back, the old generation stays active and
// the data stays readable; the attempt is reported as an error.
func (s *Service) RotateMasterKey(ctx context.Context, actor string) (RotationResult, error) {
	if actor == "" {
		return RotationResult{}, errActorRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	oldID := s.kr.ActiveKeyID()
	// Step 1: the new generation is on disk before any data refers to it.
	newID, err := s.kr.preparePending()
	if err != nil {
		return RotationResult{}, err
	}
	newKey, _ := s.kr.key(newID)
	res := RotationResult{OldKeyID: oldID, NewKeyID: newID}

	// Step 2: one transaction re-wraps and verifies everything.
	fail := func(err error) (RotationResult, error) {
		if dropErr := s.kr.dropPending(); dropErr != nil {
			err = errors.Join(err, dropErr)
		}
		return RotationResult{}, fmt.Errorf("secrets: master key rotation aborted, %s stays active: %w", oldID, err)
	}
	now := s.timestamp()
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	versions, err := loadVersions(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO master_keys(key_id, status, created_at, activated_at) VALUES(?, 'active', ?, ?)`, newID, now, now); err != nil {
		return fail(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE master_keys SET status='retired', retired_at=? WHERE key_id<>? AND status='active'`, now, newID); err != nil {
		return fail(err)
	}
	touched := map[string]bool{}
	for _, r := range versions {
		oldKey, ok := s.kr.key(r.keyID)
		if !ok {
			return fail(fmt.Errorf("%w: %s@%d under %s", ErrKeyNotFound, r.secretID, r.version, r.keyID))
		}
		pt, err := open(oldKey, r.ciphertext, aad(r.secretID, r.version))
		if err != nil {
			return fail(fmt.Errorf("%s@%d: %w", r.secretID, r.version, err))
		}
		ct, err := seal(newKey, pt, aad(r.secretID, r.version))
		if err != nil {
			zero(pt)
			return fail(err)
		}
		check, err := open(newKey, ct, aad(r.secretID, r.version))
		if err != nil || string(check) != string(pt) {
			zero(pt)
			zero(check)
			return fail(fmt.Errorf("%s@%d: re-wrapped value did not verify under %s", r.secretID, r.version, newID))
		}
		zero(pt)
		zero(check)
		if _, err := tx.ExecContext(ctx, `UPDATE secret_versions SET ciphertext=?, key_id=? WHERE secret_id=? AND version=?`, ct, newID, r.secretID, r.version); err != nil {
			return fail(err)
		}
		res.Rewrapped++
		touched[r.secretID] = true
	}
	ids := make([]string, 0, len(touched))
	for id := range touched {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM secrets WHERE id=?`, id).Scan(&name); err != nil {
			return fail(err)
		}
		if err := recordChange(ctx, tx, Change{SecretID: id, SecretName: name, Action: "rewrapped", KeyID: newID, Actor: actor}, now); err != nil {
			return fail(err)
		}
	}
	if s.rotateHook != nil {
		if err := s.rotateHook("rewrapped"); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	// From here the data is under newID; the pending mark stays until the
	// keyring is activated so a crash is completed by Open, not undone.
	if s.rotateHook != nil {
		if err := s.rotateHook("committed"); err != nil {
			return RotationResult{}, err
		}
	}
	// Step 3.
	if err := s.kr.activatePending(); err != nil {
		return RotationResult{}, fmt.Errorf("secrets: data re-wrapped to %s but the key file could not be updated; it will be completed at next start: %w", newID, err)
	}
	return res, nil
}

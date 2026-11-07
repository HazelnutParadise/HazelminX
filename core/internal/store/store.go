// Package store opens the platform SQLite database and applies migrations.
//
// Rules (spec 35.1, DAT-002, DAT-006, REL-006):
//   - pure Go driver (modernc.org/sqlite), so install needs no C toolchain;
//   - WAL journal, foreign keys on, busy timeout so a second connection waits;
//   - migrations only go up; a database newer than this binary is refused
//     instead of being opened or recreated.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNewerSchema is returned when the database was written by a newer core.
var ErrNewerSchema = errors.New("store: database schema is newer than this binary supports")

// ErrCorrupt is returned when the file exists but is not a healthy SQLite
// database. The file is left untouched; nothing is recreated (DAT-002, T-012).
var ErrCorrupt = errors.New("store: database is damaged")

// ErrMissing is returned by OpenExisting when the database file is absent on an
// installation that already has one (DAT-002, T-012): a blank database must not
// be created in its place.
var ErrMissing = errors.New("store: database file is missing")

// Store wraps the platform database.
type Store struct {
	DB   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and applies migrations.
// The parent directory is created with 0700; the file ends up 0600.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}
	// Create the file 0600 before SQLite touches it: the -wal and -shm files
	// inherit the main file's mode, and the WAL holds whole pages of data.
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err != nil {
		return nil, fmt.Errorf("store: create database file: %w", err)
	} else {
		f.Close()
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite writer; readers can be added later with a second pool.
	s := &Store{DB: db, path: path}
	if err := s.verifyIntegrity(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		db.Close()
		return nil, fmt.Errorf("store: chmod: %w", err)
	}
	return s, nil
}

// OpenExisting is Open for a data directory that is known to hold a database
// already (for example because the master key file exists): a missing file is
// ErrMissing, never a fresh database.
func OpenExisting(ctx context.Context, path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrMissing, path)
		}
		return nil, fmt.Errorf("store: stat: %w", err)
	}
	return Open(ctx, path)
}

// verifyIntegrity refuses a damaged file before any migration could touch it.
// An unreadable header surfaces from the driver as "file is not a database";
// page-level damage surfaces from quick_check.
func (s *Store) verifyIntegrity(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA quick_check`)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: quick_check: %s", ErrCorrupt, strings.Join(problems, "; "))
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// SchemaVersion returns the current applied migration number.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	return currentVersion(ctx, s.DB)
}

// Ping verifies the connection is usable within the timeout.
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.DB.PingContext(ctx)
}

type migration struct {
	version int
	name    string
	sql     string
}

// Migrations lists the embedded migrations in ascending order.
func Migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		num, _, ok := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
		if !ok {
			return nil, fmt.Errorf("store: migration %q must be NNNN_name.sql", name)
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("store: migration %q has a non-numeric prefix", name)
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: name, sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := range out {
		if out[i].version != i+1 {
			return nil, fmt.Errorf("store: migrations must be contiguous from 0001; got %q at position %d", out[i].name, i+1)
		}
	}
	return out, nil
}

// LatestVersion is the highest migration this binary knows.
func LatestVersion() int {
	ms, err := Migrations()
	if err != nil || len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].version
}

func currentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_version'`).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, nil
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT max(version) FROM schema_version`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

func (s *Store) migrate(ctx context.Context) error {
	ms, err := Migrations()
	if err != nil {
		return err
	}
	cur, err := currentVersion(ctx, s.DB)
	if err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	latest := 0
	if len(ms) > 0 {
		latest = ms[len(ms)-1].version
	}
	if cur > latest {
		return fmt.Errorf("%w: database is at %d, binary supports up to %d", ErrNewerSchema, cur, latest)
	}
	for _, m := range ms {
		if m.version <= cur {
			continue
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: apply %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version, name, applied_at) VALUES(?, ?, ?)`,
			m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: record %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

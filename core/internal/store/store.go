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
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	db, err := sql.Open("sqlite", dsn(path))
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
	s.convertAutoVacuum(ctx)
	// SQLite's advice for a long-lived connection: a light optimize when it
	// opens, then PRAGMA optimize periodically (Maintain).
	if _, err := db.ExecContext(ctx, `PRAGMA optimize=0x10002`); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: optimize: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		db.Close()
		return nil, fmt.Errorf("store: chmod: %w", err)
	}
	return s, nil
}

// JournalSizeLimit caps the WAL file left on disk after a checkpoint
// (ticket 128). Normal checkpoints keep the WAL near 4 MiB; only one large
// transaction (a table rebuild, VACUUM, a big prune) grows it past this.
const JournalSizeLimit = 64 << 20

// dsn sets the connection pragmas. auto_vacuum comes first: it only takes
// effect on a new file before anything has been written to it.
func dsn(path string) string {
	return "file:" + path + "?_pragma=auto_vacuum(INCREMENTAL)&_pragma=journal_mode(WAL)" +
		"&_pragma=journal_size_limit(" + strconv.Itoa(JournalSizeLimit) + ")" +
		"&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)"
}

// convertAutoVacuum moves a database created before ticket 128 to
// incremental auto-vacuum. That takes one VACUUM, which rewrites the whole
// file and needs about twice its size free; without the space it is skipped
// and tried again on the next start. A failed VACUUM leaves the file as it was.
func (s *Store) convertAutoVacuum(ctx context.Context) {
	var mode int
	if err := s.DB.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		slog.Warn("store: read auto_vacuum", "error", err)
		return
	}
	if mode == autoVacuumIncremental {
		return
	}
	var size int64
	if err := s.DB.QueryRowContext(ctx, `SELECT page_count * page_size FROM pragma_page_count, pragma_page_size`).Scan(&size); err != nil {
		slog.Warn("store: read database size", "error", err)
		return
	}
	need := 2*size + JournalSizeLimit
	if free, err := freeBytes(filepath.Dir(s.path)); err == nil && free < need {
		slog.Warn("store: not enough free space to turn on incremental auto-vacuum; will retry on next start",
			"freeBytes", free, "needBytes", need)
		return
	}
	if _, err := s.DB.ExecContext(ctx, `PRAGMA auto_vacuum=INCREMENTAL`); err != nil {
		slog.Warn("store: set auto_vacuum", "error", err)
		return
	}
	if _, err := s.DB.ExecContext(ctx, `VACUUM`); err != nil {
		slog.Warn("store: VACUUM to turn on incremental auto-vacuum failed; will retry on next start", "error", err)
		return
	}
	slog.Info("store: incremental auto-vacuum turned on", "bytes", size)
}

const autoVacuumIncremental = 2

// Maintain runs the periodic upkeep SQLite recommends for a long-lived
// connection: PRAGMA optimize, then incremental_vacuum to hand pages freed by
// deletes back to the file system. The host-metrics pruner calls it hourly.
func (s *Store) Maintain(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `PRAGMA optimize`); err != nil {
		return fmt.Errorf("store: optimize: %w", err)
	}
	// incremental_vacuum frees pages as it is stepped; read it to the end.
	rows, err := s.DB.QueryContext(ctx, `PRAGMA incremental_vacuum`)
	if err != nil {
		return fmt.Errorf("store: incremental_vacuum: %w", err)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("store: incremental_vacuum: %w", err)
	}
	return rows.Close()
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
		if err := s.apply(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// apply runs one migration in its own transaction on one pinned connection,
// so a PRAGMA set for it cannot leak to or miss the connection that runs it.
func (s *Store) apply(ctx context.Context, m migration) (err error) {
	hook, hasHook := hookFor(m.name)
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if hook.foreignKeysOff {
		// Steps 1 and 12 of SQLite's table rebuild: foreign_keys cannot change
		// inside a transaction.
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			return fmt.Errorf("store: %s: foreign keys off: %w", m.name, err)
		}
		defer func() {
			if _, ferr := conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys=ON`); ferr != nil {
				// A connection left without foreign keys must not go back to the pool.
				conn.Raw(func(any) error { return driver.ErrBadConn })
				if err == nil {
					err = fmt.Errorf("store: %s: foreign keys back on: %w", m.name, ferr)
				}
			}
		}()
		if _, err := conn.ExecContext(ctx, `PRAGMA legacy_alter_table=OFF`); err != nil {
			return err
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("store: apply %s: %w", m.name, err)
	}
	if hasHook {
		if err := hook.run(ctx, tx); err != nil {
			return fmt.Errorf("store: apply %s: %w", m.name, err)
		}
	}
	if hook.foreignKeysOff {
		bad, err := foreignKeyViolations(ctx, tx)
		if err != nil {
			return fmt.Errorf("store: %s: foreign_key_check: %w", m.name, err)
		}
		if len(bad) > 0 {
			return fmt.Errorf("store: %s: foreign keys broken: %s", m.name, strings.Join(bad, "; "))
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version, name, applied_at) VALUES(?, ?, ?)`,
		m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("store: record %s: %w", m.name, err)
	}
	return tx.Commit()
}

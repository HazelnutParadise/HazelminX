package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesAndMigrates(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "core.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	v, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != LatestVersion() || v < 1 {
		t.Fatalf("schema version %d, want %d", v, LatestVersion())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("db file mode %o, want 0600", perm)
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Fatalf("data dir mode %o, want 0700", perm)
	}
	var mode string
	if err := s.DB.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q err %v, want wal", mode, err)
	}
}

func TestReopenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	for i := 0; i < 2; i++ {
		s, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM schema_version`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != LatestVersion() {
			t.Fatalf("open #%d: %d version rows, want %d", i, n, LatestVersion())
		}
		s.Close()
	}
}

func TestRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO schema_version(version, name, applied_at) VALUES(9999, '9999_future.sql', '2999-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	_, err = Open(ctx, path)
	if !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("expected ErrNewerSchema, got %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("refusing to open must not delete or replace the database: %v", statErr)
	}
}

func TestMigrationsAreContiguous(t *testing.T) {
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 || ms[0].version != 1 {
		t.Fatalf("migrations must start at 0001, got %+v", ms)
	}
}

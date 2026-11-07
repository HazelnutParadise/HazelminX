package store

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// DAT-002: a process killed mid-write must not lose transactions it already
// reported as committed, and the file must reopen healthy.
func TestCommittedRowsSurviveProcessKill(t *testing.T) {
	if os.Getenv("HAZELMINX_STORE_CRASH_HELPER") == "1" {
		crashHelper()
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "core.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCommittedRowsSurviveProcessKill$")
	cmd.Env = append(os.Environ(), "HAZELMINX_STORE_CRASH_HELPER=1", "HAZELMINX_STORE_CRASH_PATH="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(out)
	lastCommitted := 0
	for sc.Scan() {
		n, err := strconv.Atoi(strings.TrimSpace(sc.Text()))
		if err != nil {
			continue
		}
		lastCommitted = n
		if n >= 25 {
			break
		}
	}
	if lastCommitted < 25 {
		t.Fatalf("helper reported only %d commits before exiting", lastCommitted)
	}
	if err := cmd.Process.Kill(); err != nil { // SIGKILL: no shutdown hooks run
		t.Fatal(err)
	}
	cmd.Wait()

	ctx := context.Background()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen after kill: %v", err)
	}
	defer s.Close()
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM crash_probe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < lastCommitted {
		t.Fatalf("lost committed rows: helper reported %d commits, database has %d", lastCommitted, n)
	}
	var missing int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT n FROM crash_probe WHERE n <= ?) `, lastCommitted).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != lastCommitted {
		t.Fatalf("rows 1..%d must all be present, found %d", lastCommitted, missing)
	}
}

// crashHelper writes rows in separate transactions forever, printing the count
// after each commit, until the parent kills it.
func crashHelper() {
	ctx := context.Background()
	s, err := Open(ctx, os.Getenv("HAZELMINX_STORE_CRASH_PATH"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper open:", err)
		os.Exit(2)
	}
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE crash_probe(n INTEGER PRIMARY KEY, payload BLOB NOT NULL)`); err != nil {
		fmt.Fprintln(os.Stderr, "helper create:", err)
		os.Exit(2)
	}
	payload := bytes.Repeat([]byte("x"), 2048)
	for n := 1; ; n++ {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			os.Exit(2)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO crash_probe(n, payload) VALUES(?, ?)`, n, payload); err != nil {
			os.Exit(2)
		}
		if err := tx.Commit(); err != nil {
			os.Exit(2)
		}
		fmt.Println(n)
		os.Stdout.Sync()
	}
}

// DAT-002: an uncommitted transaction must leave no trace, and committed ones
// must all be present, when the on-disk files are copied mid-transaction (the
// state a reader sees after an interruption).
func TestUncommittedWriteLeavesNoTraceInDiskSnapshot(t *testing.T) {
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "core.db")
	s, err := Open(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE probe(n INTEGER PRIMARY KEY, v TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10; i++ {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO probe(n, v) VALUES(?, 'committed')`, i); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO probe(n, v) VALUES(11, 'uncommitted')`); err != nil {
		t.Fatal(err)
	}
	// Snapshot the files while the transaction is open.
	dst := filepath.Join(t.TempDir(), "core.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(src + suffix)
		if err != nil {
			if suffix != "" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			t.Fatal(err)
		}
		if err := os.WriteFile(dst+suffix, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tx.Rollback()

	c, err := Open(ctx, dst)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer c.Close()
	var committed, uncommitted int
	if err := c.DB.QueryRowContext(ctx, `SELECT count(*) FROM probe WHERE v='committed'`).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if err := c.DB.QueryRowContext(ctx, `SELECT count(*) FROM probe WHERE v='uncommitted'`).Scan(&uncommitted); err != nil {
		t.Fatal(err)
	}
	if committed != 10 || uncommitted != 0 {
		t.Fatalf("snapshot has %d committed and %d uncommitted rows, want 10 and 0", committed, uncommitted)
	}
}

// DAT-002 / T-012: a file that is not a database is refused, left byte-for-byte
// as it was, and no blank database is created in its place.
func TestCorruptHeaderIsRefusedAndLeftUntouched(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	garbage := append(bytes.Repeat([]byte{0xA5}, 100), original[100:]...)
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = Open(ctx, path)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, garbage) {
		t.Fatal("refusing to open must not rewrite the damaged file")
	}
}

// DAT-002 / T-012: damage inside the file (not just the header) is detected by
// quick_check before any migration runs.
func TestCorruptPageIsRefused(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// Enough rows to need several pages.
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE filler(n INTEGER PRIMARY KEY, v BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO filler(v) VALUES(?)`, bytes.Repeat([]byte{byte(i)}, 1000)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := f.Stat()
	if info.Size() < 4*4096 {
		t.Fatalf("database only %d bytes; test needs several pages", info.Size())
	}
	// Overwrite the third page entirely.
	if _, err := f.WriteAt(bytes.Repeat([]byte{0xFF}, 4096), 2*4096); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = Open(ctx, path)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
}

// T-012: on an installation that already has data, a missing database file is
// a recovery situation, not a fresh install.
func TestOpenExistingRefusesMissingFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "core.db")
	_, err := OpenExisting(ctx, path)
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("expected ErrMissing, got %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("OpenExisting must not create a database")
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenExisting(ctx, path)
	if err != nil {
		t.Fatalf("OpenExisting on a present file: %v", err)
	}
	s.Close()
}

// DAT-001: the platform database is a local file; records written before a
// restart are there after it with nothing else running.
func TestRecordsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO secret_changes(secret_id, secret_name, action, actor, occurred_at) VALUES('sec_x', 'x', 'created', 'test', ?)`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM secret_changes WHERE secret_id='sec_x'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d rows after restart, want 1", n)
	}
}

func TestSynchronousFull(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var sync int
	if err := s.DB.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&sync); err != nil {
		t.Fatal(err)
	}
	if sync != 2 {
		t.Fatalf("PRAGMA synchronous = %d, want 2 (FULL) so a commit is on disk before it is acknowledged", sync)
	}
}

// 35.2: the WAL carries whole database pages, so it must be as private as the
// database file itself.
func TestWALFilesAreOwnerOnly(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE probe(n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if err != nil {
			t.Fatalf("%s: %v", suffix, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s%s mode %o, want 0600", path, suffix, perm)
		}
	}
}

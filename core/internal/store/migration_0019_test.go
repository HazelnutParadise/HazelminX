package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// openAtVersion builds a database the way a core from before ticket 128 left
// it: migrations up to and including upto, no auto_vacuum, foreign keys on.
func openAtVersion(t *testing.T, path string, upto int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.version > upto {
			break
		}
		if _, err := db.Exec(m.sql); err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version(version, name, applied_at) VALUES(?, ?, ?)`, m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func userTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		out = append(out, n)
	}
	return out
}

type colInfo struct {
	name, typ string
	notnull   bool
	hasDflt   bool
	pk        int
}

func columns(t *testing.T, db *sql.DB, table string) []colInfo {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value IS NOT NULL, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []colInfo
	for rows.Next() {
		var c colInfo
		rows.Scan(&c.name, &c.typ, &c.notnull, &c.hasDflt, &c.pk)
		out = append(out, c)
	}
	return out
}

type fkCol struct{ parent, from, to string }

func foreignKeys(t *testing.T, db *sql.DB, table string) []fkCol {
	t.Helper()
	rows, err := db.Query(`SELECT "table", "from", coalesce("to", '') FROM pragma_foreign_key_list(?) ORDER BY id, seq`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []fkCol
	for rows.Next() {
		var f fkCol
		rows.Scan(&f.parent, &f.from, &f.to)
		out = append(out, f)
	}
	return out
}

// seedOverrides gives values the generic seeder cannot guess (table-level
// CHECKs and the like).
var seedOverrides = map[string]map[string]any{
	"github_connections": {"app_ref": nil},
	"compose_units":      {"engine_env_id": "env:/run/docker.sock"},
	"compose_unit_files": {"secret_id": nil, "content": []byte("services: {}")},
}

// seedEveryTable inserts one row into every table, parents first, with each
// foreign key pointing at the parent's row, so foreign_key_check is clean.
func seedEveryTable(t *testing.T, db *sql.DB) {
	t.Helper()
	tables := userTables(t, db)
	seeded := map[string]bool{"schema_version": true} // already has rows
	for progress := true; progress; {
		progress = false
		for _, table := range tables {
			if seeded[table] {
				continue
			}
			ready := true
			for _, fk := range foreignKeys(t, db, table) {
				if fk.parent != table && !seeded[fk.parent] {
					ready = false
				}
			}
			if !ready {
				continue
			}
			seedTable(t, db, table)
			seeded[table], progress = true, true
		}
	}
	for _, table := range tables {
		if !seeded[table] {
			t.Fatalf("could not order %s after its parents", table)
		}
	}
}

func seedTable(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var create string
	db.QueryRow(`SELECT sql FROM sqlite_schema WHERE name = ?`, table).Scan(&create)
	fks := map[string]fkCol{}
	for _, fk := range foreignKeys(t, db, table) {
		fks[fk.from] = fk
	}
	var cols []string
	var vals []any
	for _, c := range columns(t, db, table) {
		v, ok := seedOverrides[table][c.name]
		if !ok {
			if fk, isFK := fks[c.name]; isFK && fk.parent != table {
				to := fk.to
				if to == "" {
					to = "rowid"
				}
				if err := db.QueryRow(fmt.Sprintf(`SELECT %s FROM %s LIMIT 1`, quoteIdent(to), quoteIdent(fk.parent))).Scan(&v); err != nil {
					t.Fatalf("%s.%s parent %s: %v", table, c.name, fk.parent, err)
				}
			} else if c.hasDflt || (!c.notnull && c.pk == 0) {
				continue
			} else if lit := checkInLiteral(create, c.name); lit != "" {
				v = lit
			} else {
				switch strings.ToUpper(c.typ) {
				case "INTEGER", "INT":
					v = 1
				case "REAL":
					v = 1.5
				case "BLOB":
					v = []byte(table + "." + c.name)
				default:
					v = table + "." + c.name
				}
			}
		}
		cols, vals = append(cols, quoteIdent(c.name)), append(vals, v)
	}
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, quoteIdent(table), strings.Join(cols, ","), strings.TrimSuffix(strings.Repeat("?,", len(cols)), ","))
	if _, err := db.Exec(q, vals...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

// checkInLiteral returns the first allowed value of a `col ... CHECK (col IN
// ('a', ...))` constraint in the CREATE TABLE text, if there is one.
func checkInLiteral(create, col string) string {
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(col) + `\s+IN\s*\(\s*'([^']*)'`)
	if m := re.FindStringSubmatch(create); m != nil {
		return m[1]
	}
	return ""
}

// snapshot renders everything the rebuild must keep: every row with its
// storage types, the indexes and triggers, foreign keys and AUTOINCREMENT
// counters.
func snapshot(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range userTables(t, db) {
		if table == "schema_version" {
			continue // 0019 adds its own row
		}
		rows, err := db.Query(`SELECT * FROM ` + quoteIdent(table))
		if err != nil {
			t.Fatal(err)
		}
		names, _ := rows.Columns()
		var b strings.Builder
		fmt.Fprintf(&b, "%v\n", names)
		for rows.Next() {
			vals := make([]any, len(names))
			ptrs := make([]any, len(names))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for _, v := range vals {
				fmt.Fprintf(&b, "%T=%v|", v, v)
			}
			b.WriteString("\n")
		}
		rows.Close()
		out["rows:"+table] = b.String()
		out["fk:"+table] = fmt.Sprint(foreignKeys(t, db, table))
		var idx string
		db.QueryRow(`SELECT group_concat(name || ' ' || "unique" || ' ' || origin || ' ' || partial, '; ') FROM (SELECT * FROM pragma_index_list(?) ORDER BY name)`, table).Scan(&idx)
		out["idx:"+table] = idx
	}
	rows, err := db.Query(`SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE type IN ('index', 'trigger') AND sql IS NOT NULL ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var typ, name, tbl, sqlText string
		rows.Scan(&typ, &name, &tbl, &sqlText)
		out["schema:"+name] = typ + " " + tbl + " " + sqlText
	}
	rows.Close()
	var seq string
	db.QueryRow(`SELECT group_concat(name || '=' || seq, ',') FROM (SELECT * FROM sqlite_sequence ORDER BY name)`).Scan(&seq)
	out["sqlite_sequence"] = seq
	return out
}

func TestMigration0019RebuildsEveryTableAsStrict(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	db := openAtVersion(t, path, 18)
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	seedEveryTable(t, db)
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	// A counter above the highest kept row: an id handed out and deleted
	// must not be reused after the rebuild.
	if _, err := db.Exec(`UPDATE sqlite_sequence SET seq = 1000 WHERE name = 'audit_events'`); err != nil {
		t.Fatal(err)
	}
	if bad := fkCheck(t, db); bad != "" {
		t.Fatalf("seed broke foreign keys: %s", bad)
	}
	before := snapshot(t, db)
	tables := userTables(t, db)
	db.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.SchemaVersion(ctx); v != LatestVersion() {
		t.Fatalf("version %d, want %d", v, LatestVersion())
	}
	after := snapshot(t, s.DB)
	for k, want := range before {
		if got := after[k]; got != want {
			t.Errorf("%s changed:\nbefore %s\nafter  %s", k, want, got)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("%s appeared: %s", k, after[k])
		}
	}
	if got := userTables(t, s.DB); fmt.Sprint(got) != fmt.Sprint(tables) {
		t.Fatalf("tables changed: %v → %v", tables, got)
	}
	for _, table := range tables {
		var rows int
		s.DB.QueryRow(`SELECT count(*) FROM ` + quoteIdent(table)).Scan(&rows)
		if rows == 0 {
			t.Errorf("%s has no rows; the test must cover every table", table)
		}
	}
	assertAllStrict(t, s.DB)
	if bad := fkCheck(t, s.DB); bad != "" {
		t.Fatalf("foreign keys broken after rebuild: %s", bad)
	}
	var fkOn int
	s.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&fkOn)
	if fkOn != 1 {
		t.Fatal("foreign keys left off after the migration")
	}
	// Enforcement still works through the rebuilt tables.
	if _, err := s.DB.Exec(`INSERT INTO sessions (id, user_id, token_hash, csrf_token, source_ip, created_at, last_seen_at, expires_at)
		VALUES ('s-orphan', 'no-such-user', 'th', 'c', 'ip', 't', 't', 't')`); err == nil {
		t.Fatal("session for a missing user accepted")
	}
	// The ledger triggers came back.
	if _, err := s.DB.Exec(`DELETE FROM host_ledger_lines`); err == nil {
		t.Fatal("host_ledger_lines delete allowed; trigger lost")
	}
	// AUTOINCREMENT continues past the old counter.
	var seq int
	s.DB.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'audit_events'`).Scan(&seq)
	if seq != 1000 {
		t.Fatalf("audit_events counter %d, want 1000", seq)
	}
	var ok string
	s.DB.QueryRow(`PRAGMA integrity_check`).Scan(&ok)
	if ok != "ok" {
		t.Fatalf("integrity_check: %s", ok)
	}
}

func fkCheck(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var bad []string
	for rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fk int
		rows.Scan(&table, &rowid, &parent, &fk)
		bad = append(bad, table+"→"+parent)
	}
	return strings.Join(bad, ", ")
}

func assertAllStrict(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_list WHERE schema = 'main' AND type = 'table' AND strict = 0 AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var loose []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		loose = append(loose, n)
	}
	if len(loose) > 0 {
		t.Fatalf("tables not STRICT (declare STRICT in new migrations): %v", loose)
	}
}

// Every table of a fresh database is STRICT, including tables added by
// migrations after 0019.
func TestEveryTableIsStrict(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertAllStrict(t, s.DB)
}

func TestStrictRejectsWrongType(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at, version) VALUES ('u1', 'mei', 'h', 't', 't', 'two')`)
	if err == nil || !strings.Contains(err.Error(), "cannot store TEXT value in INTEGER column") {
		t.Fatalf("wrong-typed insert: %v", err)
	}
	// Lossless conversions still happen, as in any SQLite table: '2' is 2.
	if _, err := s.DB.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at, version) VALUES ('u1', 'mei', 'h', 't', 't', '2')`); err != nil {
		t.Fatal(err)
	}
}

// A value a STRICT table would convert stops the migration: nothing is
// rebuilt and the database stays at 0018 with its data.
func TestMigration0019RefusesMismatchedValues(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	db := openAtVersion(t, path, 18)
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at, version) VALUES ('u1', 'mei', 'h', 't', 't', 'two')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at, session_idle_seconds) VALUES ('u2', 'bob', 'h', 't', 't', 1.5)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err := Open(ctx, path)
	if !errors.Is(err, ErrStrictMismatch) {
		t.Fatalf("Open: %v, want ErrStrictMismatch", err)
	}
	for _, want := range []string{"users.version (INTEGER) has 1 row(s) holding text", "users.session_idle_seconds (INTEGER) has 1 row(s) holding real"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	db, err = sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v, strict, users int
	db.QueryRow(`SELECT max(version) FROM schema_version`).Scan(&v)
	db.QueryRow(`SELECT strict FROM pragma_table_list WHERE name = 'users'`).Scan(&strict)
	db.QueryRow(`SELECT count(*) FROM users`).Scan(&users)
	if v != 18 || strict != 0 || users != 2 {
		t.Fatalf("after refused migration: version %d strict %d users %d", v, strict, users)
	}
}

func TestStrictCreateKeepsOptions(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"CREATE TABLE a (x INTEGER)", `CREATE TABLE "tmp" (x INTEGER) STRICT`},
		{"CREATE TABLE a (x INTEGER PRIMARY KEY) WITHOUT ROWID", `CREATE TABLE "tmp" (x INTEGER PRIMARY KEY) WITHOUT ROWID, STRICT`},
		{"CREATE TABLE \"a\" (\n x TEXT -- note (y)\n)", "CREATE TABLE \"tmp\" (\n x TEXT -- note (y)\n) STRICT"},
	} {
		got, err := strictCreate(strictTable{name: "a", sql: c.in}, "tmp")
		if err != nil || got != c.want {
			t.Errorf("%q → %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := strictCreate(strictTable{name: "a", sql: "CREATE TABLE b (x)"}, "tmp"); err == nil {
		t.Error("wrong table name accepted")
	}
	if _, err := strictCreate(strictTable{name: "a", sql: "CREATE TABLE a (x) STRICT"}, "tmp"); err == nil {
		t.Error("existing options accepted")
	}
}

func pragmaInt(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	var v int64
	if err := db.QueryRow(`PRAGMA ` + name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

func TestPragmasTakeEffect(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mode string
	s.DB.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	for name, want := range map[string]int64{
		"journal_size_limit": JournalSizeLimit,
		"auto_vacuum":        autoVacuumIncremental,
		"synchronous":        2,
		"foreign_keys":       1,
		"busy_timeout":       5000,
	} {
		if got := pragmaInt(t, s.DB, name); got != want {
			t.Errorf("PRAGMA %s = %d, want %d", name, got, want)
		}
	}
	if mode != "wal" {
		t.Errorf("journal_mode %s", mode)
	}
}

// A database from before ticket 128 has auto_vacuum NONE; Open converts it
// with one VACUUM, unless there is not enough free space, in which case it
// opens anyway and converts on a later start.
func TestOpenConvertsAutoVacuum(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	db := openAtVersion(t, path, 18)
	if got := pragmaInt(t, db, "auto_vacuum"); got != 0 {
		t.Fatalf("old database auto_vacuum %d", got)
	}
	seedRow(t, db, "users", map[string]any{"id": "u1"})
	db.Close()

	real := freeBytes
	freeBytes = func(string) (int64, error) { return 1 << 20, nil }
	s, err := Open(ctx, path)
	freeBytes = real
	if err != nil {
		t.Fatalf("Open without space: %v", err)
	}
	if got := pragmaInt(t, s.DB, "auto_vacuum"); got != 0 {
		t.Fatalf("converted without the space: auto_vacuum %d", got)
	}
	s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := pragmaInt(t, s.DB, "auto_vacuum"); got != autoVacuumIncremental {
		t.Fatalf("auto_vacuum %d after conversion", got)
	}
	var n int
	s.DB.QueryRow(`SELECT count(*) FROM users WHERE id = 'u1'`).Scan(&n)
	if n != 1 {
		t.Fatal("row lost in VACUUM")
	}
}

func TestMaintainReturnsFreedPages(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB.Exec(`CREATE TABLE filler (b BLOB) STRICT`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := s.DB.Exec(`INSERT INTO filler VALUES (zeroblob(8192))`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB.Exec(`DELETE FROM filler`); err != nil {
		t.Fatal(err)
	}
	if free := pragmaInt(t, s.DB, "freelist_count"); free < 100 {
		t.Fatalf("freelist_count %d before Maintain", free)
	}
	if err := s.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	if free := pragmaInt(t, s.DB, "freelist_count"); free != 0 {
		t.Fatalf("freelist_count %d after Maintain", free)
	}
}

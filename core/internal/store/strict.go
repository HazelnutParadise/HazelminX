package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// migrationHook is Go work a migration needs beyond its SQL file. It runs in
// the migration's transaction, after the file's SQL.
type migrationHook struct {
	// foreignKeysOff runs the migration with foreign key enforcement off,
	// as SQLite's table rebuild procedure requires (dropping a parent table
	// with enforcement on would cascade or be refused). foreign_key_check
	// must come back empty before the transaction commits.
	foreignKeysOff bool
	run            func(ctx context.Context, tx *sql.Tx) error
}

// migrationHooks is keyed by the migration file name without its number, so
// renumbering a migration does not detach its hook.
var migrationHooks = map[string]migrationHook{
	"strict_tables": {foreignKeysOff: true, run: rebuildStrict},
}

func hookFor(name string) (migrationHook, bool) {
	_, rest, _ := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
	h, ok := migrationHooks[rest]
	return h, ok
}

// ErrStrictMismatch is returned when a stored value's type does not match its
// column, so the table cannot become STRICT without converting data.
var ErrStrictMismatch = errors.New("store: stored values do not match their column types")

// strictAccepts lists, per declared column type, the storage classes a STRICT
// column holds without converting the value. REAL takes integers because
// SQLite converts them to REAL without loss.
var strictAccepts = map[string][]string{
	"INT":     {"integer"},
	"INTEGER": {"integer"},
	"REAL":    {"real", "integer"},
	"TEXT":    {"text"},
	"BLOB":    {"blob"},
	"ANY":     nil, // anything
}

type strictTable struct {
	name string
	sql  string
}

// rebuildStrict rebuilds every non-STRICT table as STRICT following steps 3-9
// of https://www.sqlite.org/lang_altertable.html#otheralter; the caller does
// steps 1-2 and 10-12 (foreign keys off, transaction, foreign_key_check).
func rebuildStrict(ctx context.Context, tx *sql.Tx) error {
	tables, err := nonStrictTables(ctx, tx)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return nil
	}
	var views int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='view'`).Scan(&views); err != nil {
		return err
	}
	if views > 0 {
		return errors.New("store: strict rebuild does not handle views; recreate them in the migration")
	}
	// Check every table before changing any, so the error lists all mismatches.
	var mismatches []string
	cols := map[string][]string{}
	for _, t := range tables {
		names, bad, err := checkStrictFit(ctx, tx, t.name)
		if err != nil {
			return err
		}
		cols[t.name] = names
		mismatches = append(mismatches, bad...)
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("%w: %s", ErrStrictMismatch, strings.Join(mismatches, "; "))
	}
	for _, t := range tables {
		if err := rebuildOne(ctx, tx, t, cols[t.name]); err != nil {
			return fmt.Errorf("store: rebuild %s as STRICT: %w", t.name, err)
		}
	}
	return nil
}

func nonStrictTables(ctx context.Context, tx *sql.Tx) ([]strictTable, error) {
	rows, err := tx.QueryContext(ctx, `SELECT l.name, s.sql FROM pragma_table_list AS l
		JOIN sqlite_schema AS s ON s.type = 'table' AND s.name = l.name
		WHERE l.schema = 'main' AND l.type = 'table' AND l.strict = 0 AND l.name NOT LIKE 'sqlite\_%' ESCAPE '\'
		ORDER BY l.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []strictTable
	for rows.Next() {
		var t strictTable
		if err := rows.Scan(&t.name, &t.sql); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// checkStrictFit returns the table's columns and one line per column holding
// values a STRICT table would convert or refuse.
func checkStrictFit(ctx context.Context, tx *sql.Tx, table string) ([]string, []string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name, type, hidden FROM pragma_table_xinfo(?) ORDER BY cid`, table)
	if err != nil {
		return nil, nil, err
	}
	type col struct{ name, typ string }
	var all []col
	for rows.Next() {
		var c col
		var hidden int
		if err := rows.Scan(&c.name, &c.typ, &hidden); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if hidden != 0 {
			rows.Close()
			return nil, nil, fmt.Errorf("store: %s.%s is a generated or hidden column; the strict rebuild does not copy those", table, c.name)
		}
		all = append(all, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var names, bad []string
	for _, c := range all {
		names = append(names, c.name)
		typ := strings.ToUpper(strings.TrimSpace(c.typ))
		accepts, ok := strictAccepts[typ]
		if !ok {
			bad = append(bad, fmt.Sprintf("%s.%s has type %q, which a STRICT table does not allow", table, c.name, c.typ))
			continue
		}
		if accepts == nil {
			continue
		}
		q := fmt.Sprintf(`SELECT count(*), group_concat(DISTINCT typeof(%[1]s)) FROM %[2]s WHERE %[1]s IS NOT NULL AND typeof(%[1]s) NOT IN ('%[3]s')`,
			quoteIdent(c.name), quoteIdent(table), strings.Join(accepts, "','"))
		var n int
		var kinds sql.NullString
		if err := tx.QueryRowContext(ctx, q).Scan(&n, &kinds); err != nil {
			return nil, nil, err
		}
		if n > 0 {
			bad = append(bad, fmt.Sprintf("%s.%s (%s) has %d row(s) holding %s", table, c.name, typ, n, kinds.String))
		}
	}
	return names, bad, nil
}

func rebuildOne(ctx context.Context, tx *sql.Tx, t strictTable, cols []string) error {
	tmp := "strict_rebuild_" + t.name
	create, err := strictCreate(t, tmp)
	if err != nil {
		return err
	}
	// Step 3: remember the indexes and triggers that belong to the table.
	// Automatic indexes (sql IS NULL) come back from the constraints.
	rows, err := tx.QueryContext(ctx, `SELECT sql FROM sqlite_schema WHERE tbl_name = ? AND type IN ('index', 'trigger') AND sql IS NOT NULL ORDER BY type, name`, t.name)
	if err != nil {
		return err
	}
	var extras []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		extras = append(extras, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var seq sql.NullInt64
	hasSeq := false
	var seqTable int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'sqlite_sequence'`).Scan(&seqTable); err != nil {
		return err
	}
	if seqTable == 1 {
		err := tx.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = ?`, t.name).Scan(&seq)
		switch {
		case err == nil:
			hasSeq = true
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quoteIdent(c)
	}
	list := strings.Join(quoted, ", ")
	steps := []string{
		create, // step 4
		fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM %s`, quoteIdent(tmp), list, list, quoteIdent(t.name)), // step 5
		`DROP TABLE ` + quoteIdent(t.name),                                              // step 6
		fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, quoteIdent(tmp), quoteIdent(t.name)), // step 7
	}
	steps = append(steps, extras...) // step 8
	for _, s := range steps {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", firstLine(s), err)
		}
	}
	if hasSeq && seq.Valid {
		// The copy set the counter to the highest seq copied; keep the old
		// counter if it was higher, so deleted ids are never reused.
		if _, err := tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq = max(seq, ?) WHERE name = ?`, seq.Int64, t.name); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_sequence WHERE name = ?`, t.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO sqlite_sequence(name, seq) VALUES(?, ?)`, t.name, seq.Int64); err != nil {
				return err
			}
		}
	}
	return nil
}

// strictCreate turns the table's stored CREATE TABLE text into the same
// statement for table tmp with STRICT added to its options.
func strictCreate(t strictTable, tmp string) (string, error) {
	open := strings.Index(t.sql, "(")
	end := strings.LastIndex(t.sql, ")")
	if open < 0 || end < open {
		return "", fmt.Errorf("unexpected CREATE TABLE text for %s", t.name)
	}
	head := strings.Join(strings.Fields(t.sql[:open]), " ")
	if !strings.EqualFold(head, "CREATE TABLE "+t.name) && !strings.EqualFold(head, `CREATE TABLE "`+t.name+`"`) {
		return "", fmt.Errorf("unexpected CREATE TABLE head %q for %s", head, t.name)
	}
	body := t.sql[open : end+1]
	options := strings.Join(strings.Fields(t.sql[end+1:]), " ")
	switch {
	case options == "":
		options = "STRICT"
	case strings.EqualFold(options, "WITHOUT ROWID"):
		options += ", STRICT"
	default:
		return "", fmt.Errorf("unexpected table options %q for %s", options, t.name)
	}
	return "CREATE TABLE " + quoteIdent(tmp) + " " + body + " " + options, nil
}

// foreignKeyViolations lists what PRAGMA foreign_key_check reports.
func foreignKeyViolations(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fk int
		if err := rows.Scan(&table, &rowid, &parent, &fk); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("%s row %d → %s", table, rowid.Int64, parent))
	}
	return out, rows.Err()
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

package paging

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

var cols = map[string]Column{"createdAt": {Expr: "created_at"}, "id": {Expr: "id"}, "seq": {Expr: "seq", Int: true}}

func TestSQLFirstPage(t *testing.T) {
	o, w, a, err := Query{Sort: []SortKey{{"createdAt", true}, {"id", true}}}.SQL(cols)
	if err != nil || o != "created_at DESC, id DESC" || w != "" || a != nil {
		t.Fatalf("%q %q %v %v", o, w, a, err)
	}
}

func TestSQLKeysetMixedDirections(t *testing.T) {
	_, w, a, err := Query{Sort: []SortKey{{"createdAt", false}, {"id", true}}, After: []string{"t1", "x"}}.SQL(cols)
	if err != nil {
		t.Fatal(err)
	}
	if w != "((created_at > ?) OR (created_at = ? AND id < ?))" || !reflect.DeepEqual(a, []any{"t1", "t1", "x"}) {
		t.Fatalf("%q %v", w, a)
	}
}

func TestSQLRejectsBadQueries(t *testing.T) {
	for name, q := range map[string]Query{
		"empty":       {},
		"unknown":     {Sort: []SortKey{{"nope", false}}},
		"length":      {Sort: []SortKey{{"id", false}}, After: []string{"a", "b"}},
		"int-not-int": {Sort: []SortKey{{"seq", false}}, After: []string{"x"}},
	} {
		if _, _, _, err := q.SQL(cols); !errors.Is(err, ErrBadQuery) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestKeysetWalkMatchesFullOrder pages a real SQLite table with ties in the
// first key and checks the pages concatenate to the full ordered result.
func TestKeysetWalkMatchesFullOrder(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE r(id TEXT PRIMARY KEY, created_at TEXT, seq INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 23; i++ {
		if _, err := db.Exec(`INSERT INTO r VALUES(?,?,?)`, fmt.Sprintf("id%02d", i), fmt.Sprintf("t%d", i%4), i*7%23); err != nil {
			t.Fatal(err)
		}
	}
	for _, sort := range [][]SortKey{
		{{"createdAt", true}, {"id", true}},
		{{"createdAt", false}, {"id", true}},
		{{"seq", false}, {"id", false}},
	} {
		all := walk(t, db, Query{Sort: sort, Limit: 1000})
		var paged []string
		q := Query{Sort: sort, Limit: 5}
		for {
			page := walk(t, db, q)
			if len(page) == 0 {
				break
			}
			paged = append(paged, page...)
			last := page[len(page)-1]
			var c string
			var s int
			if err := db.QueryRow(`SELECT created_at, seq FROM r WHERE id=?`, last).Scan(&c, &s); err != nil {
				t.Fatal(err)
			}
			q.After = nil
			for _, k := range sort {
				switch k.Field {
				case "createdAt":
					q.After = append(q.After, c)
				case "seq":
					q.After = append(q.After, fmt.Sprint(s))
				default:
					q.After = append(q.After, last)
				}
			}
		}
		if !reflect.DeepEqual(all, paged) || len(all) != 23 {
			t.Fatalf("sort %v: paged %v, full %v", sort, paged, all)
		}
	}
}

func walk(t *testing.T, db *sql.DB, q Query) []string {
	t.Helper()
	o, w, a, err := q.SQL(cols)
	if err != nil {
		t.Fatal(err)
	}
	s := `SELECT id FROM r`
	if w != "" {
		s += " WHERE " + w
	}
	rows, err := db.Query(s+" ORDER BY "+o+" LIMIT ?", append(a, q.Limit)...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		out = append(out, id)
	}
	return out
}

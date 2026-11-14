package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedRow inserts a row with id into table, filling every required column
// with a placeholder, unless fixed gives the value.
func seedRow(t *testing.T, db *sql.DB, table string, fixed map[string]any) {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	var vals []any
	for rows.Next() {
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		rows.Scan(&name, &typ, &notnull, &dflt, &pk)
		if v, ok := fixed[name]; ok {
			cols, vals = append(cols, name), append(vals, v)
			continue
		}
		if (notnull == 1 || pk == 1) && !dflt.Valid {
			var v any = name + "-x"
			if strings.Contains(strings.ToUpper(typ), "INT") {
				v = 1
			}
			cols, vals = append(cols, name), append(vals, v)
		}
	}
	rows.Close()
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, table, strings.Join(cols, ","), strings.TrimSuffix(strings.Repeat("?,", len(cols)), ","))
	if _, err := db.Exec(q, vals...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

// The 0018 rebuild of compose_units must keep every unit, file and
// deployment: dropping the old tables with foreign keys on would cascade.
func TestMigration0018KeepsUnitsFilesAndDeployments(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.version >= 18 {
			break
		}
		if _, err := db.Exec(m.sql); err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version(version, name, applied_at) VALUES(?, ?, ?)`, m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(`PRAGMA foreign_keys=OFF`)
	seedRow(t, db, "users", map[string]any{"id": "u1"})
	seedRow(t, db, "nodes", map[string]any{"id": "n1", "state": "active"})
	seedRow(t, db, "projects", map[string]any{"id": "p1", "created_by": "u1"})
	seedRow(t, db, "project_environments", map[string]any{"id": "e1", "project_id": "p1", "created_by": "u1"})
	if _, err := db.Exec(`INSERT INTO compose_units (id, environment_id, name, compose_project, source, main_file, node_id, engine_env_id, created_by, created_at, updated_at, version)
		VALUES ('cu1','e1','web','shop-prod-web','upload','compose.yaml','n1','env:/run/docker.sock','u1','2026-11-14T00:00:00Z','2026-11-14T00:00:00Z',3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO compose_unit_files (unit_id, path, content, size, sha256) VALUES ('cu1','compose.yaml','services: {}',12,'ab')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO compose_deployments (id, unit_id, unit_version, compose_project, revision, manifest_digest, node_id, engine_env_id, job_id, state, created_by, started_at)
		VALUES ('d1','cu1',3,'shop-prod-web','r1','m','n1','env:/run/docker.sock','j1','succeeded','u1','2026-11-14T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Exec(`PRAGMA foreign_keys=ON`)
	db.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var units, files, deps int
	s.DB.QueryRow(`SELECT COUNT(*) FROM compose_units WHERE id='cu1' AND version=3`).Scan(&units)
	s.DB.QueryRow(`SELECT COUNT(*) FROM compose_unit_files WHERE unit_id='cu1'`).Scan(&files)
	s.DB.QueryRow(`SELECT COUNT(*) FROM compose_deployments WHERE unit_id='cu1' AND source_commit=''`).Scan(&deps)
	if units != 1 || files != 1 || deps != 1 {
		t.Fatalf("after 0018: units %d files %d deployments %d", units, files, deps)
	}
	// The rebuilt children still cascade from the rebuilt units table.
	var fkTarget string
	s.DB.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('compose_unit_files')`).Scan(&fkTarget)
	if fkTarget != "compose_units" {
		t.Fatalf("compose_unit_files references %q", fkTarget)
	}
	if _, err := s.DB.Exec(`UPDATE compose_units SET source='git' WHERE id='cu1'`); err != nil {
		t.Fatalf("source git refused: %v", err)
	}
	if _, err := s.DB.Exec(`DELETE FROM compose_units WHERE id='cu1'`); err != nil {
		t.Fatal(err)
	}
	s.DB.QueryRow(`SELECT COUNT(*) FROM compose_unit_files`).Scan(&files)
	s.DB.QueryRow(`SELECT COUNT(*) FROM compose_deployments`).Scan(&deps)
	if files != 0 || deps != 0 {
		t.Fatalf("cascade lost: files %d deployments %d", files, deps)
	}
	if v, _ := s.SchemaVersion(ctx); v != LatestVersion() {
		t.Fatalf("version %d", v)
	}
}

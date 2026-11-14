// Package projects holds projects, project environments and their Compose
// units (ticket 044.1; spec 15, addendum 04 52.1 and 52.5).
//
// A project has environments (production, staging); an environment has
// one Compose unit until ticket 108 allows several. A unit is a set of
// files kept byte for byte, a main file, the files merged after it, the
// profiles to enable and the engine environment it deploys to. Its Compose
// project name is fixed at creation as <project>-<environment>-<unit> and
// never follows a rename (52.1).
//
// Authorization is the caller's job (the API checks projects:read and
// projects:write on the project scope); this package validates, stores,
// audits inside the transaction of each change and keeps secret files in
// the secret store.
package projects

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/secrets"
)

// Errors. The API maps each to one error code.
var (
	ErrNotFound        = errors.New("projects: not found")
	ErrVersionMismatch = errors.New("projects: version mismatch")
	ErrExists          = errors.New("projects: already exists")
)

// ValidationError names the field that is wrong.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return "projects: " + e.Field + ": " + e.Message }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// ReferencedError: the resource still has children.
type ReferencedError struct {
	What string
	IDs  []string
}

func (e *ReferencedError) Error() string { return "projects: still has " + e.What }

// Vault is the part of the secret store this package uses.
type Vault interface {
	Create(ctx context.Context, in secrets.CreateInput) (secrets.Secret, error)
	Delete(ctx context.Context, id, actor string) error
}

// Auditor writes audit rows inside the transaction of the change.
type Auditor interface {
	RecordTx(ctx context.Context, tx *sql.Tx, ev iam.AuditEvent) error
}

// Actor is who asks.
type Actor struct {
	UserID, Username, TraceID, SourceIP string
}

// Options builds a Service.
type Options struct {
	DB     *sql.DB
	Vault  Vault
	Audit  Auditor
	Now    func() time.Time
	Logger *slog.Logger
}

// Service stores projects.
type Service struct {
	db    *sql.DB
	vault Vault
	audit Auditor
	now   func() time.Time
	log   *slog.Logger
}

// New builds a Service.
func New(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{db: o.DB, vault: o.Vault, audit: o.Audit, now: o.Now, log: o.Logger}
}

// Limits on a unit's files. Bodies arrive as JSON of at most 1 MiB.
const (
	MaxFiles     = 200
	MaxFileBytes = 256 << 10
	MaxUnitBytes = 768 << 10
	maxPathBytes = 255
	maxText      = 2000
)

// nameRE is 52.1: lowercase letter first, lowercase letters, digits and
// '-', at most 30 characters. The same names make DNS names, Compose
// project names and Kubernetes resource names.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)

func checkName(field, n string) error {
	if !nameRE.MatchString(n) || strings.HasSuffix(n, "-") {
		return invalid(field, "use a lowercase letter first, then lowercase letters, digits and '-' (not last), at most 30 characters")
	}
	return nil
}

func checkText(field, s string, max int) error {
	if utf8.RuneCountInString(s) > max {
		return invalid(field, "at most %d characters", max)
	}
	for _, r := range s {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) {
			return invalid(field, "control and invisible format characters are not allowed")
		}
	}
	return nil
}

// ---- types ---------------------------------------------------------------

// Project is one application or product.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description"`
	Version     int64     `json:"version"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Environment is one set of variables, secrets, data and approvals of a
// project.
type Environment struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     int64     `json:"version"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Sources a unit's files came from. Git (049) and the store (052) add theirs.
const (
	SourceUpload = "upload"
	SourceEditor = "editor"
)

// File is one file of a unit. Content is set only for files that are not
// secret, and only when the unit is read with its contents.
type File struct {
	Path   string `json:"path"`
	Secret bool   `json:"secret"`
	Size   int64  `json:"size"`
	// SHA256 is empty for a secret file.
	SHA256  string  `json:"sha256"`
	Content *string `json:"content,omitempty"`
}

// Target is the engine environment a unit deploys to.
type Target struct {
	NodeID              string `json:"nodeId"`
	EngineEnvironmentID string `json:"engineEnvironmentId"`
}

// Unit is the Compose unit of an environment.
type Unit struct {
	ID             string    `json:"id"`
	EnvironmentID  string    `json:"environmentId"`
	ProjectID      string    `json:"projectId"`
	Name           string    `json:"name"`
	ComposeProject string    `json:"composeProject"`
	Source         string    `json:"source"`
	MainFile       string    `json:"mainFile"`
	ExtraFiles     []string  `json:"extraFiles"`
	WorkingDir     string    `json:"workingDir"`
	Profiles       []string  `json:"profiles"`
	Target         *Target   `json:"target"`
	Files          []File    `json:"files"`
	Version        int64     `json:"version"`
	CreatedBy      string    `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// ProjectInput creates a project.
type ProjectInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// ProjectPatch changes a project; nil fields stay. Renaming does not move
// any unit's Compose project name.
type ProjectPatch struct {
	Name        *string `json:"name,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Description *string `json:"description,omitempty"`
}

// EnvironmentInput creates an environment.
type EnvironmentInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// EnvironmentPatch changes an environment; nil fields stay.
type EnvironmentPatch struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// FileInput is one file sent by the caller. A secret file sends its
// content once; later writes send unchanged=true to keep the stored value.
type FileInput struct {
	Path      string  `json:"path"`
	Content   *string `json:"content,omitempty"`
	Secret    bool    `json:"secret,omitempty"`
	Unchanged bool    `json:"unchanged,omitempty"`
}

// UnitInput creates or replaces a unit. Name is used only on creation.
type UnitInput struct {
	Name       string      `json:"name,omitempty"`
	Source     string      `json:"source"`
	MainFile   string      `json:"mainFile"`
	ExtraFiles []string    `json:"extraFiles"`
	WorkingDir string      `json:"workingDir,omitempty"`
	Profiles   []string    `json:"profiles"`
	Target     *Target     `json:"target"`
	Files      []FileInput `json:"files"`
}

// ---- helpers -------------------------------------------------------------

const timeLayout = time.RFC3339

func ts(t time.Time) string { return t.UTC().Truncate(time.Second).Format(timeLayout) }

func pt(s string) time.Time { t, _ := time.Parse(timeLayout, s); return t }

func newID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Scope is the IAM scope of a project.
func Scope(projectID string) iam.Scope { return iam.Scope{Kind: iam.ScopeProject, ID: projectID} }

func consumer(unitID string) string { return "projects:" + unitID }

func (s *Service) write(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) auditTx(ctx context.Context, tx *sql.Tx, a Actor, action, projectID, kind, id string, before, after map[string]any) error {
	return s.audit.RecordTx(ctx, tx, iam.AuditEvent{Action: action, Scope: Scope(projectID), ResourceKind: kind, ResourceID: id,
		ActorID: a.UserID, ActorName: a.Username, Result: iam.ResultOK, Before: before, After: after, TraceID: a.TraceID, SourceIP: a.SourceIP})
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

type scanner interface{ Scan(...any) error }

// ---- projects ------------------------------------------------------------

const projectCols = `id, name, display_name, description, version, created_by, created_at, updated_at`

func scanProject(r scanner) (*Project, error) {
	var p Project
	var c, u string
	if err := r.Scan(&p.ID, &p.Name, &p.DisplayName, &p.Description, &p.Version, &p.CreatedBy, &c, &u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = pt(c), pt(u)
	return &p, nil
}

func (p *Project) audit() map[string]any {
	return map[string]any{"name": p.Name, "displayName": p.DisplayName, "description": p.Description}
}

// CreateProject adds a project.
func (s *Service) CreateProject(ctx context.Context, a Actor, in ProjectInput) (*Project, error) {
	if err := checkName("name", in.Name); err != nil {
		return nil, err
	}
	if err := checkText("displayName", in.DisplayName, 100); err != nil {
		return nil, err
	}
	if err := checkText("description", in.Description, maxText); err != nil {
		return nil, err
	}
	now := s.now()
	p := &Project{ID: newID("prj"), Name: in.Name, DisplayName: in.DisplayName, Description: in.Description, Version: 1,
		CreatedBy: a.UserID, CreatedAt: pt(ts(now)), UpdatedAt: pt(ts(now))}
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO projects (`+projectCols+`) VALUES (?,?,?,?,1,?,?,?)`,
			p.ID, p.Name, p.DisplayName, p.Description, p.CreatedBy, ts(now), ts(now)); err != nil {
			if isUnique(err) {
				return ErrExists
			}
			return err
		}
		return s.auditTx(ctx, tx, a, "project.create", p.ID, "project", p.ID, nil, p.audit())
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetProject returns one project.
func (s *Service) GetProject(ctx context.Context, id string) (*Project, error) {
	return scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
}

// ListProjects returns every project; the caller filters what may be seen.
func (s *Service) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectCols+` FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// UpdateProject changes a project at version ifVersion.
func (s *Service) UpdateProject(ctx context.Context, a Actor, id string, ifVersion int64, in ProjectPatch) (*Project, error) {
	var out *Project
	err := s.write(ctx, func(tx *sql.Tx) error {
		p, err := scanProject(tx.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
		if err != nil {
			return err
		}
		if p.Version != ifVersion {
			return ErrVersionMismatch
		}
		before := p.audit()
		if in.Name != nil {
			if err := checkName("name", *in.Name); err != nil {
				return err
			}
			p.Name = *in.Name
		}
		if in.DisplayName != nil {
			if err := checkText("displayName", *in.DisplayName, 100); err != nil {
				return err
			}
			p.DisplayName = *in.DisplayName
		}
		if in.Description != nil {
			if err := checkText("description", *in.Description, maxText); err != nil {
				return err
			}
			p.Description = *in.Description
		}
		now := s.now()
		res, err := tx.ExecContext(ctx, `UPDATE projects SET name=?, display_name=?, description=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
			p.Name, p.DisplayName, p.Description, ts(now), id, ifVersion)
		if isUnique(err) {
			return ErrExists
		}
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrVersionMismatch
		}
		p.Version, p.UpdatedAt = p.Version+1, pt(ts(now))
		out = p
		return s.auditTx(ctx, tx, a, "project.update", id, "project", id, before, p.audit())
	})
	return out, err
}

// DeleteProject removes a project without environments.
func (s *Service) DeleteProject(ctx context.Context, a Actor, id string, ifVersion int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		p, err := scanProject(tx.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
		if err != nil {
			return err
		}
		if p.Version != ifVersion {
			return ErrVersionMismatch
		}
		ids, err := column(ctx, tx, `SELECT id FROM project_environments WHERE project_id = ? ORDER BY name`, id)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			return &ReferencedError{What: "environments", IDs: ids}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id); err != nil {
			return err
		}
		return s.auditTx(ctx, tx, a, "project.delete", id, "project", id, p.audit(), nil)
	})
}

func column(ctx context.Context, tx *sql.Tx, q string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- environments ----------------------------------------------------------

const envCols = `id, project_id, name, description, version, created_by, created_at, updated_at`

func scanEnv(r scanner) (*Environment, error) {
	var e Environment
	var c, u string
	if err := r.Scan(&e.ID, &e.ProjectID, &e.Name, &e.Description, &e.Version, &e.CreatedBy, &c, &u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.CreatedAt, e.UpdatedAt = pt(c), pt(u)
	return &e, nil
}

func (e *Environment) audit() map[string]any {
	return map[string]any{"name": e.Name, "description": e.Description}
}

// CreateEnvironment adds an environment to a project.
func (s *Service) CreateEnvironment(ctx context.Context, a Actor, projectID string, in EnvironmentInput) (*Environment, error) {
	if err := checkName("name", in.Name); err != nil {
		return nil, err
	}
	if err := checkText("description", in.Description, maxText); err != nil {
		return nil, err
	}
	now := s.now()
	e := &Environment{ID: newID("env"), ProjectID: projectID, Name: in.Name, Description: in.Description, Version: 1,
		CreatedBy: a.UserID, CreatedAt: pt(ts(now)), UpdatedAt: pt(ts(now))}
	err := s.write(ctx, func(tx *sql.Tx) error {
		if _, err := scanProject(tx.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, projectID)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_environments (`+envCols+`) VALUES (?,?,?,?,1,?,?,?)`,
			e.ID, projectID, e.Name, e.Description, e.CreatedBy, ts(now), ts(now)); err != nil {
			if isUnique(err) {
				return ErrExists
			}
			return err
		}
		return s.auditTx(ctx, tx, a, "project-environment.create", projectID, "project-environment", e.ID, nil, e.audit())
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetEnvironment returns an environment of a project.
func (s *Service) GetEnvironment(ctx context.Context, projectID, id string) (*Environment, error) {
	return scanEnv(s.db.QueryRowContext(ctx, `SELECT `+envCols+` FROM project_environments WHERE id = ? AND project_id = ?`, id, projectID))
}

// ListEnvironments returns a project's environments.
func (s *Service) ListEnvironments(ctx context.Context, projectID string) ([]Environment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+envCols+` FROM project_environments WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Environment{}
	for rows.Next() {
		e, err := scanEnv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// UpdateEnvironment changes an environment at version ifVersion.
func (s *Service) UpdateEnvironment(ctx context.Context, a Actor, projectID, id string, ifVersion int64, in EnvironmentPatch) (*Environment, error) {
	var out *Environment
	err := s.write(ctx, func(tx *sql.Tx) error {
		e, err := scanEnv(tx.QueryRowContext(ctx, `SELECT `+envCols+` FROM project_environments WHERE id = ? AND project_id = ?`, id, projectID))
		if err != nil {
			return err
		}
		if e.Version != ifVersion {
			return ErrVersionMismatch
		}
		before := e.audit()
		if in.Name != nil {
			if err := checkName("name", *in.Name); err != nil {
				return err
			}
			e.Name = *in.Name
		}
		if in.Description != nil {
			if err := checkText("description", *in.Description, maxText); err != nil {
				return err
			}
			e.Description = *in.Description
		}
		now := s.now()
		res, err := tx.ExecContext(ctx, `UPDATE project_environments SET name=?, description=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
			e.Name, e.Description, ts(now), id, ifVersion)
		if isUnique(err) {
			return ErrExists
		}
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrVersionMismatch
		}
		e.Version, e.UpdatedAt = e.Version+1, pt(ts(now))
		out = e
		return s.auditTx(ctx, tx, a, "project-environment.update", projectID, "project-environment", id, before, e.audit())
	})
	return out, err
}

// DeleteEnvironment removes an environment without a unit.
func (s *Service) DeleteEnvironment(ctx context.Context, a Actor, projectID, id string, ifVersion int64) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		e, err := scanEnv(tx.QueryRowContext(ctx, `SELECT `+envCols+` FROM project_environments WHERE id = ? AND project_id = ?`, id, projectID))
		if err != nil {
			return err
		}
		if e.Version != ifVersion {
			return ErrVersionMismatch
		}
		ids, err := column(ctx, tx, `SELECT id FROM compose_units WHERE environment_id = ?`, id)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			return &ReferencedError{What: "a Compose unit", IDs: ids}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM project_environments WHERE id = ?`, id); err != nil {
			return err
		}
		return s.auditTx(ctx, tx, a, "project-environment.delete", projectID, "project-environment", id, e.audit(), nil)
	})
}

// ---- units -------------------------------------------------------------------

const unitCols = `u.id, u.environment_id, e.project_id, u.name, u.compose_project, u.source, u.main_file, u.extra_files, u.working_dir,
	u.profiles, u.node_id, u.engine_env_id, u.version, u.created_by, u.created_at, u.updated_at`

const unitFrom = ` FROM compose_units u JOIN project_environments e ON e.id = u.environment_id `

func scanUnit(r scanner) (*Unit, error) {
	var u Unit
	var extra, profiles, c, up string
	var node, env sql.NullString
	if err := r.Scan(&u.ID, &u.EnvironmentID, &u.ProjectID, &u.Name, &u.ComposeProject, &u.Source, &u.MainFile, &extra, &u.WorkingDir,
		&profiles, &node, &env, &u.Version, &u.CreatedBy, &c, &up); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(extra), &u.ExtraFiles); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(profiles), &u.Profiles); err != nil {
		return nil, err
	}
	if node.Valid {
		u.Target = &Target{NodeID: node.String, EngineEnvironmentID: env.String}
	}
	u.CreatedAt, u.UpdatedAt = pt(c), pt(up)
	return &u, nil
}

type storedFile struct {
	File
	content  []byte
	secretID string
}

func (s *Service) loadFiles(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, unitID string) ([]storedFile, error) {
	rows, err := q.QueryContext(ctx, `SELECT path, content, secret_id, size, sha256 FROM compose_unit_files WHERE unit_id = ? ORDER BY path`, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedFile
	for rows.Next() {
		var f storedFile
		var sid sql.NullString
		if err := rows.Scan(&f.Path, &f.content, &sid, &f.Size, &f.SHA256); err != nil {
			return nil, err
		}
		f.Secret, f.secretID = sid.Valid, sid.String
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetUnit returns the unit of an environment. withContent includes the
// content of the files that are not secret.
func (s *Service) GetUnit(ctx context.Context, projectID, envID string, withContent bool) (*Unit, error) {
	u, err := scanUnit(s.db.QueryRowContext(ctx, `SELECT `+unitCols+unitFrom+`WHERE u.environment_id = ? AND e.project_id = ?`, envID, projectID))
	if err != nil {
		return nil, err
	}
	files, err := s.loadFiles(ctx, s.db, u.ID)
	if err != nil {
		return nil, err
	}
	u.Files = make([]File, 0, len(files))
	for _, f := range files {
		out := f.File
		if withContent && !f.Secret {
			c := string(f.content)
			out.Content = &c
		}
		u.Files = append(u.Files, out)
	}
	return u, nil
}

// spec of a stored unit for Analyze.
func (s *Service) sourceSpec(ctx context.Context, u *Unit) (SourceSpec, error) {
	files, err := s.loadFiles(ctx, s.db, u.ID)
	if err != nil {
		return SourceSpec{}, err
	}
	spec := SourceSpec{ComposeProject: u.ComposeProject, MainFile: u.MainFile, ExtraFiles: u.ExtraFiles, WorkingDir: u.WorkingDir, Profiles: u.Profiles}
	for _, f := range files {
		spec.Files = append(spec.Files, SourceFile{Path: f.Path, Content: f.content, Secret: f.Secret})
	}
	return spec, nil
}

// AnalyzeUnit checks the stored files of a unit.
func (s *Service) AnalyzeUnit(ctx context.Context, u *Unit) (Analysis, error) {
	spec, err := s.sourceSpec(ctx, u)
	if err != nil {
		return Analysis{}, err
	}
	return Analyze(ctx, spec), nil
}

// checkPath: '/'-separated, relative, no empty, "." or ".." segment, no
// backslash, no control or invisible characters, valid UTF-8.
func checkPath(field, p string) error {
	if p == "" || len(p) > maxPathBytes || !utf8.ValidString(p) {
		return invalid(field, "a relative path of 1 to %d bytes", maxPathBytes)
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return invalid(field, "use a relative path with '/' separators")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return invalid(field, "no empty, '.' or '..' path segments")
		}
	}
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return invalid(field, "control and invisible format characters are not allowed")
		}
	}
	return nil
}

// prepared is a validated UnitInput: the files to store and the secrets
// they need.
type prepared struct {
	in    UnitInput
	files []storedFile
	// newSecrets are file indexes whose content goes to the secret store.
	newSecrets []int
	// keep are file indexes whose secret is kept from the stored unit.
	keep []int
}

func prepare(in UnitInput, old map[string]storedFile) (*prepared, error) {
	if in.Source != SourceUpload && in.Source != SourceEditor {
		return nil, invalid("source", "one of upload, editor")
	}
	if len(in.Files) == 0 || len(in.Files) > MaxFiles {
		return nil, invalid("files", "1 to %d files", MaxFiles)
	}
	p := &prepared{in: in}
	seen := map[string]bool{}
	var total int64
	for i, f := range in.Files {
		field := fmt.Sprintf("files[%d]", i)
		if err := checkPath(field+".path", f.Path); err != nil {
			return nil, err
		}
		if seen[f.Path] {
			return nil, invalid(field+".path", "%s is listed twice", f.Path)
		}
		seen[f.Path] = true
		sf := storedFile{File: File{Path: f.Path, Secret: f.Secret}}
		switch {
		case f.Unchanged:
			o, ok := old[f.Path]
			if !f.Secret || !ok || !o.Secret || f.Content != nil {
				return nil, invalid(field+".unchanged", "only an already stored secret file can be kept unchanged, without content")
			}
			sf.Size, sf.SHA256, sf.secretID = o.Size, o.SHA256, o.secretID
			p.keep = append(p.keep, i)
		case f.Content == nil:
			return nil, invalid(field+".content", "content is required")
		default:
			b := []byte(*f.Content)
			if len(b) > MaxFileBytes {
				return nil, invalid(field+".content", "at most %d bytes", MaxFileBytes)
			}
			if !utf8.Valid(b) {
				return nil, invalid(field+".content", "text in UTF-8 only; build contexts with other files come from Git (ticket 049)")
			}
			sf.Size, sf.SHA256, sf.content = int64(len(b)), sum(b), b
			if f.Secret {
				// A digest of a short secret is guessable offline: secret
				// files carry no digest anywhere (API, audit, table).
				sf.SHA256 = ""
				p.newSecrets = append(p.newSecrets, i)
			}
		}
		total += sf.Size
		p.files = append(p.files, sf)
	}
	if total > MaxUnitBytes {
		return nil, invalid("files", "at most %d bytes in all", MaxUnitBytes)
	}
	compose := func(field, name string) error {
		if err := checkPath(field, name); err != nil {
			return err
		}
		for _, f := range p.files {
			if f.Path == name {
				if f.Secret {
					return invalid(field, "%s is a secret file; a Compose file cannot be secret", name)
				}
				return nil
			}
		}
		return invalid(field, "%s is not among the files", name)
	}
	if err := compose("mainFile", in.MainFile); err != nil {
		return nil, err
	}
	used := map[string]bool{in.MainFile: true}
	for i, x := range in.ExtraFiles {
		field := fmt.Sprintf("extraFiles[%d]", i)
		if err := compose(field, x); err != nil {
			return nil, err
		}
		if used[x] {
			return nil, invalid(field, "%s is already in the merge order", x)
		}
		used[x] = true
	}
	if in.WorkingDir != "" && in.WorkingDir != "." {
		if err := checkPath("workingDir", in.WorkingDir); err != nil {
			return nil, err
		}
	}
	if len(in.Profiles) > 50 {
		return nil, invalid("profiles", "at most 50")
	}
	for i, pr := range in.Profiles {
		if !profileRE.MatchString(pr) {
			return nil, invalid(fmt.Sprintf("profiles[%d]", i), "a Compose profile name: letters, digits, '.', '_', '-', starting with a letter or digit")
		}
	}
	if t := in.Target; t != nil {
		if t.NodeID == "" || len(t.NodeID) > 100 || t.EngineEnvironmentID == "" || len(t.EngineEnvironmentID) > 600 || !utf8.ValidString(t.EngineEnvironmentID) {
			return nil, invalid("target", "nodeId and engineEnvironmentId are both required")
		}
	}
	return p, nil
}

// profileRE is the Compose specification's profile pattern.
var profileRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

// CheckInput analyses a unit as it would be saved, without saving it.
// Secret files sent unchanged keep the stored unit's values.
func (s *Service) CheckInput(ctx context.Context, projectID, envID string, in UnitInput) (Analysis, error) {
	env, proj, err := s.envAndProject(ctx, projectID, envID)
	if err != nil {
		return Analysis{}, err
	}
	old := map[string]storedFile{}
	composeProject := composeName(proj.Name, env.Name, in.Name)
	if u, err := s.GetUnit(ctx, projectID, envID, false); err == nil {
		composeProject = u.ComposeProject
		files, err := s.loadFiles(ctx, s.db, u.ID)
		if err != nil {
			return Analysis{}, err
		}
		for _, f := range files {
			old[f.Path] = f
		}
	} else if !errors.Is(err, ErrNotFound) {
		return Analysis{}, err
	} else if err := checkName("name", in.Name); err != nil {
		return Analysis{}, err
	}
	p, err := prepare(in, old)
	if err != nil {
		return Analysis{}, err
	}
	return Analyze(ctx, p.spec(composeProject)), nil
}

func (p *prepared) spec(composeProject string) SourceSpec {
	sp := SourceSpec{ComposeProject: composeProject, MainFile: p.in.MainFile, ExtraFiles: p.in.ExtraFiles, WorkingDir: p.in.WorkingDir, Profiles: p.in.Profiles}
	for _, f := range p.files {
		sp.Files = append(sp.Files, SourceFile{Path: f.Path, Content: f.content, Secret: f.Secret})
	}
	return sp
}

func composeName(project, env, unit string) string { return project + "-" + env + "-" + unit }

func (s *Service) envAndProject(ctx context.Context, projectID, envID string) (*Environment, *Project, error) {
	env, err := s.GetEnvironment(ctx, projectID, envID)
	if err != nil {
		return nil, nil, err
	}
	proj, err := s.GetProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	return env, proj, nil
}

// storeSecrets moves secret contents into the secret store before the
// transaction (the store has one connection; nothing else may run inside
// a transaction).
func (s *Service) storeSecrets(ctx context.Context, a Actor, unitID string, p *prepared) ([]string, error) {
	var created []string
	for _, i := range p.newSecrets {
		f := &p.files[i]
		sec, err := s.vault.Create(ctx, secrets.CreateInput{Kind: secrets.KindGeneric, Name: "projects/" + unitID + "/" + f.Path + "#" + newID("v"),
			Value: f.content, Scope: []string{consumer(unitID)}, Actor: a.UserID})
		if err != nil {
			s.dropSecrets(ctx, a, created...)
			return nil, fmt.Errorf("projects: store secret file %s: %w", f.Path, err)
		}
		created = append(created, sec.ID)
		f.secretID, f.content = sec.ID, nil
	}
	return created, nil
}

func (s *Service) dropSecrets(ctx context.Context, a Actor, ids ...string) {
	for _, id := range ids {
		if err := s.vault.Delete(context.WithoutCancel(ctx), id, a.UserID); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			s.log.Error("projects: unreferenced secret could not be deleted", "secret", id, "error", err)
		}
	}
}

func insertFiles(ctx context.Context, tx *sql.Tx, unitID string, files []storedFile) error {
	for _, f := range files {
		var content, sid any
		if f.secretID != "" {
			sid = f.secretID
		} else {
			content = f.content
			if f.content == nil {
				content = []byte{}
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO compose_unit_files (unit_id, path, content, secret_id, size, sha256) VALUES (?,?,?,?,?,?)`,
			unitID, f.Path, content, sid, f.Size, f.SHA256); err != nil {
			return err
		}
	}
	return nil
}

func unitAudit(u *Unit, files []storedFile) map[string]any {
	fl := make([]map[string]any, 0, len(files))
	for _, f := range files {
		fl = append(fl, map[string]any{"path": f.Path, "secret": f.Secret, "size": f.Size, "sha256": f.SHA256})
	}
	m := map[string]any{"name": u.Name, "composeProject": u.ComposeProject, "source": u.Source, "mainFile": u.MainFile,
		"extraFiles": u.ExtraFiles, "workingDir": u.WorkingDir, "profiles": u.Profiles, "files": fl}
	if u.Target != nil {
		m["target"] = map[string]any{"nodeId": u.Target.NodeID, "engineEnvironmentId": u.Target.EngineEnvironmentID}
	}
	return m
}

func jsonList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// CreateUnit adds the Compose unit of an environment.
func (s *Service) CreateUnit(ctx context.Context, a Actor, projectID, envID string, in UnitInput) (*Unit, error) {
	if err := checkName("name", in.Name); err != nil {
		return nil, err
	}
	env, proj, err := s.envAndProject(ctx, projectID, envID)
	if err != nil {
		return nil, err
	}
	p, err := prepare(in, nil)
	if err != nil {
		return nil, err
	}
	now := s.now()
	u := &Unit{ID: newID("cu"), EnvironmentID: envID, ProjectID: projectID, Name: in.Name, ComposeProject: composeName(proj.Name, env.Name, in.Name),
		Source: in.Source, MainFile: in.MainFile, ExtraFiles: nonNil(in.ExtraFiles), WorkingDir: in.WorkingDir, Profiles: nonNil(in.Profiles),
		Target: in.Target, Version: 1, CreatedBy: a.UserID, CreatedAt: pt(ts(now)), UpdatedAt: pt(ts(now))}
	created, err := s.storeSecrets(ctx, a, u.ID, p)
	if err != nil {
		return nil, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		if _, err := scanEnv(tx.QueryRowContext(ctx, `SELECT `+envCols+` FROM project_environments WHERE id = ? AND project_id = ?`, envID, projectID)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO compose_units (id, environment_id, name, compose_project, source, main_file, extra_files, working_dir,
			profiles, node_id, engine_env_id, version, created_by, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,1,?,?,?)`,
			u.ID, envID, u.Name, u.ComposeProject, u.Source, u.MainFile, jsonList(u.ExtraFiles), u.WorkingDir, jsonList(u.Profiles),
			targetNode(u.Target), targetEnv(u.Target), u.CreatedBy, ts(now), ts(now)); err != nil {
			if isUnique(err) {
				return ErrExists
			}
			if strings.Contains(err.Error(), "FOREIGN KEY") {
				return invalid("target.nodeId", "no such node")
			}
			return err
		}
		if err := insertFiles(ctx, tx, u.ID, p.files); err != nil {
			return err
		}
		return s.auditTx(ctx, tx, a, "compose-unit.create", projectID, "compose-unit", u.ID, nil, unitAudit(u, p.files))
	})
	if err != nil {
		s.dropSecrets(ctx, a, created...)
		return nil, err
	}
	return s.GetUnit(ctx, projectID, envID, true)
}

func targetNode(t *Target) any {
	if t == nil {
		return nil
	}
	return t.NodeID
}

func targetEnv(t *Target) any {
	if t == nil {
		return nil
	}
	return t.EngineEnvironmentID
}

// ReplaceUnit replaces a unit's files and settings at version ifVersion.
// Its name and Compose project name do not change.
func (s *Service) ReplaceUnit(ctx context.Context, a Actor, projectID, envID string, ifVersion int64, in UnitInput) (*Unit, error) {
	cur, err := s.GetUnit(ctx, projectID, envID, false)
	if err != nil {
		return nil, err
	}
	if cur.Version != ifVersion {
		return nil, ErrVersionMismatch
	}
	if in.Name != "" && in.Name != cur.Name {
		return nil, invalid("name", "a unit keeps its name (ticket 108 adds renaming)")
	}
	oldFiles, err := s.loadFiles(ctx, s.db, cur.ID)
	if err != nil {
		return nil, err
	}
	old := map[string]storedFile{}
	for _, f := range oldFiles {
		old[f.Path] = f
	}
	p, err := prepare(in, old)
	if err != nil {
		return nil, err
	}
	created, err := s.storeSecrets(ctx, a, cur.ID, p)
	if err != nil {
		return nil, err
	}
	next := *cur
	next.Source, next.MainFile, next.ExtraFiles, next.WorkingDir, next.Profiles, next.Target =
		in.Source, in.MainFile, nonNil(in.ExtraFiles), in.WorkingDir, nonNil(in.Profiles), in.Target
	now := s.now()
	err = s.write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE compose_units SET source=?, main_file=?, extra_files=?, working_dir=?, profiles=?, node_id=?, engine_env_id=?,
			version=version+1, updated_at=? WHERE id=? AND version=?`,
			next.Source, next.MainFile, jsonList(next.ExtraFiles), next.WorkingDir, jsonList(next.Profiles), targetNode(next.Target), targetEnv(next.Target),
			ts(now), cur.ID, ifVersion)
		if err != nil {
			if strings.Contains(err.Error(), "FOREIGN KEY") {
				return invalid("target.nodeId", "no such node")
			}
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrVersionMismatch
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM compose_unit_files WHERE unit_id = ?`, cur.ID); err != nil {
			return err
		}
		if err := insertFiles(ctx, tx, cur.ID, p.files); err != nil {
			return err
		}
		return s.auditTx(ctx, tx, a, "compose-unit.update", projectID, "compose-unit", cur.ID, unitAudit(cur, oldFiles), unitAudit(&next, p.files))
	})
	if err != nil {
		s.dropSecrets(ctx, a, created...)
		return nil, err
	}
	kept := map[string]bool{}
	for _, f := range p.files {
		if f.secretID != "" {
			kept[f.secretID] = true
		}
	}
	var stale []string
	for _, f := range oldFiles {
		if f.secretID != "" && !kept[f.secretID] {
			stale = append(stale, f.secretID)
		}
	}
	s.dropSecrets(ctx, a, stale...)
	return s.GetUnit(ctx, projectID, envID, true)
}

// DeleteUnit removes an environment's unit and its secret files.
func (s *Service) DeleteUnit(ctx context.Context, a Actor, projectID, envID string, ifVersion int64) error {
	cur, err := s.GetUnit(ctx, projectID, envID, false)
	if err != nil {
		return err
	}
	if cur.Version != ifVersion {
		return ErrVersionMismatch
	}
	files, err := s.loadFiles(ctx, s.db, cur.ID)
	if err != nil {
		return err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM compose_units WHERE id = ? AND version = ?`, cur.ID, ifVersion)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrVersionMismatch
		}
		return s.auditTx(ctx, tx, a, "compose-unit.delete", projectID, "compose-unit", cur.ID, unitAudit(cur, files), nil)
	})
	if err != nil {
		return err
	}
	var ids []string
	for _, f := range files {
		if f.secretID != "" {
			ids = append(ids, f.secretID)
		}
	}
	s.dropSecrets(ctx, a, ids...)
	return nil
}

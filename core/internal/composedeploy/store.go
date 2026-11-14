package composedeploy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Deployment states.
const (
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

// Deployment is one deployment job of a unit.
type Deployment struct {
	ID                  string          `json:"id"`
	UnitID              string          `json:"unitId"`
	UnitVersion         int64           `json:"unitVersion"`
	ComposeProject      string          `json:"composeProject"`
	Revision            string          `json:"revision"`
	ManifestDigest      string          `json:"manifestDigest"`
	NodeID              string          `json:"nodeId"`
	EngineEnvironmentID string          `json:"engineEnvironmentId"`
	JobID               string          `json:"jobId"`
	PlanID              string          `json:"planId,omitempty"`
	State               string          `json:"state"`
	Result              json.RawMessage `json:"result"`
	ErrorCode           string          `json:"errorCode,omitempty"`
	ErrorMessage        string          `json:"errorMessage,omitempty"`
	CreatedBy           string          `json:"createdBy"`
	StartedAt           time.Time       `json:"startedAt"`
	FinishedAt          *time.Time      `json:"finishedAt,omitempty"`
}

// ErrNotFound: no such deployment.
var ErrNotFound = errors.New("composedeploy: not found")

// Store keeps deployments.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore builds a Store; now defaults to time.Now.
func NewStore(db *sql.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

func ts(t time.Time) string { return t.UTC().Truncate(time.Second).Format(time.RFC3339) }

const cols = `id, unit_id, unit_version, compose_project, revision, manifest_digest, node_id, engine_env_id, job_id, plan_id, state, result,
	error_code, error_message, created_by, started_at, finished_at`

func scan(r interface{ Scan(...any) error }) (*Deployment, error) {
	var d Deployment
	var result, started string
	var finished sql.NullString
	err := r.Scan(&d.ID, &d.UnitID, &d.UnitVersion, &d.ComposeProject, &d.Revision, &d.ManifestDigest, &d.NodeID, &d.EngineEnvironmentID,
		&d.JobID, &d.PlanID, &d.State, &result, &d.ErrorCode, &d.ErrorMessage, &d.CreatedBy, &started, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Result = json.RawMessage(result)
	d.StartedAt, _ = time.Parse(time.RFC3339, started)
	if finished.Valid {
		t, _ := time.Parse(time.RFC3339, finished.String)
		d.FinishedAt = &t
	}
	return &d, nil
}

// Begin records a deployment as running. A second call for the same job
// (the step retried) returns the existing row.
func (s *Store) Begin(ctx context.Context, d Deployment) (*Deployment, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO compose_deployments (id, unit_id, unit_version, compose_project, revision, manifest_digest, node_id,
		engine_env_id, job_id, plan_id, state, created_by, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'running', ?, ?)
		ON CONFLICT (job_id) DO NOTHING`,
		d.ID, d.UnitID, d.UnitVersion, d.ComposeProject, d.Revision, d.ManifestDigest, d.NodeID, d.EngineEnvironmentID, d.JobID, d.PlanID,
		d.CreatedBy, ts(s.now()))
	if err != nil {
		return nil, err
	}
	return s.ByJob(ctx, d.JobID)
}

// Finish ends the deployment of job. result must not contain secrets.
func (s *Store) Finish(ctx context.Context, jobID, state string, result json.RawMessage, code, message string) error {
	if len(result) == 0 {
		result = json.RawMessage("{}")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE compose_deployments SET state = ?, result = ?, error_code = ?, error_message = ?, finished_at = ?
		WHERE job_id = ?`, state, string(result), code, message, ts(s.now()), jobID)
	return err
}

// ByJob returns the deployment of a job.
func (s *Store) ByJob(ctx context.Context, jobID string) (*Deployment, error) {
	return scan(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM compose_deployments WHERE job_id = ?`, jobID))
}

// List returns a unit's deployments, newest first.
func (s *Store) List(ctx context.Context, unitID string) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM compose_deployments WHERE unit_id = ? ORDER BY started_at DESC, id DESC`, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Deployment{}
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// OnNode is a Compose project that has run on a node: its latest
// successful deployment there (NOD-008).
type OnNode struct {
	UnitID         string    `json:"unitId"`
	ComposeProject string    `json:"composeProject"`
	Revision       string    `json:"revision"`
	DeployedAt     time.Time `json:"deployedAt"`
}

// DeployedOn lists the units whose latest successful deployment ran on
// nodeID.
func (s *Store) DeployedOn(ctx context.Context, nodeID string) ([]OnNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.unit_id, d.compose_project, d.revision, d.started_at FROM compose_deployments d
		WHERE d.node_id = ? AND d.state = 'succeeded' AND d.started_at = (SELECT MAX(x.started_at) FROM compose_deployments x
		WHERE x.unit_id = d.unit_id AND x.state = 'succeeded') ORDER BY d.compose_project`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OnNode{}
	for rows.Next() {
		var o OnNode
		var at string
		if err := rows.Scan(&o.UnitID, &o.ComposeProject, &o.Revision, &at); err != nil {
			return nil, err
		}
		o.DeployedAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, o)
	}
	return out, rows.Err()
}

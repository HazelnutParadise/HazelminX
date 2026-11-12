package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/iam"
	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
	"github.com/HazelnutParadise/hazelminx/core/internal/paging"
)

// Auditor writes audit rows. RecordTx writes inside the caller's
// transaction, so a plan's state and its audit row commit together.
type Auditor interface {
	RecordTx(ctx context.Context, tx *sql.Tx, ev iam.AuditEvent) error
	RecordDenied(ctx context.Context, p *iam.Principal, a iam.Action, on iam.Scope, src iam.Source)
}

// Submitter creates jobs; *jobs.Engine implements it.
type Submitter interface {
	Submit(ctx context.Context, req jobs.SubmitRequest) (*jobs.Job, bool, error)
}

// Options configures a Service.
type Options struct {
	DB    *sql.DB
	Audit Auditor
	Jobs  Submitter
	Now   func() time.Time
}

// Service is the change-plan engine.
type Service struct{ o Options }

// New returns a Service.
func New(o Options) *Service { return &Service{o: o} }

var errTODO = errors.New("plans: not implemented")

// Register adds a plan kind.
func (s *Service) Register(k Kind) error { return nil }

// Kinds lists the registered plan kinds.
func (s *Service) Kinds() []string { return nil }

// RequiresPlan reports whether jobs of jobKind may only come from a plan.
func (s *Service) RequiresPlan(jobKind string) bool { return false }

// ListOptions narrows List and Count.
type ListOptions struct {
	States      []State
	Kind        string
	RequestedBy string
	Page        paging.Query
}

// ExecuteResult is what Execute returns.
type ExecuteResult struct {
	Plan     *Plan
	Job      *jobs.Job
	Replayed bool
}

func (s *Service) Create(ctx context.Context, p *iam.Principal, kind string, params json.RawMessage, src iam.Source) (*Plan, error) {
	return nil, errTODO
}
func (s *Service) Get(ctx context.Context, p *iam.Principal, id string) (*Plan, error) {
	return nil, errTODO
}
func (s *Service) List(ctx context.Context, p *iam.Principal, o ListOptions) ([]*Plan, error) {
	return nil, errTODO
}
func (s *Service) Count(ctx context.Context, p *iam.Principal, o ListOptions) (int, error) {
	return 0, errTODO
}
func (s *Service) Approve(ctx context.Context, p *iam.Principal, id, digest string, src iam.Source) (*Plan, error) {
	return nil, errTODO
}
func (s *Service) Reject(ctx context.Context, p *iam.Principal, id, reason string, src iam.Source) (*Plan, error) {
	return nil, errTODO
}
func (s *Service) Withdraw(ctx context.Context, p *iam.Principal, id, reason string, src iam.Source) (*Plan, error) {
	return nil, errTODO
}
func (s *Service) Execute(ctx context.Context, p *iam.Principal, id, digest, requestID string, src iam.Source) (*ExecuteResult, error) {
	return nil, errTODO
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

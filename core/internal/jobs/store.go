package jobs

import (
	"context"
	"time"
)

// Store persists jobs and their events. The SQLite implementation lives in
// sqlstore.go; tests may use it against a temp file.
type Store interface {
	// CreateJob inserts a new job. It must fail if RequestID already exists.
	CreateJob(ctx context.Context, j *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	GetJobByRequestID(ctx context.Context, requestID string) (*Job, error)
	ListJobs(ctx context.Context, opts ListOptions) ([]*Job, error)
	// UpdateJob writes every mutable field of j.
	UpdateJob(ctx context.Context, j *Job) error
	// Touch updates the heartbeat and progress of a job without rewriting
	// the whole record; it is the cheap path a flood of log lines must not
	// delay.
	Touch(ctx context.Context, id string, heartbeatAt time.Time, progress int) error
	// AppendEvent assigns the next per-job seq, stores the event and returns
	// the seq.
	AppendEvent(ctx context.Context, e *Event) (int64, error)
	// ListEvents returns events with seq > afterSeq, ascending, at most limit.
	ListEvents(ctx context.Context, jobID string, afterSeq int64, limit int) ([]Event, error)
}

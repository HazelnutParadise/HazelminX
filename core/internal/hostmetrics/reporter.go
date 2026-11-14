package hostmetrics

import (
	"context"
	"fmt"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Reporter stores the host reports of agent sessions (nodes.HostReports).
type Reporter struct {
	Store *Store
	// NodeID maps the fingerprint a session proved to its node id.
	NodeID func(ctx context.Context, fingerprint string) (string, error)
}

func (r Reporter) node(ctx context.Context, s *nodes.Session) (string, error) {
	id, err := r.NodeID(ctx, s.NodeID())
	if err != nil {
		return "", fmt.Errorf("hostmetrics: no node for this session: %w", err)
	}
	return id, nil
}

func (r Reporter) HostFacts(ctx context.Context, s *nodes.Session, m *protocol.HostFacts) error {
	id, err := r.node(ctx, s)
	if err != nil {
		return err
	}
	return r.Store.RecordFacts(ctx, id, m)
}

func (r Reporter) MetricsReport(ctx context.Context, s *nodes.Session, m *protocol.MetricsReport) (uint64, error) {
	id, err := r.node(ctx, s)
	if err != nil {
		return 0, err
	}
	return r.Store.RecordReport(ctx, id, m)
}

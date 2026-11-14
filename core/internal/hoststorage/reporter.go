package hoststorage

import (
	"context"
	"fmt"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Reporter stores the storage reports of agent sessions
// (nodes.StorageReports).
type Reporter struct {
	Store *Store
	// NodeID maps the fingerprint a session proved to its node id.
	NodeID func(ctx context.Context, fingerprint string) (string, error)
}

func (r Reporter) node(ctx context.Context, s *nodes.Session) (string, error) {
	id, err := r.NodeID(ctx, s.NodeID())
	if err != nil {
		return "", fmt.Errorf("hoststorage: no node for this session: %w", err)
	}
	return id, nil
}

func (r Reporter) HostStorage(ctx context.Context, s *nodes.Session, m *protocol.HostStorage) error {
	id, err := r.node(ctx, s)
	if err != nil {
		return err
	}
	return r.Store.RecordInventory(ctx, id, m)
}

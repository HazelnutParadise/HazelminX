package nodes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// ErrStorageUnsupported: the agent did not declare host.storage v1;
// nothing was sent.
var ErrStorageUnsupported = errors.New("nodes: agent does not support host.storage v1")

// StorageUsageUnavailableError is the agent's storage_usage_unavailable
// answer.
type StorageUsageUnavailableError struct {
	Code    string
	Message string
}

func (e *StorageUsageUnavailableError) Error() string {
	return fmt.Sprintf("agent could not measure storage usage: %s: %s", e.Code, e.Message)
}

// RequestStorageUsage asks node's agent for a usage measurement and waits
// for it. grant must carry host.storage.read; the agent passes it to the
// privileged executor unchanged.
//
// Expected results: no session is ErrNodeOffline and an agent without the
// capability is ErrStorageUnsupported (nothing sent in either case); the
// agent's storage_usage_unavailable is a *StorageUsageUnavailableError; a
// session or ctx that ends first is ErrSessionClosed or ctx.Err();
// otherwise the measurement as the agent sent it.
func (h *Hub) RequestStorageUsage(ctx context.Context, nodeID string, grant protocol.ExecutorGrant) (*protocol.StorageUsage, error) {
	s, ok := h.Session(nodeID)
	if !ok {
		return nil, ErrNodeOffline
	}
	if !s.Supports(protocol.CapHostStorage, 1) {
		return nil, ErrStorageUnsupported
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	reqID := "sur_" + hex.EncodeToString(b)
	p := &pending{result: make(chan any, 1)}
	msg := protocol.StorageUsageRequest{Type: protocol.TypeStorageUsageRequest, RequestID: reqID, Grant: grant}
	if err := s.request(reqID, p, msg); err != nil {
		return nil, err
	}
	defer s.forget(reqID)
	select {
	case v := <-p.result:
		switch m := v.(type) {
		case *protocol.StorageUsage:
			return m, nil
		case *protocol.StorageUsageUnavailable:
			return nil, &StorageUsageUnavailableError{Code: m.Code, Message: m.Message}
		}
		return nil, fmt.Errorf("nodes: unexpected answer %T to storage_usage_request", v)
	case <-s.Done():
		return nil, ErrSessionClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

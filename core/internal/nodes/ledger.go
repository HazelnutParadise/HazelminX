package nodes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Errors of PullLedger. A *LedgerUnavailableError carries the agent's code.
var (
	// ErrNodeOffline: the node has no live session; nothing was sent.
	ErrNodeOffline = errors.New("nodes: node is not connected")
	// ErrLedgerUnsupported: the agent did not declare host.ledger v1;
	// nothing was sent.
	ErrLedgerUnsupported = errors.New("nodes: agent does not support host.ledger v1")
)

// LedgerUnavailableError is the agent's ledger_unavailable answer.
type LedgerUnavailableError struct {
	Code    string
	Message string
}

func (e *LedgerUnavailableError) Error() string {
	return fmt.Sprintf("agent could not export the host ledger: %s: %s", e.Code, e.Message)
}

// PullLedger asks node's agent for the host ledger lines after afterSeq and
// waits for the page. grant must carry host.ledger.read; the agent passes it
// to the privileged executor unchanged.
//
// Expected results: no session is ErrNodeOffline and an agent without the
// capability is ErrLedgerUnsupported (nothing sent in either case); the
// agent's ledger_unavailable is a *LedgerUnavailableError; a session that
// ends or a ctx that ends first is ErrSessionClosed or ctx.Err(); otherwise
// the page as the agent sent it (the caller verifies it).
func (h *Hub) PullLedger(ctx context.Context, nodeID string, afterSeq uint64, limit int, grant protocol.ExecutorGrant) (*protocol.LedgerPage, error) {
	s, ok := h.Session(nodeID)
	if !ok {
		return nil, ErrNodeOffline
	}
	if !s.Supports(protocol.CapHostLedger, 1) {
		return nil, ErrLedgerUnsupported
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	reqID := "lgp_" + hex.EncodeToString(b)
	p := &pending{result: make(chan any, 1)}
	msg := protocol.LedgerPull{Type: protocol.TypeLedgerPull, RequestID: reqID, AfterSeq: afterSeq, Limit: limit, Grant: grant}
	if err := s.request(reqID, p, msg); err != nil {
		return nil, err
	}
	defer s.forget(reqID)
	select {
	case v := <-p.result:
		switch m := v.(type) {
		case *protocol.LedgerPage:
			return m, nil
		case *protocol.LedgerUnavailable:
			return nil, &LedgerUnavailableError{Code: m.Code, Message: m.Message}
		}
		return nil, fmt.Errorf("nodes: unexpected answer %T to ledger_pull", v)
	case <-s.Done():
		return nil, ErrSessionClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

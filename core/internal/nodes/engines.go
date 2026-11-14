package nodes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// ErrEnginesUnsupported: the agent did not declare host.engines v1;
// nothing was sent.
var ErrEnginesUnsupported = errors.New("nodes: agent does not support host.engines v1")

// EnginesUnavailableError is the agent's engines_unavailable answer.
type EnginesUnavailableError struct {
	Code    string
	Message string
}

func (e *EnginesUnavailableError) Error() string {
	return fmt.Sprintf("agent could not inspect container engines: %s: %s", e.Code, e.Message)
}

// ProbeEngines asks node's agent which container engines its host has and
// waits for the report. grant must carry host.engines.read; the agent
// passes it to the privileged executor unchanged.
//
// Expected results: no session is ErrNodeOffline and an agent without the
// capability is ErrEnginesUnsupported (nothing sent in either case); the
// agent's engines_unavailable is an *EnginesUnavailableError; a session
// that ends or a ctx that ends first is ErrSessionClosed or ctx.Err();
// otherwise the report as the agent sent it.
func (h *Hub) ProbeEngines(ctx context.Context, nodeID string, grant protocol.ExecutorGrant) (*protocol.EnginesReport, error) {
	s, ok := h.Session(nodeID)
	if !ok {
		return nil, ErrNodeOffline
	}
	if !s.Supports(protocol.CapHostEngines, 1) {
		return nil, ErrEnginesUnsupported
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	reqID := "enp_" + hex.EncodeToString(b)
	p := &pending{result: make(chan any, 1)}
	msg := protocol.EnginesProbe{Type: protocol.TypeEnginesProbe, RequestID: reqID, Grant: grant}
	if err := s.request(reqID, p, msg); err != nil {
		return nil, err
	}
	defer s.forget(reqID)
	select {
	case v := <-p.result:
		switch m := v.(type) {
		case *protocol.EnginesReport:
			return m, nil
		case *protocol.EnginesUnavailable:
			return nil, &EnginesUnavailableError{Code: m.Code, Message: m.Message}
		}
		return nil, fmt.Errorf("nodes: unexpected answer %T to engines_probe", v)
	case <-s.Done():
		return nil, ErrSessionClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

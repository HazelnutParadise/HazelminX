package nodes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// ErrSessionClosed: the session ended before the request was answered.
var ErrSessionClosed = errors.New("nodes: agent session closed")

// Session is one authenticated agent connection.
type Session struct {
	hub             *Hub
	conn            *websocket.Conn
	id              NodeIdentity
	sessionID       string
	protocolVersion int
	agentVersion    string
	agentRange      protocol.Range
	caps            []protocol.Capability
	transport       string
	remote          string
	connectedAt     time.Time
	lastSeen        atomic.Int64 // unix nanos

	// keys authenticate every frame after ready; sendN and recvN count
	// frames per direction (sendN is only touched by writeLoop).
	keys         protocol.SessionKeys
	sendN, recvN uint64

	out       chan []byte
	done      chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	pending map[string]*pending // by requestId
}

// pending is a request waiting for the agent's answer. Exactly one of the
// terminal fields is delivered through result.
type pending struct {
	jobID string
	step  int
	// event and progress are set for step_execute waiters only.
	progress func(*protocol.StepProgress)
	event    func(*protocol.Event) // non-terminal events (started)
	result   chan any              // *protocol.Event (completed/failed), *protocol.StepRejected, *protocol.ReconcileReport, *protocol.LedgerPage, *protocol.LedgerUnavailable
}

// NodeID is the node fingerprint.
func (s *Session) NodeID() string { return s.id.Fingerprint }

// ID is the session id.
func (s *Session) ID() string { return s.sessionID }

// Supports reports whether the agent declared capability name at version
// or higher.
func (s *Session) Supports(name string, version int) bool {
	return protocol.Supports(s.caps, name, version)
}

// Capabilities returns the agent's declared capabilities.
func (s *Session) Capabilities() []protocol.Capability {
	return append([]protocol.Capability(nil), s.caps...)
}

// Done is closed when the session ends.
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) touch() { s.lastSeen.Store(s.hub.opts.Now().UnixNano()) }

func (s *Session) info() ConnectionInfo {
	return ConnectionInfo{NodeID: s.id.Fingerprint, SessionID: s.sessionID, AgentVersion: s.agentVersion, ProtocolVersion: s.protocolVersion,
		AgentRange: s.agentRange, Capabilities: s.Capabilities(), Transport: s.transport, RemoteAddr: s.remote,
		ConnectedAt: s.connectedAt, LastSeenAt: time.Unix(0, s.lastSeen.Load()).UTC()}
}

// send queues a message. Core→agent traffic is small (requests, acks,
// heartbeats); a full queue means the agent stopped reading, so the
// session is closed rather than blocking the caller.
func (s *Session) send(msg any) error {
	b, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	select {
	case <-s.done:
		return ErrSessionClosed
	default:
	}
	select {
	case s.out <- b:
		return nil
	case <-s.done:
		return ErrSessionClosed
	default:
		s.close(websocket.StatusPolicyViolation, "agent is not reading its queue")
		return ErrSessionClosed
	}
}

// request registers p under requestID and sends msg.
func (s *Session) request(requestID string, p *pending, msg any) error {
	s.mu.Lock()
	s.pending[requestID] = p
	s.mu.Unlock()
	if err := s.send(msg); err != nil {
		s.forget(requestID)
		return err
	}
	return nil
}

func (s *Session) forget(requestID string) {
	s.mu.Lock()
	delete(s.pending, requestID)
	s.mu.Unlock()
}

// executeWaiter finds the step_execute waiting on (jobID, step).
func (s *Session) executeWaiter(jobID string, step int) (*pending, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.pending {
		if p.event != nil && p.jobID == jobID && p.step == step {
			return p, id
		}
	}
	return nil, ""
}

func (s *Session) lookup(requestID string) *pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending[requestID]
}

// outstandingJobs lists jobs with a step_execute still unanswered.
func (s *Session) outstandingJobs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, p := range s.pending {
		if p.event != nil && !seen[p.jobID] {
			seen[p.jobID] = true
			out = append(out, p.jobID)
		}
	}
	return out
}

func (s *Session) close(code websocket.StatusCode, reason string) {
	s.closeOnce.Do(func() {
		s.hub.unregister(s)
		close(s.done)
		_ = s.conn.Close(code, reason)
	})
}

// run drives the session until the connection ends.
func (s *Session) run(ctx context.Context) {
	defer s.close(websocket.StatusNormalClosure, "")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.writeLoop(ctx)
	idle := 3 * s.hub.opts.Heartbeat
	for {
		rctx, rcancel := context.WithTimeout(ctx, idle)
		typ, data, err := s.conn.Read(rctx)
		rcancel()
		if err != nil {
			reason := err.Error()
			if errors.Is(err, context.DeadlineExceeded) {
				reason = "no message from agent for three heartbeat intervals"
			}
			s.hub.opts.Logger.Info("agent disconnected", "node", s.id.Fingerprint, "session", s.sessionID, "reason", reason)
			return
		}
		if typ != websocket.MessageText {
			s.securityClose("binary frame in an authenticated session")
			return
		}
		// Every frame must carry the next sequence number and a valid MAC
		// under this session's key; anything else (an injected, replayed or
		// reordered frame, for example from a TLS-terminating proxy) ends
		// the session before its content is looked at.
		body, err := protocol.OpenFrame(s.keys.Recv, s.recvN+1, data)
		if err != nil {
			s.securityClose(err.Error())
			return
		}
		s.recvN++
		s.touch()
		msg, err := protocol.Decode(body)
		if err != nil {
			// Never act on a message we cannot read exactly (NOD-011).
			s.protocolError(protocol.ErrorCode(err), err.Error())
			continue
		}
		s.handle(msg)
	}
}

func (s *Session) securityClose(reason string) {
	s.hub.opts.Logger.Warn("agent session closed: frame failed authentication", "node", s.id.Fingerprint, "session", s.sessionID, "reason", reason)
	s.close(websocket.StatusPolicyViolation, "frame rejected")
}

func (s *Session) protocolError(code, message string) {
	s.hub.opts.Logger.Warn("agent sent an unreadable message", "node", s.id.Fingerprint, "code", code, "detail", message)
	_ = s.send(protocol.ProtocolError{Type: protocol.TypeProtocolError, Code: code, Message: message})
}

func (s *Session) handle(msg any) {
	switch m := msg.(type) {
	case *protocol.Heartbeat:
	case *protocol.Event:
		// Events are routed by job and step, not by request id: an agent
		// answers a repeated step_execute by re-sending the recorded event of
		// the original request. Replayed events nobody waits for are only
		// acknowledged.
		if p, reqID := s.executeWaiter(m.JobID, m.Step); p != nil {
			if m.Kind == protocol.EventStarted {
				p.event(m)
			} else {
				s.deliver(reqID, p, m)
			}
		}
		_ = s.send(protocol.Ack{Type: protocol.TypeAck, Seq: m.Seq})
	case *protocol.StepProgress:
		if p, _ := s.executeWaiter(m.JobID, m.Step); p != nil {
			p.progress(m)
		}
	case *protocol.StepRejected:
		if p := s.lookup(m.RequestID); p != nil {
			s.deliver(m.RequestID, p, m)
		}
	case *protocol.ReconcileReport:
		if p := s.lookup(m.RequestID); p != nil {
			s.deliver(m.RequestID, p, m)
		}
	case *protocol.LedgerPage:
		if p := s.lookup(m.RequestID); p != nil {
			s.deliver(m.RequestID, p, m)
		}
	case *protocol.LedgerUnavailable:
		if p := s.lookup(m.RequestID); p != nil {
			s.deliver(m.RequestID, p, m)
		}
	case *protocol.ProtocolError:
		s.hub.opts.Logger.Warn("agent could not read a core message", "node", s.id.Fingerprint, "code", m.Code, "detail", m.Message)
	default:
		// step_execute, step_reconcile, ack and ledger_pull only flow core→agent.
		s.protocolError(protocol.ErrorUnknownMessage, "message is not valid in the agent→core direction")
	}
}

func (s *Session) deliver(requestID string, p *pending, v any) {
	s.forget(requestID)
	select {
	case p.result <- v:
	default:
	}
}

func (s *Session) writeLoop(ctx context.Context) {
	tick := time.NewTicker(s.hub.opts.Heartbeat)
	defer tick.Stop()
	write := func(body []byte) bool {
		s.sendN++
		frame, err := protocol.SealFrame(s.keys.Send, s.sendN, body)
		if err != nil {
			s.close(websocket.StatusInternalError, "seal failed")
			return false
		}
		wctx, cancel := context.WithTimeout(ctx, s.hub.opts.Heartbeat)
		defer cancel()
		if err := s.conn.Write(wctx, websocket.MessageText, frame); err != nil {
			s.close(websocket.StatusGoingAway, "write failed")
			return false
		}
		return true
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case b := <-s.out:
			if !write(b) {
				return
			}
		case <-tick.C:
			b, _ := protocol.Encode(protocol.Heartbeat{Type: protocol.TypeHeartbeat, At: s.hub.opts.Now().UTC().Truncate(time.Second)})
			if !write(b) {
				return
			}
		}
	}
}

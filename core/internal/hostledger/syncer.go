package hostledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/nodes"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

// Grant the Core issues for each page: read-only, short-lived.
const (
	GrantSubject    = "system:ledger-sync"
	GrantPermission = "host.ledger.read"
	GrantLifetime   = 5 * time.Minute
)

// Codes RecordError stores, besides the agent's ledger_unavailable codes.
const (
	CodeAgentUnsupported = "AGENT_UNSUPPORTED"
	CodeChainBroken      = "CHAIN_BROKEN"
	CodeLedgerReplaced   = "LEDGER_REPLACED"
	CodeNoAnswer         = "NO_ANSWER"
	CodeStoreFailed      = "STORE_FAILED"
)

// Puller fetches one page from a node's agent (nodes.Hub implements it).
type Puller interface {
	PullLedger(ctx context.Context, nodeID string, afterSeq uint64, limit int, grant protocol.ExecutorGrant) (*protocol.LedgerPage, error)
}

// GrantIssuer signs executor grants (grants.Issuer implements it).
type GrantIssuer interface {
	Issue(subject string, perms []string, lifetime time.Duration) (protocol.ExecutorGrant, error)
}

// Syncer copies host ledgers into the Store.
type Syncer struct {
	Store  *Store
	Grants GrantIssuer
	Pull   Puller
	// PageLimit is the most lines asked for per page (the executor also
	// stops before a page could exceed the channel's message limit).
	// Default 200.
	PageLimit int
	// PageTimeout bounds one round trip. Default 30 s.
	PageTimeout time.Duration
	// MaxPages bounds one SyncNode call; the next call continues. Default 500.
	MaxPages int
	Logger   *slog.Logger

	once sync.Once // applies defaults; SyncNode runs from several goroutines
	mu   sync.Mutex
	busy map[string]*sync.Mutex
}

func (s *Syncer) defaults() {
	if s.PageLimit <= 0 {
		s.PageLimit = 200
	}
	if s.PageTimeout <= 0 {
		s.PageTimeout = 30 * time.Second
	}
	if s.MaxPages <= 0 {
		s.MaxPages = 500
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
}

func (s *Syncer) lock(nodeID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy == nil {
		s.busy = map[string]*sync.Mutex{}
	}
	m, ok := s.busy[nodeID]
	if !ok {
		m = &sync.Mutex{}
		s.busy[nodeID] = m
	}
	return m
}

// SyncNode brings Core's copy of nodeID up to the host's head. One sync per
// node runs at a time; a second caller waits for the first.
//
// Expected results:
//   - node offline: nodes.ErrNodeOffline, nothing recorded (it is normal);
//   - agent without host.ledger: error recorded as AGENT_UNSUPPORTED;
//   - agent's ledger_unavailable: its code and message recorded;
//   - page that does not continue the copy: CHAIN_BROKEN recorded, nothing
//     stored; host ledger id changed: LEDGER_REPLACED recorded, nothing
//     stored;
//   - otherwise every page is stored until the host reports no more, the
//     error columns are cleared, and the new head is returned.
func (s *Syncer) SyncNode(ctx context.Context, nodeID string) (protocol.LedgerHead, error) {
	s.once.Do(s.defaults)
	m := s.lock(nodeID)
	m.Lock()
	defer m.Unlock()

	cur, _, err := s.Store.Get(ctx, nodeID)
	if err != nil {
		return protocol.LedgerHead{}, err
	}
	head := cur.Head
	for page := 0; page < s.MaxPages; page++ {
		grant, err := s.Grants.Issue(GrantSubject, []string{GrantPermission}, GrantLifetime)
		if err != nil {
			return head, fmt.Errorf("hostledger: issue grant: %w", err)
		}
		pctx, cancel := context.WithTimeout(ctx, s.PageTimeout)
		p, err := s.Pull.PullLedger(pctx, nodeID, head.Seq, s.PageLimit, grant)
		timedOut := errors.Is(pctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
		cancel()
		if err != nil {
			return head, s.fail(ctx, nodeID, err, timedOut)
		}
		newHead, err := s.Store.Append(ctx, nodeID, p)
		if err != nil {
			return head, s.fail(ctx, nodeID, err, false)
		}
		head = newHead
		if !p.More {
			return head, nil
		}
	}
	return head, nil
}

func (s *Syncer) fail(ctx context.Context, nodeID string, err error, timedOut bool) error {
	var code string
	var unavailable *nodes.LedgerUnavailableError
	switch {
	case errors.Is(err, nodes.ErrNodeOffline), errors.Is(err, nodes.ErrSessionClosed), ctx.Err() != nil:
		return err // not the copy's fault; the next session retries
	case errors.Is(err, nodes.ErrLedgerUnsupported):
		code = CodeAgentUnsupported
	case errors.As(err, &unavailable):
		code = unavailable.Code
	case errors.Is(err, ErrChain):
		code = CodeChainBroken
	case errors.Is(err, ErrLedgerReplaced):
		code = CodeLedgerReplaced
	case timedOut:
		code = CodeNoAnswer
	default:
		code = CodeStoreFailed
	}
	s.Logger.Warn("host ledger sync failed", "node", nodeID, "code", code, "error", err.Error())
	if rerr := s.Store.RecordError(ctx, nodeID, code, err.Error()); rerr != nil {
		s.Logger.Error("cannot record host ledger sync failure", "node", nodeID, "error", rerr.Error())
	}
	return err
}

// Run syncs every node nodeIDs returns, every interval, until ctx ends.
// Sessions starting call SyncNode themselves (see OnReady wiring), so this
// loop only catches up on changes made during a session.
func (s *Syncer) Run(ctx context.Context, nodeIDs func() []string, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, id := range nodeIDs() {
				if _, err := s.SyncNode(ctx, id); err != nil && ctx.Err() == nil &&
					!errors.Is(err, nodes.ErrNodeOffline) && !errors.Is(err, nodes.ErrSessionClosed) {
					s.Logger.Debug("periodic host ledger sync did not finish", "node", id, "error", err.Error())
				}
			}
		}
	}
}

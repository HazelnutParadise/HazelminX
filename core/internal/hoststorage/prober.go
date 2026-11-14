package hoststorage

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

// The grant the agent passes to the privileged executor's storage_usage.
const (
	GrantSubject    = "system:storage-usage"
	GrantPermission = "host.storage.read"
	// GrantLifetime covers the request reaching the executor; the walk may
	// run longer, the grant is checked when the action starts.
	GrantLifetime = 5 * time.Minute
	// UsageInterval is how old the latest measurement may get before a
	// connected node is asked again.
	UsageInterval = 6 * time.Hour
	// UsageTimeout bounds one request: the executor's walk stops after 20
	// minutes.
	UsageTimeout = 25 * time.Minute
)

// Failure codes Core records itself; the agent's own codes
// (EXECUTOR_UNREACHABLE, EXECUTOR_REFUSED, MEASURE_FAILED) are recorded as
// sent.
const (
	CodeAgentUnsupported = "AGENT_UNSUPPORTED"
	CodeGrantFailed      = "GRANT_FAILED"
	CodeNoAnswer         = "NO_ANSWER"
	CodeInvalidUsage     = "INVALID_USAGE"
)

// UsageHub asks an agent for a measurement (nodes.Hub implements it).
type UsageHub interface {
	RequestStorageUsage(ctx context.Context, fingerprint string, grant protocol.ExecutorGrant) (*protocol.StorageUsage, error)
}

// GrantIssuer signs executor grants (grants.Issuer implements it).
type GrantIssuer interface {
	Issue(subject string, perms []string, lifetime time.Duration) (protocol.ExecutorGrant, error)
}

// Prober asks agents for usage measurements and stores them.
type Prober struct {
	Store  *Store
	Grants GrantIssuer
	Hub    UsageHub
	// NodeID maps a session fingerprint to its node id.
	NodeID func(ctx context.Context, fingerprint string) (string, error)
	// Timeout bounds one request; default UsageTimeout.
	Timeout time.Duration
	// Interval is how old a measurement may be before Due; default
	// UsageInterval.
	Interval time.Duration
	Logger   *slog.Logger
	Now      func() time.Time

	once sync.Once
	mu   sync.Mutex
	busy map[string]bool
}

func (p *Prober) defaults() {
	if p.Timeout <= 0 {
		p.Timeout = UsageTimeout
	}
	if p.Interval <= 0 {
		p.Interval = UsageInterval
	}
	if p.Logger == nil {
		p.Logger = slog.Default()
	}
	if p.Now == nil {
		p.Now = time.Now
	}
}

// ErrBusy: a request to this node is already running.
var ErrBusy = errors.New("hoststorage: a usage request to this node is already running")

// Probe asks the node behind fingerprint for a measurement. One request per
// node runs at a time; a second caller gets ErrBusy.
//
// Expected results:
//   - node offline or session ended: that error, nothing recorded;
//   - agent without host.storage: AGENT_UNSUPPORTED recorded;
//   - the agent's storage_usage_unavailable: its code and message recorded;
//   - no answer within Timeout: NO_ANSWER recorded;
//   - a measurement RecordUsage refuses: INVALID_USAGE recorded;
//   - otherwise the measurement is stored and the failure cleared.
func (p *Prober) Probe(ctx context.Context, fingerprint string) error {
	p.once.Do(p.defaults)
	p.mu.Lock()
	if p.busy == nil {
		p.busy = map[string]bool{}
	}
	if p.busy[fingerprint] {
		p.mu.Unlock()
		return ErrBusy
	}
	p.busy[fingerprint] = true
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.busy, fingerprint); p.mu.Unlock() }()

	nodeID, err := p.NodeID(ctx, fingerprint)
	if err != nil {
		return fmt.Errorf("hoststorage: no node for %s: %w", fingerprint, err)
	}
	grant, err := p.Grants.Issue(GrantSubject, []string{GrantPermission}, GrantLifetime)
	if err != nil {
		return p.fail(ctx, nodeID, CodeGrantFailed, err)
	}
	rctx, cancel := context.WithTimeout(ctx, p.Timeout)
	u, err := p.Hub.RequestStorageUsage(rctx, fingerprint, grant)
	cancel()
	var unavailable *nodes.StorageUsageUnavailableError
	switch {
	case err == nil:
	case errors.Is(err, nodes.ErrNodeOffline), errors.Is(err, nodes.ErrSessionClosed), ctx.Err() != nil:
		return err
	case errors.Is(err, nodes.ErrStorageUnsupported):
		return p.fail(ctx, nodeID, CodeAgentUnsupported, err)
	case errors.As(err, &unavailable):
		return p.fail(ctx, nodeID, unavailable.Code, errors.New(unavailable.Message))
	default:
		return p.fail(ctx, nodeID, CodeNoAnswer, err)
	}
	if err := p.Store.RecordUsage(ctx, nodeID, u); err != nil {
		if errors.Is(err, ErrValidation) {
			return p.fail(ctx, nodeID, CodeInvalidUsage, err)
		}
		return err
	}
	return nil
}

func (p *Prober) fail(ctx context.Context, nodeID, code string, err error) error {
	p.Logger.Warn("storage usage request failed", "node", nodeID, "code", code, "error", err.Error())
	if rerr := p.Store.RecordUsageError(ctx, nodeID, code, err.Error()); rerr != nil {
		p.Logger.Error("cannot record storage usage failure", "node", nodeID, "error", rerr.Error())
	}
	return fmt.Errorf("hoststorage: %s: %w", code, err)
}

// Due reports whether the node needs a new measurement: none yet, or the
// latest started more than Interval ago, or the latest request failed
// more than Interval ago. A failure does not retry sooner, so a host
// without the executor is not asked every few minutes.
func (p *Prober) Due(ctx context.Context, nodeID string) (bool, error) {
	p.once.Do(p.defaults)
	u, err := p.Store.Usage(ctx, nodeID)
	if err != nil || u == nil {
		return u == nil, err
	}
	last := time.Time{}
	if u.Measured {
		last = u.Usage.StartedAt
	}
	if u.Error != nil && u.Error.At.After(last) {
		last = u.Error.At
	}
	return p.Now().Sub(last) >= p.Interval, nil
}

// Run checks every connected node every tick and asks those that are Due,
// each on its own goroutine, until ctx ends.
func (p *Prober) Run(ctx context.Context, fingerprints func() []string, tick time.Duration) {
	p.once.Do(p.defaults)
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, fp := range fingerprints() {
				id, err := p.NodeID(ctx, fp)
				if err != nil {
					continue
				}
				if due, err := p.Due(ctx, id); err != nil || !due {
					continue
				}
				go func() { _ = p.Probe(ctx, fp) }()
			}
		}
	}
}

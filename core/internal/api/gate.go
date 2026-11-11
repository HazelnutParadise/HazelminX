package api

import (
	"context"
	"slices"
	"time"

	"github.com/HazelnutParadise/hazelminx/core/internal/jobs"
)

// PlatformDegradation is a platform-level degraded mode (addendum 03 機制四):
// "database-read-only", "data-disk-low", … AllowKinds are the job kinds
// still accepted in this mode (cleanup and move kinds when the disk is low).
// PlatformDBReadOnly is the degraded mode in which the platform database
// takes no writes.
const PlatformDBReadOnly = "database-read-only"

type PlatformDegradation struct {
	Mode       string
	AllowKinds []string
}

// SetPlatformState installs the platform degradation probe. Ticket 017
// connects it to the store write probe and the disk monitor.
func (s *Server) SetPlatformState(f func() *PlatformDegradation) { s.platform = f }

// writeBlockers collects every refusal for a write of kind that touches res
// on a resource in state st (51.1): platform degradation, the resource's own
// write gate and pending ROB-010 confirmations. The caller picks the one to
// send with pickBlocking, or lists them all in a dryRun preview.
func (s *Server) writeBlockers(ctx context.Context, kind string, st ResourceState, res []jobs.ResourceRef) ([]apiError, error) {
	var out []apiError
	if s.platform != nil {
		if d := s.platform(); d != nil && !slices.Contains(d.AllowKinds, kind) {
			e := newError(CodePlatformDegraded, "the platform is degraded ("+d.Mode+"); this operation is not available until it recovers")
			e.Details = map[string]any{"mode": d.Mode}
			out = append(out, e)
		}
	}
	if s.confirm != nil && len(res) > 0 && st.PendingConfirmation == "" {
		p, err := s.confirm.PendingFor(ctx, res)
		if err != nil {
			return nil, err
		}
		if p != nil {
			e := newError(CodeConfirmRequired, "a change on the same resource is waiting for its operator's confirmation; try again after it is confirmed or reverted")
			// The confirmation id stays out: only its operator may see it.
			e.Details = map[string]any{"expiresAt": p.ExpiresAt.UTC().Format(time.RFC3339)}
			e.RetryAfter = max(1, int(time.Until(p.ExpiresAt).Seconds())+1)
			out = append(out, e)
		}
	}
	out = append(out, WriteBlockers(st)...)
	return out, nil
}

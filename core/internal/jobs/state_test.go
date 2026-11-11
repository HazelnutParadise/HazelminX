package jobs

import "testing"

// TestStateTableMatchesSpec pins the "允許的下一步" column of spec 33.1.
func TestStateTableMatchesSpec(t *testing.T) {
	allowed := []struct{ from, to State }{
		{StateCreated, StatePrechecking}, {StateCreated, StateCancelled},
		{StatePrechecking, StateAwaitingApproval}, {StatePrechecking, StateQueued}, {StatePrechecking, StateFailed}, {StatePrechecking, StateCancelled},
		{StateAwaitingApproval, StateQueued}, {StateAwaitingApproval, StateCreated},
		{StateQueued, StateRunning}, {StateQueued, StateCancelled}, {StateQueued, StateFailed},
		{StateRunning, StateVerifying}, {StateRunning, StateFailed}, {StateRunning, StateCancelRequested}, {StateRunning, StateUnknown},
		{StateVerifying, StateSucceeded}, {StateVerifying, StateFailed}, {StateVerifying, StateRollingBack},
		{StateFailed, StateRollingBack}, {StateFailed, StateNeedsAttention},
		{StateRollingBack, StateRolledBack}, {StateRollingBack, StateNeedsAttention}, {StateRollingBack, StateUnknown},
		{StateCancelRequested, StateCancelled},
		{StateUnknown, StateRunning}, {StateUnknown, StateNeedsAttention},
	}
	for _, a := range allowed {
		if !CanTransition(a.from, a.to) {
			t.Errorf("%s → %s must be allowed", a.from, a.to)
		}
	}
	forbidden := []struct{ from, to State }{
		{StateSucceeded, StateRunning},    // history is never modified
		{StateRolledBack, StateSucceeded}, // rolled back must not look like success
		{StateCancelled, StateRunning},
		{StateNeedsAttention, StateSucceeded},
		{StateUnknown, StateSucceeded}, // unknown needs reconciliation first
		{StateCreated, StateRunning},   // no skipping precheck
		{StateRunning, StateSucceeded}, // no success without verification
	}
	for _, f := range forbidden {
		if CanTransition(f.from, f.to) {
			t.Errorf("%s → %s must be forbidden", f.from, f.to)
		}
	}
	for _, s := range []State{StateSucceeded, StateFailed, StateRolledBack, StateCancelled, StateNeedsAttention} {
		if s == StateFailed {
			continue // failed may still roll back
		}
		if !s.IsTerminal() {
			t.Errorf("%s must be terminal", s)
		}
	}
	if StateUnknown.IsTerminal() || !StateUnknown.IsActive() {
		t.Error("unknown is active (locks held) and not terminal")
	}
}

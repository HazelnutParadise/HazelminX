// Package jobs is the Core-side job engine (spec chapter 33).
//
// A job is the durable record of one long-running operation: it survives the
// HTTP request that created it, carries its own steps, events and evidence,
// and only moves between the states listed in spec 33.1. Execution of the
// steps is delegated to a Handler per job kind; today those handlers are
// in-process fakes, later ones proxy to an Agent (tickets 004/007).
package jobs

// State is one of the job states of spec 33.1. The wire form is the snake_case
// string; the Chinese label is the spec's wording.
type State string

const (
	StateCreated          State = "created"           // 已建立
	StatePrechecking      State = "prechecking"       // 預檢中
	StateAwaitingApproval State = "awaiting_approval" // 等待批准
	StateQueued           State = "queued"            // 排隊中
	StateRunning          State = "running"           // 執行中
	StateVerifying        State = "verifying"         // 驗證中
	StateSucceeded        State = "succeeded"         // 成功
	StateFailed           State = "failed"            // 失敗
	StateRollingBack      State = "rolling_back"      // 復原中
	StateRolledBack       State = "rolled_back"       // 已復原
	StateCancelRequested  State = "cancel_requested"  // 要求取消
	StateCancelled        State = "cancelled"         // 已取消
	StateUnknown          State = "unknown"           // 狀態不明
	StateNeedsAttention   State = "needs_attention"   // 需要處理
)

// transitions is the "允許的下一步" column of spec 33.1, plus the cancel
// paths the table describes in prose (created/queued → cancel; running →
// cancel requested; rolling back may end unknown).
var transitions = map[State][]State{
	StateCreated:          {StatePrechecking, StateCancelled},
	StatePrechecking:      {StateAwaitingApproval, StateQueued, StateFailed, StateCancelled},
	StateAwaitingApproval: {StateQueued, StateCreated, StateCancelled},
	StateQueued:           {StateRunning, StateCancelled, StateFailed},
	StateRunning:          {StateVerifying, StateFailed, StateCancelRequested, StateUnknown},
	StateVerifying:        {StateSucceeded, StateFailed, StateRollingBack, StateUnknown},
	StateSucceeded:        {},
	StateFailed:           {StateRollingBack, StateNeedsAttention},
	StateRollingBack:      {StateRolledBack, StateNeedsAttention, StateUnknown},
	StateRolledBack:       {},
	StateCancelRequested:  {StateCancelled, StateFailed, StateUnknown, StateRollingBack},
	StateCancelled:        {},
	StateUnknown:          {StateRunning, StateVerifying, StateFailed, StateNeedsAttention, StateRollingBack},
	StateNeedsAttention:   {},
}

// CanTransition reports whether spec 33.1 allows moving from one state to
// the next.
func CanTransition(from, to State) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// IsTerminal reports whether no further automatic transition exists. A job in
// a terminal state is history: it is never modified, only read.
func (s State) IsTerminal() bool {
	return len(transitions[s]) == 0
}

// IsActive reports whether the job may still hold resource locks and budget.
func (s State) IsActive() bool {
	switch s {
	case StateQueued, StateRunning, StateVerifying, StateRollingBack, StateCancelRequested, StateUnknown:
		return true
	}
	return false
}

// Valid reports whether s is one of the known states.
func (s State) Valid() bool {
	_, ok := transitions[s]
	return ok
}

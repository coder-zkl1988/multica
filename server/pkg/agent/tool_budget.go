package agent

import (
	"errors"
	"sync/atomic"
)

// ErrToolBudgetExceeded is the terminal error a backend reports when a run
// was force-stopped because its tool-call count reached ExecOptions.
// MaxToolCalls. The daemon maps it onto a task failure whose reason names the
// budget, so operators can tell an exhausted budget apart from a crash.
var ErrToolBudgetExceeded = errors.New("agent tool-call budget exceeded")

// toolCallBudget counts tool-call events against a cap. A nil, zero-cap, or
// negative-cap budget is disabled: Allow always returns true, matching the
// historical uncapped behaviour when MaxToolCalls is unset.
//
// It is goroutine-safe: backends observe tool-call events on reader
// goroutines while the run's result is assembled elsewhere, so both the
// counter and the disabled flag are atomics. limit is immutable after
// construction and is used only to preview an observed event without
// consuming the budget a second time during final buffer parsing.
type toolCallBudget struct {
	remaining atomic.Int64
	disabled  atomic.Bool
	limit     int64
}

// newToolCallBudget returns a budget of cap tool calls. cap <= 0 returns a
// disabled budget whose Allow is always true — this is the 0 = uncapped
// escape hatch.
func newToolCallBudget(cap int) *toolCallBudget {
	b := &toolCallBudget{limit: int64(cap)}
	if cap > 0 {
		b.remaining.Store(int64(cap))
	} else {
		b.disabled.Store(true)
	}
	return b
}

// Allow consumes one tool call and reports whether the run may continue.
// Contract: exactly cap calls are allowed (calls 1..cap return true); the
// cap+1-th and every later call return false. A backend stops the run
// before executing the tool call that Allow denied.
func (b *toolCallBudget) Allow() bool {
	if b == nil || b.disabled.Load() {
		return true
	}
	for {
		cur := b.remaining.Load()
		if cur <= 0 {
			return false
		}
		if b.remaining.CompareAndSwap(cur, cur-1) {
			return true
		}
	}
}

// allowsObserved reports whether the ordinal-th event is inside the cap
// without decrementing remaining. It is for a reader callback that must kill
// a live process immediately, while the final parser later decides which
// admitted events to forward without double-counting them.
func (b *toolCallBudget) allowsObserved(ordinal int64) bool {
	return b == nil || b.disabled.Load() || ordinal <= b.limit
}

package agent

import (
	"errors"
	"sync"
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
// goroutines while the run's result is assembled elsewhere, so the counter
// and the disabled flag are atomics.
type toolCallBudget struct {
	remaining atomic.Int64
	disabled  atomic.Bool
}

// newToolCallBudget returns a budget of cap tool calls. cap <= 0 returns a
// disabled budget whose Allow is always true — this is the 0 = uncapped
// escape hatch.
func newToolCallBudget(cap int) *toolCallBudget {
	b := &toolCallBudget{}
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

// liveAdmittedCalls records tool calls a live stream observer has already
// charged to the budget, so a post-hoc pass (openclaw's session transcript)
// can skip their duplicates instead of double-charging.
//
// Membership is only ever ADDED by the live observer. A post-hoc pass
// consumes entries with take — decrementing, not remembering — so a
// transcript-only CallID never enters the set and later rows bearing the
// same ID are still charged every time they appear. A nil *liveAdmittedCalls
// is a no-op: take always misses, meaning "not previously charged".
//
// Calls with no wire ID are matched by count only: take("") succeeds at most
// as many times as unIDed calls were admitted live. Safe for concurrent use:
// the live reader and the final parser run on different goroutines.
type liveAdmittedCalls struct {
	mu       sync.Mutex
	byID     map[string]int
	noIDLeft int
}

// addLive records a live-admitted call. Live observer only.
func (c *liveAdmittedCalls) addLive(callID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byID == nil {
		c.byID = make(map[string]int)
	}
	if callID == "" {
		c.noIDLeft++
		return
	}
	c.byID[callID]++
}

// peek reports whether a call with this ID was admitted live, WITHOUT
// consuming the admission. The buffered final parse uses this: it re-visits
// every line the live reader already charged and must skip them without
// spending their admissions — only the session-transcript pass takes, so a
// call visible in both stream and transcript is charged exactly once.
func (c *liveAdmittedCalls) peek(callID string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if callID == "" {
		return c.noIDLeft > 0
	}
	return c.byID[callID] > 0
}

// take reports whether this exact call was already charged by the live
// observer, consuming that admission. Session-transcript pass only: each
// live admission can be taken at most once, so a call that appears in both
// stream and transcript skips exactly once, and any further occurrence of
// the same ID (whose admission was already consumed) charges afresh.
func (c *liveAdmittedCalls) take(callID string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if callID == "" {
		if c.noIDLeft > 0 {
			c.noIDLeft--
			return true
		}
		return false
	}
	if n := c.byID[callID]; n > 0 {
		c.byID[callID] = n - 1
		return true
	}
	return false
}

package agent

import (
	"errors"
	"sync"
	"testing"
)

func TestToolCallBudgetDisabled(t *testing.T) {
	for _, cap := range []int{0, -1, -100} {
		b := newToolCallBudget(cap)
		for i := 0; i < 100; i++ {
			if !b.Allow() {
				t.Fatalf("cap %d: disabled budget must always allow (call %d)", cap, i)
			}
		}
	}
}

func TestToolCallBudgetNilIsDisabled(t *testing.T) {
	var b *toolCallBudget
	if !b.Allow() {
		t.Fatal("nil budget must always allow")
	}
}

func TestToolCallBudgetCapsAtN(t *testing.T) {
	const cap = 3
	b := newToolCallBudget(cap)
	for i := 1; i <= cap; i++ {
		if !b.Allow() {
			t.Fatalf("call %d of %d should be allowed", i, cap)
		}
	}
	if b.Allow() {
		t.Fatalf("call %d must be denied — budget exhausted", cap+1)
	}
	if b.Allow() {
		t.Fatal("post-exhaustion calls must stay denied")
	}
}

func TestToolCallBudgetCapOne(t *testing.T) {
	b := newToolCallBudget(1)
	if !b.Allow() {
		t.Fatal("a budget of 1 must allow the first call")
	}
	if b.Allow() {
		t.Fatal("a budget of 1 must deny the second call")
	}
}

func TestToolCallBudgetConcurrent(t *testing.T) {
	const cap = 50
	b := newToolCallBudget(cap)
	var mu sync.Mutex
	denied := 0
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if !b.Allow() {
					mu.Lock()
					denied++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	// 160 attempts against a 50 budget: exactly 50 allowed, 110 denied.
	if denied != 160-cap {
		t.Fatalf("denied = %d, want %d", denied, 160-cap)
	}
}

func TestErrToolBudgetExceededSentinel(t *testing.T) {
	if !errors.Is(ErrToolBudgetExceeded, ErrToolBudgetExceeded) {
		t.Fatal("sentinel must satisfy errors.Is with itself")
	}
}

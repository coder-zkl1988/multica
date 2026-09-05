package handler

// NOTE: All tests in this package are skipped when the database is
// unreachable (TestMain calls os.Exit(0)). The tests below are compile-time
// guards for the capability matching helpers and resolveRunCapabilities; the
// logic tests that MUST produce --- PASS lines live in
// internal/integrations/testcapability/dispatch_test.go and
// internal/daemon/capabilities_test.go.

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// matchCapabilityTarget
// ---------------------------------------------------------------------------

func TestMatchCapabilityTarget_EmptyMatch_AlwaysTrue(t *testing.T) {
	got := matchCapabilityTarget(nil, map[string]string{"browser": "chromium"})
	if !got {
		t.Error("empty match map must match any target")
	}
}

func TestMatchCapabilityTarget_ExactMatch(t *testing.T) {
	match := map[string]string{"browser": "chromium"}
	target := map[string]string{"browser": "chromium", "provider": "playwright"}
	if !matchCapabilityTarget(match, target) {
		t.Error("exact key match must return true")
	}
}

func TestMatchCapabilityTarget_MissingKey_ReturnsFalse(t *testing.T) {
	match := map[string]string{"os": "android"}
	target := map[string]string{"browser": "chromium"}
	if matchCapabilityTarget(match, target) {
		t.Error("missing key in target must return false")
	}
}

func TestMatchCapabilityTarget_WrongValue_ReturnsFalse(t *testing.T) {
	match := map[string]string{"browser": "firefox"}
	target := map[string]string{"browser": "chromium"}
	if matchCapabilityTarget(match, target) {
		t.Error("wrong value in target must return false")
	}
}

// ---------------------------------------------------------------------------
// satisfiesConstraint
// ---------------------------------------------------------------------------

func TestSatisfiesConstraint_Exact(t *testing.T) {
	if !satisfiesConstraint("chromium", "chromium") {
		t.Error("exact match must be true")
	}
	if satisfiesConstraint("firefox", "chromium") {
		t.Error("different value must be false")
	}
}

func TestSatisfiesConstraint_GTE(t *testing.T) {
	if !satisfiesConstraint("131", ">=120") {
		t.Error("131 >= 120 must be true")
	}
	if satisfiesConstraint("100", ">=120") {
		t.Error("100 >= 120 must be false")
	}
	if !satisfiesConstraint("120", ">=120") {
		t.Error("120 >= 120 must be true (equal satisfies >=)")
	}
}

func TestSatisfiesConstraint_GT(t *testing.T) {
	if !satisfiesConstraint("121", ">120") {
		t.Error("121 > 120 must be true")
	}
	if satisfiesConstraint("120", ">120") {
		t.Error("120 > 120 must be false (equal does not satisfy >)")
	}
}

func TestSatisfiesConstraint_LTE(t *testing.T) {
	if !satisfiesConstraint("100", "<=120") {
		t.Error("100 <= 120 must be true")
	}
	if satisfiesConstraint("130", "<=120") {
		t.Error("130 <= 120 must be false")
	}
}

// ---------------------------------------------------------------------------
// compareVersionLike
// ---------------------------------------------------------------------------

func TestCompareVersionLike_SingleSegment(t *testing.T) {
	if got := compareVersionLike("10", "9"); got <= 0 {
		t.Errorf("compareVersionLike(10, 9) = %d, want > 0", got)
	}
	if got := compareVersionLike("9", "10"); got >= 0 {
		t.Errorf("compareVersionLike(9, 10) = %d, want < 0", got)
	}
	if got := compareVersionLike("10", "10"); got != 0 {
		t.Errorf("compareVersionLike(10, 10) = %d, want 0", got)
	}
}

func TestCompareVersionLike_MultiSegment(t *testing.T) {
	if got := compareVersionLike("1.10.0", "1.9.0"); got <= 0 {
		t.Errorf("compareVersionLike(1.10.0, 1.9.0) = %d, want > 0", got)
	}
	if got := compareVersionLike("2.0.0", "1.99.99"); got <= 0 {
		t.Errorf("compareVersionLike(2.0.0, 1.99.99) = %d, want > 0", got)
	}
}

func TestCompareVersionLike_UnequalLengths(t *testing.T) {
	if got := compareVersionLike("1.0", "1"); got != 0 {
		t.Errorf("compareVersionLike(1.0, 1) = %d, want 0 (trailing zero)", got)
	}
}

// ---------------------------------------------------------------------------
// InMemoryCapabilityScanStore
// ---------------------------------------------------------------------------

func TestInMemoryCapabilityScanStore_ClaimsOldestPendingOnce(t *testing.T) {
	store := NewInMemoryCapabilityScanStore()
	ctx := context.Background()

	first, err := store.Create(ctx, "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	// Force a distinct, later creation time so ordering is deterministic.
	second, err := store.Create(ctx, "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.requests[second.ID].CreatedAt = first.CreatedAt.Add(time.Second)
	store.mu.Unlock()

	if has, _ := store.HasPending(ctx, "rt-1"); !has {
		t.Fatal("HasPending must be true after Create")
	}
	if has, _ := store.HasPending(ctx, "rt-other"); has {
		t.Fatal("HasPending must be scoped to the runtime")
	}

	claimed, err := store.PopPending(ctx, "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != first.ID {
		t.Fatalf("PopPending claimed %v, want the oldest request %s", claimed, first.ID)
	}
	if claimed.Status != CapabilityScanRunning {
		t.Errorf("claimed status = %s, want running", claimed.Status)
	}

	// The claimed request must not be handed out twice.
	again, _ := store.PopPending(ctx, "rt-1")
	if again == nil || again.ID != second.ID {
		t.Fatalf("second PopPending = %v, want %s", again, second.ID)
	}
	if third, _ := store.PopPending(ctx, "rt-1"); third != nil {
		t.Errorf("third PopPending = %v, want nil", third)
	}
	if has, _ := store.HasPending(ctx, "rt-1"); has {
		t.Error("HasPending must be false once every request is claimed")
	}
}

func TestInMemoryCapabilityScanStore_CompleteAndFailAreTerminal(t *testing.T) {
	store := NewInMemoryCapabilityScanStore()
	ctx := context.Background()

	req, _ := store.Create(ctx, "rt-1")
	if err := store.Complete(ctx, req.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ctx, req.ID)
	if got == nil || got.Status != CapabilityScanCompleted {
		t.Fatalf("after Complete: %v", got)
	}

	failed, _ := store.Create(ctx, "rt-1")
	if err := store.Fail(ctx, failed.ID, "daemon offline"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(ctx, failed.ID)
	if got == nil || got.Status != CapabilityScanFailed || got.Error != "daemon offline" {
		t.Fatalf("after Fail: %v", got)
	}

	// Terminal requests are no longer pending work.
	if has, _ := store.HasPending(ctx, "rt-1"); has {
		t.Error("terminal requests must not count as pending")
	}
	if unknown, _ := store.Get(ctx, "nope"); unknown != nil {
		t.Errorf("Get(unknown) = %v, want nil", unknown)
	}
}

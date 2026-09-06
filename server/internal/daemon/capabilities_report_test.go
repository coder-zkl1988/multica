package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The capability report is what makes browser / device requirements
// resolvable at dispatch. Until it existed the probe had no caller, so every
// run that declared a requirement was parked as blocked.

func withNoCapabilityTools(t *testing.T) {
	t.Helper()
	prev := capabilitiesLookPath
	capabilitiesLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { capabilitiesLookPath = prev })
}

type capturedReport struct {
	mu   sync.Mutex
	path string
	body map[string]any
}

func (c *capturedReport) handler(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = r.URL.Path
	_ = json.NewDecoder(r.Body).Decode(&c.body)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

func (c *capturedReport) snapshot() (string, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path, c.body
}

func TestReportRuntimeCapabilities_PostsInventoryUnderTheScanRequest(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)
	withNoCapabilityTools(t)

	rec := &capturedReport{}
	d, calls := localSkillReportDaemon(t, rec.handler)

	d.reportRuntimeCapabilities(context.Background(), Runtime{ID: "rt-1"}, "scan-1")

	if n := atomic.LoadInt32(calls); n != 1 {
		t.Fatalf("report calls = %d, want 1", n)
	}
	path, body := rec.snapshot()
	if path != "/api/daemon/runtimes/rt-1/capabilities" {
		t.Errorf("report path = %q", path)
	}
	if body["request_id"] != "scan-1" {
		t.Errorf("request_id = %v, want scan-1", body["request_id"])
	}
	// An empty inventory is still a report: it is how a daemon retires
	// capabilities that are gone. JSON null would read as "no report".
	caps, ok := body["capabilities"].([]any)
	if !ok {
		t.Fatalf("capabilities = %#v, want an array", body["capabilities"])
	}
	if len(caps) != 0 {
		t.Errorf("capabilities = %v, want empty with no tools in PATH", caps)
	}
}

func TestReportRuntimeCapabilities_UnsolicitedReportOmitsRequestID(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)
	withNoCapabilityTools(t)

	rec := &capturedReport{}
	d, _ := localSkillReportDaemon(t, rec.handler)

	d.reportRuntimeCapabilities(context.Background(), Runtime{ID: "rt-1"}, "")

	_, body := rec.snapshot()
	if _, present := body["request_id"]; present {
		t.Errorf("an unsolicited report must not carry a request_id, got %v", body["request_id"])
	}
}

func TestHandleHeartbeatActions_DispatchesCapabilityScan(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)
	withNoCapabilityTools(t)

	rec := &capturedReport{}
	d, calls := localSkillReportDaemon(t, rec.handler)
	d.runtimeIndex = map[string]Runtime{"rt-1": {ID: "rt-1"}}

	d.handleHeartbeatActions(context.Background(), "rt-1", &HeartbeatResponse{
		PendingCapabilityScan: &PendingCapabilityScan{ID: "scan-9"},
	})

	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(calls) == 0 {
		t.Fatal("heartbeat ack with pending_capability_scan did not produce a report")
	}
	_, body := rec.snapshot()
	if body["request_id"] != "scan-9" {
		t.Errorf("request_id = %v, want scan-9", body["request_id"])
	}
}

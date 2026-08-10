package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientCapturesAuthExpiry(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Auth-Expires-At", expiresAt.Format(time.RFC3339))
		_ = json.NewEncoder(w).Encode([]WorkspaceInfo{})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL)
	if _, err := client.ListWorkspaces(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := client.AuthExpiresAt(); !got.Equal(expiresAt) {
		t.Fatalf("auth expiry = %s, want %s", got, expiresAt)
	}
}

func TestAuthExpiryDrainFailsRunningTaskWithoutRetry(t *testing.T) {
	var (
		mu       sync.Mutex
		failBody map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/fail"):
			mu.Lock()
			defer mu.Unlock()
			_ = json.NewDecoder(r.Body).Decode(&failBody)
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "running"})
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	runnerStarted := make(chan struct{})
	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeTaskCancels:  make(map[string]context.CancelCauseFunc),
		authExpiresAt:      time.Now().Add(time.Minute),
		cancelPollInterval: time.Hour,
	}
	d.runner = taskRunnerFunc(func(ctx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		close(runnerStarted)
		<-ctx.Done()
		return TaskResult{SessionID: "session-1", WorkDir: "/tmp/work-1"}, ctx.Err()
	})

	done := make(chan struct{})
	go func() {
		d.handleTask(context.Background(), Task{ID: "task-1", RuntimeID: "rt-1"}, 0)
		close(done)
	}()
	<-runnerStarted
	d.beginAuthExpiryDrain()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not stop during auth expiry drain")
	}
	if d.tryEnterClaim() {
		t.Fatal("daemon accepted a claim during auth expiry drain")
	}

	mu.Lock()
	defer mu.Unlock()
	if failBody["error"] != "authentication expired" {
		t.Fatalf("failure error = %v", failBody["error"])
	}
	if failBody["failure_reason"] != "authentication_expired" {
		t.Fatalf("failure reason = %v", failBody["failure_reason"])
	}
	if failBody["session_id"] != "session-1" || failBody["work_dir"] != "/tmp/work-1" {
		t.Fatalf("artifacts were not preserved: %#v", failBody)
	}
}

func TestAuthExpiryLoopTerminatesAtExpiry(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	expiresAt := time.Now().Add(150 * time.Millisecond)
	d := &Daemon{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		cancelFunc:        cancel,
		authExpiresAt:     expiresAt,
		authDrainLead:     100 * time.Millisecond,
		activeTaskCancels: make(map[string]context.CancelCauseFunc),
	}

	go d.authExpiryLoop(root)
	select {
	case <-root.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not terminate at auth expiry")
	}
	if time.Now().Before(expiresAt) {
		t.Fatal("daemon terminated before auth expiry")
	}
}

func TestAuthExpiryLoopDrainsSSOCredentials(t *testing.T) {
	for _, token := range []string{"eyJhbGciOiJIUzI1NiJ9.payload.signature", "msa_service"} {
		t.Run(strings.SplitN(token, "_", 2)[0], func(t *testing.T) {
			expiresAt := time.Now().UTC().Add(150 * time.Millisecond)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/daemon/workspaces" {
					t.Fatalf("unexpected request path %s", r.URL.Path)
				}
				w.Header().Set("X-Auth-Expires-At", expiresAt.Format(time.RFC3339Nano))
				_, _ = w.Write([]byte(`[]`))
			}))
			t.Cleanup(srv.Close)

			root, cancel := context.WithCancel(context.Background())
			d := &Daemon{
				client:            NewClient(srv.URL),
				logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
				cancelFunc:        cancel,
				authDrainLead:     100 * time.Millisecond,
				activeTaskCancels: make(map[string]context.CancelCauseFunc),
			}
			d.client.SetToken(token)
			if err := d.preflightAuth(root); err != nil {
				t.Fatal(err)
			}
			go d.authExpiryLoop(root)
			select {
			case <-root.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("daemon did not terminate at auth expiry")
			}
			if !d.authDraining.Load() {
				t.Fatal("daemon did not enter auth drain before expiry")
			}
		})
	}
}

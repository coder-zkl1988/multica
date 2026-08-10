package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestClient_RenewToken_PostsToCorrectEndpoint(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/tokens/current/renew" {
			t.Fatalf("unexpected renewal request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer mul_abc" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"expires_at": "2099-01-02T03:04:05Z",
			"renewed":    true,
		})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL)
	c.SetToken("mul_abc")
	resp, err := c.RenewToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if called.Load() != 1 || !resp.Renewed || resp.ExpiresAt != "2099-01-02T03:04:05Z" {
		t.Fatalf("unexpected renewal response/calls: %#v, %d", resp, called.Load())
	}
}

func TestTryRenewToken_LogsRenewalOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"expires_at": "2099-01-02T03:04:05Z",
			"renewed":    true,
		})
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.tryRenewToken(context.Background())
	if out := buf.String(); !strings.Contains(out, "auth token renewed") || !strings.Contains(out, "2099-01-02T03:04:05Z") {
		t.Fatalf("unexpected renewal log: %s", out)
	}
}

func TestTryRenewToken_LogsNotEligibleOnNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"expires_at": "2099-01-02T03:04:05Z",
			"renewed":    false,
		})
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.tryRenewToken(context.Background())
	if strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("no-op renewal logged warning: %s", buf.String())
	}
}

func TestTryRenewToken_SurfacesReloginWarningOn401(t *testing.T) {
	for _, profile := range []string{"", "staging"} {
		t.Run(profile, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			}))
			t.Cleanup(srv.Close)

			var buf bytes.Buffer
			d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf), cfg: Config{Profile: profile}}
			d.tryRenewToken(context.Background())
			out := buf.String()
			if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "multica login") {
				t.Fatalf("missing re-login warning: %s", out)
			}
			if profile != "" && !strings.Contains(out, "--profile staging") {
				t.Fatalf("missing profile hint: %s", out)
			}
		})
	}
}

func TestTryRenewToken_TransientErrorIsDebugNotWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"db down"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.tryRenewToken(context.Background())
	out := buf.String()
	if strings.Contains(out, "level=WARN") || !strings.Contains(out, "token renewal failed") {
		t.Fatalf("unexpected transient-error log: %s", out)
	}
}

func TestPreflightAuth_RenewsBeforeWorkspaceSyncOnExpiredToken(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.client.SetToken("mul_already_revoked")
	if err := d.preflightAuth(context.Background()); err == nil {
		t.Fatal("expected workspace sync to fail")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 || seen[0] != "/api/tokens/current/renew" || seen[1] != "/api/daemon/workspaces" {
		t.Fatalf("unexpected preflight order: %v", seen)
	}
	if seen[0] != "/api/tokens/current/renew" {
		t.Fatalf("renew must be the first API call so the WARN fires before the sync 401s; got order %v", seen)
	}
	if seen[1] != "/api/daemon/workspaces" {
		t.Fatalf("workspace sync should follow renew; got order %v", seen)
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected re-login WARN, got: %s", out)
	}
	if !strings.Contains(out, "multica login") {
		t.Fatalf("expected the actionable 'run multica login' hint in the WARN, got: %s", out)
	}
}

func TestPreflightAuth_SyncProceedsWhenRenewIsNoOp(t *testing.T) {
	var syncCalled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tokens/current/renew":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"expires_at": "2099-01-02T03:04:05Z",
				"renewed":    false,
			})
		case "/api/daemon/workspaces":
			syncCalled.Store(true)
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.client.SetToken("mul_healthy")
	if err := d.preflightAuth(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !syncCalled.Load() {
		t.Fatal("workspace sync was skipped")
	}
}

func TestPreflightAuth_TransientRenewFailureDoesNotBlockStartup(t *testing.T) {
	var syncCalled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tokens/current/renew":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"db down"}`))
		case "/api/daemon/workspaces":
			syncCalled.Store(true)
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	d.client.SetToken("mul_healthy")
	if err := d.preflightAuth(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !syncCalled.Load() || strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("unexpected transient renewal behavior: synced=%v log=%s", syncCalled.Load(), buf.String())
	}
}

func TestPreflightAuth_RenewsOnlyLegacyPAT(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	for _, tc := range []struct {
		name, token string
		wantRenew   bool
		wantExpiry  bool
	}{
		{name: "legacy PAT", token: "mul_pat", wantRenew: true},
		{name: "SSO JWT", token: "eyJhbGciOiJIUzI1NiJ9.payload.signature", wantExpiry: true},
		{name: "service token", token: "msa_service", wantExpiry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var renewCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/tokens/current/renew":
					renewCalls.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]any{"renewed": false})
				case "/api/workspaces":
					if tc.wantExpiry {
						w.Header().Set("X-Auth-Expires-At", expiresAt.Format(time.RFC3339))
					}
					_, _ = w.Write([]byte(`[]`))
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(srv.Close)

			d := &Daemon{client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			d.client.SetToken(tc.token)
			if err := d.preflightAuth(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := renewCalls.Load() > 0; got != tc.wantRenew {
				t.Fatalf("renew called = %v, want %v", got, tc.wantRenew)
			}
			if tc.wantExpiry && !d.authExpiresAt.Equal(expiresAt) {
				t.Fatalf("auth expiry = %s, want %s", d.authExpiresAt, expiresAt)
			}
		})
	}
}

func TestTryRenewToken_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	d := &Daemon{client: NewClient(srv.URL), logger: captureLogger(&buf)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		d.tryRenewToken(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("renewal did not honor context cancellation")
	}
}

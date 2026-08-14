package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newChatSessionTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	return cmd
}

func newChatSessionTaskMessagesTestCmd() *cobra.Command {
	cmd := newChatSessionTestCmd()
	cmd.Flags().String("task", "", "")
	cmd.Flags().Int("since", 0, "")
	return cmd
}

func newChatSessionSendTestCmd() *cobra.Command {
	cmd := newChatSessionTestCmd()
	cmd.Flags().String("content", "", "")
	cmd.Flags().Bool("content-stdin", false, "")
	cmd.Flags().String("content-file", "", "")
	cmd.Flags().Bool("allow-external-file", false, "")
	return cmd
}

func chatSessionCommandTestServer(t *testing.T, assert func(*testing.T, *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert(t, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	return srv
}

func TestChatSessionReadCommandsHitSessionAPIs(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*cobra.Command, []string) error
		path string
	}{
		{name: "get", run: runChatSessionGet, path: "/api/chat/sessions/session-1"},
		{name: "messages", run: runChatSessionMessages, path: "/api/chat/sessions/session-1/messages"},
		{name: "pending task", run: runChatSessionPendingTask, path: "/api/chat/sessions/session-1/pending-task"},
		{name: "tasks", run: runChatSessionTasks, path: "/api/chat/sessions/session-1/tasks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chatSessionCommandTestServer(t, func(t *testing.T, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("method = %s, want GET", r.Method)
				}
				if r.URL.Path != tc.path {
					t.Fatalf("path = %s, want %s", r.URL.Path, tc.path)
				}
			})

			if _, err := captureStdout(t, func() error { return tc.run(newChatSessionTestCmd(), []string{"session-1"}) }); err != nil {
				t.Fatalf("%s command returned error: %v", tc.name, err)
			}
		})
	}
}

func TestChatSessionTaskMessagesBuildsQuery(t *testing.T) {
	chatSessionCommandTestServer(t, func(t *testing.T, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/chat/sessions/session-1/task-messages" {
			t.Fatalf("path = %s, want task-messages path", r.URL.Path)
		}
		want := url.Values{"task": []string{"task-1"}, "since": []string{"7"}}
		if got := r.URL.Query(); got.Encode() != want.Encode() {
			t.Fatalf("query = %s, want %s", got.Encode(), want.Encode())
		}
	})

	cmd := newChatSessionTaskMessagesTestCmd()
	_ = cmd.Flags().Set("task", "task-1")
	_ = cmd.Flags().Set("since", "7")
	if _, err := captureStdout(t, func() error { return runChatSessionTaskMessages(cmd, []string{"session-1"}) }); err != nil {
		t.Fatalf("task-messages command returned error: %v", err)
	}
}

func TestChatSessionSendPostsContent(t *testing.T) {
	chatSessionCommandTestServer(t, func(t *testing.T, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/chat/sessions/session-1/messages" {
			t.Fatalf("path = %s, want messages path", r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["content"] != "hello\nworld" {
			t.Fatalf("content = %q, want decoded newline", body["content"])
		}
	})

	cmd := newChatSessionSendTestCmd()
	_ = cmd.Flags().Set("content", `hello\nworld`)
	if _, err := captureStdout(t, func() error { return runChatSessionSend(cmd, []string{"session-1"}) }); err != nil {
		t.Fatalf("send command returned error: %v", err)
	}
}

func TestChatSessionSendRequiresContent(t *testing.T) {
	err := runChatSessionSend(newChatSessionSendTestCmd(), []string{"session-1"})
	if err == nil || !strings.Contains(err.Error(), "--content") {
		t.Fatalf("runChatSessionSend error = %v, want missing content error", err)
	}
}

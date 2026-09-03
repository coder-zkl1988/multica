package daemon

import (
	"strings"
	"testing"
)

func TestBuildDirectPromptPreservesFlowInputs(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want []string
	}{
		{
			name: "comment delivery and thread routing",
			task: Task{
				IssueID:               "issue-1",
				TriggerCommentID:      "comment-1",
				TriggerThreadID:       "thread-1",
				TriggerCommentContent: "Please inspect the timeout.",
				TriggerAuthorType:     "member",
				TriggerAuthorName:     "Alice",
				CoalescedComments: []CoalescedCommentData{{
					ID: "comment-0", ThreadID: "thread-0", AuthorType: "agent", AuthorName: "Bot",
					Content: "Also check the retry path.", CreatedAt: "2026-08-01T00:00:00Z",
				}},
			},
			want: []string{
				"Issue: issue-1", "Trigger comment: comment-1 (thread thread-1)", "Author: member (Alice)",
				"Please inspect the timeout.", "comment-0", "Also check the retry path.",
				"multica issue comment add issue-1 --parent comment-0 --content-file ./reply.md",
				"multica issue comment add issue-1 --parent comment-1 --content-file ./reply.md",
				"rm ./reply.md",
			},
		},
		{
			name: "chat attachments and delivery",
			task: Task{
				ChatSessionID: "chat-1", ChatChannelType: "slack", ChatType: "group", ChatInThread: true,
				ChatMessage: "How does this parser work?", ChatChannelDeliversFiles: true,
				ChatMessageAttachments: []ChatAttachmentMeta{{ID: "att-1", Filename: "trace.txt", ContentType: "text/plain"}},
			},
			want: []string{
				"Chat session: chat-1", "Surface: slack, group, thread reply", "How does this parser work?",
				"att-1 trace.txt (text/plain)", "multica attachment download <id>",
				"stdout is delivered to this chat", "multica attachment upload <local-path>",
			},
		},
		{
			name: "autopilot trigger payload",
			task: Task{
				AutopilotRunID: "run-1", AutopilotID: "auto-1", AutopilotTitle: "Nightly check",
				AutopilotSource: "webhook", AutopilotDescription: "Check the release status.",
				AutopilotTriggerPayload: []byte(`{"ref":"main","sha":"abc"}`),
			},
			want: []string{
				"Autopilot run: run-1", "Autopilot: auto-1", "Title: Nightly check", "Source: webhook",
				"Check the release status.", `{"ref":"main","sha":"abc"}`,
			},
		},
		{
			name: "quick create structured fields",
			task: Task{
				AgentID: "agent-1", QuickCreatePrompt: "Create a release checklist.", QuickCreatePriority: "high",
				QuickCreateDueDate: "2026-09-10", ProjectID: "project-1", ProjectTitle: "Web",
				ParentIssueID: "parent-1", ParentIssueIdentifier: "MUL-9",
				QuickCreateAttachmentIDs: []string{"att-1", "att-2"}, QuickCreateSourceContext: []byte(`{"issue":"MUL-9"}`),
			},
			want: []string{
				"Create exactly one issue", "Create a release checklist.", "--assignee-id agent-1", "--priority high",
				"--due-date 2026-09-10", "--project project-1 (Web)", "--parent parent-1 (MUL-9)",
				"--attachment-id att-1", "--attachment-id att-2", `{"issue":"MUL-9"}`,
				"multica issue create --output json", "--title", "--description-file ./description.md",
			},
		},
		{
			name: "assignment",
			task: Task{IssueID: "issue-2", HandoffNote: "Focus on the regression test."},
			want: []string{
				"Issue: issue-2", "multica issue get issue-2 --output json", "multica issue comment add issue-2",
				"multica issue status issue-2 in_review", "Focus on the regression test.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildDirectPrompt(tt.task)
			for _, want := range tt.want {
				if !strings.Contains(prompt, want) {
					t.Errorf("BuildDirectPrompt() missing %q in %q", want, prompt)
				}
			}
		})
	}
}

func TestBuildDirectPromptPassesRawJSONContextsUnchanged(t *testing.T) {
	raw := []byte(`{"kind":"ui-draft","issue_id":"issue-1"}`)
	if got := BuildDirectPrompt(Task{UIDraftCreateContext: raw}); got != string(raw) {
		t.Fatalf("BuildDirectPrompt() = %q, want raw context %q", got, raw)
	}
}

func TestBuildDirectPromptOmitsGenericWorkflowScaffolding(t *testing.T) {
	prompt := BuildDirectPrompt(Task{
		ChatSessionID: "chat-1",
		ChatMessage:   "Please summarize this.",
	})

	for _, banned := range []string{
		"You are running as a local coding agent",
		"Read the discussion",
		"Active sibling runs",
		"AGENTS.md",
		"CLAUDE.md",
	} {
		if strings.Contains(prompt, banned) {
			t.Errorf("direct prompt contains generic workflow text %q: %q", banned, prompt)
		}
	}
}

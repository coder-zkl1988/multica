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
				"Issue: issue-2", "multica issue get issue-2 --output json",
				"multica issue comment add issue-2 --content-file ./reply.md",
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

func TestBuildConcisePromptAddsOperationalContract(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want []string
	}{
		{
			name: "assignment",
			task: Task{IssueID: "issue-1", HandoffNote: "Check the timeout path."},
			want: []string{
				"## Concise execution",
				"multica issue get issue-1 --output json",
				"multica issue comment add issue-1 --content-file ./reply.md",
				"multica issue comment list issue-1 --roots-only --summary --compact --output json",
				"skip the transient status for an immediate read-only answer",
				"stop without re-reading the issue",
				"Never background work and yield",
			},
		},
		{
			name: "comment with ids only",
			task: Task{
				IssueID: "issue-2", TriggerCommentID: "comment-2", TriggerCommentContent: "Please investigate.",
				CoalescedCommentIDs: []string{"comment-1"},
			},
			want: []string{
				"multica issue get issue-2 --output json",
				"multica issue comment list issue-2 --thread <comment-id> --tail 30 --compact --output json",
				"use the supplied trigger and coalesced comments",
			},
		},
		{
			name: "chat",
			task: Task{ChatSessionID: "chat-1", ChatMessage: "Explain the timeout."},
			want: []string{
				"Use the supplied chat message and attachments first",
				"Return the final answer through the requested chat surface",
			},
		},
		{
			name: "autopilot",
			task: Task{AutopilotRunID: "run-1", AutopilotDescription: "Check the release."},
			want: []string{
				"Use the autopilot description and trigger payload as task input",
				"Follow the flow's exact output and delivery contract",
			},
		},
		{
			name: "quick create",
			task: Task{QuickCreatePrompt: "Create a release issue."},
			want: []string{
				"Use the selected fields and create exactly one issue",
				"do not query or comment on an issue that does not exist yet",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildConcisePrompt(tt.task)
			for _, want := range tt.want {
				if !strings.Contains(prompt, want) {
					t.Errorf("buildConcisePrompt() missing %q in %q", want, prompt)
				}
			}
			if strings.Contains(prompt, "You are running as a local coding agent") ||
				strings.Contains(prompt, "## Available Commands") {
				t.Errorf("concise prompt unexpectedly contains the full runtime brief:\n%s", prompt)
			}
			if tt.name == "assignment" && strings.Contains(prompt, "--content \"") {
				t.Errorf("concise assignment prompt permits shell-fragile inline comment bodies:\n%s", prompt)
			}
		})
	}
}

func TestBuildConcisePromptPreservesIdentityAndRunSafety(t *testing.T) {
	prompt := buildConcisePrompt(Task{
		IssueID:                       "issue-1",
		PriorSessionResumeUnavailable: true,
		InitiatorType:                 "member",
		InitiatorName:                 "Alice",
		Agent: &AgentData{
			ID:           "agent-1",
			Name:         "Mika",
			Instructions: "Only make read-only investigations.",
		},
		ConnectedApps: []ConnectedAppData{{Provider: "composio", ServerName: "composio", ToolkitSlug: "github"}},
		ActiveSiblingRuns: []ActiveSiblingRunData{{
			TaskID: "task-2", IssueID: "issue-2", IssueTitle: "Overlapping fix", Status: "running",
		}},
	}, WithSharedLocalDirectory(), WithWorktreeReplayConflicts([]string{"parser.go"}))

	for _, want := range []string{
		"## Agent Identity",
		"**You are: Mika** (ID: `agent-1`)",
		"Only make read-only investigations.",
		"applicable nested instruction files on the target path",
		"Agent Identity instructions override this contract",
		"task-scoped access",
		"Never search parent directories",
		"do not load generic Multica workflow skills merely to restate them",
		"## Shared working directory",
		"## Unresolved merge in your working tree",
		"## Session Continuity Notice",
		"## Task Initiator",
		"## Connected Apps",
		"## Active sibling runs",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("concise prompt missing %q:\n%s", want, prompt)
		}
	}
	if got := strings.Count(prompt, "## Session Continuity Notice"); got != 1 {
		t.Fatalf("concise prompt rendered %d continuity notices, want exactly one:\n%s", got, prompt)
	}
}

func TestBuildConcisePromptLeavesRawContextsUnchanged(t *testing.T) {
	raw := []byte(`{"kind":"raw","issue_id":"issue-1"}`)
	tests := []struct {
		name string
		set  func(*Task)
	}{
		{"ui draft", func(task *Task) { task.UIDraftCreateContext = raw }},
		{"design restore", func(task *Task) { task.DesignRestoreContext = raw }},
		{"test generation", func(task *Task) { task.TestGenerationContext = string(raw) }},
		{"test run", func(task *Task) { task.TestRunContext = string(raw) }},
		{"design system profile", func(task *Task) { task.DesignSystemProfileAnalyzeContext = raw }},
		{"template blueprint", func(task *Task) { task.TemplateBlueprintAnalyzeContext = raw }},
		{"project design system", func(task *Task) { task.ProjectDesignSystemContext = raw }},
		{"design document", func(task *Task) { task.DesignDocumentContext = raw }},
		{"design delivery", func(task *Task) { task.DesignDeliveryContext = raw }},
		{"pmo sync", func(task *Task) { task.PMOSyncContext = raw }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{ConciseMode: true, IssueID: "issue-1"}
			tt.set(&task)
			if got := buildConcisePrompt(task); got != string(raw) {
				t.Fatalf("buildConcisePrompt() = %q, want raw context %q", got, raw)
			}
		})
	}
}

func TestBuildTaskPromptModePrecedence(t *testing.T) {
	tests := []struct {
		name             string
		task             Task
		configuredDirect bool
		wantConcise      bool
		wantFull         bool
	}{
		{name: "normal", task: Task{IssueID: "issue-1"}, wantFull: true},
		{name: "task concise", task: Task{IssueID: "issue-1", ConciseMode: true}, wantConcise: true},
		{name: "configured direct", task: Task{IssueID: "issue-1"}, configuredDirect: true},
		{name: "configured direct wins", task: Task{IssueID: "issue-1", ConciseMode: true}, configuredDirect: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTaskPrompt(tt.task, "claude", tt.configuredDirect)
			if tt.wantConcise && !strings.Contains(got, "## Concise execution") {
				t.Fatalf("concise task did not use compact prompt:\n%s", got)
			}
			if !tt.wantConcise && strings.Contains(got, "## Concise execution") {
				t.Fatalf("non-concise prompt unexpectedly contains compact contract:\n%s", got)
			}
			if tt.wantFull && !strings.Contains(got, "You are running as a local coding agent") {
				t.Fatalf("normal task lost the full workflow prompt:\n%s", got)
			}
			if !tt.configuredDirect && !tt.task.ConciseMode {
				want := BuildPrompt(tt.task, "claude")
				if got != want {
					t.Fatalf("normal prompt changed through helper:\n got: %q\nwant: %q", got, want)
				}
			}
			if tt.configuredDirect && got != BuildDirectPrompt(tt.task) {
				t.Fatalf("configured direct mode changed:\n got: %q\nwant: %q", got, BuildDirectPrompt(tt.task))
			}
		})
	}
}

func TestBuildConcisePromptBoundsScaffoldingNotTaskInput(t *testing.T) {
	large := strings.Repeat("Required detail.\n", 2048)
	for _, tc := range []struct {
		kind string
		task Task
	}{
		{"assignment", Task{IssueID: "issue-1", HandoffNote: large}},
		{"chat", Task{ChatSessionID: "chat-1", ChatMessage: large}},
		{"comment", Task{IssueID: "issue-1", TriggerCommentID: "comment-1", TriggerCommentContent: large, CoalescedCommentIDs: []string{"comment-2"}}},
		{"autopilot", Task{AutopilotRunID: "run-1", AutopilotDescription: large}},
		{"quick_create", Task{QuickCreatePrompt: large}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			contract := buildConciseExecutionContract(tc.task, tc.kind)
			if len(contract) > 1800 {
				t.Errorf("generated %s contract = %d bytes, budget 1800 (excludes task input)", tc.kind, len(contract))
			}
			for _, want := range []string{"Bound tool output with fields/line ranges", "fetch only missing ranges, not the same full output"} {
				if !strings.Contains(contract, want) {
					t.Errorf("contract missing %q", want)
				}
			}
			if !strings.Contains(buildConcisePrompt(tc.task), large) {
				t.Fatal("scaffolding budget must not truncate supplied task input")
			}
		})
	}
}

func TestBuildConcisePromptSiblingSnapshotGuidance(t *testing.T) {
	task := Task{IssueID: "issue-1", ConciseMode: true, ActiveSiblingRuns: []ActiveSiblingRunData{{TaskID: "task-2", IssueID: "issue-2", Status: "running"}}}
	for _, want := range []string{
		"scan comment roots once",
		"Reuse this scan for sibling claims",
		"The sibling list is a snapshot",
		"before handing off or waiting, check `multica issue runs <issue-id> --output json`",
		"multica issue run-messages <task-id> --since <last-seq>",
	} {
		if !strings.Contains(buildTaskPrompt(task, "codex", false), want) {
			t.Errorf("concise sibling prompt missing %q", want)
		}
	}
	if n := len(buildConciseExecutionContract(task, "assignment")); n > 2050 {
		t.Errorf("sibling contract = %d bytes, budget 2050", n)
	}
	for _, tc := range []struct {
		name   string
		task   Task
		direct bool
	}{
		{"no siblings", Task{IssueID: "issue-1", ConciseMode: true}, false},
		{"no issue", Task{HandoffNote: "Investigate", ConciseMode: true, ActiveSiblingRuns: task.ActiveSiblingRuns}, false},
		{"normal", Task{IssueID: task.IssueID, ActiveSiblingRuns: task.ActiveSiblingRuns}, false},
		{"legacy direct", task, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(buildTaskPrompt(tc.task, "codex", tc.direct), "The sibling list is a snapshot") {
				t.Fatal("concise sibling guidance leaked into an unrelated mode/context")
			}
		})
	}
}

var measuredConcisePrompt string

func BenchmarkConcisePromptEnvelope(b *testing.B) {
	for _, tc := range []struct {
		name string
		task Task
	}{
		{"assignment", Task{IssueID: "issue-1"}},
		{"assignment_sibling", Task{IssueID: "issue-1", ActiveSiblingRuns: []ActiveSiblingRunData{{TaskID: "task-2", IssueID: "issue-2", IssueTitle: "Overlapping fix", Status: "running"}}}},
		{"chat", Task{ChatSessionID: "chat-1", ChatMessage: "Explain the timeout."}},
		{"comment", Task{IssueID: "issue-1", TriggerCommentID: "comment-1", TriggerCommentContent: "Inspect the timeout."}},
		{"autopilot", Task{AutopilotRunID: "run-1", AutopilotDescription: "Check the release."}},
		{"quick_create", Task{QuickCreatePrompt: "Create a release issue."}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				measuredConcisePrompt = buildConcisePrompt(tc.task)
			}
			b.ReportMetric(float64(len(measuredConcisePrompt)), "prompt-bytes")
		})
	}
}

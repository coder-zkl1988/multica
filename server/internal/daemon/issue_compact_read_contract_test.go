package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestCompactIssueBriefDefersReadDecisionsToCurrentTurn(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		brief := execenv.RenderRuntimeBrief(provider, execenv.TaskContextForEnv{
			IssueID: "issue-1", IssueCompletionContractVersion: 1,
		})
		for _, duplicate := range []string{"--roots-only", "--tail 30", "## Authoritative Issue Body Snapshot", "## Verified Empty Comment History"} {
			if strings.Contains(brief, duplicate) {
				t.Errorf("%s stable brief repeats per-turn read logic: %q", provider, duplicate)
			}
		}
		for _, tc := range []struct {
			name         string
			mutate       func(*testing.T, *Task)
			wantSnapshot bool
			wantRoots    bool
			wantEmpty    bool
			wantText     []string
		}{
			{name: "fresh assignment", wantSnapshot: true, wantRoots: true},
			{name: "no snapshot", mutate: func(_ *testing.T, task *Task) { task.IssueSnapshot = nil }, wantRoots: true},
			{name: "expired snapshot", mutate: func(_ *testing.T, task *Task) {
				task.IssueSnapshot.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			}, wantRoots: true},
			{name: "cold comment", mutate: func(_ *testing.T, task *Task) { compactReadCommentTask(task) }, wantSnapshot: true, wantRoots: true,
				wantText: []string{"--thread thread-1 --tail 30 --compact", "current request"}},
			{name: "resumed comment", mutate: func(_ *testing.T, task *Task) {
				compactReadCommentTask(task)
				task.PriorSessionID = "session-1"
			}, wantSnapshot: true, wantText: []string{"multica issue comment list issue-1 --since ", "current request"}},
			{name: "unavailable continuation", mutate: func(_ *testing.T, task *Task) {
				compactReadCommentTask(task)
				task.PriorSessionID = "session-1"
				task.PriorSessionResumeUnavailable = true
			}, wantSnapshot: true, wantRoots: true, wantText: []string{"--thread thread-1 --tail 30 --compact"}},
			{name: "verified empty", mutate: func(t *testing.T, task *Task) { *task = issueStartWithEmptyHistory(t, nil) },
				wantSnapshot: true, wantEmpty: true, wantText: []string{"at task start", "later comments"}},
			{name: "new comment invalidates empty", mutate: func(t *testing.T, task *Task) {
				*task = issueStartWithEmptyHistory(t, nil)
				compactReadCommentTask(task)
			}, wantSnapshot: true, wantRoots: true},
			{name: "coalesced threads", mutate: func(_ *testing.T, task *Task) {
				compactReadCommentTask(task)
				task.CoalescedCommentIDs = []string{"older-comment"}
				task.CoalescedComments = []CoalescedCommentData{{ID: "older-comment", ThreadID: "thread-2", Content: "earlier requirement"}}
				task.IssueSnapshot.Trigger.CoalescedCommentsEmbedded = 1
			}, wantSnapshot: true, wantRoots: true, wantText: []string{"older-comment", "thread-2", "earlier requirement"}},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				task := Task{IssueID: "issue-1", IssueCompletionContractVersion: 1, IssueSnapshot: freshIssueSnapshot("issue-1")}
				if tc.mutate != nil {
					tc.mutate(t, &task)
				}
				combined := brief + "\n" + BuildPrompt(task, provider)
				for text, want := range map[string]bool{
					"\n## Authoritative Issue Body Snapshot\n":                                          tc.wantSnapshot,
					"multica issue get issue-1 --output json":                                           !tc.wantSnapshot,
					"multica issue comment list issue-1 --roots-only --summary --compact --output json": tc.wantRoots,
					"\n## Verified Empty Comment History\n":                                             tc.wantEmpty,
				} {
					if got := strings.Contains(combined, text); got != want {
						t.Errorf("presence of %q = %t, want %t", text, got, want)
					}
				}
				for _, text := range tc.wantText {
					if !strings.Contains(combined, text) {
						t.Errorf("missing current-turn context %q", text)
					}
				}
				if strings.Count(combined, "\n## Final Issue Delivery\n") != 1 {
					t.Error("current turn must provide exactly one final delivery definition")
				}
				if tc.wantSnapshot {
					for _, text := range []string{"does not include prior comment history", "omitted detail", "revision conflict"} {
						if !strings.Contains(combined, text) {
							t.Errorf("snapshot authority lost %q", text)
						}
					}
				}
			})
		}
	}
}

func compactReadCommentTask(task *Task) {
	task.TriggerCommentID = "comment-1"
	task.TriggerThreadID = "thread-1"
	task.TriggerCommentContent = "current request"
	task.NewCommentCount = 1
	task.NewCommentsSince = time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	task.IssueSnapshot.Trigger = &IssueTaskTriggerSnapshot{
		CommentID: task.TriggerCommentID, ThreadID: task.TriggerThreadID, ContentEmbedded: true,
		NewCommentCount: task.NewCommentCount, NewCommentsSince: task.NewCommentsSince,
	}
}

package daemon

import (
	"strings"
	"testing"
)

func TestCommentCatchUpDeltaRequiresResumableHistory(t *testing.T) {
	const since = "2026-09-06T00:00:00Z"
	for _, flow := range []struct {
		name  string
		build func(Task) string
	}{
		{name: "ordinary", build: func(task Task) string { return BuildPrompt(task, "claude") }},
		{name: "direct", build: func(task Task) string { return BuildDirectPrompt(task) }},
	} {
		for _, continuation := range []struct {
			name        string
			sessionID   string
			unavailable bool
			wantDelta   bool
		}{
			{name: "no session with delta"},
			{name: "resumable session with delta", sessionID: "session-1", wantDelta: true},
			{name: "unavailable session with delta", sessionID: "session-1", unavailable: true},
		} {
			for _, withSnapshot := range []bool{false, true} {
				snapshotName := "without snapshot"
				if withSnapshot {
					snapshotName = "fresh body snapshot"
				}
				t.Run(flow.name+"/"+continuation.name+"/"+snapshotName, func(t *testing.T) {
					task := Task{
						IssueID:                       "issue-1",
						TriggerCommentID:              "comment-1",
						TriggerThreadID:               "thread-1",
						TriggerCommentContent:         "Include the earlier requirements.",
						NewCommentsSince:              since,
						NewCommentCount:               2,
						PriorSessionID:                continuation.sessionID,
						PriorSessionResumeUnavailable: continuation.unavailable,
					}
					if withSnapshot {
						task.IssueSnapshot = freshIssueSnapshot(task.IssueID)
						task.IssueSnapshot.Trigger = &IssueTaskTriggerSnapshot{
							CommentID: task.TriggerCommentID, ThreadID: task.TriggerThreadID, ContentEmbedded: true,
							NewCommentsSince: since, NewCommentCount: task.NewCommentCount,
						}
					}
					prompt := flow.build(task)
					roots := "multica issue comment list issue-1 --roots-only --summary --compact --output json"
					// MUL-7344 (upstream): the delta read is one issue-wide
					// `--since` call, not the triggering thread narrowed by an
					// anchor — `--thread` with `--since` drops the thread root.
					delta := "multica issue comment list issue-1 --since " + since + " --compact --output json"
					if continuation.wantDelta {
						if !strings.Contains(prompt, delta) || strings.Contains(prompt, roots) {
							t.Fatal("resumable history must use the comment delta without a repeated roots scan")
						}
					} else {
						if !strings.Contains(prompt, roots) || strings.Contains(prompt, delta) {
							t.Fatal("missing or unavailable resumed history must require roots catch-up, even when a delta exists")
						}
						if !strings.Contains(prompt, "--thread thread-1 --tail 30 --compact --output json") {
							t.Fatal("fresh turn must retain the bounded triggering-thread context read")
						}
					}
					if withSnapshot && !strings.Contains(prompt, "## Authoritative Issue Body Snapshot") {
						t.Fatal("fresh body snapshot must still be rendered")
					}
				})
			}
		}
	}
}

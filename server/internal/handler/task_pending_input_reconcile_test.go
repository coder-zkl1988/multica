package handler

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestTaskPendingInputDeliveryReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		ack          bool
		legacyAck    bool
		extraComment bool
		wantFollowup int
	}{
		{name: "acknowledged answer is not replayed", ack: true},
		{name: "legacy acknowledgement gains missing receipt", ack: true, legacyAck: true},
		{name: "unacknowledged answer remains recoverable", wantFollowup: 1},
		{name: "ordinary new comment is still reconciled", ack: true, extraComment: true, wantFollowup: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := dbfx.Runtime(t, "Pending input reconciliation runtime", testutil.Cols{"daemon_id": "legit-daemon"})
			agentID := dbfx.Agent(t, "Pending input reconciliation agent", runtimeID)
			issueID := dbfx.Issue(t, "Pending input reconciliation issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
			dispatchedAt := time.Now().UTC().Truncate(time.Microsecond)
			priorID := uuid.NewString()
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
				"dispatched_at": dispatchedAt, "started_at": dispatchedAt,
				"delivered_comment_ids": []string{priorID},
			})
			t.Cleanup(func() {
				_, _ = testPool.Exec(ctx, `DELETE FROM task_pending_input WHERE issue_id = $1`, issueID)
				_, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
				_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
			})
			registration := validPendingInputRegistration()
			registration.ClaimGeneration = dispatchedAt.UnixMicro()
			base := "/api/daemon/runtimes/" + runtimeID + "/tasks/" + taskID + "/pending-inputs"
			var pending taskPendingInputResponse
			testutil.Call(t, testHandler.RegisterTaskPendingInput, pendingInputDaemonRequest(http.MethodPost, base,
				registration, runtimeID, taskID, "", testWorkspaceID, "legit-daemon")).Want(http.StatusCreated).JSON(&pending)
			answer := answerTaskPendingInputRequest{
				IdempotencyKey: uuid.NewString(),
				Answers: map[string]taskPendingInputAnswer{
					"deployment": {Answers: []string{"Staging"}},
					"notes":      {Answers: []string{"Mention the migration."}},
				},
			}
			testutil.Call(t, testHandler.AnswerTaskPendingInput, pendingInputIssueRequest(http.MethodPost,
				"/api/issues/"+issueID+"/pending-inputs/"+pending.ID+"/answer", answer, issueID, pending.ID)).Want(http.StatusOK)
			var answerID string
			dbfx.QueryRow(t, `SELECT answer_comment_id::text FROM task_pending_input WHERE id = $1`, pending.ID).Scan(&answerID)
			if tc.legacyAck {
				dbfx.Exec(t, `UPDATE task_pending_input SET acked_at = now() WHERE id = $1`, pending.ID)
			}
			if tc.ack {
				original := testHandler.TxStarter
				testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
				t.Cleanup(func() { testHandler.TxStarter = original })
				testutil.Call(t, testHandler.AckTaskPendingInput, pendingInputDaemonRequest(http.MethodPost,
					base+"/"+pending.ID+"/ack", map[string]any{"claim_generation": registration.ClaimGeneration},
					runtimeID, taskID, pending.ID, testWorkspaceID, "legit-daemon")).Want(http.StatusInternalServerError)
				testHandler.TxStarter = original
				var hasReceipt, hasAck bool
				dbfx.QueryRow(t, `SELECT $2::uuid = ANY(delivered_comment_ids) FROM agent_task_queue WHERE id = $1`, taskID, answerID).Scan(&hasReceipt)
				dbfx.QueryRow(t, `SELECT acked_at IS NOT NULL FROM task_pending_input WHERE id = $1`, pending.ID).Scan(&hasAck)
				if hasReceipt || hasAck != tc.legacyAck {
					t.Fatal("failed commit persisted a delivery receipt or acknowledgement")
				}
				for range 2 {
					testutil.Call(t, testHandler.AckTaskPendingInput, pendingInputDaemonRequest(http.MethodPost,
						base+"/"+pending.ID+"/ack", map[string]any{"claim_generation": registration.ClaimGeneration},
						runtimeID, taskID, pending.ID, testWorkspaceID, "legit-daemon")).Want(http.StatusOK)
				}
			}
			var delivered []string
			dbfx.QueryRow(t, `SELECT delivered_comment_ids::text[] FROM agent_task_queue WHERE id = $1`, taskID).Scan(&delivered)
			wantReceipts := 1
			if tc.ack {
				wantReceipts++
			}
			if !slices.Contains(delivered, priorID) || slices.Contains(delivered, answerID) != tc.ack || len(delivered) != wantReceipts {
				t.Errorf("delivery receipt = %v; want prior receipt preserved and answer present exactly once iff acked", delivered)
			}
			var extraID string
			if tc.extraComment {
				// Posted the way a member posts one, not written straight into
				// the table: the create path is what registers a comment as
				// planned input on the run that is already claimed, and the
				// completion sweep is scoped to the run's own comment thread
				// plus exactly those planned ids.
				var created CommentResponse
				testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost,
					"/api/issues/"+issueID+"/comments",
					map[string]any{"content": "Also add a separate regression."}), "id", issueID),
				).Want(http.StatusCreated).JSON(&created)
				extraID = created.ID
			}
			if response := completeTaskViaHandler(t, taskID, "Completed after clarification."); response.Code != http.StatusOK {
				t.Fatalf("complete task: %d %s", response.Code, response.Body.String())
			}
			if got := pendingTaskCountForAgentIssue(t, issueID, agentID); got != tc.wantFollowup {
				t.Fatalf("follow-up task count = %d, want %d", got, tc.wantFollowup)
			}
			if tc.extraComment {
				trigger, _, coalesced := taskTriggerOriginatorCoalesced(t, issueID, agentID)
				if trigger != extraID || slices.Contains(coalesced, answerID) {
					t.Fatalf("ordinary follow-up trigger=%s coalesced=%v; consumed answer must not be included", trigger, coalesced)
				}
			}
		})
	}
}

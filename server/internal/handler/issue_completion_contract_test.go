package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCompleteTaskTypedIssueCompletionRejectsStaleClaim(t *testing.T) {
	fx := newIssueCompletionFixture(t, "stale claim", false)
	w := fx.complete(t, map[string]any{
		"output":           "done",
		"claim_generation": fx.claimGeneration - 1,
		"issue_completion": map[string]any{"version": 1, "outcome": "delivered", "comment": "done"},
	})
	w.Want(http.StatusConflict)
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, fx.taskID).Scan(&status)
	if status != "running" {
		t.Fatalf("stale completion changed task status to %q", status)
	}
}

func TestCompleteTaskTypedIssueCompletionRejectsInvalidPresentObject(t *testing.T) {
	fx := newIssueCompletionFixture(t, "invalid object", false)
	fx.complete(t, map[string]any{
		"output":           "done",
		"claim_generation": fx.claimGeneration,
		"issue_completion": map[string]any{"version": 99, "outcome": "delivered", "comment": "done"},
	}).Want(http.StatusBadRequest)
}

func TestCompleteTaskTypedIssueCompletionRejectsConflictingFinalTexts(t *testing.T) {
	fx := newIssueCompletionFixture(t, "conflicting final", false)
	fx.complete(t, map[string]any{
		"output":           "provider final",
		"claim_generation": fx.claimGeneration,
		"issue_completion": map[string]any{"version": 1, "outcome": "delivered", "comment": "different final"},
	}).Want(http.StatusBadRequest)
}

func TestCompleteTaskTypedIssueCompletionAcceptsCustomActiveStatusBaseline(t *testing.T) {
	fx := newIssueCompletionFixture(t, "custom active baseline", false)
	customStatus := "r06_verifying"
	dbfx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": testWorkspaceID, "key": customStatus, "name": "R06 Verifying",
		"description": "R06 completion contract test", "category": "started",
		"color": "#475569", "position": 1,
	})
	dbfx.Exec(t, `UPDATE issue SET status=$2 WHERE id=$1`, fx.issueID, customStatus)
	revision, _ := fx.issueVersion(t)
	fx.complete(t, map[string]any{
		"output":           "verified final",
		"claim_generation": fx.claimGeneration,
		"issue_completion": map[string]any{
			"version": 1, "outcome": "review_ready", "comment": "verified final",
			"base_revision": revision, "base_etag": issueTaskSnapshotETag(fx.issueID, revision), "base_status": customStatus,
		},
	}).Want(http.StatusOK)
	_, status := fx.issueVersion(t)
	if status != "in_review" {
		t.Fatalf("issue status = %q, want in_review", status)
	}
}

func TestCompleteTaskDeliveredDoesNotRequireRecognizedIssueStatus(t *testing.T) {
	fx := newIssueCompletionFixture(t, "unknown legacy status", false)
	dbfx.Exec(t, `UPDATE issue SET status='legacy_unknown_status' WHERE id=$1`, fx.issueID)
	fx.complete(t, fx.deliveredBody("delivered without status mutation")).Want(http.StatusOK)
	_, status := fx.issueVersion(t)
	if status != "legacy_unknown_status" {
		t.Fatalf("issue status = %q, want legacy_unknown_status", status)
	}
}

func TestCompleteTaskTypedIssueCompletionProgressDoesNotSuppressFinalAndReplayIsIdempotent(t *testing.T) {
	fx := newIssueCompletionFixture(t, "progress then final", false)
	dbfx.Exec(t, `INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, source_task_id)
		VALUES ($1, $2, 'agent', $3, 'working', 'progress_update', $4)`, fx.issueID, testWorkspaceID, fx.agentID, fx.taskID)
	body := map[string]any{
		"output":           "final result",
		"claim_generation": fx.claimGeneration,
		"issue_completion": map[string]any{"version": 1, "outcome": "delivered", "comment": "final result"},
	}
	fx.complete(t, body).Want(http.StatusOK)
	fx.complete(t, body).Want(http.StatusOK)
	var finals int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND author_id=$2 AND source_task_id=$3 AND type='comment' AND content='final result'`, fx.issueID, fx.agentID, fx.taskID).Scan(&finals)
	if finals != 1 {
		t.Fatalf("final comments = %d, want 1", finals)
	}
}

func TestCompleteTaskTypedIssueCompletionReusesIdenticalCLIComment(t *testing.T) {
	fx := newIssueCompletionFixture(t, "cli dedupe", true)
	fx.createComment(t, "already delivered").Want(http.StatusCreated)
	fx.complete(t, map[string]any{
		"output":           "already delivered",
		"claim_generation": fx.claimGeneration,
		"issue_completion": map[string]any{"version": 1, "outcome": "delivered", "comment": "already delivered"},
	}).Want(http.StatusOK)
	var finals int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND source_task_id=$2 AND type='comment' AND content='already delivered'`, fx.issueID, fx.taskID).Scan(&finals)
	if finals != 1 {
		t.Fatalf("identical final comments = %d, want 1", finals)
	}
}

func TestTypedIssueCompletionDedupesCanonicalEquivalentCLIComment(t *testing.T) {
	for _, tc := range []struct {
		name            string
		completeFirst   bool
		cliContent      string
		completionFinal string
	}{
		{name: "CLI first escaped newline", cliContent: `line one\nline two`, completionFinal: `line one\nline two`},
		{name: "completion first escaped newline", completeFirst: true, cliContent: `line one\nline two`, completionFinal: `line one\nline two`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newIssueCompletionFixture(t, tc.name, false)
			if tc.completeFirst {
				fx.complete(t, fx.deliveredBody(tc.completionFinal)).Want(http.StatusOK)
				fx.createComment(t, tc.cliContent).Want(http.StatusCreated)
			} else {
				fx.createComment(t, tc.cliContent).Want(http.StatusCreated)
				fx.complete(t, fx.deliveredBody(tc.completionFinal)).Want(http.StatusOK)
			}
			var finals int
			dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND source_task_id=$2 AND type='comment'`, fx.issueID, fx.taskID).Scan(&finals)
			if finals != 1 {
				t.Fatalf("canonical-equivalent final comments = %d, want 1", finals)
			}
		})
	}
}

func TestCreateCommentTypedIssueCompletionReturnsExistingFinalAfterCompletion(t *testing.T) {
	fx := newIssueCompletionFixture(t, "completion first dedupe", false)
	fx.complete(t, fx.deliveredBody("completion won")).Want(http.StatusOK)

	var existingID string
	var revisionBefore int64
	dbfx.QueryRow(t, `SELECT id FROM comment WHERE issue_id=$1 AND source_task_id=$2 AND type='comment' AND content='completion won'`, fx.issueID, fx.taskID).Scan(&existingID)
	dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, fx.issueID).Scan(&revisionBefore)

	w := fx.createComment(t, "completion won").Want(http.StatusCreated)
	var response CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ID != existingID {
		t.Fatalf("late identical comment id = %s, want existing %s", response.ID, existingID)
	}
	var revisionAfter int64
	dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, fx.issueID).Scan(&revisionAfter)
	if revisionAfter != revisionBefore {
		t.Fatalf("late identical comment changed issue revision from %d to %d", revisionBefore, revisionAfter)
	}
}

func TestCreateCommentTypedIssueCompletionRejectsDifferentLateComment(t *testing.T) {
	fx := newIssueCompletionFixture(t, "late different", false)
	fx.complete(t, fx.deliveredBody("canonical final")).Want(http.StatusOK)
	fx.createComment(t, "different late final").Want(http.StatusConflict)
}

func TestTypedIssueCompletionAndCLICommentConcurrentCreateOneFinal(t *testing.T) {
	fx := newIssueCompletionFixture(t, "concurrent final", false)
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		statuses <- fx.complete(t, fx.deliveredBody("racing final")).Code
	}()
	go func() {
		defer wg.Done()
		<-start
		statuses <- fx.createComment(t, "racing final").Code
	}()
	close(start)
	wg.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK && status != http.StatusCreated {
			t.Fatalf("concurrent terminal status = %d, want 200 or 201", status)
		}
	}
	var finals int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND source_task_id=$2 AND type='comment' AND content='racing final'`, fx.issueID, fx.taskID).Scan(&finals)
	if finals != 1 {
		t.Fatalf("concurrent final comments = %d, want 1", finals)
	}
}

func TestCompleteTaskTypedIssueCompletionDeliversOncePerDistinctThread(t *testing.T) {
	fx := newIssueCompletionFixture(t, "multiple threads", true)
	otherRoot := dbfx.Comment(t, fx.issueID, "second request")
	dbfx.Exec(t, `UPDATE agent_task_queue SET coalesced_comment_ids=ARRAY[$1::uuid] WHERE id=$2`, otherRoot, fx.taskID)
	fx.complete(t, map[string]any{
		"output":           "one consolidated final",
		"claim_generation": fx.claimGeneration,
		"issue_completion": map[string]any{"version": 1, "outcome": "delivered", "comment": "one consolidated final"},
	}).Want(http.StatusOK)
	rows, err := testPool.Query(context.Background(), `SELECT parent_id::text FROM comment WHERE issue_id=$1 AND source_task_id=$2 AND type='comment' AND content='one consolidated final' ORDER BY parent_id`, fx.issueID, fx.taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	parents := map[string]bool{}
	for rows.Next() {
		var parent string
		if err := rows.Scan(&parent); err != nil {
			t.Fatal(err)
		}
		parents[parent] = true
	}
	if len(parents) != 2 || !parents[fx.triggerID] || !parents[otherRoot] {
		t.Fatalf("final parents = %#v, want trigger %s and root %s", parents, fx.triggerID, otherRoot)
	}
}

func TestCompleteTaskTypedIssueCompletionGuardsReviewStatusByRevision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate bool
		want   string
	}{{"matching", false, "in_review"}, {"human changed", true, "in_progress"}} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newIssueCompletionFixture(t, tc.name, false)
			baseRevision, baseStatus := fx.issueVersion(t)
			if tc.mutate {
				dbfx.Exec(t, `UPDATE issue SET revision=revision+1 WHERE id=$1`, fx.issueID)
			}
			fx.complete(t, map[string]any{
				"output":           "ready",
				"claim_generation": fx.claimGeneration,
				"issue_completion": map[string]any{
					"version": 1, "outcome": "review_ready", "comment": "ready",
					"base_revision": baseRevision, "base_etag": issueTaskSnapshotETag(fx.issueID, baseRevision), "base_status": baseStatus,
				},
			}).Want(http.StatusOK)
			var status string
			dbfx.QueryRow(t, `SELECT status FROM issue WHERE id=$1`, fx.issueID).Scan(&status)
			if status != tc.want {
				t.Fatalf("status = %q, want %q", status, tc.want)
			}
		})
	}
}

func TestCompleteTaskTypedIssueCompletionAcceptsAcknowledgedPendingInputRevisions(t *testing.T) {
	fx := newIssueCompletionFixture(t, "acknowledged pending input", false)
	baseRevision, baseStatus := fx.issueVersion(t)
	pending := createAnsweredPendingInput(t, fx, baseRevision)
	pending.ack(t)

	body := map[string]any{
		"output":           "ready after clarification",
		"claim_generation": fx.claimGeneration,
		"issue_completion": map[string]any{
			"version": 1, "outcome": "review_ready", "comment": "ready after clarification",
			"base_revision": baseRevision, "base_etag": issueTaskSnapshotETag(fx.issueID, baseRevision), "base_status": baseStatus,
		},
	}
	fx.complete(t, body).Want(http.StatusOK)
	fx.complete(t, body).Want(http.StatusOK)
	_, status := fx.issueVersion(t)
	if status != "in_review" {
		t.Fatalf("issue status = %q, want in_review", status)
	}
	var finals int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND source_task_id=$2 AND content='ready after clarification'`, fx.issueID, fx.taskID).Scan(&finals)
	if finals != 1 {
		t.Fatalf("final comments = %d, want 1", finals)
	}
}

func TestCompleteTaskTypedIssueCompletionAcceptsInterleavedPendingInputRevisions(t *testing.T) {
	fx := newIssueCompletionFixture(t, "interleaved pending input", false)
	baseRevision, baseStatus := fx.issueVersion(t)
	daemonID := "typed-completion-interleaved-" + uuid.NewString()
	var runtimeID string
	var originalDaemonID *string
	dbfx.QueryRow(t, `
		SELECT agent.runtime_id::text, runtime.daemon_id
		FROM agent
		JOIN agent_runtime AS runtime ON runtime.id=agent.runtime_id
		WHERE agent.id=$1
	`, fx.agentID).Scan(&runtimeID, &originalDaemonID)
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id=$2 WHERE id=$1`, runtimeID, daemonID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_pending_input WHERE task_id=$1`, fx.taskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id=$1`, fx.issueID)
		_, _ = testPool.Exec(context.Background(), `UPDATE agent_runtime SET daemon_id=$2 WHERE id=$1`, runtimeID, originalDaemonID)
	})

	basePath := "/api/daemon/runtimes/" + runtimeID + "/tasks/" + fx.taskID + "/pending-inputs"
	register := func(keyByte string) taskPendingInputResponse {
		request := validPendingInputRegistration()
		request.ClaimGeneration = fx.claimGeneration
		request.RequestKey = "sha256:" + strings.Repeat(keyByte, 64)
		var pending taskPendingInputResponse
		testutil.Call(t, testHandler.RegisterTaskPendingInput, pendingInputDaemonRequest(
			http.MethodPost, basePath, request, runtimeID, fx.taskID, "", testWorkspaceID, daemonID,
		)).Want(http.StatusCreated).JSON(&pending)
		return pending
	}
	answer := func(pending taskPendingInputResponse) acknowledgedPendingInput {
		body := answerTaskPendingInputRequest{
			IdempotencyKey: uuid.NewString(),
			Answers: map[string]taskPendingInputAnswer{
				"deployment": {Answers: []string{"Staging"}},
				"notes":      {Answers: []string{"Mention the migration."}},
			},
		}
		testutil.Call(t, testHandler.AnswerTaskPendingInput, pendingInputIssueRequest(
			http.MethodPost, "/api/issues/"+fx.issueID+"/pending-inputs/"+pending.ID+"/answer", body, fx.issueID, pending.ID,
		)).Want(http.StatusOK)
		return acknowledgedPendingInput{
			id: pending.ID, runtimeID: runtimeID, daemonID: daemonID,
			taskID: fx.taskID, claimGeneration: fx.claimGeneration,
		}
	}

	first := register("c")
	// Model two concurrently outstanding native questions. The server currently
	// serializes registration, so temporarily close the first row only to create
	// the persisted Q1,Q2,A2,A1 receipt order exercised by the completion guard.
	dbfx.Exec(t, `UPDATE task_pending_input SET state='answered' WHERE id=$1`, first.ID)
	second := register("d")
	secondAck := answer(second)
	secondAck.ack(t)
	dbfx.Exec(t, `UPDATE task_pending_input SET state='open' WHERE id=$1`, first.ID)
	firstAck := answer(first)
	firstAck.ack(t)

	var firstQuestion, firstAnswer, secondQuestion, secondAnswer int64
	dbfx.QueryRow(t, `SELECT question_issue_revision, answer_issue_revision FROM task_pending_input WHERE id=$1`, first.ID).
		Scan(&firstQuestion, &firstAnswer)
	dbfx.QueryRow(t, `SELECT question_issue_revision, answer_issue_revision FROM task_pending_input WHERE id=$1`, second.ID).
		Scan(&secondQuestion, &secondAnswer)
	ordered := []int64{firstQuestion, secondQuestion, secondAnswer, firstAnswer}
	if !slices.Equal(ordered, []int64{baseRevision + 1, baseRevision + 2, baseRevision + 3, baseRevision + 4}) {
		t.Fatalf("interleaved attested revisions = %v", ordered)
	}

	fx.complete(t, map[string]any{
		"output":           "ready after interleaved clarification",
		"claim_generation": fx.claimGeneration,
		"issue_completion": map[string]any{
			"version": 1, "outcome": "review_ready", "comment": "ready after interleaved clarification",
			"base_revision": baseRevision, "base_etag": issueTaskSnapshotETag(fx.issueID, baseRevision), "base_status": baseStatus,
		},
	}).Want(http.StatusOK)
	_, status := fx.issueVersion(t)
	if status != "in_review" {
		t.Fatalf("issue status = %q, want in_review", status)
	}
}

func TestCompleteTaskTypedIssueCompletionRejectsUnattestedPendingInputRevisionDrift(t *testing.T) {
	tests := []struct {
		name       string
		skipAck    bool
		wantStatus string
		mutate     func(*testing.T, issueCompletionFixture, acknowledgedPendingInput)
		check      func(*testing.T, issueCompletionFixture)
	}{
		{name: "unacknowledged answer", skipAck: true, mutate: func(*testing.T, issueCompletionFixture, acknowledgedPendingInput) {}},
		{
			name: "ordinary member comment",
			mutate: func(t *testing.T, fx issueCompletionFixture, _ acknowledgedPendingInput) {
				testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost,
					"/api/issues/"+fx.issueID+"/comments", map[string]any{
						"content": "human follow-up", "suppress_agent_ids": []string{fx.agentID},
					}), "id", fx.issueID)).Want(http.StatusCreated)
			},
			check: func(t *testing.T, fx issueCompletionFixture) {
				var count int
				dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id=$1 AND author_type='member' AND content='human follow-up'`, fx.issueID).Scan(&count)
				if count != 1 {
					t.Fatalf("member comment count = %d, want 1", count)
				}
			},
		},
		{
			name: "description edit",
			mutate: func(t *testing.T, fx issueCompletionFixture, _ acknowledgedPendingInput) {
				dbfx.Exec(t, `UPDATE issue SET description='human description', revision=revision+1 WHERE id=$1`, fx.issueID)
			},
			check: func(t *testing.T, fx issueCompletionFixture) {
				var description string
				dbfx.QueryRow(t, `SELECT description FROM issue WHERE id=$1`, fx.issueID).Scan(&description)
				if description != "human description" {
					t.Fatalf("description = %q, want human description", description)
				}
			},
		},
		{
			name:       "status edit",
			wantStatus: "todo",
			mutate: func(t *testing.T, fx issueCompletionFixture, _ acknowledgedPendingInput) {
				dbfx.Exec(t, `UPDATE issue SET status='todo', revision=revision+1 WHERE id=$1`, fx.issueID)
			},
			check: func(t *testing.T, fx issueCompletionFixture) {
				_, status := fx.issueVersion(t)
				if status != "todo" {
					t.Fatalf("status = %q, want todo", status)
				}
			},
		},
		{
			name: "assignee edit",
			mutate: func(t *testing.T, fx issueCompletionFixture, _ acknowledgedPendingInput) {
				dbfx.Exec(t, `UPDATE issue SET assignee_type='member', assignee_id=$2, revision=revision+1 WHERE id=$1`, fx.issueID, testUserID)
			},
			check: func(t *testing.T, fx issueCompletionFixture) {
				var assigneeType, assigneeID string
				dbfx.QueryRow(t, `SELECT assignee_type, assignee_id::text FROM issue WHERE id=$1`, fx.issueID).Scan(&assigneeType, &assigneeID)
				if assigneeType != "member" || assigneeID != testUserID {
					t.Fatalf("assignee = %s/%s, want member/%s", assigneeType, assigneeID, testUserID)
				}
			},
		},
		{
			name: "question comment edit",
			mutate: func(t *testing.T, _ issueCompletionFixture, pending acknowledgedPendingInput) {
				testutil.Call(t, testHandler.UpdateComment, withURLParam(newRequest(http.MethodPut,
					"/api/comments/"+pending.questionCommentID, map[string]any{"content": "edited question"}),
					"commentId", pending.questionCommentID)).Want(http.StatusOK)
			},
		},
		{
			name: "answer comment edit",
			mutate: func(t *testing.T, _ issueCompletionFixture, pending acknowledgedPendingInput) {
				testutil.Call(t, testHandler.UpdateComment, withURLParam(newRequest(http.MethodPut,
					"/api/comments/"+pending.answerCommentID, map[string]any{"content": "edited answer"}),
					"commentId", pending.answerCommentID)).Want(http.StatusOK)
			},
		},
		{
			name: "question comment delete",
			mutate: func(t *testing.T, _ issueCompletionFixture, pending acknowledgedPendingInput) {
				testutil.Call(t, testHandler.DeleteComment, withURLParam(newRequest(http.MethodDelete,
					"/api/comments/"+pending.questionCommentID, nil), "commentId", pending.questionCommentID)).Want(http.StatusNoContent)
			},
		},
		{
			name: "answer comment delete",
			mutate: func(t *testing.T, _ issueCompletionFixture, pending acknowledgedPendingInput) {
				testutil.Call(t, testHandler.DeleteComment, withURLParam(newRequest(http.MethodDelete,
					"/api/comments/"+pending.answerCommentID, nil), "commentId", pending.answerCommentID)).Want(http.StatusNoContent)
			},
		},
		{
			name: "legacy missing revision",
			mutate: func(t *testing.T, _ issueCompletionFixture, pending acknowledgedPendingInput) {
				dbfx.Exec(t, `UPDATE task_pending_input SET question_issue_revision=NULL, answer_issue_revision=NULL WHERE id=$1`, pending.id)
			},
		},
		{
			name: "wrong claim receipt",
			mutate: func(t *testing.T, _ issueCompletionFixture, pending acknowledgedPendingInput) {
				dbfx.Exec(t, `UPDATE task_pending_input SET claim_generation=claim_generation+1 WHERE id=$1`, pending.id)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := newIssueCompletionFixture(t, tc.name, false)
			baseRevision, baseStatus := fx.issueVersion(t)
			pending := createAnsweredPendingInput(t, fx, baseRevision)
			if !tc.skipAck {
				pending.ack(t)
			}
			tc.mutate(t, fx, pending)
			fx.complete(t, map[string]any{
				"output":           "guarded final " + tc.name,
				"claim_generation": fx.claimGeneration,
				"issue_completion": map[string]any{
					"version": 1, "outcome": "review_ready", "comment": "guarded final " + tc.name,
					"base_revision": baseRevision, "base_etag": issueTaskSnapshotETag(fx.issueID, baseRevision), "base_status": baseStatus,
				},
			}).Want(http.StatusOK)
			wantStatus := tc.wantStatus
			if wantStatus == "" {
				wantStatus = "in_progress"
			}
			_, status := fx.issueVersion(t)
			if status != wantStatus {
				t.Fatalf("status = %q, want %q", status, wantStatus)
			}
			if tc.check != nil {
				tc.check(t, fx)
			}
		})
	}
}

type acknowledgedPendingInput struct {
	id, questionCommentID, answerCommentID string
	runtimeID, daemonID                    string
	taskID                                 string
	claimGeneration                        int64
}

func createAnsweredPendingInput(t *testing.T, fx issueCompletionFixture, baseRevision int64) acknowledgedPendingInput {
	t.Helper()
	daemonID := "typed-completion-pending-input-" + uuid.NewString()
	var runtimeID string
	var originalDaemonID *string
	dbfx.QueryRow(t, `
		SELECT agent.runtime_id::text, runtime.daemon_id
		FROM agent
		JOIN agent_runtime AS runtime ON runtime.id=agent.runtime_id
		WHERE agent.id=$1
	`, fx.agentID).Scan(&runtimeID, &originalDaemonID)
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id=$2 WHERE id=$1`, runtimeID, daemonID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_pending_input WHERE task_id=$1`, fx.taskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id=$1`, fx.issueID)
		_, _ = testPool.Exec(context.Background(), `UPDATE agent_runtime SET daemon_id=$2 WHERE id=$1`, runtimeID, originalDaemonID)
	})

	registration := validPendingInputRegistration()
	registration.ClaimGeneration = fx.claimGeneration
	basePath := "/api/daemon/runtimes/" + runtimeID + "/tasks/" + fx.taskID + "/pending-inputs"
	var created taskPendingInputResponse
	testutil.Call(t, testHandler.RegisterTaskPendingInput, pendingInputDaemonRequest(
		http.MethodPost, basePath, registration, runtimeID, fx.taskID, "", testWorkspaceID, daemonID,
	)).Want(http.StatusCreated).JSON(&created)
	answer := answerTaskPendingInputRequest{
		IdempotencyKey: uuid.NewString(),
		Answers: map[string]taskPendingInputAnswer{
			"deployment": {Answers: []string{"Staging"}},
			"notes":      {Answers: []string{"Mention the migration."}},
		},
	}
	testutil.Call(t, testHandler.AnswerTaskPendingInput, pendingInputIssueRequest(
		http.MethodPost, "/api/issues/"+fx.issueID+"/pending-inputs/"+created.ID+"/answer", answer, fx.issueID, created.ID,
	)).Want(http.StatusOK)
	var questionRevision, answerRevision int64
	var answerCommentID string
	dbfx.QueryRow(t, `SELECT question_issue_revision, answer_issue_revision, answer_comment_id::text FROM task_pending_input WHERE id=$1`, created.ID).
		Scan(&questionRevision, &answerRevision, &answerCommentID)
	if questionRevision != baseRevision+1 || answerRevision != baseRevision+2 {
		t.Fatalf("pending input revisions = %d/%d, want %d/%d", questionRevision, answerRevision, baseRevision+1, baseRevision+2)
	}
	pending := acknowledgedPendingInput{
		id: created.ID, questionCommentID: created.QuestionCommentID, answerCommentID: answerCommentID,
		runtimeID: runtimeID, daemonID: daemonID,
		taskID: fx.taskID, claimGeneration: fx.claimGeneration,
	}
	return pending
}

func (p acknowledgedPendingInput) ack(t *testing.T) {
	t.Helper()
	path := "/api/daemon/runtimes/" + p.runtimeID + "/tasks/" + p.taskID + "/pending-inputs/" + p.id + "/ack"
	testutil.Call(t, testHandler.AckTaskPendingInput, pendingInputDaemonRequest(
		http.MethodPost, path, map[string]any{"claim_generation": p.claimGeneration},
		p.runtimeID, p.taskID, p.id, testWorkspaceID, p.daemonID,
	)).Want(http.StatusOK)
}

type issueCompletionFixture struct {
	taskID, issueID, agentID, triggerID string
	claimGeneration                     int64
}

func newIssueCompletionFixture(t *testing.T, title string, triggered bool) issueCompletionFixture {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	var agentID, runtimeID string
	dbfx.QueryRow(t, `SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id=$1 LIMIT 1`, testWorkspaceID).Scan(&agentID, &runtimeID)
	issueID := dbfx.Issue(t, "typed completion "+title, testutil.Cols{
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	triggerID := ""
	cols := testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "running", "started_at": testutil.Raw("now()"), "dispatched_at": testutil.Raw("clock_timestamp()")}
	if triggered {
		triggerID = dbfx.Comment(t, issueID, "please finish")
		cols["trigger_comment_id"] = triggerID
	}
	taskID := dbfx.Task(t, agentID, cols)
	var claimGeneration int64
	dbfx.QueryRow(t, `SELECT floor(extract(epoch from dispatched_at)*1000000)::bigint FROM agent_task_queue WHERE id=$1`, taskID).Scan(&claimGeneration)
	return issueCompletionFixture{taskID: taskID, issueID: issueID, agentID: agentID, triggerID: triggerID, claimGeneration: claimGeneration}
}

func (f issueCompletionFixture) complete(t *testing.T, body map[string]any) *testutil.Response {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+f.taskID+"/complete", body, testWorkspaceID, "typed-completion")
	req = withURLParam(req, "taskId", f.taskID)
	return testutil.Call(t, testHandler.CompleteTask, req)
}

func (f issueCompletionFixture) deliveredBody(comment string) map[string]any {
	return map[string]any{
		"output":           comment,
		"claim_generation": f.claimGeneration,
		"issue_completion": map[string]any{"version": 1, "outcome": "delivered", "comment": comment},
	}
}

func (f issueCompletionFixture) createComment(t *testing.T, content string) *testutil.Response {
	t.Helper()
	body := map[string]any{"content": content}
	if f.triggerID != "" {
		body["parent_id"] = f.triggerID
	}
	req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/comments", body), "id", f.issueID)
	req.Header.Set("X-Agent-ID", f.agentID)
	req.Header.Set("X-Task-ID", f.taskID)
	return testutil.Call(t, testHandler.CreateComment, req)
}

func (f issueCompletionFixture) issueVersion(t *testing.T) (int64, string) {
	t.Helper()
	var revision int64
	var status string
	dbfx.QueryRow(t, `SELECT revision,status FROM issue WHERE id=$1`, f.issueID).Scan(&revision, &status)
	return revision, status
}

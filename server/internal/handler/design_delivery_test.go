package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/entitlement"
)

func createDesignDeliveryIssueForTest(t *testing.T, title, status, parentID, projectID string) string {
	t.Helper()

	ctx := context.Background()
	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	var parentArg any
	if parentID != "" {
		parentArg = parentID
	}
	var projectArg any
	if projectID != "" {
		projectArg = projectID
	}
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, parent_issue_id, project_id, number)
		VALUES ($1, $2, $3, 'medium', 'member', $4, $5, $6, $7)
		RETURNING id
	`, testWorkspaceID, title, status, testUserID, parentArg, projectArg, number).Scan(&id); err != nil {
		t.Fatalf("insert design delivery issue: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

func createDesignDeliveryRequestBody(sourceIssueID, targetIssueID, fileID, revisionID, frameID string) map[string]any {
	return map[string]any{
		"source_issue_id": sourceIssueID,
		"target_issue_id": targetIssueID,
		"file_id":         fileID,
		"revision_id":     revisionID,
		"scope": map[string]any{
			"version":         "1.0",
			"source":          "issue_delivery",
			"source_type":     "raw_design_revision",
			"fallback_policy": "frontend_full_restore_fallback",
			"sourceIssueId":   sourceIssueID,
			"targetIssueId":   targetIssueID,
			"items": []map[string]any{{
				"itemId":       "delivery-" + frameID,
				"order":        1,
				"designFileId": fileID,
				"revisionId":   revisionID,
				"frameId":      frameID,
				"frameName":    "Main Screen",
				"source":       "frame",
			}},
		},
	}
}

func TestCreateDesignDeliveryPromotesTargetAndSupersedesPrevious(t *testing.T) {
	created := createDesignFileForTest(t, "Design Delivery API Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Design Delivery API Project")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, created.File.ID); err != nil {
		t.Fatalf("attach design file to project: %v", err)
	}
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "UI设计", "todo", parentID, projectID)
	frontendIssueID := createDesignDeliveryIssueForTest(t, "前端开发", "backlog", parentID, projectID)

	firstW := httptest.NewRecorder()
	firstReq := newRequest("POST", "/api/design-deliveries?workspace_id="+testWorkspaceID, createDesignDeliveryRequestBody(uiIssueID, frontendIssueID, created.File.ID, created.CurrentRevision.ID, "frame-main"))
	testHandler.CreateDesignDelivery(firstW, firstReq)
	if firstW.Code != http.StatusCreated {
		t.Fatalf("first CreateDesignDelivery: expected 201, got %d: %s", firstW.Code, firstW.Body.String())
	}
	var first DesignDeliveryResponse
	if err := json.NewDecoder(firstW.Body).Decode(&first); err != nil {
		t.Fatalf("decode first delivery: %v", err)
	}
	if first.Status != "active" {
		t.Fatalf("first status = %q, want active", first.Status)
	}
	if first.ProjectID == nil || *first.ProjectID != projectID {
		t.Fatalf("first project_id = %v, want %s", first.ProjectID, projectID)
	}
	var firstScope map[string]any
	if err := json.Unmarshal(first.Scope, &firstScope); err != nil {
		t.Fatalf("decode first scope: %v", err)
	}
	if firstScope["source_type"] != "raw_design_revision" || firstScope["fallback_policy"] != "frontend_full_restore_fallback" {
		t.Fatalf("first scope handoff metadata = %#v, want raw design fallback", firstScope)
	}

	var frontendStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, frontendIssueID).Scan(&frontendStatus); err != nil {
		t.Fatalf("load frontend issue status: %v", err)
	}
	if frontendStatus != "todo" {
		t.Fatalf("frontend issue status = %q, want todo", frontendStatus)
	}

	secondW := httptest.NewRecorder()
	secondReq := newRequest("POST", "/api/design-deliveries?workspace_id="+testWorkspaceID, createDesignDeliveryRequestBody(uiIssueID, frontendIssueID, created.File.ID, created.CurrentRevision.ID, "frame-secondary"))
	testHandler.CreateDesignDelivery(secondW, secondReq)
	if secondW.Code != http.StatusCreated {
		t.Fatalf("second CreateDesignDelivery: expected 201, got %d: %s", secondW.Code, secondW.Body.String())
	}
	var second DesignDeliveryResponse
	if err := json.NewDecoder(secondW.Body).Decode(&second); err != nil {
		t.Fatalf("decode second delivery: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("second delivery reused first id %s", first.ID)
	}

	var activeCount, supersededCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE status = 'active'),
			count(*) FILTER (WHERE status = 'superseded')
		FROM design_delivery
		WHERE source_issue_id = $1 AND target_issue_id = $2
	`, uiIssueID, frontendIssueID).Scan(&activeCount, &supersededCount); err != nil {
		t.Fatalf("count design deliveries: %v", err)
	}
	if activeCount != 1 || supersededCount != 1 {
		t.Fatalf("delivery counts active=%d superseded=%d, want 1/1", activeCount, supersededCount)
	}

	listW := httptest.NewRecorder()
	testHandler.ListDesignDeliveries(listW, newRequest("GET", "/api/design-deliveries?workspace_id="+testWorkspaceID+"&issue_id="+frontendIssueID, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignDeliveries: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResp DesignDeliveryListResponse
	if err := json.NewDecoder(listW.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode delivery list: %v", err)
	}
	if len(listResp.Deliveries) != 2 {
		t.Fatalf("delivery list length = %d, want 2", len(listResp.Deliveries))
	}
	if listResp.Deliveries[0].Status != "active" {
		t.Fatalf("first listed delivery status = %q, want active", listResp.Deliveries[0].Status)
	}
	var listedScope map[string]any
	if err := json.Unmarshal(listResp.Deliveries[0].Scope, &listedScope); err != nil {
		t.Fatalf("decode listed scope: %v", err)
	}
	if listedScope["source_type"] != "raw_design_revision" || listedScope["fallback_policy"] != "frontend_full_restore_fallback" {
		t.Fatalf("listed scope handoff metadata = %#v, want raw design fallback", listedScope)
	}
}

func TestListDesignDeliveriesFiltersHiddenLinkedIssue(t *testing.T) {
	created := createDesignFileForTest(t, "Design Delivery Issue Window Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Design Delivery Issue Window Project")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, created.File.ID); err != nil {
		t.Fatalf("attach design file to project: %v", err)
	}
	targetIssueID := createDesignDeliveryIssueForTest(t, "hidden delivery target", "todo", "", projectID)
	sourceIssueID := createDesignDeliveryIssueForTest(t, "visible delivery source", "todo", "", projectID)

	createW := httptest.NewRecorder()
	createReq := newRequest("POST", "/api/design-deliveries?workspace_id="+testWorkspaceID, createDesignDeliveryRequestBody(sourceIssueID, targetIssueID, created.File.ID, created.CurrentRevision.ID, "frame-window"))
	testHandler.CreateDesignDelivery(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignDelivery: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}

	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)
	listW := httptest.NewRecorder()
	listReq := newRequest("GET", "/api/design-deliveries?workspace_id="+testWorkspaceID+"&issue_id="+sourceIssueID, nil)
	h.ListDesignDeliveries(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignDeliveries: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResp DesignDeliveryListResponse
	if err := json.NewDecoder(listW.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode delivery list: %v", err)
	}
	if len(listResp.Deliveries) != 0 {
		t.Fatalf("delivery list exposed hidden linked issue: %#v", listResp.Deliveries)
	}
}

func TestCreateDesignDeliverySupersedesPreviousTargetForSourceIssue(t *testing.T) {
	created := createDesignFileForTest(t, "Design Delivery Switch Target Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Design Delivery Switch Target Project")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, created.File.ID); err != nil {
		t.Fatalf("attach design file to project: %v", err)
	}
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "UI设计", "todo", parentID, projectID)
	frontendIssueAID := createDesignDeliveryIssueForTest(t, "前端开发 A", "todo", parentID, projectID)
	frontendIssueBID := createDesignDeliveryIssueForTest(t, "前端开发 B", "backlog", parentID, projectID)

	firstW := httptest.NewRecorder()
	firstReq := newRequest("POST", "/api/design-deliveries?workspace_id="+testWorkspaceID, createDesignDeliveryRequestBody(uiIssueID, frontendIssueAID, created.File.ID, created.CurrentRevision.ID, "frame-a"))
	testHandler.CreateDesignDelivery(firstW, firstReq)
	if firstW.Code != http.StatusCreated {
		t.Fatalf("first CreateDesignDelivery: expected 201, got %d: %s", firstW.Code, firstW.Body.String())
	}

	secondW := httptest.NewRecorder()
	secondReq := newRequest("POST", "/api/design-deliveries?workspace_id="+testWorkspaceID, createDesignDeliveryRequestBody(uiIssueID, frontendIssueBID, created.File.ID, created.CurrentRevision.ID, "frame-b"))
	testHandler.CreateDesignDelivery(secondW, secondReq)
	if secondW.Code != http.StatusCreated {
		t.Fatalf("second CreateDesignDelivery: expected 201, got %d: %s", secondW.Code, secondW.Body.String())
	}
	var second DesignDeliveryResponse
	if err := json.NewDecoder(secondW.Body).Decode(&second); err != nil {
		t.Fatalf("decode second delivery: %v", err)
	}

	var activeCount, supersededCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE status = 'active'),
			count(*) FILTER (WHERE status = 'superseded')
		FROM design_delivery
		WHERE source_issue_id = $1
	`, uiIssueID).Scan(&activeCount, &supersededCount); err != nil {
		t.Fatalf("count source design deliveries: %v", err)
	}
	if activeCount != 1 || supersededCount != 1 {
		t.Fatalf("source delivery counts active=%d superseded=%d, want 1/1", activeCount, supersededCount)
	}

	var activeTargetID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT target_issue_id FROM design_delivery
		WHERE source_issue_id = $1 AND status = 'active'
	`, uiIssueID).Scan(&activeTargetID); err != nil {
		t.Fatalf("load active delivery target: %v", err)
	}
	if activeTargetID != frontendIssueBID {
		t.Fatalf("active target = %s, want %s", activeTargetID, frontendIssueBID)
	}

	var targetAActiveCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM design_delivery
		WHERE source_issue_id = $1 AND target_issue_id = $2 AND status = 'active'
	`, uiIssueID, frontendIssueAID).Scan(&targetAActiveCount); err != nil {
		t.Fatalf("count target A active deliveries: %v", err)
	}
	if targetAActiveCount != 0 {
		t.Fatalf("target A active delivery count = %d, want 0", targetAActiveCount)
	}

	var supersededAuditRaw []byte
	if err := testPool.QueryRow(context.Background(), `
		SELECT audit_metadata FROM design_delivery
		WHERE source_issue_id = $1 AND target_issue_id = $2 AND status = 'superseded'
	`, uiIssueID, frontendIssueAID).Scan(&supersededAuditRaw); err != nil {
		t.Fatalf("load superseded delivery audit metadata: %v", err)
	}
	var supersededAudit map[string]any
	if err := json.Unmarshal(supersededAuditRaw, &supersededAudit); err != nil {
		t.Fatalf("decode superseded audit metadata: %v", err)
	}
	if supersededAudit["superseded_by_delivery_id"] != second.ID {
		t.Fatalf("superseded_by_delivery_id = %v, want %s", supersededAudit["superseded_by_delivery_id"], second.ID)
	}
	if supersededAudit["superseded_by_target_issue_id"] != frontendIssueBID {
		t.Fatalf("superseded_by_target_issue_id = %v, want %s", supersededAudit["superseded_by_target_issue_id"], frontendIssueBID)
	}
}

func TestCreateDesignRestoreTaskBindsAndReusesDesignDelivery(t *testing.T) {
	created := createDesignFileForTest(t, "Design Delivery Restore Task Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Design Delivery Restore Task Project")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, created.File.ID); err != nil {
		t.Fatalf("attach design file to project: %v", err)
	}
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "UI设计", "todo", parentID, projectID)
	frontendIssueID := createDesignDeliveryIssueForTest(t, "前端开发", "backlog", parentID, projectID)

	deliveryW := httptest.NewRecorder()
	deliveryReq := newRequest("POST", "/api/design-deliveries?workspace_id="+testWorkspaceID, createDesignDeliveryRequestBody(uiIssueID, frontendIssueID, created.File.ID, created.CurrentRevision.ID, "frame-main"))
	testHandler.CreateDesignDelivery(deliveryW, deliveryReq)
	if deliveryW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignDelivery: expected 201, got %d: %s", deliveryW.Code, deliveryW.Body.String())
	}
	var delivery DesignDeliveryResponse
	if err := json.NewDecoder(deliveryW.Body).Decode(&delivery); err != nil {
		t.Fatalf("decode delivery: %v", err)
	}

	body := map[string]any{
		"file_id":     created.File.ID,
		"revision_id": created.CurrentRevision.ID,
		"issue_id":    frontendIssueID,
		"delivery_id": delivery.ID,
		"input": map[string]any{
			"version":       "1.0",
			"projectId":     projectID,
			"sourceIssueId": frontendIssueID,
			"purpose":       "frontend_restore",
			"items": []map[string]any{{
				"itemId":       "delivery-restore-frame",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"source":       "frame",
			}},
		},
	}

	firstW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(firstW, newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, body))
	if firstW.Code != http.StatusCreated {
		t.Fatalf("first CreateDesignRestoreTask: expected 201, got %d: %s", firstW.Code, firstW.Body.String())
	}
	var first DesignRestoreTaskResponse
	if err := json.NewDecoder(firstW.Body).Decode(&first); err != nil {
		t.Fatalf("decode first restore task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, first.ID)
	})
	if first.DeliveryID == nil || *first.DeliveryID != delivery.ID {
		t.Fatalf("delivery_id = %v, want %s", first.DeliveryID, delivery.ID)
	}

	var storedDeliveryID string
	if err := testPool.QueryRow(context.Background(), `SELECT delivery_id::text FROM design_restore_task WHERE id = $1`, first.ID).Scan(&storedDeliveryID); err != nil {
		t.Fatalf("load stored delivery_id: %v", err)
	}
	if storedDeliveryID != delivery.ID {
		t.Fatalf("stored delivery_id = %s, want %s", storedDeliveryID, delivery.ID)
	}

	secondW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(secondW, newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, body))
	if secondW.Code != http.StatusOK {
		t.Fatalf("second CreateDesignRestoreTask: expected 200 reuse, got %d: %s", secondW.Code, secondW.Body.String())
	}
	var second DesignRestoreTaskResponse
	if err := json.NewDecoder(secondW.Body).Decode(&second); err != nil {
		t.Fatalf("decode second restore task: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("reused task id = %s, want %s", second.ID, first.ID)
	}

	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_restore_task WHERE delivery_id = $1`, delivery.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count restore tasks by delivery: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("restore task count = %d, want 1", taskCount)
	}

	mismatchBody := map[string]any{}
	for key, value := range body {
		mismatchBody[key] = value
	}
	mismatchBody["issue_id"] = uiIssueID
	mismatchW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(mismatchW, newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, mismatchBody))
	if mismatchW.Code != http.StatusBadRequest {
		t.Fatalf("mismatched delivery issue: expected 400, got %d: %s", mismatchW.Code, mismatchW.Body.String())
	}
}

func TestCancelDesignDeliveryMarksActiveDeliveryCancelled(t *testing.T) {
	created := createDesignFileForTest(t, "Cancel Design Delivery Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Cancel Design Delivery Project")
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "UI设计", "todo", parentID, projectID)
	frontendIssueID := createDesignDeliveryIssueForTest(t, "前端开发", "todo", parentID, projectID)

	var deliveryID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_delivery (
			workspace_id, project_id, source_issue_id, target_issue_id,
			file_id, revision_id, scope, status, delivered_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, '{"version":"1.0","items":[]}'::jsonb, 'active', $7)
		RETURNING id
	`, testWorkspaceID, projectID, uiIssueID, frontendIssueID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&deliveryID); err != nil {
		t.Fatalf("insert active design delivery: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_delivery WHERE id = $1`, deliveryID)
	})

	cancelW := httptest.NewRecorder()
	cancelReason := "设计稿需要重新确认"
	cancelReq := withURLParam(newRequest("POST", "/api/design-deliveries/"+deliveryID+"/cancel?workspace_id="+testWorkspaceID, map[string]any{"reason": cancelReason}), "id", deliveryID)
	testHandler.CancelDesignDelivery(cancelW, cancelReq)
	if cancelW.Code != http.StatusOK {
		t.Fatalf("CancelDesignDelivery: expected 200, got %d: %s", cancelW.Code, cancelW.Body.String())
	}
	var resp DesignDeliveryResponse
	if err := json.NewDecoder(cancelW.Body).Decode(&resp); err != nil {
		t.Fatalf("decode cancel delivery: %v", err)
	}
	if resp.Status != "cancelled" {
		t.Fatalf("cancelled status = %q, want cancelled", resp.Status)
	}
	if resp.CancelledBy == nil || *resp.CancelledBy != testUserID {
		t.Fatalf("cancelled_by = %v, want %s", resp.CancelledBy, testUserID)
	}
	if resp.CancelledAt == nil || *resp.CancelledAt == "" {
		t.Fatalf("cancelled_at should be set, got %v", resp.CancelledAt)
	}
	if resp.CancelReason == nil || *resp.CancelReason != cancelReason {
		t.Fatalf("cancel_reason = %v, want %q", resp.CancelReason, cancelReason)
	}
	var audit map[string]any
	if err := json.Unmarshal(resp.AuditMetadata, &audit); err != nil {
		t.Fatalf("decode audit metadata: %v", err)
	}
	if audit["cancel_reason"] != cancelReason {
		t.Fatalf("audit cancel_reason = %v, want %q", audit["cancel_reason"], cancelReason)
	}

	var activeCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM design_delivery
		WHERE source_issue_id = $1 AND target_issue_id = $2 AND status = 'active'
	`, uiIssueID, frontendIssueID).Scan(&activeCount); err != nil {
		t.Fatalf("count active deliveries: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("active delivery count = %d, want 0", activeCount)
	}

	var sourceCommentCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND content LIKE '%' || $2 || '%'
	`, uiIssueID, cancelReason).Scan(&sourceCommentCount); err != nil {
		t.Fatalf("count source cancel comments: %v", err)
	}
	if sourceCommentCount == 0 {
		t.Fatalf("expected source issue cancel comment to include reason")
	}

	secondW := httptest.NewRecorder()
	secondReq := withURLParam(newRequest("POST", "/api/design-deliveries/"+deliveryID+"/cancel?workspace_id="+testWorkspaceID, nil), "id", deliveryID)
	testHandler.CancelDesignDelivery(secondW, secondReq)
	if secondW.Code != http.StatusConflict {
		t.Fatalf("second CancelDesignDelivery: expected 409, got %d: %s", secondW.Code, secondW.Body.String())
	}
}

func TestUIDesignDoneRejectsPlainActiveDeliveryWithoutRestoreOrFallback(t *testing.T) {
	created := createDesignFileForTest(t, "UI Done Active Delivery Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "UI Done Active Delivery Project")
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "UI设计", "todo", parentID, projectID)
	frontendIssueID := createDesignDeliveryIssueForTest(t, "前端开发", "backlog", parentID, projectID)

	var deliveryID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_delivery (
			workspace_id, project_id, source_issue_id, target_issue_id,
			file_id, revision_id, scope, status, delivered_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, '{"version":"1.0","items":[]}'::jsonb, 'active', $7)
		RETURNING id
	`, testWorkspaceID, projectID, uiIssueID, frontendIssueID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&deliveryID); err != nil {
		t.Fatalf("insert active design delivery: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_delivery WHERE id = $1`, deliveryID)
	})

	updateW := httptest.NewRecorder()
	updateReq := withURLParam(newRequest("PUT", "/api/issues/"+uiIssueID+"?workspace_id="+testWorkspaceID, map[string]any{"status": "done"}), "id", uiIssueID)
	testHandler.UpdateIssue(updateW, updateReq)
	if updateW.Code != http.StatusConflict {
		t.Fatalf("UpdateIssue UI done with plain active delivery: expected 409, got %d: %s", updateW.Code, updateW.Body.String())
	}

	var uiStatus, frontendStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, uiIssueID).Scan(&uiStatus); err != nil {
		t.Fatalf("load ui issue status: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, frontendIssueID).Scan(&frontendStatus); err != nil {
		t.Fatalf("load frontend issue status: %v", err)
	}
	if uiStatus != "todo" {
		t.Fatalf("ui issue status = %q, want todo", uiStatus)
	}
	if frontendStatus != "backlog" {
		t.Fatalf("frontend issue status = %q, want backlog", frontendStatus)
	}
}

func TestUIDesignDonePromotesFrontendIssueWithRawDesignFallbackDelivery(t *testing.T) {
	created := createDesignFileForTest(t, "UI Done Raw Fallback Delivery Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "UI Done Raw Fallback Delivery Project")
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "UI设计", "todo", parentID, projectID)
	frontendIssueID := createDesignDeliveryIssueForTest(t, "前端开发", "backlog", parentID, projectID)

	var deliveryID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_delivery (
			workspace_id, project_id, source_issue_id, target_issue_id,
			file_id, revision_id, scope, status, delivered_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, '{"version":"1.0","source_type":"raw_design_revision","fallback_policy":"frontend_full_restore_fallback","items":[]}'::jsonb, 'active', $7)
		RETURNING id
	`, testWorkspaceID, projectID, uiIssueID, frontendIssueID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&deliveryID); err != nil {
		t.Fatalf("insert raw fallback design delivery: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_delivery WHERE id = $1`, deliveryID)
	})

	updateW := httptest.NewRecorder()
	updateReq := withURLParam(newRequest("PUT", "/api/issues/"+uiIssueID+"?workspace_id="+testWorkspaceID, map[string]any{"status": "done"}), "id", uiIssueID)
	testHandler.UpdateIssue(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("UpdateIssue UI done: expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}

	var frontendStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, frontendIssueID).Scan(&frontendStatus); err != nil {
		t.Fatalf("load frontend issue status: %v", err)
	}
	if frontendStatus != "todo" {
		t.Fatalf("frontend issue status = %q, want todo", frontendStatus)
	}

	var frontendRestoreTaskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_restore_task WHERE issue_id = $1`, frontendIssueID).Scan(&frontendRestoreTaskCount); err != nil {
		t.Fatalf("count frontend restore tasks: %v", err)
	}
	if frontendRestoreTaskCount != 0 {
		t.Fatalf("frontend restore task count = %d, want 0", frontendRestoreTaskCount)
	}
}

func TestUIDesignDoneRequiresActiveDelivery(t *testing.T) {
	projectID := createProjectForDesignTest(t, "UI Done Requires Delivery Project")
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "体验确认", "todo", parentID, projectID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET metadata = '{"design_role":"ui_design"}'::jsonb
		WHERE id = $1
	`, uiIssueID); err != nil {
		t.Fatalf("mark ui issue role: %v", err)
	}

	updateW := httptest.NewRecorder()
	updateReq := withURLParam(newRequest("PUT", "/api/issues/"+uiIssueID+"?workspace_id="+testWorkspaceID, map[string]any{"status": "done"}), "id", uiIssueID)
	testHandler.UpdateIssue(updateW, updateReq)
	if updateW.Code != http.StatusConflict {
		t.Fatalf("UpdateIssue UI done without delivery: expected 409, got %d: %s", updateW.Code, updateW.Body.String())
	}

	var issueStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, uiIssueID).Scan(&issueStatus); err != nil {
		t.Fatalf("load ui issue status: %v", err)
	}
	if issueStatus != "todo" {
		t.Fatalf("ui issue status = %q, want todo", issueStatus)
	}
	if !strings.Contains(updateW.Body.String(), uiDesignDeliveryRequiredBeforeDoneMessage) {
		t.Fatalf("expected error message %q, got %s", uiDesignDeliveryRequiredBeforeDoneMessage, updateW.Body.String())
	}
}

func TestBatchUIDesignDoneSkipsIssueWithoutActiveDelivery(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Batch UI Done Requires Delivery Project")
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "体验确认", "todo", parentID, projectID)
	plainIssueID := createDesignDeliveryIssueForTest(t, "文案确认", "todo", parentID, projectID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET metadata = '{"design_role":"ui_design"}'::jsonb
		WHERE id = $1
	`, uiIssueID); err != nil {
		t.Fatalf("mark ui issue role: %v", err)
	}

	updateW := httptest.NewRecorder()
	updateReq := newRequest("POST", "/api/issues/batch-update?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": []string{uiIssueID, plainIssueID},
		"updates":   map[string]any{"status": "done"},
	})
	testHandler.BatchUpdateIssues(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues: expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
		Skipped []struct {
			IssueID    string `json:"issue_id"`
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			Reason     string `json:"reason"`
		} `json:"skipped"`
	}
	if err := json.NewDecoder(updateW.Body).Decode(&resp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if resp.Updated != 1 {
		t.Fatalf("updated = %d, want 1", resp.Updated)
	}
	if len(resp.Skipped) != 1 {
		t.Fatalf("skipped length = %d, want 1", len(resp.Skipped))
	}
	if resp.Skipped[0].IssueID != uiIssueID {
		t.Fatalf("skipped issue_id = %q, want %q", resp.Skipped[0].IssueID, uiIssueID)
	}
	if resp.Skipped[0].Title != "体验确认" {
		t.Fatalf("skipped title = %q, want 体验确认", resp.Skipped[0].Title)
	}
	if resp.Skipped[0].Reason != uiDesignDeliveryRequiredBeforeDoneMessage {
		t.Fatalf("skipped reason = %q, want %q", resp.Skipped[0].Reason, uiDesignDeliveryRequiredBeforeDoneMessage)
	}

	statuses := map[string]string{}
	rows, err := testPool.Query(context.Background(), `SELECT id::text, status FROM issue WHERE id IN ($1, $2)`, uiIssueID, plainIssueID)
	if err != nil {
		t.Fatalf("load issue statuses: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan issue status: %v", err)
		}
		statuses[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate issue statuses: %v", err)
	}
	if statuses[uiIssueID] != "todo" {
		t.Fatalf("ui issue status = %q, want todo", statuses[uiIssueID])
	}
	if statuses[plainIssueID] != "done" {
		t.Fatalf("plain issue status = %q, want done", statuses[plainIssueID])
	}
}

func TestGitHubAdvanceUIDesignDoneRequiresActiveDelivery(t *testing.T) {
	projectID := createProjectForDesignTest(t, "GitHub UI Done Requires Delivery Project")
	parentID := createDesignDeliveryIssueForTest(t, "服务记录开发", "todo", "", projectID)
	uiIssueID := createDesignDeliveryIssueForTest(t, "体验确认", "in_progress", parentID, projectID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET metadata = '{"design_role":"ui_design"}'::jsonb
		WHERE id = $1
	`, uiIssueID); err != nil {
		t.Fatalf("mark ui issue role: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(uiIssueID))
	if err != nil {
		t.Fatalf("load ui issue: %v", err)
	}

	testHandler.advanceIssueToDone(context.Background(), issue, testWorkspaceID)

	var issueStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, uiIssueID).Scan(&issueStatus); err != nil {
		t.Fatalf("load ui issue status: %v", err)
	}
	if issueStatus != "in_progress" {
		t.Fatalf("ui issue status = %q, want in_progress", issueStatus)
	}
}

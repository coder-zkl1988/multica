package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createProjectForDesignTest(t *testing.T, title string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, description, status, priority)
		VALUES ($1, $2, '', 'planned', 'medium')
		RETURNING id
	`, testWorkspaceID, title).Scan(&id); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, id) })
	return id
}

func createIssueForDesignTest(t *testing.T, title string, projectID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, project_id, number)
		VALUES ($1, $2, 'todo', 'medium', 'member', $3, $4, COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, title, testUserID, projectID).Scan(&id); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

func createCompletedDesignRepoAnalysisForDesignTest(t *testing.T, projectID string, framework string) string {
	t.Helper()
	var resourceID string
	resourceRef, err := json.Marshal(map[string]any{"localPath": "/tmp/multica-design-restore-" + projectID})
	if err != nil {
		t.Fatalf("marshal project resource ref: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'local_directory', $3::jsonb, 'Repository root', 0, $4)
		RETURNING id
	`, projectID, testWorkspaceID, resourceRef, testUserID).Scan(&resourceID); err != nil {
		t.Fatalf("insert project_resource: %v", err)
	}
	var analysisID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_repo_analysis (
			workspace_id, project_id, project_resource_id, status, schema_version, source_fingerprint,
			framework, language, package_manager, app_type, routing, styling, directories,
			commands, boundaries, target_candidates, confidence, summary, raw_result, analyzed_at
		) VALUES (
			$1, $2, $3, 'completed', '1.0', $4,
			$5, 'TypeScript', 'npm', 'single_app', '{"kind":"client_router","owners":["src/router"]}'::jsonb, '{}'::jsonb,
			'{"appRoots":["src"],"businessViews":["src/views"],"uiComponents":["src/components"]}'::jsonb,
			'{"typecheck":["npm exec tsc --noEmit --pretty false"]}'::jsonb,
			'{"forbiddenPaths":["node_modules","dist"]}'::jsonb,
			'[{"kind":"view_file","path":"src/views/HomeView.vue","allowedPaths":["src/views","src/components","src/router"]}]'::jsonb,
			0.95, $6, '{}'::jsonb, now()
		)
		RETURNING id
	`, testWorkspaceID, projectID, resourceID, "fingerprint-"+resourceID, framework, framework+" / TypeScript / npm").Scan(&analysisID); err != nil {
		t.Fatalf("insert design_repo_analysis: %v", err)
	}
	return analysisID
}

func createDesignFolderForTest(t *testing.T, projectID string, name string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_folder (workspace_id, project_id, name)
		VALUES ($1, $2, $3)
		RETURNING id
	`, testWorkspaceID, projectID, name).Scan(&id); err != nil {
		t.Fatalf("insert design folder: %v", err)
	}
	return id
}

func minimalDesignNativeJSON(title string) map[string]any {
	return map[string]any{
		"version": "1.0",
		"file": map[string]any{
			"title":      title,
			"sourceType": "upload",
		},
		"frames": []map[string]any{{
			"id":          "frame-1",
			"name":        title,
			"rootLayerId": "layer-1",
			"width":       1440,
			"height":      900,
		}},
		"layers": map[string]any{
			"layer-1": map[string]any{
				"id":      "layer-1",
				"frameId": "frame-1",
				"name":    "Page",
				"type":    "frame",
				"visible": true,
				"x":       0,
				"y":       0,
				"width":   1440,
				"height":  900,
			},
		},
		"assets": map[string]any{},
	}
}

func nativeJSONWithThumbnailForTest(title string, thumbnailURL string) map[string]any {
	doc := minimalDesignNativeJSON(title)
	frames := doc["frames"].([]map[string]any)
	frames[0]["thumbnailAssetId"] = "asset-thumb-main"
	doc["assets"].(map[string]any)["asset-thumb-main"] = map[string]any{
		"id":  "asset-thumb-main",
		"url": thumbnailURL,
	}
	return doc
}

func filterTableTemplateNativeJSONForTest(title string) map[string]any {
	doc := minimalDesignNativeJSON(title)
	layers := doc["layers"].(map[string]any)
	layers["filter-title"] = map[string]any{
		"id":      "filter-title",
		"frameId": "frame-1",
		"name":    "筛选区 / 请输入 / 请选择",
		"type":    "text",
		"visible": true,
		"x":       40,
		"y":       32,
		"width":   320,
		"height":  32,
		"text": map[string]any{
			"characters": "筛选条件 请输入 请选择 查询",
			"text":       "筛选条件 请输入 请选择 查询",
		},
	}
	layers["table-title"] = map[string]any{
		"id":      "table-title",
		"frameId": "frame-1",
		"name":    "表格 / 状态 / 操作",
		"type":    "text",
		"visible": true,
		"x":       40,
		"y":       160,
		"width":   520,
		"height":  32,
		"text": map[string]any{
			"characters": "表格 列表 状态 操作",
			"text":       "表格 列表 状态 操作",
		},
	}
	layers["pagination-title"] = map[string]any{
		"id":      "pagination-title",
		"frameId": "frame-1",
		"name":    "分页 / Pagination",
		"type":    "text",
		"visible": true,
		"x":       40,
		"y":       760,
		"width":   320,
		"height":  32,
		"text": map[string]any{
			"characters": "分页 10条/页 上一页 下一页",
			"text":       "分页 10条/页 上一页 下一页",
		},
	}
	doc["slots"] = map[string]any{
		"page_title":    map[string]any{"slotKey": "page_title", "layerIds": []any{"filter-title"}},
		"filter_fields": map[string]any{"slotKey": "filter_fields", "type": "array"},
		"table_columns": map[string]any{"slotKey": "table_columns", "type": "array"},
	}
	doc["componentBindings"] = map[string]any{
		"filter-title":     map[string]any{"componentKey": "FilterForm"},
		"table-title":      map[string]any{"componentKey": "DataTable"},
		"pagination-title": map[string]any{"componentKey": "Pagination"},
	}
	return doc
}

func figmaDesignNativeJSONWithSourceNodes(title string, sourceNodeIDs ...string) map[string]any {
	frames := make([]map[string]any, 0, len(sourceNodeIDs))
	layers := make(map[string]any, len(sourceNodeIDs))
	for i, sourceNodeID := range sourceNodeIDs {
		frameID := fmt.Sprintf("figma-frame-%d", i+1)
		layerID := fmt.Sprintf("figma-layer-%d", i+1)
		frames = append(frames, map[string]any{
			"id":           frameID,
			"sourceNodeId": sourceNodeID,
			"name":         fmt.Sprintf("Frame %d", i+1),
			"rootLayerId":  layerID,
			"width":        1440,
			"height":       900,
			"source": map[string]any{
				"tool":      "figma",
				"groupId":   "4-189",
				"groupName": "Group 43",
			},
		})
		layers[layerID] = map[string]any{
			"id":           layerID,
			"sourceNodeId": sourceNodeID,
			"frameId":      frameID,
			"name":         fmt.Sprintf("Frame %d Root", i+1),
			"type":         "frame",
			"visible":      true,
			"x":            0,
			"y":            0,
			"width":        1440,
			"height":       900,
		}
	}
	return map[string]any{
		"version": "1.0",
		"file": map[string]any{
			"title":      title,
			"sourceType": "import",
		},
		"frames": frames,
		"layers": layers,
		"assets": map[string]any{},
		"source": map[string]any{"provider": "figma"},
	}
}

func frameCountFromNativeJSONForTest(t *testing.T, raw json.RawMessage) int {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode native json: %v", err)
	}
	frames, ok := doc["frames"].([]any)
	if !ok {
		t.Fatalf("native_json frames type = %T", doc["frames"])
	}
	return len(frames)
}

func contextDesignNativeJSON(title string) map[string]any {
	return map[string]any{
		"version": "1.0",
		"file": map[string]any{
			"title":      title,
			"sourceType": "upload",
		},
		"frames": []map[string]any{
			{
				"id":               "frame-main",
				"name":             "Main Screen",
				"rootLayerId":      "main-root",
				"width":            800,
				"height":           600,
				"previewAssetId":   "asset-preview-main",
				"thumbnailAssetId": "asset-thumb-main",
			},
			{
				"id":          "frame-secondary",
				"name":        "Secondary Screen",
				"rootLayerId": "secondary-root",
				"width":       400,
				"height":      300,
			},
		},
		"layers": map[string]any{
			"main-root": map[string]any{
				"id":      "main-root",
				"frameId": "frame-main",
				"name":    "Main Root",
				"type":    "frame",
				"visible": true,
				"x":       0,
				"y":       0,
				"width":   800,
				"height":  600,
			},
			"main-title": map[string]any{
				"id":      "main-title",
				"frameId": "frame-main",
				"name":    "Title",
				"type":    "text",
				"visible": true,
				"x":       40,
				"y":       40,
				"width":   200,
				"height":  50,
				"text": map[string]any{
					"text":       "Welcome",
					"fontFamily": "Inter",
					"fontSize":   24,
					"fontWeight": 700,
					"color":      map[string]any{"r": 0, "g": 0, "b": 0, "a": 1},
				},
			},
			"main-image": map[string]any{
				"id":      "main-image",
				"frameId": "frame-main",
				"name":    "Hero Image",
				"type":    "image",
				"visible": true,
				"x":       300,
				"y":       80,
				"width":   120,
				"height":  120,
				"image":   map[string]any{"assetId": "asset-hero"},
				"exportable": []map[string]any{{
					"assetId": "asset-export-main",
					"format":  "png",
					"url":     "https://example.test/export-main.png",
				}},
			},
			"main-offscreen": map[string]any{
				"id":      "main-offscreen",
				"frameId": "frame-main",
				"name":    "Offscreen",
				"type":    "rectangle",
				"visible": true,
				"x":       650,
				"y":       450,
				"width":   80,
				"height":  80,
			},
			"secondary-root": map[string]any{
				"id":      "secondary-root",
				"frameId": "frame-secondary",
				"name":    "Secondary Root",
				"type":    "frame",
				"visible": true,
				"x":       0,
				"y":       0,
				"width":   400,
				"height":  300,
			},
			"secondary-title": map[string]any{
				"id":      "secondary-title",
				"frameId": "frame-secondary",
				"name":    "Secondary Title",
				"type":    "text",
				"visible": true,
				"x":       20,
				"y":       20,
				"width":   160,
				"height":  40,
				"text":    map[string]any{"text": "Other", "fontFamily": "Inter", "fontSize": 18},
			},
		},
		"assets": map[string]any{
			"asset-preview-main": map[string]any{"url": "https://example.test/preview-main.png"},
			"asset-thumb-main":   map[string]any{"url": "https://example.test/thumb-main.png"},
			"asset-hero":         map[string]any{"url": "https://example.test/hero.png"},
			"asset-export-main":  map[string]any{"url": "https://example.test/export-main.png"},
			"asset-secondary":    map[string]any{"url": "https://example.test/secondary.png"},
		},
		"annotations": []map[string]any{{"frameId": "frame-main", "layerId": "main-title", "text": "Check copy"}},
	}
}

func restorePackGroupedNativeJSONForTest(title string) map[string]any {
	nativeJSON := contextDesignNativeJSON(title)
	frames := nativeJSON["frames"].([]map[string]any)
	frames[0]["source"] = map[string]any{
		"tool":      "figma",
		"groupId":   "group-wallet",
		"groupName": "钱包首页",
		"groupPath": []string{"钱包首页"},
	}
	frames[1]["source"] = map[string]any{
		"tool":      "figma",
		"groupId":   "group-wallet",
		"groupName": "钱包首页",
		"groupPath": []string{"钱包首页"},
	}
	nativeJSON["restoreHints"] = map[string]any{
		"figmaGroups": map[string]any{
			"group-wallet": map[string]any{
				"id":           "group-wallet",
				"sourceNodeId": "4:189",
				"name":         "钱包首页",
				"nodeType":     "GROUP",
				"frameIds":     []string{"frame-main", "frame-secondary"},
			},
		},
	}
	return nativeJSON
}

func nativeJSONWithFrameNamesForTest(names []string) map[string]any {
	frames := make([]map[string]any, 0, len(names))
	layers := make(map[string]any, len(names)*2)
	for i, name := range names {
		frameID := fmt.Sprintf("frame-%d", i+1)
		rootID := frameID + "-root"
		titleID := frameID + "-title"
		frames = append(frames, map[string]any{
			"id":          frameID,
			"name":        name,
			"rootLayerId": rootID,
			"width":       375,
			"height":      812,
		})
		layers[rootID] = map[string]any{
			"id":      rootID,
			"frameId": frameID,
			"name":    name + " Root",
			"type":    "frame",
			"visible": true,
			"x":       0,
			"y":       0,
			"width":   375,
			"height":  812,
		}
		layers[titleID] = map[string]any{
			"id":      titleID,
			"frameId": frameID,
			"name":    "Title",
			"type":    "text",
			"visible": true,
			"x":       24,
			"y":       48,
			"width":   200,
			"height":  32,
			"text":    map[string]any{"text": name, "fontFamily": "Inter", "fontSize": 18},
		}
	}
	return map[string]any{
		"version": "1.0",
		"file":    map[string]any{"title": "Semantic Frames", "sourceType": "upload"},
		"frames":  frames,
		"layers":  layers,
		"assets":  map[string]any{},
		"source":  map[string]any{"provider": "figma"},
	}
}

func createDesignFileForTest(t *testing.T, title string) DesignFileDetailResponse {
	t.Helper()

	req := newRequest("POST", "/api/design-files?workspace_id="+testWorkspaceID, map[string]any{
		"title":       title,
		"source_type": "upload",
		"native_json": minimalDesignNativeJSON(title),
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignFile(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignFile: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode CreateDesignFile response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, resp.File.ID)
	})
	return resp
}

func updateDesignRevisionNativeJSONForTest(t *testing.T, revisionID string, nativeJSON map[string]any) {
	t.Helper()
	raw, err := json.Marshal(nativeJSON)
	if err != nil {
		t.Fatalf("marshal native json: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE design_revision SET native_json = $1 WHERE id = $2`, raw, revisionID); err != nil {
		t.Fatalf("update design revision native json: %v", err)
	}
}

func createDesignRevisionForTest(t *testing.T, fileID string, revisionNumber int, nativeJSON map[string]any, makeCurrent bool) string {
	t.Helper()
	raw, err := json.Marshal(nativeJSON)
	if err != nil {
		t.Fatalf("marshal native json: %v", err)
	}
	var revisionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_revision (file_id, workspace_id, revision_number, status, native_json, validation_errors, created_by)
		VALUES ($1, $2, $3, 'valid', $4::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, fileID, testWorkspaceID, revisionNumber, raw, testUserID).Scan(&revisionID); err != nil {
		t.Fatalf("insert design revision: %v", err)
	}
	if makeCurrent {
		if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET current_revision_id = $1 WHERE id = $2`, revisionID, fileID); err != nil {
			t.Fatalf("update current revision: %v", err)
		}
	}
	return revisionID
}

func withDesignURLParams(req *http.Request, kv ...string) *http.Request {
	rctx := chi.NewRouteContext()
	for i := 0; i+1 < len(kv); i += 2 {
		rctx.URLParams.Add(kv[i], kv[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestCreateDesignFileCreatesCurrentRevision(t *testing.T) {
	resp := createDesignFileForTest(t, "Handler Test Design")
	if resp.File.ID == "" {
		t.Fatal("expected file id")
	}
	if resp.File.CurrentRevisionID == nil || *resp.File.CurrentRevisionID == "" {
		t.Fatal("expected current revision id")
	}
	if resp.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	if resp.CurrentRevision.RevisionNumber != 1 {
		t.Fatalf("revision number = %d, want 1", resp.CurrentRevision.RevisionNumber)
	}
	if resp.CurrentRevision.Status != "valid" {
		t.Fatalf("revision status = %q, want valid", resp.CurrentRevision.Status)
	}
}

func TestPublishDesignRevisionAsTemplateAndListGet(t *testing.T) {
	design := createDesignFileForTest(t, "Template Source Design")
	if design.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	templateThumbnailURL := "https://static.example.test/template-thumb.png"
	updateDesignRevisionNativeJSONForTest(t, design.CurrentRevision.ID, nativeJSONWithThumbnailForTest("Template Source Design", templateThumbnailURL))
	libraryKey := fmt.Sprintf("test-library-%d", time.Now().UnixNano())
	templateKey := fmt.Sprintf("test-template-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_template_library WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, libraryKey)
	})

	req := newRequest("POST", "/api/design-revisions/"+design.CurrentRevision.ID+"/publish-template?workspace_id="+testWorkspaceID, map[string]any{
		"library_key":  libraryKey,
		"library_name": "Test Template Library",
		"template_key": templateKey,
		"name":         "Checkout Template",
		"description":  "Reusable checkout screen",
		"category":     "checkout",
		"slot_schema":  map[string]any{"title": map[string]any{"type": "text"}},
		"metadata":     map[string]any{"source": "handler-test"},
	})
	req = withDesignURLParams(req, "revisionId", design.CurrentRevision.ID)
	w := httptest.NewRecorder()
	testHandler.PublishDesignRevisionAsTemplate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("PublishDesignRevisionAsTemplate: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var published DesignCatalogTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&published); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	if published.Key != templateKey || published.Name != "Checkout Template" || published.Category != "checkout" {
		t.Fatalf("unexpected published template: %+v", published)
	}
	if published.DesignRevisionID == nil || *published.DesignRevisionID != design.CurrentRevision.ID {
		t.Fatalf("design_revision_id = %v, want %s", published.DesignRevisionID, design.CurrentRevision.ID)
	}
	if published.TemplateRevisionNumber == nil || *published.TemplateRevisionNumber != 1 {
		t.Fatalf("template_revision_number = %v, want 1", published.TemplateRevisionNumber)
	}
	if published.ThumbnailURL == nil || *published.ThumbnailURL != templateThumbnailURL {
		t.Fatalf("published thumbnail_url = %v, want %s", published.ThumbnailURL, templateThumbnailURL)
	}

	listReq := newRequest("GET", "/api/design-templates?workspace_id="+testWorkspaceID+"&category=checkout", nil)
	listW := httptest.NewRecorder()
	testHandler.ListDesignCatalogTemplates(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignCatalogTemplates: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResp struct {
		Templates []DesignCatalogTemplateResponse `json:"templates"`
		Total     int                             `json:"total"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, item := range listResp.Templates {
		if item.ID == published.ID {
			if item.ThumbnailURL == nil || *item.ThumbnailURL != templateThumbnailURL {
				t.Fatalf("listed thumbnail_url = %v, want %s", item.ThumbnailURL, templateThumbnailURL)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("published template %s not found in list: %+v", published.ID, listResp.Templates)
	}

	getReq := newRequest("GET", "/api/design-templates/"+published.ID+"?workspace_id="+testWorkspaceID, nil)
	getReq = withDesignURLParams(getReq, "id", published.ID)
	getW := httptest.NewRecorder()
	testHandler.GetDesignCatalogTemplate(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GetDesignCatalogTemplate: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	var got DesignCatalogTemplateResponse
	if err := json.NewDecoder(getW.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.ID != published.ID || got.DesignRevisionID == nil || *got.DesignRevisionID != design.CurrentRevision.ID {
		t.Fatalf("unexpected get response: %+v", got)
	}
	if got.ThumbnailURL == nil || *got.ThumbnailURL != templateThumbnailURL {
		t.Fatalf("get thumbnail_url = %v, want %s", got.ThumbnailURL, templateThumbnailURL)
	}
}

func TestPublishDesignRevisionAsTemplateAddsTemplateProfile(t *testing.T) {
	design := createDesignFileForTest(t, "筛选表格分页模板")
	if design.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, design.CurrentRevision.ID, filterTableTemplateNativeJSONForTest("筛选表格分页模板"))
	libraryKey := fmt.Sprintf("profile-library-%d", time.Now().UnixNano())
	templateKey := fmt.Sprintf("profile-template-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_template_library WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, libraryKey)
	})

	req := newRequest("POST", "/api/design-revisions/"+design.CurrentRevision.ID+"/publish-template?workspace_id="+testWorkspaceID, map[string]any{
		"library_key":  libraryKey,
		"template_key": templateKey,
		"name":         "标准筛选表格分页模板",
		"category":     "b端列表页",
	})
	req = withDesignURLParams(req, "revisionId", design.CurrentRevision.ID)
	w := httptest.NewRecorder()
	testHandler.PublishDesignRevisionAsTemplate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("PublishDesignRevisionAsTemplate: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var published DesignCatalogTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&published); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(published.Metadata, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	profile, ok := metadata["template_profile"].(map[string]any)
	if !ok {
		t.Fatalf("expected template_profile metadata, got %+v", metadata)
	}
	if profile["page_type"] != "saas.filter-table-pagination" {
		t.Fatalf("page_type = %v, want saas.filter-table-pagination; profile=%+v", profile["page_type"], profile)
	}
	tags := asStringSlice(profile["tags"])
	for _, want := range []string{"筛选", "表格", "分页"} {
		if !containsString(tags, want) {
			t.Fatalf("profile tags = %v, missing %q", tags, want)
		}
	}
}

func createCatalogTemplateForDraftTest(t *testing.T) DesignCatalogTemplateResponse {
	t.Helper()
	design := createDesignFileForTest(t, "Draft Template Source")
	if design.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	libraryKey := fmt.Sprintf("draft-library-%d", time.Now().UnixNano())
	templateKey := fmt.Sprintf("draft-template-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_template_library WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, libraryKey)
	})
	req := newRequest("POST", "/api/design-revisions/"+design.CurrentRevision.ID+"/publish-template?workspace_id="+testWorkspaceID, map[string]any{
		"library_key":  libraryKey,
		"template_key": templateKey,
		"name":         "Draftable Template",
	})
	req = withDesignURLParams(req, "revisionId", design.CurrentRevision.ID)
	w := httptest.NewRecorder()
	testHandler.PublishDesignRevisionAsTemplate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("PublishDesignRevisionAsTemplate: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var published DesignCatalogTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&published); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	return published
}

func createFilterTableCatalogTemplateForDraftTest(t *testing.T) DesignCatalogTemplateResponse {
	t.Helper()
	design := createDesignFileForTest(t, "筛选表格分页模板源")
	if design.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, design.CurrentRevision.ID, filterTableTemplateNativeJSONForTest("筛选表格分页模板源"))
	libraryKey := fmt.Sprintf("filter-table-library-%d", time.Now().UnixNano())
	templateKey := fmt.Sprintf("filter-table-template-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_template_library WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, libraryKey)
	})
	req := newRequest("POST", "/api/design-revisions/"+design.CurrentRevision.ID+"/publish-template?workspace_id="+testWorkspaceID, map[string]any{
		"library_key":  libraryKey,
		"template_key": templateKey,
		"name":         "标准筛选表格分页模板",
		"category":     "b端列表页",
		"slot_schema": map[string]any{
			"page_title":    map[string]any{"type": "text", "required": true},
			"filter_fields": map[string]any{"type": "array"},
			"table_columns": map[string]any{"type": "array"},
		},
	})
	req = withDesignURLParams(req, "revisionId", design.CurrentRevision.ID)
	w := httptest.NewRecorder()
	testHandler.PublishDesignRevisionAsTemplate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("PublishDesignRevisionAsTemplate: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var published DesignCatalogTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&published); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	return published
}

func createFilterTableCatalogTemplateWithoutSlotsForDraftTest(t *testing.T) DesignCatalogTemplateResponse {
	t.Helper()
	design := createDesignFileForTest(t, "无槽位筛选表格模板源")
	if design.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, design.CurrentRevision.ID, filterTableTemplateNativeJSONForTest("无槽位筛选表格模板源"))
	libraryKey := fmt.Sprintf("filter-table-no-slots-library-%d", time.Now().UnixNano())
	templateKey := fmt.Sprintf("filter-table-no-slots-template-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_template_library WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, libraryKey)
	})
	req := newRequest("POST", "/api/design-revisions/"+design.CurrentRevision.ID+"/publish-template?workspace_id="+testWorkspaceID, map[string]any{
		"library_key":  libraryKey,
		"template_key": templateKey,
		"name":         "无槽位标准筛选表格分页模板",
		"category":     "b端列表页",
	})
	req = withDesignURLParams(req, "revisionId", design.CurrentRevision.ID)
	w := httptest.NewRecorder()
	testHandler.PublishDesignRevisionAsTemplate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("PublishDesignRevisionAsTemplate without slots: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var published DesignCatalogTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&published); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	return published
}

func createCatalogTemplateWithTextSlotForDraftTest(t *testing.T) DesignCatalogTemplateResponse {
	t.Helper()
	design := createDesignFileForTest(t, "Draft Template With Slot")
	if design.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	nativeJSON := minimalDesignNativeJSON("Draft Template With Slot")
	layers := nativeJSON["layers"].(map[string]any)
	layers["title-layer"] = map[string]any{
		"id":      "title-layer",
		"frameId": "frame-1",
		"name":    "Title",
		"type":    "text",
		"visible": true,
		"x":       40,
		"y":       40,
		"width":   320,
		"height":  48,
		"text": map[string]any{
			"characters": "Original title",
			"text":       "Original title",
			"fontSize":   24,
		},
	}
	nativeJSON["slots"] = map[string]any{"title": map[string]any{"slotKey": "title", "layerIds": []any{"title-layer"}}}
	updateDesignRevisionNativeJSONForTest(t, design.CurrentRevision.ID, nativeJSON)
	libraryKey := fmt.Sprintf("slot-draft-library-%d", time.Now().UnixNano())
	templateKey := fmt.Sprintf("slot-draft-template-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_template_library WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, libraryKey)
	})
	req := newRequest("POST", "/api/design-revisions/"+design.CurrentRevision.ID+"/publish-template?workspace_id="+testWorkspaceID, map[string]any{
		"library_key":  libraryKey,
		"template_key": templateKey,
		"name":         "Draftable Slot Template",
		"slot_schema":  map[string]any{"title": map[string]any{"type": "text"}},
	})
	req = withDesignURLParams(req, "revisionId", design.CurrentRevision.ID)
	w := httptest.NewRecorder()
	testHandler.PublishDesignRevisionAsTemplate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("PublishDesignRevisionAsTemplate: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var published DesignCatalogTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&published); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	return published
}

func handlerTestAgentID(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent' LIMIT 1`, testWorkspaceID).Scan(&id); err != nil {
		t.Fatalf("get handler test agent: %v", err)
	}
	return id
}

func createLocalUIRestoreAgentForDesignTest(t *testing.T) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, 'Local UI Restore Agent', '', 'local', '{}'::jsonb, $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
		t.Fatalf("failed to create Local UI Restore Agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func attachDesignFileToProjectForTest(t *testing.T, fileID string, projectID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, fileID); err != nil {
		t.Fatalf("attach design file to project: %v", err)
	}
}

func TestCreateDesignDraftFromCatalogTemplate(t *testing.T) {
	template := createCatalogTemplateForDraftTest(t)
	req := newRequest("POST", "/api/design-drafts?workspace_id="+testWorkspaceID, map[string]any{
		"catalog_template_id": template.ID,
		"title":               "Generated Checkout Draft",
		"requirement_core":    map[string]any{"version": "1.0", "title": "Checkout"},
		"slot_values":         map[string]any{"title": "Pay now"},
		"patch":               []map[string]any{{"op": "replace", "path": "/layers/main-title/text/text", "value": "Pay now"}},
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraft(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignDraft: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var draft DesignDraftResponse
	if err := json.NewDecoder(w.Body).Decode(&draft); err != nil {
		t.Fatalf("decode draft response: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM design_draft WHERE id = $1`, draft.ID) })
	if draft.CatalogTemplateID == nil || *draft.CatalogTemplateID != template.ID {
		t.Fatalf("catalog_template_id = %v, want %s", draft.CatalogTemplateID, template.ID)
	}
	if draft.TemplateRevisionID == nil || template.CurrentRevisionID == nil || *draft.TemplateRevisionID != *template.CurrentRevisionID {
		t.Fatalf("template_revision_id = %v, want %v", draft.TemplateRevisionID, template.CurrentRevisionID)
	}
	if draft.Status != "generated" {
		t.Fatalf("status = %q, want generated", draft.Status)
	}

	listReq := newRequest("GET", "/api/design-drafts?workspace_id="+testWorkspaceID, nil)
	listW := httptest.NewRecorder()
	testHandler.ListDesignDrafts(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignDrafts: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}

	getReq := newRequest("GET", "/api/design-drafts/"+draft.ID+"?workspace_id="+testWorkspaceID, nil)
	getReq = withDesignURLParams(getReq, "id", draft.ID)
	getW := httptest.NewRecorder()
	testHandler.GetDesignDraft(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GetDesignDraft: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
}

func TestCreateDesignSystemProfileFromDesignFile(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Design System Project")
	created := createDesignFileForTest(t, "Design System Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	createLocalUIRestoreAgentForDesignTest(t)

	req := newRequest("POST", "/api/design-systems?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":         projectID,
		"source_file_id":     created.File.ID,
		"source_revision_id": created.CurrentRevision.ID,
		"name":               "CRM 后台设计系统",
		"description":        "CRM admin base components",
		"is_default":         true,
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignSystemProfile(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignSystemProfile: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp DesignSystemProfileResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "CRM 后台设计系统" {
		t.Fatalf("name = %q", resp.Name)
	}
	if resp.Status != "analyzing" {
		t.Fatalf("status = %q, want analyzing", resp.Status)
	}
	if resp.IsDefault {
		t.Fatal("created design system should wait for agent analysis before becoming default")
	}
	var taskID string
	var taskContext []byte
	var agentName string
	if err := testPool.QueryRow(ctx, `
		SELECT atq.id, atq.context, a.name
		FROM agent_task_queue atq
		JOIN agent a ON a.id = atq.agent_id
		WHERE atq.context->>'type' = $1
		  AND atq.context->>'design_system_profile_id' = $2
		ORDER BY atq.created_at DESC
		LIMIT 1
	`, service.DesignSystemProfileAnalyzeContextType, resp.ID).Scan(&taskID, &taskContext, &agentName); err != nil {
		t.Fatalf("expected design system analyze task: %v", err)
	}
	if agentName != "Local UI Restore Agent" {
		t.Fatalf("analyze task agent = %q, want Local UI Restore Agent", agentName)
	}
	var payload map[string]any
	if err := json.Unmarshal(taskContext, &payload); err != nil {
		t.Fatalf("decode analyze task context: %v", err)
	}
	if payload["profile_name"] != "CRM 后台设计系统" {
		t.Fatalf("profile_name = %v", payload["profile_name"])
	}
	if _, ok := payload["candidate_layers"].([]any); !ok {
		t.Fatalf("analyze task context missing candidate_layers: %+v", payload)
	}
	if _, ok := payload["deterministic_profile"]; ok {
		t.Fatalf("analyze task context must not include backend semantic classification: %+v", payload["deterministic_profile"])
	}
	if string(resp.ProfileJSON) != "{}" {
		t.Fatalf("profile_json = %s, want empty while Agent analysis is pending", resp.ProfileJSON)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, resp.ID)
	})
}

func TestListDesignSystemProfilesByProject(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Design System List Project")
	created := createDesignFileForTest(t, "Design System List Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	createLocalUIRestoreAgentForDesignTest(t)
	designSystemThumbnailURL := "https://static.example.test/design-system-thumb.png"
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSONWithThumbnailForTest("Design System List Source", designSystemThumbnailURL))

	createReq := newRequest("POST", "/api/design-systems?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":         projectID,
		"source_file_id":     created.File.ID,
		"source_revision_id": created.CurrentRevision.ID,
		"name":               "Listable Design System",
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignSystemProfile(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create setup: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}

	listReq := newRequest("GET", "/api/design-systems?workspace_id="+testWorkspaceID+"&project_id="+projectID, nil)
	listW := httptest.NewRecorder()
	testHandler.ListDesignSystemProfiles(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignSystemProfiles: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var list DesignSystemProfileListResponse
	if err := json.NewDecoder(listW.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.DesignSystems) == 0 {
		t.Fatal("expected at least one design system")
	}
	if list.DesignSystems[0].Name != "Listable Design System" {
		t.Fatalf("first design system = %q", list.DesignSystems[0].Name)
	}
	if list.DesignSystems[0].ThumbnailURL == nil || *list.DesignSystems[0].ThumbnailURL != designSystemThumbnailURL {
		t.Fatalf("design system thumbnail_url = %v, want %s", list.DesignSystems[0].ThumbnailURL, designSystemThumbnailURL)
	}
}

func TestCreateDesignDraftAgentTaskEnqueuesTaskContext(t *testing.T) {
	template := createCatalogTemplateForDraftTest(t)
	agentID := handlerTestAgentID(t)
	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id":            agentID,
		"catalog_template_id": template.ID,
		"title":               "Agent Draft",
		"prompt":              "Generate a draft",
		"requirement_core":    map[string]any{"version": "1.0", "title": "Agent Draft"},
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateDesignDraftAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.TaskID)
	})
	var contextRaw []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, resp.TaskID).Scan(&contextRaw); err != nil {
		t.Fatalf("get task context: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(contextRaw, &payload); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	if payload["type"] != "ui_agent_draft_create" {
		t.Fatalf("context type = %v, want ui_agent_draft_create", payload["type"])
	}
	if payload["catalog_template_id"] != template.ID {
		t.Fatalf("catalog_template_id = %v, want %s", payload["catalog_template_id"], template.ID)
	}
	if _, ok := payload["output_policy"].(map[string]any); !ok {
		t.Fatalf("expected output_policy in context: %+v", payload)
	}
}

func TestCreateDesignDraftAgentTaskFromIssueProvidesTemplateCandidates(t *testing.T) {
	template := createFilterTableCatalogTemplateForDraftTest(t)
	projectID := createProjectForDesignTest(t, "UI Agent Draft Project")
	issueID := createIssueForDesignTest(t, "服务记录开发 UI设计", projectID)
	_, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET description = $2
		WHERE id = $1
	`, issueID, "需要新增服务记录列表页，包含门店、治疗师、日期筛选，表格展示服务时间、客户、状态、操作，并支持分页。")
	if err != nil {
		t.Fatalf("update issue description: %v", err)
	}
	agentID := handlerTestAgentID(t)
	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentID,
		"issue_id": issueID,
		"title":    "服务记录列表草稿",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateDesignDraftAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.TaskID)
	})
	var contextRaw []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, resp.TaskID).Scan(&contextRaw); err != nil {
		t.Fatalf("get task context: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(contextRaw, &payload); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	issue, ok := payload["issue"].(map[string]any)
	if !ok || issue["id"] != issueID || issue["title"] != "服务记录开发 UI设计" {
		t.Fatalf("unexpected issue context: %+v", payload["issue"])
	}
	candidates, ok := payload["template_candidates"].([]any)
	if !ok || len(candidates) == 0 {
		t.Fatalf("expected template_candidates, got %+v", payload["template_candidates"])
	}
	candidate, _ := candidates[0].(map[string]any)
	if candidate["id"] != template.ID {
		t.Fatalf("first candidate id = %v, want %s", candidate["id"], template.ID)
	}
	profile, _ := candidate["template_profile"].(map[string]any)
	if profile["page_type"] != "saas.filter-table-pagination" {
		t.Fatalf("candidate profile = %+v", profile)
	}
	textLayers, ok := candidate["editable_text_layers"].([]any)
	if !ok || len(textLayers) == 0 {
		t.Fatalf("expected editable_text_layers in candidate, got %+v", candidate["editable_text_layers"])
	}
	firstTextLayer, _ := textLayers[0].(map[string]any)
	patchPaths, _ := firstTextLayer["patch_paths"].([]any)
	if firstTextLayer["id"] == "" || firstTextLayer["text"] == "" || len(patchPaths) == 0 {
		t.Fatalf("unexpected editable text layer summary: %+v", firstTextLayer)
	}
	policy, _ := payload["selection_policy"].(map[string]any)
	if policy["agent_must_select_catalog_template_id"] != true {
		t.Fatalf("selection_policy = %+v", policy)
	}
}

func TestCreateDesignDraftAgentTaskFromChildIssueIncludesParentIssueContext(t *testing.T) {
	createFilterTableCatalogTemplateForDraftTest(t)
	projectID := createProjectForDesignTest(t, "UI Agent Parent PRD Project")
	parentIssueID := createIssueForDesignTest(t, "CRM 客户管理开发", projectID)
	childIssueID := createIssueForDesignTest(t, "UI设计", projectID)
	_, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET description = $2,
		    acceptance_criteria = $3::jsonb
		WHERE id = $1
	`, parentIssueID, "父 Issue PRD：客户管理页面需要筛选、表格、分页和客户状态操作。", `["筛选项完整","表格字段完整"]`)
	if err != nil {
		t.Fatalf("update parent issue: %v", err)
	}
	_, err = testPool.Exec(context.Background(), `
		UPDATE issue
		SET description = $2,
		    parent_issue_id = $3
		WHERE id = $1
	`, childIssueID, "子 Issue 范围：请产出 UI 设计稿。", parentIssueID)
	if err != nil {
		t.Fatalf("update child issue: %v", err)
	}

	agentID := handlerTestAgentID(t)
	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentID,
		"issue_id": childIssueID,
		"title":    "CRM UI 草稿",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateDesignDraftAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.TaskID)
	})
	var contextRaw []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, resp.TaskID).Scan(&contextRaw); err != nil {
		t.Fatalf("get task context: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(contextRaw, &payload); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	parentIssue, ok := payload["parent_issue"].(map[string]any)
	if !ok {
		t.Fatalf("expected parent_issue in context, got %+v", payload)
	}
	if parentIssue["id"] != parentIssueID || parentIssue["title"] != "CRM 客户管理开发" {
		t.Fatalf("unexpected parent_issue identity: %+v", parentIssue)
	}
	if parentIssue["description"] != "父 Issue PRD：客户管理页面需要筛选、表格、分页和客户状态操作。" {
		t.Fatalf("parent_issue description = %v", parentIssue["description"])
	}
	criteria, ok := parentIssue["acceptance_criteria"].([]any)
	if !ok || len(criteria) != 2 {
		t.Fatalf("parent_issue acceptance_criteria = %+v", parentIssue["acceptance_criteria"])
	}
	childIssue, _ := payload["issue"].(map[string]any)
	if childIssue["parent_issue_id"] != parentIssueID {
		t.Fatalf("child issue parent_issue_id = %v, want %s", childIssue["parent_issue_id"], parentIssueID)
	}
}

func TestCreateDesignDraftAgentTaskIncludesDefaultDesignSystem(t *testing.T) {
	createFilterTableCatalogTemplateForDraftTest(t)
	projectID := createProjectForDesignTest(t, "UI Agent Design System Project")
	issueID := createIssueForDesignTest(t, "CRM 客户管理 UI设计", projectID)
	created := createDesignFileForTest(t, "Default Design System Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	profileJSON := []byte(`{"version":"1.0","tokens":{"colors":[{"value":"#1677ff"}]},"components":{"button":[{"name":"主按钮"}]}}`)
	var profileID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		)
		VALUES ($1, $2, $3, $4, 'Default Design System', 'analyzed', true, $5, '[]', $6)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, profileJSON, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert design system: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_system_profile WHERE id = $1`, profileID)
	})

	agentID := handlerTestAgentID(t)
	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentID,
		"issue_id": issueID,
		"title":    "CRM UI 草稿",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateDesignDraftAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.TaskID)
	})
	var contextRaw []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, resp.TaskID).Scan(&contextRaw); err != nil {
		t.Fatalf("get task context: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(contextRaw, &payload); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	designSystem, ok := payload["design_system"].(map[string]any)
	if !ok {
		t.Fatalf("context missing design_system: %+v", payload)
	}
	if designSystem["id"] != profileID || designSystem["name"] != "Default Design System" {
		t.Fatalf("unexpected design_system identity: %+v", designSystem)
	}
	profile, ok := designSystem["profile"].(map[string]any)
	if !ok {
		t.Fatalf("design_system profile missing: %+v", designSystem)
	}
	tokens, ok := profile["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("design_system profile tokens missing: %+v", profile)
	}
	colors, ok := tokens["colors"].([]any)
	if !ok || len(colors) == 0 {
		t.Fatalf("design_system profile colors missing: %+v", tokens)
	}
}

func TestClaimUIDraftCreateTaskReturnsContext(t *testing.T) {
	template := createCatalogTemplateForDraftTest(t)
	projectID := createProjectForDesignTest(t, "Claim UI Draft Project")
	issueID := createIssueForDesignTest(t, "Claim UI Draft Issue", projectID)
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	var daemonID string
	if err := testPool.QueryRow(context.Background(), `SELECT daemon_id FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&daemonID); err != nil {
		t.Fatalf("load UI draft runtime daemon id: %v", err)
	}
	resourceRef, err := json.Marshal(map[string]any{"local_path": "/tmp/multica-claim-ui-draft", "daemon_id": daemonID})
	if err != nil {
		t.Fatalf("marshal UI draft project resource: %v", err)
	}
	var resourceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'local_directory', $3::jsonb, 'Repository root', 0, $4)
		RETURNING id
	`, projectID, testWorkspaceID, resourceRef, testUserID).Scan(&resourceID); err != nil {
		t.Fatalf("insert UI draft project resource: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_resource WHERE id = $1`, resourceID)
	})
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Claim UI Draft Agent %d", time.Now().UnixNano()), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert UI draft agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id":            agentID,
		"catalog_template_id": template.ID,
		"issue_id":            issueID,
		"title":               "Claimed Agent Draft",
		"prompt":              "Generate draft JSON",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var created CreateDesignDraftAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.TaskID)
	})

	claimReq := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "ui-draft-claim")
	claimReq = withURLParam(claimReq, "runtimeId", runtimeID)
	claimW := httptest.NewRecorder()
	testHandler.ClaimTaskByRuntime(claimW, claimReq)
	if claimW.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", claimW.Code, claimW.Body.String())
	}
	var claimResp struct {
		Task *struct {
			ID                   string                `json:"id"`
			WorkspaceID          string                `json:"workspace_id"`
			ProjectID            string                `json:"project_id"`
			ProjectResources     []ProjectResourceData `json:"project_resources"`
			UIDraftCreateContext json.RawMessage       `json:"ui_draft_create_context"`
		} `json:"task"`
	}
	if err := json.Unmarshal(claimW.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if claimResp.Task == nil || claimResp.Task.ID != created.TaskID {
		t.Fatalf("claimed task = %+v, want %s", claimResp.Task, created.TaskID)
	}
	if claimResp.Task.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %q, want %q", claimResp.Task.WorkspaceID, testWorkspaceID)
	}
	if claimResp.Task.ProjectID != projectID {
		t.Fatalf("project_id = %q, want %q", claimResp.Task.ProjectID, projectID)
	}
	if len(claimResp.Task.ProjectResources) != 1 || claimResp.Task.ProjectResources[0].ID != resourceID {
		t.Fatalf("project_resources = %+v, want resource %s", claimResp.Task.ProjectResources, resourceID)
	}
	var ctxPayload map[string]any
	if err := json.Unmarshal(claimResp.Task.UIDraftCreateContext, &ctxPayload); err != nil {
		t.Fatalf("decode ui draft context: %v", err)
	}
	if ctxPayload["type"] != "ui_agent_draft_create" {
		t.Fatalf("context type = %v", ctxPayload["type"])
	}
	if ctxPayload["project_id"] != projectID {
		t.Fatalf("context project_id = %v, want %s", ctxPayload["project_id"], projectID)
	}
}

func TestClaimDesignSystemProfileAnalyzeTaskReturnsContext(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Claim Design System Analyze Project")
	created := createDesignFileForTest(t, "Claim Design System Analyze Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Local UI Restore Agent', '', 'local', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert Local UI Restore Agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		)
		VALUES ($1, $2, $3, $4, 'Claim UI Spec', 'analyzing', false, '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert design system profile: %v", err)
	}
	contextPayload := map[string]any{
		"type":                     service.DesignSystemProfileAnalyzeContextType,
		"requester_id":             testUserID,
		"workspace_id":             testWorkspaceID,
		"agent_id":                 agentID,
		"design_system_profile_id": profileID,
		"source_file_id":           created.File.ID,
		"source_revision_id":       created.CurrentRevision.ID,
		"project_id":               projectID,
		"profile_name":             "Claim UI Spec",
		"candidate_layers":         []map[string]any{{"id": "button-1", "name": "按钮 - 主按钮 - 默认"}},
	}
	contextJSON, _ := json.Marshal(contextPayload)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, context)
		VALUES ($1, $2, NULL, 'queued', 0, $3)
		RETURNING id
	`, agentID, runtimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("insert analyze task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, profileID)
	})

	claimReq := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "design-system-analyze-claim")
	claimReq = withURLParam(claimReq, "runtimeId", runtimeID)
	claimW := httptest.NewRecorder()
	testHandler.ClaimTaskByRuntime(claimW, claimReq)
	if claimW.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", claimW.Code, claimW.Body.String())
	}
	var claimResp struct {
		Task *struct {
			ID                                string          `json:"id"`
			WorkspaceID                       string          `json:"workspace_id"`
			DesignSystemProfileAnalyzeContext json.RawMessage `json:"design_system_profile_analyze_context"`
		} `json:"task"`
	}
	if err := json.Unmarshal(claimW.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if claimResp.Task == nil || claimResp.Task.ID != taskID {
		t.Fatalf("claimed task = %+v, want %s", claimResp.Task, taskID)
	}
	if claimResp.Task.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %q, want %q", claimResp.Task.WorkspaceID, testWorkspaceID)
	}
	var gotContext map[string]any
	if err := json.Unmarshal(claimResp.Task.DesignSystemProfileAnalyzeContext, &gotContext); err != nil {
		t.Fatalf("decode design system analyze context: %v", err)
	}
	if gotContext["type"] != service.DesignSystemProfileAnalyzeContextType || gotContext["design_system_profile_id"] != profileID {
		t.Fatalf("unexpected analyze context: %+v", gotContext)
	}
}

func TestClaimDesignRestoreTaskUsesDispatchProjectContext(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Claim Design Restore Project")
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	var daemonID string
	if err := testPool.QueryRow(ctx, `SELECT daemon_id FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&daemonID); err != nil {
		t.Fatalf("load design restore runtime daemon id: %v", err)
	}
	resourceRef, _ := json.Marshal(map[string]any{"local_path": "/tmp/multica-claim-design-restore", "daemon_id": daemonID})
	var resourceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'local_directory', $3::jsonb, 'Repository root', 0, $4)
		RETURNING id
	`, projectID, testWorkspaceID, resourceRef, testUserID).Scan(&resourceID); err != nil {
		t.Fatalf("insert design restore project resource: %v", err)
	}
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Claim Design Restore Agent %d", time.Now().UnixNano()), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert claim design restore agent: %v", err)
	}
	contextJSON, _ := json.Marshal(map[string]any{
		"type":            service.DesignRestoreTaskContextType,
		"workspace_id":    testWorkspaceID,
		"project_id":      projectID,
		"restore_task_id": "11111111-1111-1111-1111-111111111111",
		"input":           map[string]any{"version": "1.0"},
	})
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context)
		VALUES ($1, $2, 'queued', 0, $3)
		RETURNING id
	`, agentID, runtimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("insert claim design restore task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM project_resource WHERE id = $1`, resourceID)
	})

	claimReq := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "design-restore-project-claim")
	claimReq = withURLParam(claimReq, "runtimeId", runtimeID)
	claimW := httptest.NewRecorder()
	testHandler.ClaimTaskByRuntime(claimW, claimReq)
	if claimW.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", claimW.Code, claimW.Body.String())
	}
	var claimResp struct {
		Task *struct {
			ID               string                `json:"id"`
			ProjectID        string                `json:"project_id"`
			ProjectResources []ProjectResourceData `json:"project_resources"`
		} `json:"task"`
	}
	if err := json.Unmarshal(claimW.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode design restore claim response: %v", err)
	}
	if claimResp.Task == nil || claimResp.Task.ID != taskID {
		t.Fatalf("claimed task = %+v, want %s", claimResp.Task, taskID)
	}
	if claimResp.Task.ProjectID != projectID {
		t.Fatalf("project_id = %q, want %q", claimResp.Task.ProjectID, projectID)
	}
	if len(claimResp.Task.ProjectResources) != 1 || claimResp.Task.ProjectResources[0].ID != resourceID {
		t.Fatalf("project_resources = %+v, want resource %s", claimResp.Task.ProjectResources, resourceID)
	}
	if _, err := testHandler.TaskService.CancelTask(ctx, parseUUID(taskID)); err != nil {
		t.Fatalf("cancel first claimed restore task: %v", err)
	}

	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Foreign restore workspace', $1, '', 'FRR')
		RETURNING id
	`, fmt.Sprintf("foreign-restore-%d", time.Now().UnixNano())).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("insert foreign restore workspace: %v", err)
	}
	var foreignProjectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, priority)
		VALUES ($1, 'Foreign restore project', '', 'planned', 'medium')
		RETURNING id
	`, foreignWorkspaceID).Scan(&foreignProjectID); err != nil {
		t.Fatalf("insert foreign restore project: %v", err)
	}
	foreignResourceRef, _ := json.Marshal(map[string]any{"local_path": "/tmp/foreign-restore-secret"})
	var foreignResourceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'local_directory', $3::jsonb, 'Foreign secret', 0, $4)
		RETURNING id
	`, foreignProjectID, foreignWorkspaceID, foreignResourceRef, testUserID).Scan(&foreignResourceID); err != nil {
		t.Fatalf("insert foreign restore resource: %v", err)
	}
	foreignContextJSON, _ := json.Marshal(map[string]any{
		"type":            service.DesignRestoreTaskContextType,
		"workspace_id":    testWorkspaceID,
		"project_id":      foreignProjectID,
		"restore_task_id": "22222222-2222-2222-2222-222222222222",
		"input":           map[string]any{"version": "1.0"},
	})
	var foreignTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context)
		VALUES ($1, $2, 'queued', 0, $3)
		RETURNING id
	`, agentID, runtimeID, foreignContextJSON).Scan(&foreignTaskID); err != nil {
		t.Fatalf("insert foreign project restore task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, foreignTaskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})

	foreignClaimReq := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "design-restore-foreign-project-claim")
	foreignClaimReq = withURLParam(foreignClaimReq, "runtimeId", runtimeID)
	foreignClaimW := httptest.NewRecorder()
	testHandler.ClaimTaskByRuntime(foreignClaimW, foreignClaimReq)
	if foreignClaimW.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime foreign project: expected 200, got %d: %s", foreignClaimW.Code, foreignClaimW.Body.String())
	}
	var foreignClaimResp struct {
		Task *struct {
			ID               string                `json:"id"`
			ProjectID        string                `json:"project_id"`
			ProjectResources []ProjectResourceData `json:"project_resources"`
		} `json:"task"`
	}
	if err := json.Unmarshal(foreignClaimW.Body.Bytes(), &foreignClaimResp); err != nil {
		t.Fatalf("decode foreign project claim response: %v", err)
	}
	if foreignClaimResp.Task == nil || foreignClaimResp.Task.ID != foreignTaskID {
		t.Fatalf("foreign claimed task = %+v, want %s", foreignClaimResp.Task, foreignTaskID)
	}
	if foreignClaimResp.Task.ProjectID != "" || len(foreignClaimResp.Task.ProjectResources) != 0 {
		t.Fatalf("foreign project leaked through claim: project_id=%q resources=%+v", foreignClaimResp.Task.ProjectID, foreignClaimResp.Task.ProjectResources)
	}
}

func TestCompleteUIDraftCreateTaskCreatesDraft(t *testing.T) {
	template := createCatalogTemplateWithTextSlotForDraftTest(t)
	agentID := handlerTestAgentID(t)
	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id":            agentID,
		"catalog_template_id": template.ID,
		"title":               "Task Draft Title",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var created CreateDesignDraftAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.TaskID)
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, created.TaskID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
	output := map[string]any{
		"title":            "Agent Generated Draft",
		"requirement_core": map[string]any{"version": "1.0", "title": "Agent Generated Draft"},
		"slot_values":      map[string]any{"title": "Pay now"},
		"patch":            []any{},
	}
	outputJSON, _ := json.Marshal(output)
	completeReq := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+created.TaskID+"/complete", map[string]any{"output": string(outputJSON)}, testWorkspaceID, "ui-draft-complete")
	completeReq = withURLParam(completeReq, "taskId", created.TaskID)
	completeW := httptest.NewRecorder()
	testHandler.CompleteTask(completeW, completeReq)
	if completeW.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", completeW.Code, completeW.Body.String())
	}
	var draftID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM design_draft WHERE workspace_id = $1 AND title = 'Agent Generated Draft' ORDER BY created_at DESC LIMIT 1`, testWorkspaceID).Scan(&draftID); err != nil {
		t.Fatalf("expected created design draft: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM design_draft WHERE id = $1`, draftID) })
}

func TestCompleteDesignSystemProfileAnalyzeTaskUpdatesProfile(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Complete Design System Analyze Project")
	created := createDesignFileForTest(t, "Complete Design System Analyze Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	agentID := createLocalUIRestoreAgentForDesignTest(t)
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		)
		VALUES ($1, $2, $3, $4, 'Complete UI Spec', 'analyzing', false, '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert design system profile: %v", err)
	}
	contextPayload := map[string]any{
		"type":                     service.DesignSystemProfileAnalyzeContextType,
		"requester_id":             testUserID,
		"workspace_id":             testWorkspaceID,
		"agent_id":                 agentID,
		"design_system_profile_id": profileID,
		"source_file_id":           created.File.ID,
		"source_revision_id":       created.CurrentRevision.ID,
		"project_id":               projectID,
		"profile_name":             "Complete UI Spec",
		"make_default":             true,
		"candidate_layers":         []map[string]any{{"id": "button-1", "name": "按钮 - 主按钮 - 默认"}},
	}
	contextJSON, _ := json.Marshal(contextPayload)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, context)
		VALUES ($1, $2, NULL, 'running', 0, now(), $3)
		RETURNING id
	`, agentID, testRuntimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("insert analyze task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, profileID)
	})

	output := map[string]any{
		"profile_json": map[string]any{
			"version": "agent-1.0",
			"components": map[string]any{
				"button": map[string]any{
					"label":    "按钮",
					"variants": []map[string]any{{"name": "主按钮", "states": []string{"默认"}}},
				},
			},
			"guidelines": []string{"Use primary button for main submission actions."},
		},
		"analysis_errors": []map[string]any{{"severity": "warning", "message": "部分图层缺少状态命名"}},
		"summary":         "CRM UI specification analyzed.",
	}
	outputJSON, _ := json.Marshal(output)
	completeReq := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete", map[string]any{"output": string(outputJSON)}, testWorkspaceID, "design-system-analyze-complete")
	completeReq = withURLParam(completeReq, "taskId", taskID)
	completeW := httptest.NewRecorder()
	testHandler.CompleteTask(completeW, completeReq)
	if completeW.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", completeW.Code, completeW.Body.String())
	}
	var status string
	var isDefault bool
	var profileJSON []byte
	var analysisErrors []byte
	if err := testPool.QueryRow(ctx, `
		SELECT status, is_default, profile_json, analysis_errors
		FROM design_system_profile
		WHERE id = $1
	`, profileID).Scan(&status, &isDefault, &profileJSON, &analysisErrors); err != nil {
		t.Fatalf("query updated design system profile: %v", err)
	}
	if status != "analyzed" || !isDefault {
		t.Fatalf("profile status/default = %s/%v, want analyzed/true", status, isDefault)
	}
	var profile map[string]any
	if err := json.Unmarshal(profileJSON, &profile); err != nil {
		t.Fatalf("decode profile_json: %v", err)
	}
	if profile["version"] != "agent-1.0" {
		t.Fatalf("profile_json.version = %v", profile["version"])
	}
	var warnings []any
	if err := json.Unmarshal(analysisErrors, &warnings); err != nil {
		t.Fatalf("decode analysis_errors: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("analysis_errors length = %d, want 1", len(warnings))
	}
}

func TestParseDesignSystemProfileAnalyzeOutputRequiresStrictContract(t *testing.T) {
	valid := `{"profile_json":{"version":"agent-1.0"},"analysis_errors":[],"summary":"Analyzed."}`
	for _, tc := range []struct {
		name   string
		output string
	}{
		{name: "wrapped prose", output: "result: " + valid},
		{name: "missing analysis errors", output: `{"profile_json":{"version":"agent-1.0"},"summary":"Analyzed."}`},
		{name: "null analysis errors", output: `{"profile_json":{"version":"agent-1.0"},"analysis_errors":null,"summary":"Analyzed."}`},
		{name: "missing profile version", output: `{"profile_json":{"components":{}},"analysis_errors":[],"summary":"Analyzed."}`},
		{name: "wrong profile version", output: `{"profile_json":{"version":"1.1"},"analysis_errors":[],"summary":"Analyzed."}`},
		{name: "missing summary", output: `{"profile_json":{"version":"agent-1.0"},"analysis_errors":[]}`},
		{name: "empty summary", output: `{"profile_json":{"version":"agent-1.0"},"analysis_errors":[],"summary":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseDesignSystemProfileAnalyzeOutput(tc.output); err == nil {
				t.Fatalf("parseDesignSystemProfileAnalyzeOutput(%q) succeeded, want error", tc.output)
			}
		})
	}
	if _, err := parseDesignSystemProfileAnalyzeOutput(valid); err != nil {
		t.Fatalf("strict valid output rejected: %v", err)
	}
}

func TestSelectDesignSystemProfileAnalyzerAgentRequiresExactLocalAgent(t *testing.T) {
	var cloudAgentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Local UI Restore Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&cloudAgentID); err != nil {
		t.Fatalf("insert cloud Local UI Restore Agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, cloudAgentID)
	})

	workspaceID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace id: %v", err)
	}
	if agent, ok, err := testHandler.selectDesignSystemProfileAnalyzerAgent(context.Background(), workspaceID); err != nil {
		t.Fatalf("select analyzer agent: %v", err)
	} else if ok {
		t.Fatalf("selected non-local analyzer agent %s", uuidToString(agent.ID))
	}
}

func TestCreateDesignSystemProfileWithoutLocalAgentDoesNotPersistProfile(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "No Analyzer Design System Project")
	created := createDesignFileForTest(t, "No Analyzer Design System Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)

	rows, err := testPool.Query(ctx, `
		UPDATE agent
		SET name = name || ' unavailable ' || id::text
		WHERE workspace_id = $1
		  AND name = 'Local UI Restore Agent'
		RETURNING id
	`, testWorkspaceID)
	if err != nil {
		t.Fatalf("hide existing Local UI Restore Agents: %v", err)
	}
	var renamedAgentIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scan renamed analyzer agent: %v", err)
		}
		renamedAgentIDs = append(renamedAgentIDs, id)
	}
	rows.Close()
	t.Cleanup(func() {
		for _, id := range renamedAgentIDs {
			_, _ = testPool.Exec(ctx, `UPDATE agent SET name = 'Local UI Restore Agent' WHERE id = $1`, id)
		}
	})

	workspaceUUID, _ := util.ParseUUID(testWorkspaceID)
	projectUUID, _ := util.ParseUUID(projectID)
	fileUUID, _ := util.ParseUUID(created.File.ID)
	revisionUUID, _ := util.ParseUUID(created.CurrentRevision.ID)
	file, err := testHandler.Queries.GetDesignFileInWorkspace(ctx, db.GetDesignFileInWorkspaceParams{ID: fileUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		t.Fatalf("load design file: %v", err)
	}
	revision, err := testHandler.Queries.GetDesignRevisionInWorkspace(ctx, db.GetDesignRevisionInWorkspaceParams{ID: revisionUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		t.Fatalf("load design revision: %v", err)
	}
	profileName := fmt.Sprintf("No Analyzer UI Spec %d", time.Now().UnixNano())
	if _, err := testHandler.createDesignSystemProfileFromRevision(ctx, createDesignSystemProfileFromRevisionParams{
		WorkspaceID: workspaceUUID,
		ProjectID:   projectUUID,
		File:        file,
		Revision:    revision,
		Name:        profileName,
		IsDefault:   true,
		CreatedBy:   parseUUID(testUserID),
	}); err == nil {
		t.Fatal("create design system profile succeeded without Local UI Restore Agent")
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM design_system_profile WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, profileName).Scan(&count); err != nil {
		t.Fatalf("count orphaned design system profiles: %v", err)
	}
	if count != 0 {
		t.Fatalf("persisted design system profile count = %d, want 0", count)
	}
}

func TestFailDesignSystemProfileAnalyzeTaskMarksProfileFailed(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Fail Design System Analyze Project")
	created := createDesignFileForTest(t, "Fail Design System Analyze Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, 'local', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Fail UI Spec Agent %d", time.Now().UnixNano()), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert fail analyzer agent: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID) })
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES ($1, $2, $3, $4, 'Fail UI Spec', 'analyzing', false, '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert fail design system profile: %v", err)
	}
	contextJSON, _ := json.Marshal(map[string]any{
		"type":                     service.DesignSystemProfileAnalyzeContextType,
		"workspace_id":             testWorkspaceID,
		"project_id":               projectID,
		"design_system_profile_id": profileID,
	})
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, context)
		VALUES ($1, $2, 'running', 0, now(), $3)
		RETURNING id
	`, agentID, runtimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("insert fail analyze task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, profileID)
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/fail", map[string]any{
		"error":          "provider exited before returning JSON",
		"failure_reason": "agent_error",
	}, testWorkspaceID, "design-system-analyze-fail")
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.FailTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("FailTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var taskStatus string
	var profileStatus string
	var analysisErrors []byte
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskStatus); err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status, analysis_errors FROM design_system_profile WHERE id = $1`, profileID).Scan(&profileStatus, &analysisErrors); err != nil {
		t.Fatalf("load failed profile: %v", err)
	}
	if taskStatus != "failed" || profileStatus != "failed" {
		t.Fatalf("task/profile status = %s/%s, want failed/failed", taskStatus, profileStatus)
	}
	if !strings.Contains(string(analysisErrors), "provider exited before returning JSON") {
		t.Fatalf("analysis_errors = %s", string(analysisErrors))
	}
}

func TestCancelDesignSystemProfileAnalyzeTaskMarksProfileFailed(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Cancel Design System Analyze Project")
	created := createDesignFileForTest(t, "Cancel Design System Analyze Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	agentID := handlerTestAgentID(t)
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES ($1, $2, $3, $4, 'Cancel UI Spec', 'analyzing', false, '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert cancel design system profile: %v", err)
	}
	contextJSON, _ := json.Marshal(map[string]any{
		"type":                     service.DesignSystemProfileAnalyzeContextType,
		"workspace_id":             testWorkspaceID,
		"project_id":               projectID,
		"design_system_profile_id": profileID,
	})
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, context)
		VALUES ($1, $2, 'running', 0, now(), $3)
		RETURNING id
	`, agentID, testRuntimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("insert cancel analyze task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, profileID)
	})

	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		t.Fatalf("parse task id: %v", err)
	}
	if _, err := testHandler.TaskService.CancelTask(ctx, taskUUID); err != nil {
		t.Fatalf("cancel analyze task: %v", err)
	}
	var profileStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM design_system_profile WHERE id = $1`, profileID).Scan(&profileStatus); err != nil {
		t.Fatalf("load cancelled profile: %v", err)
	}
	if profileStatus != "failed" {
		t.Fatalf("profile status = %s, want failed", profileStatus)
	}
}

func TestCancelTasksForAgentMarksDesignSystemProfileFailed(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Bulk Cancel Design System Project")
	created := createDesignFileForTest(t, "Bulk Cancel Design System Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, 'local', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Bulk Cancel UI Spec Agent %d", time.Now().UnixNano()), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert bulk cancel analyzer agent: %v", err)
	}
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES ($1, $2, $3, $4, 'Bulk Cancel UI Spec', 'analyzing', false, '{}', '[]', $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert bulk cancel design system profile: %v", err)
	}
	contextJSON, _ := json.Marshal(map[string]any{
		"type":                     service.DesignSystemProfileAnalyzeContextType,
		"workspace_id":             testWorkspaceID,
		"project_id":               projectID,
		"design_system_profile_id": profileID,
	})
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, context)
		VALUES ($1, $2, 'running', 0, now(), $3)
		RETURNING id
	`, agentID, runtimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("insert bulk cancel analyze task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, profileID)
		_, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	if _, err := testHandler.TaskService.CancelTasksForAgent(ctx, parseUUID(agentID)); err != nil {
		t.Fatalf("cancel analyzer tasks: %v", err)
	}
	var profileStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM design_system_profile WHERE id = $1`, profileID).Scan(&profileStatus); err != nil {
		t.Fatalf("load bulk cancelled profile: %v", err)
	}
	if profileStatus != "failed" {
		t.Fatalf("profile status = %s, want failed", profileStatus)
	}
}

func TestHandleFailedTasksMarksDesignSystemProfileFailed(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Sweeper Design System Analyze Project")
	created := createDesignFileForTest(t, "Sweeper Design System Analyze Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	agentID := handlerTestAgentID(t)
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES ($1, $2, $3, $4, 'Sweeper UI Spec', 'analyzing', false, '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert sweeper design system profile: %v", err)
	}
	contextJSON, _ := json.Marshal(map[string]any{
		"type":                     service.DesignSystemProfileAnalyzeContextType,
		"workspace_id":             testWorkspaceID,
		"project_id":               projectID,
		"design_system_profile_id": profileID,
	})
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, started_at, completed_at,
			error, failure_reason, context
		)
		VALUES ($1, $2, 'failed', 0, now() - interval '5 minutes', now(), 'task timed out', 'timeout', $3)
		RETURNING id
	`, agentID, testRuntimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("insert swept analyze task: %v", err)
	}
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatalf("load swept analyze task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, profileID)
	})

	testHandler.TaskService.HandleFailedTasks(ctx, []db.AgentTaskQueue{task})
	var profileStatus string
	var analysisErrors []byte
	if err := testPool.QueryRow(ctx, `SELECT status, analysis_errors FROM design_system_profile WHERE id = $1`, profileID).Scan(&profileStatus, &analysisErrors); err != nil {
		t.Fatalf("load swept profile: %v", err)
	}
	if profileStatus != "failed" || !strings.Contains(string(analysisErrors), "task timed out") {
		t.Fatalf("profile status/errors = %s/%s, want failed timeout error", profileStatus, analysisErrors)
	}
}

func TestCompleteDesignSystemProfileAnalyzeTaskDoesNotOverrideNewerDefault(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectForDesignTest(t, "Default Guard Design System Project")
	created := createDesignFileForTest(t, "Default Guard Design System Source")
	attachDesignFileToProjectForTest(t, created.File.ID, projectID)
	agentID := handlerTestAgentID(t)
	var originalDefaultID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES ($1, $2, $3, $4, 'Original Default UI Spec', 'analyzed', true, '{}', '[]', $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&originalDefaultID); err != nil {
		t.Fatalf("insert original default profile: %v", err)
	}
	var pendingProfileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES ($1, $2, $3, $4, 'Pending Default UI Spec', 'analyzing', false, '{}', '[]', $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&pendingProfileID); err != nil {
		t.Fatalf("insert pending default profile: %v", err)
	}
	contextJSON, _ := json.Marshal(service.DesignSystemProfileAnalyzeContext{
		Type:                      service.DesignSystemProfileAnalyzeContextType,
		WorkspaceID:               testWorkspaceID,
		ProjectID:                 projectID,
		DesignSystemProfileID:     pendingProfileID,
		MakeDefault:               true,
		DefaultProfileIDAtEnqueue: originalDefaultID,
	})
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, context)
		VALUES ($1, $2, 'running', 0, now(), $3)
		RETURNING id
	`, agentID, testRuntimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("insert pending default analysis task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE design_system_profile SET is_default = false WHERE id = $1`, originalDefaultID); err != nil {
		t.Fatalf("clear original default profile: %v", err)
	}
	var newerDefaultID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES ($1, $2, $3, $4, 'Newer Default UI Spec', 'analyzed', true, '{}', '[]', $5)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&newerDefaultID); err != nil {
		t.Fatalf("insert newer default profile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id IN ($1, $2, $3)`, pendingProfileID, newerDefaultID, originalDefaultID)
	})

	output := `{"profile_json":{"version":"agent-1.0"},"analysis_errors":[],"summary":"Analyzed."}`
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete", map[string]any{"output": output}, testWorkspaceID, "design-system-default-guard")
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var pendingStatus string
	var pendingDefault bool
	var newerDefault bool
	if err := testPool.QueryRow(ctx, `SELECT status, is_default FROM design_system_profile WHERE id = $1`, pendingProfileID).Scan(&pendingStatus, &pendingDefault); err != nil {
		t.Fatalf("load pending profile: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT is_default FROM design_system_profile WHERE id = $1`, newerDefaultID).Scan(&newerDefault); err != nil {
		t.Fatalf("load newer default profile: %v", err)
	}
	if pendingStatus != "analyzed" || pendingDefault || !newerDefault {
		t.Fatalf("pending status/default and newer default = %s/%v/%v, want analyzed/false/true", pendingStatus, pendingDefault, newerDefault)
	}
}

func TestCompleteTaskWithMutationRollsBackTaskOnMutationError(t *testing.T) {
	ctx := context.Background()
	agentID := handlerTestAgentID(t)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, context)
		VALUES ($1, $2, 'running', 0, now(), '{}'::jsonb)
		RETURNING id
	`, agentID, testRuntimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert completion mutation task: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		t.Fatalf("parse task id: %v", err)
	}
	_, err = testHandler.TaskService.CompleteTaskWithMutation(ctx, taskUUID, []byte(`{"output":"done"}`), "", "", func(*db.Queries, db.AgentTaskQueue) error {
		return errors.New("profile mutation failed")
	})
	if err == nil {
		t.Fatal("CompleteTaskWithMutation succeeded, want error")
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("load completion mutation task: %v", err)
	}
	if status != "running" {
		t.Fatalf("task status = %s, want running after rolled-back mutation", status)
	}
}

func TestCompleteTaskWithMutationDoesNotTreatMutationNoRowsAsFinalized(t *testing.T) {
	ctx := context.Background()
	agentID := handlerTestAgentID(t)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, context)
		VALUES ($1, $2, 'running', 0, now(), '{}'::jsonb)
		RETURNING id
	`, agentID, testRuntimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert no-rows completion mutation task: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	task, err := testHandler.TaskService.CompleteTaskWithMutation(ctx, parseUUID(taskID), []byte(`{"output":"done"}`), "", "", func(*db.Queries, db.AgentTaskQueue) error {
		return pgx.ErrNoRows
	})
	if err == nil {
		t.Fatalf("CompleteTaskWithMutation returned task %+v without error for mutation pgx.ErrNoRows", task)
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("load no-rows completion mutation task: %v", err)
	}
	if status != "running" {
		t.Fatalf("task status = %s, want running after rolled-back no-rows mutation", status)
	}
}

func TestCompleteUIDraftCreateTaskRejectsEmptyDraftChanges(t *testing.T) {
	template := createFilterTableCatalogTemplateWithoutSlotsForDraftTest(t)
	agentID := handlerTestAgentID(t)
	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id":            agentID,
		"catalog_template_id": template.ID,
		"title":               "Empty Agent Draft",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var created CreateDesignDraftAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.TaskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_draft WHERE workspace_id = $1 AND title = 'Empty Agent Draft'`, testWorkspaceID)
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, created.TaskID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
	output := map[string]any{
		"title":            "Empty Agent Draft",
		"requirement_core": map[string]any{"selected_catalog_template_id": template.ID},
		"slot_values":      map[string]any{},
		"patch":            []any{},
	}
	outputJSON, _ := json.Marshal(output)
	completeReq := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+created.TaskID+"/complete", map[string]any{"output": string(outputJSON)}, testWorkspaceID, "ui-draft-empty-output")
	completeReq = withURLParam(completeReq, "taskId", created.TaskID)
	completeW := httptest.NewRecorder()
	testHandler.CompleteTask(completeW, completeReq)
	if completeW.Code != http.StatusBadRequest {
		t.Fatalf("CompleteTask empty draft: expected 400, got %d: %s", completeW.Code, completeW.Body.String())
	}
	if !strings.Contains(completeW.Body.String(), "non-empty patch") {
		t.Fatalf("CompleteTask empty draft error = %s", completeW.Body.String())
	}
	var status string
	var taskError pgtype.Text
	if err := testPool.QueryRow(context.Background(), `SELECT status, error FROM agent_task_queue WHERE id = $1`, created.TaskID).Scan(&status, &taskError); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	if status != "failed" || !strings.Contains(taskError.String, "non-empty patch") {
		t.Fatalf("task status/error = %s / %q", status, taskError.String)
	}
	var draftCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_draft WHERE workspace_id = $1 AND title = 'Empty Agent Draft'`, testWorkspaceID).Scan(&draftCount); err != nil {
		t.Fatalf("count design drafts: %v", err)
	}
	if draftCount != 0 {
		t.Fatalf("created %d empty drafts, want 0", draftCount)
	}
}

func TestCompleteUIDraftCreateTaskCreatesIssueDraftFromSelectedTemplate(t *testing.T) {
	template := createFilterTableCatalogTemplateForDraftTest(t)
	projectID := createProjectForDesignTest(t, "UI Agent Selected Template Project")
	issueID := createIssueForDesignTest(t, "服务记录列表 UI设计", projectID)
	_, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET description = $2
		WHERE id = $1
	`, issueID, "服务记录列表页，需要筛选、表格和分页。")
	if err != nil {
		t.Fatalf("update issue description: %v", err)
	}
	agentID := handlerTestAgentID(t)
	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentID,
		"issue_id": issueID,
		"title":    "服务记录列表草稿",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var created CreateDesignDraftAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.TaskID)
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, created.TaskID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
	output := map[string]any{
		"title": "服务记录列表生成稿",
		"requirement_core": map[string]any{
			"version":                      "1.0",
			"title":                        "服务记录列表",
			"pageType":                     "saas.filter-table-pagination",
			"selected_catalog_template_id": template.ID,
		},
		"slot_values": map[string]any{
			"page_title":    "服务记录",
			"filter_fields": []any{"门店", "治疗师", "日期"},
			"table_columns": []any{"服务时间", "客户", "状态", "操作"},
		},
		"patch": []any{},
	}
	outputJSON, _ := json.Marshal(output)
	completeReq := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+created.TaskID+"/complete", map[string]any{"output": string(outputJSON)}, testWorkspaceID, "ui-draft-selected-template")
	completeReq = withURLParam(completeReq, "taskId", created.TaskID)
	completeW := httptest.NewRecorder()
	testHandler.CompleteTask(completeW, completeReq)
	if completeW.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", completeW.Code, completeW.Body.String())
	}
	var draftID, draftIssueID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id, issue_id
		FROM design_draft
		WHERE workspace_id = $1 AND title = '服务记录列表生成稿'
		ORDER BY created_at DESC
		LIMIT 1
	`, testWorkspaceID).Scan(&draftID, &draftIssueID); err != nil {
		t.Fatalf("expected created issue design draft: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM design_draft WHERE id = $1`, draftID) })
	if draftIssueID != issueID {
		t.Fatalf("draft issue_id = %s, want %s", draftIssueID, issueID)
	}
}

func TestCreateDesignDraftRejectsLayoutPatch(t *testing.T) {
	template := createCatalogTemplateForDraftTest(t)
	req := newRequest("POST", "/api/design-drafts?workspace_id="+testWorkspaceID, map[string]any{
		"catalog_template_id": template.ID,
		"patch":               []map[string]any{{"op": "replace", "path": "/layers/main-title/x", "value": 10}},
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraft(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateDesignDraft layout patch: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDesignDraftValidatesTemplateSlotSchema(t *testing.T) {
	design := createDesignFileForTest(t, "Slot Schema Template Source")
	if design.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	libraryKey := fmt.Sprintf("slot-schema-library-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_template_library WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, libraryKey)
	})
	publishReq := newRequest("POST", "/api/design-revisions/"+design.CurrentRevision.ID+"/publish-template?workspace_id="+testWorkspaceID, map[string]any{
		"library_key":  libraryKey,
		"template_key": fmt.Sprintf("slot-schema-template-%d", time.Now().UnixNano()),
		"name":         "Slot Schema Template",
		"slot_schema": map[string]any{
			"title": map[string]any{"type": "text", "required": true},
			"count": map[string]any{"type": "number"},
		},
	})
	publishReq = withDesignURLParams(publishReq, "revisionId", design.CurrentRevision.ID)
	publishW := httptest.NewRecorder()
	testHandler.PublishDesignRevisionAsTemplate(publishW, publishReq)
	if publishW.Code != http.StatusCreated {
		t.Fatalf("PublishDesignRevisionAsTemplate: expected 201, got %d: %s", publishW.Code, publishW.Body.String())
	}
	var template DesignCatalogTemplateResponse
	if err := json.NewDecoder(publishW.Body).Decode(&template); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	if len(template.SlotSchema) == 0 || string(template.SlotSchema) == "null" {
		t.Fatal("expected published template response to include slot_schema")
	}

	missingReq := newRequest("POST", "/api/design-drafts?workspace_id="+testWorkspaceID, map[string]any{
		"catalog_template_id": template.ID,
		"slot_values":         map[string]any{"count": 1},
	})
	missingW := httptest.NewRecorder()
	testHandler.CreateDesignDraft(missingW, missingReq)
	if missingW.Code != http.StatusBadRequest {
		t.Fatalf("missing required slot: expected 400, got %d: %s", missingW.Code, missingW.Body.String())
	}

	typeReq := newRequest("POST", "/api/design-drafts?workspace_id="+testWorkspaceID, map[string]any{
		"catalog_template_id": template.ID,
		"slot_values":         map[string]any{"title": "Hello", "count": "not-number"},
	})
	typeW := httptest.NewRecorder()
	testHandler.CreateDesignDraft(typeW, typeReq)
	if typeW.Code != http.StatusBadRequest {
		t.Fatalf("wrong slot type: expected 400, got %d: %s", typeW.Code, typeW.Body.String())
	}

	validReq := newRequest("POST", "/api/design-drafts?workspace_id="+testWorkspaceID, map[string]any{
		"catalog_template_id": template.ID,
		"slot_values":         map[string]any{"title": "Hello", "count": 2},
	})
	validW := httptest.NewRecorder()
	testHandler.CreateDesignDraft(validW, validReq)
	if validW.Code != http.StatusCreated {
		t.Fatalf("valid slots: expected 201, got %d: %s", validW.Code, validW.Body.String())
	}
	var draft DesignDraftResponse
	if err := json.NewDecoder(validW.Body).Decode(&draft); err != nil {
		t.Fatalf("decode draft response: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM design_draft WHERE id = $1`, draft.ID) })
}

func TestMaterializeDesignDraftCreatesGeneratedDesign(t *testing.T) {
	template := createCatalogTemplateWithTextSlotForDraftTest(t)
	createReq := newRequest("POST", "/api/design-drafts?workspace_id="+testWorkspaceID, map[string]any{
		"catalog_template_id": template.ID,
		"title":               "Materialized Draft",
		"slot_values":         map[string]any{"title": "Slot title"},
		"patch":               []map[string]any{{"op": "replace", "path": "/layers/layer-1/name", "value": "Generated Page"}},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignDraft(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignDraft: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var draft DesignDraftResponse
	if err := json.NewDecoder(createW.Body).Decode(&draft); err != nil {
		t.Fatalf("decode draft response: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM design_draft WHERE id = $1`, draft.ID) })

	matReq := newRequest("POST", "/api/design-drafts/"+draft.ID+"/materialize?workspace_id="+testWorkspaceID, nil)
	matReq = withDesignURLParams(matReq, "id", draft.ID)
	matW := httptest.NewRecorder()
	testHandler.MaterializeDesignDraft(matW, matReq)
	if matW.Code != http.StatusCreated {
		t.Fatalf("MaterializeDesignDraft: expected 201, got %d: %s", matW.Code, matW.Body.String())
	}
	var resp DesignDraftMaterializeResponse
	if err := json.NewDecoder(matW.Body).Decode(&resp); err != nil {
		t.Fatalf("decode materialize response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, resp.DesignFile.File.ID)
	})
	if resp.DesignFile.File.SourceType != "ai_generated" {
		t.Fatalf("source_type = %q, want ai_generated", resp.DesignFile.File.SourceType)
	}
	if resp.Draft.Status != "validated" {
		t.Fatalf("draft status = %q, want validated", resp.Draft.Status)
	}
	if resp.Draft.GeneratedFileID == nil || *resp.Draft.GeneratedFileID != resp.DesignFile.File.ID {
		t.Fatalf("generated_file_id = %v, want %s", resp.Draft.GeneratedFileID, resp.DesignFile.File.ID)
	}
	if resp.Draft.MaterializedAt == nil || *resp.Draft.MaterializedAt == "" {
		t.Fatal("expected materialized_at")
	}
	if resp.DesignFile.CurrentRevision == nil {
		t.Fatal("expected generated current revision")
	}
	var native map[string]any
	if err := json.Unmarshal(resp.DesignFile.CurrentRevision.NativeJSON, &native); err != nil {
		t.Fatalf("decode generated native json: %v", err)
	}
	layers := native["layers"].(map[string]any)
	layer := layers["layer-1"].(map[string]any)
	if layer["name"] != "Generated Page" {
		t.Fatalf("materialized layer name = %q, want Generated Page", layer["name"])
	}
	titleLayer := layers["title-layer"].(map[string]any)
	text := titleLayer["text"].(map[string]any)
	if text["characters"] != "Slot title" || text["text"] != "Slot title" {
		t.Fatalf("materialized slot text = %#v, want Slot title", text)
	}
}

func TestCreateDesignFileWithProjectAndFolder(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Design Project")
	folderID := createDesignFolderForTest(t, projectID, "App Screens")
	req := newRequest("POST", "/api/design-files?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Project Design",
		"project_id":  projectID,
		"folder_id":   folderID,
		"source_type": "upload",
		"native_json": minimalDesignNativeJSON("Project Design"),
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignFile(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignFile with project/folder: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.File.ProjectID == nil || *resp.File.ProjectID != projectID {
		t.Fatalf("project_id = %v, want %s", resp.File.ProjectID, projectID)
	}
	if resp.File.FolderID == nil || *resp.File.FolderID != folderID {
		t.Fatalf("folder_id = %v, want %s", resp.File.FolderID, folderID)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, resp.File.ID)
	})
}

func TestCreateDesignFileRejectsFolderFromAnotherProject(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Design Project A")
	otherProjectID := createProjectForDesignTest(t, "Design Project B")
	folderID := createDesignFolderForTest(t, otherProjectID, "Other Project Folder")
	req := newRequest("POST", "/api/design-files?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Wrong Folder Design",
		"project_id":  projectID,
		"folder_id":   folderID,
		"source_type": "upload",
		"native_json": minimalDesignNativeJSON("Wrong Folder Design"),
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignFile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateDesignFile wrong folder: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDesignFileRejectsInvalidNativeJSON(t *testing.T) {
	req := newRequest("POST", "/api/design-files?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Broken Design",
		"source_type": "upload",
		"native_json": map[string]any{
			"version": "1.0",
			"file":    map[string]any{"title": "Broken", "sourceType": "upload"},
			"frames":  []map[string]any{{"id": "frame-1", "name": "Broken", "rootLayerId": "missing", "width": 100, "height": 100}},
			"layers":  map[string]any{},
			"assets":  map[string]any{},
		},
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignFile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateDesignFile invalid JSON: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAndGetDesignFiles(t *testing.T) {
	created := createDesignFileForTest(t, "Listable Design")

	listReq := newRequest("GET", "/api/design-files?workspace_id="+testWorkspaceID, nil)
	listW := httptest.NewRecorder()
	testHandler.ListDesignFiles(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignFiles: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResp struct {
		DesignFiles []DesignFileResponse `json:"design_files"`
		Total       int                  `json:"total"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode ListDesignFiles: %v", err)
	}
	found := false
	for _, file := range listResp.DesignFiles {
		if file.ID == created.File.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created design file %s not found in list", created.File.ID)
	}

	getReq := withURLParam(newRequest("GET", "/api/design-files/"+created.File.ID+"?workspace_id="+testWorkspaceID, nil), "id", created.File.ID)
	getW := httptest.NewRecorder()
	testHandler.GetDesignFile(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GetDesignFile: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
}

func TestGetDesignFileContextReturnsSummaryWithoutNativeJSON(t *testing.T) {
	created := createDesignFileForTest(t, "Context Design")
	req := withURLParam(newRequest("GET", "/api/design-files/"+created.File.ID+"/context?workspace_id="+testWorkspaceID, nil), "id", created.File.ID)
	w := httptest.NewRecorder()
	testHandler.GetDesignFileContext(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetDesignFileContext: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode context response: %v", err)
	}
	if _, ok := resp["native_json"]; ok {
		t.Fatal("context response should not include native_json")
	}
	if _, ok := resp["nativeJson"]; ok {
		t.Fatal("context response should not include nativeJson")
	}
	frames, ok := resp["frames"].([]any)
	if !ok || len(frames) != 1 {
		t.Fatalf("frames = %T len %d, want one frame summary", resp["frames"], len(frames))
	}
	frame, ok := frames[0].(map[string]any)
	if !ok {
		t.Fatalf("frame summary type = %T", frames[0])
	}
	if frame["id"] != "frame-1" || frame["name"] != "Context Design" || frame["layerCount"] != float64(1) {
		t.Fatalf("unexpected frame summary: %+v", frame)
	}
	if _, ok := frame["layers"]; ok {
		t.Fatal("frame summary should not include full layers")
	}
}

func TestGetDesignFrameContextReturnsOnlyRequestedFrameDetails(t *testing.T) {
	created := createDesignFileForTest(t, "Frame Context Design")
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Frame Context Design"))

	req := withDesignURLParams(newRequest("GET", "/api/design-files/"+created.File.ID+"/frames/frame-main/context?workspace_id="+testWorkspaceID, nil), "id", created.File.ID, "frameId", "frame-main")
	w := httptest.NewRecorder()
	testHandler.GetDesignFrameContext(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetDesignFrameContext: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode frame context response: %v", err)
	}
	frame := resp["frame"].(map[string]any)
	if frame["id"] != "frame-main" {
		t.Fatalf("frame id = %v, want frame-main", frame["id"])
	}
	layers := resp["layers"].(map[string]any)
	for _, id := range []string{"main-root", "main-title", "main-image", "main-offscreen"} {
		if _, ok := layers[id]; !ok {
			t.Fatalf("expected layer %s in frame context", id)
		}
	}
	if _, ok := layers["secondary-title"]; ok {
		t.Fatal("frame context included layer from another frame")
	}
	assets := resp["assets"].(map[string]any)
	for _, id := range []string{"asset-preview-main", "asset-thumb-main", "asset-hero", "asset-export-main"} {
		if _, ok := assets[id]; !ok {
			t.Fatalf("expected asset %s in frame context", id)
		}
	}
	if _, ok := assets["asset-secondary"]; ok {
		t.Fatal("frame context included unrelated asset")
	}
	if exportables := resp["exportables"].([]any); len(exportables) != 1 {
		t.Fatalf("exportables len = %d, want 1", len(exportables))
	}
	text := resp["text"].([]any)
	if len(text) != 1 || text[0].(map[string]any)["layerId"] != "main-title" {
		t.Fatalf("unexpected text context: %+v", text)
	}
}

func TestDesignContextSanitizesHistoricalEmbeddedBinary(t *testing.T) {
	created := createDesignFileForTest(t, "Historical Binary Context Design")
	nativeJSON := contextDesignNativeJSON("Historical Binary Context Design")
	nativeJSON["frames"].([]map[string]any)[0]["thumbnailDataUrl"] = "data:image/png;base64,AAAA"
	assets := nativeJSON["assets"].(map[string]any)
	assets["asset-hero"].(map[string]any)["bytes"] = []int{1, 2, 3}
	assets["asset-hero"].(map[string]any)["url"] = "data:image/png;base64,BBBB"
	layers := nativeJSON["layers"].(map[string]any)
	layers["main-image"].(map[string]any)["buffer"] = []int{4, 5, 6}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)

	req := withDesignURLParams(newRequest("GET", "/api/design-files/"+created.File.ID+"/frames/frame-main/context?workspace_id="+testWorkspaceID, nil), "id", created.File.ID, "frameId", "frame-main")
	w := httptest.NewRecorder()
	testHandler.GetDesignFrameContext(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetDesignFrameContext: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode frame context response: %v", err)
	}
	frame := resp["frame"].(map[string]any)
	if _, ok := frame["thumbnailDataUrl"]; ok {
		t.Fatal("frame context leaked thumbnailDataUrl data URL")
	}
	assetsResp := resp["assets"].(map[string]any)
	hero := assetsResp["asset-hero"].(map[string]any)
	if _, ok := hero["bytes"]; ok {
		t.Fatal("frame context leaked asset bytes")
	}
	if _, ok := hero["url"]; ok {
		t.Fatal("frame context leaked data:image asset URL")
	}
	layersResp := resp["layers"].(map[string]any)
	imageLayer := layersResp["main-image"].(map[string]any)
	if _, ok := imageLayer["buffer"]; ok {
		t.Fatal("frame context leaked layer buffer")
	}
}

func TestGetDesignSelectionContextWithBoundsReturnsIntersectingLayers(t *testing.T) {
	created := createDesignFileForTest(t, "Selection Context Design")
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Selection Context Design"))

	req := withDesignURLParams(newRequest("POST", "/api/design-files/"+created.File.ID+"/frames/frame-main/selection-context?workspace_id="+testWorkspaceID, map[string]any{
		"selectionBounds": map[string]any{"x": 35, "y": 35, "width": 230, "height": 80},
	}), "id", created.File.ID, "frameId", "frame-main")
	w := httptest.NewRecorder()
	testHandler.GetDesignSelectionContext(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetDesignSelectionContext: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode selection context response: %v", err)
	}
	layers := resp["layers"].(map[string]any)
	if _, ok := layers["main-title"]; !ok {
		t.Fatal("selection context should include intersecting main-title layer")
	}
	if _, ok := layers["main-root"]; !ok {
		t.Fatal("selection context should include intersecting main-root layer")
	}
	for _, id := range []string{"main-image", "main-offscreen", "secondary-title"} {
		if _, ok := layers[id]; ok {
			t.Fatalf("selection context should not include non-intersecting layer %s", id)
		}
	}
	resolved := resp["resolvedLayerIds"].([]any)
	if len(resolved) != 2 || resolved[0] != "main-root" || resolved[1] != "main-title" {
		t.Fatalf("resolvedLayerIds = %+v, want [main-root main-title]", resolved)
	}
	text := resp["text"].([]any)
	if len(text) != 1 || text[0].(map[string]any)["layerId"] != "main-title" {
		t.Fatalf("unexpected selection text context: %+v", text)
	}
}

func TestCreateDesignRestorePackFrameScope(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Pack Frame Design")
	nativeJSON := contextDesignNativeJSON("Restore Pack Frame Design")
	layers := nativeJSON["layers"].(map[string]any)
	layers["hidden-trash"] = map[string]any{
		"id":      "hidden-trash",
		"frameId": "frame-main",
		"name":    "Hidden Draft",
		"type":    "text",
		"visible": false,
		"x":       16,
		"y":       16,
		"width":   100,
		"height":  20,
		"text":    map[string]any{"text": "do not upload"},
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)

	req := withURLParam(newRequest("POST", "/api/design-files/"+created.File.ID+"/restore-pack?workspace_id="+testWorkspaceID, map[string]any{
		"scope": map[string]any{
			"version":      "1.0",
			"kind":         "frame",
			"designFileId": created.File.ID,
			"revisionId":   created.CurrentRevision.ID,
			"frameId":      "frame-main",
		},
	}), "id", created.File.ID)
	w := httptest.NewRecorder()
	testHandler.CreateDesignRestorePack(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CreateDesignRestorePack frame: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode restore pack: %v", err)
	}
	if resp["version"] != "1.0" {
		t.Fatalf("version = %#v", resp["version"])
	}
	scope := resp["scope"].(map[string]any)
	if scope["kind"] != "frame" || scope["frameId"] != "frame-main" {
		t.Fatalf("scope = %#v", scope)
	}
	frames := resp["frames"].([]any)
	if len(frames) != 1 {
		t.Fatalf("frames = %#v, want one frame", frames)
	}
	frame := frames[0].(map[string]any)
	layersResp := frame["layers"].(map[string]any)
	if _, ok := layersResp["main-image"]; !ok {
		t.Fatalf("restore pack should include visible image layer: %#v", layersResp)
	}
	if _, ok := layersResp["hidden-trash"]; ok {
		t.Fatalf("restore pack should exclude hidden layer: %#v", layersResp)
	}
	assets := resp["assets"].(map[string]any)
	if _, ok := assets["asset-hero"]; !ok {
		t.Fatalf("restore pack assets missing image asset: %#v", assets)
	}
	hints := resp["implementationHints"].(map[string]any)
	if !designRestoreAnySliceContainsString(hints["assetLayerIds"], "main-image") {
		t.Fatalf("implementationHints.assetLayerIds = %#v, want main-image", hints["assetLayerIds"])
	}
}

func TestCreateDesignRestorePackFigmaGroupScope(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Pack Group Design")
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, restorePackGroupedNativeJSONForTest("Restore Pack Group Design"))

	req := withURLParam(newRequest("POST", "/api/design-files/"+created.File.ID+"/restore-pack?workspace_id="+testWorkspaceID, map[string]any{
		"scope": map[string]any{
			"version":      "1.0",
			"kind":         "figma_group",
			"designFileId": created.File.ID,
			"revisionId":   created.CurrentRevision.ID,
			"groupId":      "group-wallet",
		},
	}), "id", created.File.ID)
	w := httptest.NewRecorder()
	testHandler.CreateDesignRestorePack(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CreateDesignRestorePack group: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode restore pack: %v", err)
	}
	frames := resp["frames"].([]any)
	if len(frames) != 2 {
		t.Fatalf("frames = %#v, want grouped frames", frames)
	}
	structure := resp["designStructure"].(map[string]any)
	if structure["mode"] != "figma_group" || structure["groupName"] != "钱包首页" {
		t.Fatalf("designStructure = %#v", structure)
	}
}

func TestCreateDesignRestorePackSelectionBoundsScope(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Pack Selection Design")
	nativeJSON := contextDesignNativeJSON("Restore Pack Selection Design")
	layers := nativeJSON["layers"].(map[string]any)
	layers["select-account"] = map[string]any{
		"id":      "select-account",
		"frameId": "frame-main",
		"name":    "请选择提现账户",
		"type":    "text",
		"visible": true,
		"x":       48,
		"y":       92,
		"width":   180,
		"height":  24,
		"text":    map[string]any{"characters": "请选择提现账户", "fontSize": 16},
	}
	layers["amount-input"] = map[string]any{
		"id":      "amount-input",
		"frameId": "frame-main",
		"name":    "请输入提现金额",
		"type":    "text",
		"visible": true,
		"x":       48,
		"y":       132,
		"width":   180,
		"height":  24,
		"text":    map[string]any{"characters": "请输入提现金额", "fontSize": 16},
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)

	req := withURLParam(newRequest("POST", "/api/design-files/"+created.File.ID+"/restore-pack?workspace_id="+testWorkspaceID, map[string]any{
		"scope": map[string]any{
			"version":      "1.0",
			"kind":         "selection_bounds",
			"designFileId": created.File.ID,
			"revisionId":   created.CurrentRevision.ID,
			"frameId":      "frame-main",
			"selectionBounds": map[string]any{
				"x":      35,
				"y":      35,
				"width":  260,
				"height": 150,
			},
		},
	}), "id", created.File.ID)
	w := httptest.NewRecorder()
	testHandler.CreateDesignRestorePack(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CreateDesignRestorePack selection bounds: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode restore pack: %v", err)
	}
	scope := resp["scope"].(map[string]any)
	if scope["kind"] != "selection_bounds" {
		t.Fatalf("scope = %#v", scope)
	}
	hints := resp["implementationHints"].(map[string]any)
	if hints["interactionCueCount"].(float64) < 2 {
		t.Fatalf("implementationHints = %#v, want select and input cues", hints)
	}
	if !designRestoreAnySliceContainsString(hints["interactionLayerIds"], "select-account") || !designRestoreAnySliceContainsString(hints["interactionLayerIds"], "amount-input") {
		t.Fatalf("interactionLayerIds = %#v, want select-account and amount-input", hints["interactionLayerIds"])
	}
}

func TestDesignContextsCanReadRequestedHistoricalRevision(t *testing.T) {
	created := createDesignFileForTest(t, "Historical Requested Context Design")
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Historical Requested Context Design"))
	currentRevisionID := createDesignRevisionForTest(t, created.File.ID, 2, minimalDesignNativeJSON("Current Context Design"), true)
	historicalRevisionID := created.CurrentRevision.ID

	fileReq := withURLParam(newRequest("GET", "/api/design-files/"+created.File.ID+"/context?workspace_id="+testWorkspaceID+"&revision_id="+historicalRevisionID, nil), "id", created.File.ID)
	fileW := httptest.NewRecorder()
	testHandler.GetDesignFileContext(fileW, fileReq)
	if fileW.Code != http.StatusOK {
		t.Fatalf("GetDesignFileContext historical: expected 200, got %d: %s", fileW.Code, fileW.Body.String())
	}
	var fileResp map[string]any
	if err := json.NewDecoder(fileW.Body).Decode(&fileResp); err != nil {
		t.Fatalf("decode file context response: %v", err)
	}
	if fileResp["revisionId"] != historicalRevisionID {
		t.Fatalf("file context revisionId = %v, want %s", fileResp["revisionId"], historicalRevisionID)
	}
	if fileResp["name"] != "Historical Requested Context Design" {
		t.Fatalf("file context name = %v, want historical title", fileResp["name"])
	}

	frameReq := withDesignURLParams(newRequest("GET", "/api/design-files/"+created.File.ID+"/frames/frame-main/context?workspace_id="+testWorkspaceID+"&revision_id="+historicalRevisionID, nil), "id", created.File.ID, "frameId", "frame-main")
	frameW := httptest.NewRecorder()
	testHandler.GetDesignFrameContext(frameW, frameReq)
	if frameW.Code != http.StatusOK {
		t.Fatalf("GetDesignFrameContext historical: expected 200, got %d: %s", frameW.Code, frameW.Body.String())
	}
	var frameResp map[string]any
	if err := json.NewDecoder(frameW.Body).Decode(&frameResp); err != nil {
		t.Fatalf("decode frame context response: %v", err)
	}
	if frameResp["revisionId"] != historicalRevisionID {
		t.Fatalf("frame context revisionId = %v, want %s", frameResp["revisionId"], historicalRevisionID)
	}
	frame := frameResp["frame"].(map[string]any)
	if frame["id"] != "frame-main" {
		t.Fatalf("frame id = %v, want historical frame-main", frame["id"])
	}

	selectionReq := withDesignURLParams(newRequest("POST", "/api/design-files/"+created.File.ID+"/frames/frame-main/selection-context?workspace_id="+testWorkspaceID+"&revision_id="+historicalRevisionID, map[string]any{
		"layerIds": []string{"main-title"},
	}), "id", created.File.ID, "frameId", "frame-main")
	selectionW := httptest.NewRecorder()
	testHandler.GetDesignSelectionContext(selectionW, selectionReq)
	if selectionW.Code != http.StatusOK {
		t.Fatalf("GetDesignSelectionContext historical: expected 200, got %d: %s", selectionW.Code, selectionW.Body.String())
	}
	var selectionResp map[string]any
	if err := json.NewDecoder(selectionW.Body).Decode(&selectionResp); err != nil {
		t.Fatalf("decode selection context response: %v", err)
	}
	if selectionResp["revisionId"] != historicalRevisionID {
		t.Fatalf("selection context revisionId = %v, want %s", selectionResp["revisionId"], historicalRevisionID)
	}
	layers := selectionResp["layers"].(map[string]any)
	if _, ok := layers["main-title"]; !ok {
		t.Fatal("selection context should include historical main-title layer")
	}

	currentFrameReq := withDesignURLParams(newRequest("GET", "/api/design-files/"+created.File.ID+"/frames/frame-main/context?workspace_id="+testWorkspaceID, nil), "id", created.File.ID, "frameId", "frame-main")
	currentFrameW := httptest.NewRecorder()
	testHandler.GetDesignFrameContext(currentFrameW, currentFrameReq)
	if currentFrameW.Code != http.StatusNotFound {
		t.Fatalf("GetDesignFrameContext current: expected 404 for old frame on current revision %s, got %d: %s", currentRevisionID, currentFrameW.Code, currentFrameW.Body.String())
	}
}

func TestGetDesignRevision(t *testing.T) {
	created := createDesignFileForTest(t, "Revision Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}

	listReq := withURLParam(newRequest("GET", "/api/design-files/"+created.File.ID+"/revisions?workspace_id="+testWorkspaceID, nil), "id", created.File.ID)
	listW := httptest.NewRecorder()
	testHandler.ListDesignRevisions(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignRevisions: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResp struct {
		Revisions []map[string]any `json:"revisions"`
		Total     int              `json:"total"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode ListDesignRevisions: %v", err)
	}
	if listResp.Total == 0 || len(listResp.Revisions) == 0 {
		t.Fatal("expected revisions in list response")
	}
	if _, ok := listResp.Revisions[0]["native_json"]; ok {
		t.Fatal("ListDesignRevisions response should not include native_json")
	}

	revisionReq := withURLParam(newRequest("GET", "/api/design-revisions/"+created.CurrentRevision.ID+"?workspace_id="+testWorkspaceID, nil), "revisionId", created.CurrentRevision.ID)
	revisionW := httptest.NewRecorder()
	testHandler.GetDesignRevision(revisionW, revisionReq)
	if revisionW.Code != http.StatusOK {
		t.Fatalf("GetDesignRevision: expected 200, got %d: %s", revisionW.Code, revisionW.Body.String())
	}
	var revisionResp DesignRevisionResponse
	if err := json.NewDecoder(revisionW.Body).Decode(&revisionResp); err != nil {
		t.Fatalf("decode GetDesignRevision: %v", err)
	}
	if len(revisionResp.NativeJSON) == 0 {
		t.Fatal("GetDesignRevision response should include native_json")
	}
}

func postDesignLayerLightweightEditForTest(t *testing.T, fileID string, layerID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := withDesignURLParams(newRequest("POST", "/api/design-files/"+fileID+"/layers/"+layerID+"/lightweight-edit?workspace_id="+testWorkspaceID, body), "id", fileID, "layerId", layerID)
	w := httptest.NewRecorder()
	testHandler.UpdateDesignLayerLightweight(w, req)
	return w
}

func decodeDesignRevisionNativeJSONForTest(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode revision native_json: %v", err)
	}
	return doc
}

func layerFromNativeJSONForTest(t *testing.T, doc map[string]any, layerID string) map[string]any {
	t.Helper()
	layers, ok := doc["layers"].(map[string]any)
	if !ok {
		t.Fatalf("native_json layers type = %T", doc["layers"])
	}
	layer, ok := layers[layerID].(map[string]any)
	if !ok {
		t.Fatalf("native_json layer %s type = %T", layerID, layers[layerID])
	}
	return layer
}

func lastLightweightEditFromNativeJSONForTest(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	source, ok := doc["source"].(map[string]any)
	if !ok {
		t.Fatalf("native_json source type = %T", doc["source"])
	}
	lastEdit, ok := source["lastLightweightEdit"].(map[string]any)
	if !ok {
		t.Fatalf("native_json source.lastLightweightEdit type = %T", source["lastLightweightEdit"])
	}
	return lastEdit
}

func assertLightweightEditChangedFieldsForTest(t *testing.T, lastEdit map[string]any, want []string) {
	t.Helper()
	changedFields, ok := lastEdit["changedFields"].([]any)
	if !ok {
		t.Fatalf("lastLightweightEdit.changedFields type = %T", lastEdit["changedFields"])
	}
	if len(changedFields) != len(want) {
		t.Fatalf("lastLightweightEdit.changedFields = %+v, want %+v", changedFields, want)
	}
	for i, field := range want {
		if changedFields[i] != field {
			t.Fatalf("lastLightweightEdit.changedFields[%d] = %v, want %s (changedFields=%+v)", i, changedFields[i], field, changedFields)
		}
	}
}

func importFidelityReportFromNativeJSONForTest(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	source, ok := doc["source"].(map[string]any)
	if !ok {
		t.Fatalf("native_json source type = %T", doc["source"])
	}
	report, ok := source["importFidelityReport"].(map[string]any)
	if !ok {
		t.Fatalf("source.importFidelityReport type = %T", source["importFidelityReport"])
	}
	return report
}

func TestUpdateDesignLayerLightweightTextMutatesCurrentRevisionAndPreservesStyle(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Text Edit Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	nativeJSON := contextDesignNativeJSON("Lightweight Text Edit Design")
	nativeJSON["source"] = map[string]any{"fixtureKey": "preserve-me"}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)

	w := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-title", map[string]any{
		"revision_id": created.CurrentRevision.ID,
		"text":        "Start building",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateDesignLayerLightweight text: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode lightweight edit response: %v", err)
	}
	if resp.CurrentRevision == nil {
		t.Fatal("expected current revision response")
	}
	if resp.CurrentRevision.ID != created.CurrentRevision.ID {
		t.Fatalf("revision id = %s, want existing revision %s", resp.CurrentRevision.ID, created.CurrentRevision.ID)
	}
	if resp.CurrentRevision.RevisionNumber != created.CurrentRevision.RevisionNumber {
		t.Fatalf("revision number = %d, want existing revision number %d", resp.CurrentRevision.RevisionNumber, created.CurrentRevision.RevisionNumber)
	}
	if resp.File.CurrentRevisionID == nil || *resp.File.CurrentRevisionID != resp.CurrentRevision.ID {
		t.Fatalf("response current_revision_id = %v, want %s", resp.File.CurrentRevisionID, resp.CurrentRevision.ID)
	}

	var currentRevisionID string
	if err := testPool.QueryRow(context.Background(), `SELECT current_revision_id FROM design_file WHERE id = $1`, created.File.ID).Scan(&currentRevisionID); err != nil {
		t.Fatalf("query current_revision_id: %v", err)
	}
	if currentRevisionID != resp.CurrentRevision.ID {
		t.Fatalf("db current_revision_id = %s, want %s", currentRevisionID, resp.CurrentRevision.ID)
	}

	doc := decodeDesignRevisionNativeJSONForTest(t, resp.CurrentRevision.NativeJSON)
	textLayer := layerFromNativeJSONForTest(t, doc, "main-title")
	text, ok := textLayer["text"].(map[string]any)
	if !ok {
		t.Fatalf("text layer text type = %T", textLayer["text"])
	}
	if text["text"] != "Start building" {
		t.Fatalf("text.text = %v, want Start building", text["text"])
	}
	if text["characters"] != "Start building" {
		t.Fatalf("text.characters = %v, want Start building", text["characters"])
	}
	if text["fontFamily"] != "Inter" || text["fontSize"] != float64(24) || text["fontWeight"] != float64(700) {
		t.Fatalf("text style was not preserved: %+v", text)
	}
	color, ok := text["color"].(map[string]any)
	if !ok || color["a"] != float64(1) {
		t.Fatalf("text color style was not preserved: %+v", text["color"])
	}
	source, ok := doc["source"].(map[string]any)
	if !ok {
		t.Fatalf("native_json source type = %T", doc["source"])
	}
	if source["fixtureKey"] != "preserve-me" {
		t.Fatalf("source.fixtureKey = %v, want preserve-me (source=%+v)", source["fixtureKey"], source)
	}
	lastEdit := lastLightweightEditFromNativeJSONForTest(t, doc)
	if lastEdit["layerId"] != "main-title" {
		t.Fatalf("lastLightweightEdit.layerId = %v, want main-title", lastEdit["layerId"])
	}
	if lastEdit["layerName"] != "Title" {
		t.Fatalf("lastLightweightEdit.layerName = %v, want Title", lastEdit["layerName"])
	}
	if lastEdit["frameId"] != "frame-main" {
		t.Fatalf("lastLightweightEdit.frameId = %v, want frame-main", lastEdit["frameId"])
	}
	if lastEdit["summary"] != "Updated text for Title" {
		t.Fatalf("lastLightweightEdit.summary = %v, want Updated text for Title", lastEdit["summary"])
	}
	assertLightweightEditChangedFieldsForTest(t, lastEdit, []string{"text"})
}

func TestUpdateDesignLayerLightweightSemanticUpdatesKeys(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Semantic Edit Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Lightweight Semantic Edit Design"))

	w := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-title", map[string]any{
		"revision_id": created.CurrentRevision.ID,
		"semantic": map[string]string{
			"role":      "headline",
			"moduleKey": "hero",
			"stateKey":  "default",
			"slotKey":   "title",
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateDesignLayerLightweight semantic: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode lightweight edit response: %v", err)
	}
	if resp.CurrentRevision == nil || resp.CurrentRevision.ID != created.CurrentRevision.ID {
		t.Fatalf("current revision = %+v, want existing revision %s", resp.CurrentRevision, created.CurrentRevision.ID)
	}
	doc := decodeDesignRevisionNativeJSONForTest(t, resp.CurrentRevision.NativeJSON)
	semantic, ok := layerFromNativeJSONForTest(t, doc, "main-title")["semantic"].(map[string]any)
	if !ok {
		t.Fatalf("semantic type = %T", layerFromNativeJSONForTest(t, doc, "main-title")["semantic"])
	}
	want := map[string]string{"role": "headline", "moduleKey": "hero", "stateKey": "default", "slotKey": "title"}
	for key, value := range want {
		if semantic[key] != value {
			t.Fatalf("semantic[%s] = %v, want %s (semantic=%+v)", key, semantic[key], value, semantic)
		}
	}
	lastEdit := lastLightweightEditFromNativeJSONForTest(t, doc)
	if lastEdit["layerId"] != "main-title" {
		t.Fatalf("lastLightweightEdit.layerId = %v, want main-title", lastEdit["layerId"])
	}
	if lastEdit["layerName"] != "Title" {
		t.Fatalf("lastLightweightEdit.layerName = %v, want Title", lastEdit["layerName"])
	}
	if lastEdit["frameId"] != "frame-main" {
		t.Fatalf("lastLightweightEdit.frameId = %v, want frame-main", lastEdit["frameId"])
	}
	if lastEdit["summary"] != "Updated semantic.role, semantic.moduleKey, semantic.stateKey, semantic.slotKey for Title" {
		t.Fatalf("lastLightweightEdit.summary = %v, want semantic summary", lastEdit["summary"])
	}
	assertLightweightEditChangedFieldsForTest(t, lastEdit, []string{"semantic.role", "semantic.moduleKey", "semantic.stateKey", "semantic.slotKey"})
}

func TestUpdateDesignLayerLightweightNameVisibleAndFidelityReport(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Name Visible Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Lightweight Name Visible Design"))

	w := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-image", map[string]any{
		"revision_id": created.CurrentRevision.ID,
		"name":        "Hero Media",
		"visible":     false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateDesignLayerLightweight name/visible: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode lightweight edit response: %v", err)
	}
	if resp.CurrentRevision == nil {
		t.Fatal("expected current revision response")
	}
	doc := decodeDesignRevisionNativeJSONForTest(t, resp.CurrentRevision.NativeJSON)
	layer := layerFromNativeJSONForTest(t, doc, "main-image")
	if layer["name"] != "Hero Media" {
		t.Fatalf("layer.name = %v, want Hero Media", layer["name"])
	}
	if layer["visible"] != false {
		t.Fatalf("layer.visible = %v, want false", layer["visible"])
	}
	lastEdit := lastLightweightEditFromNativeJSONForTest(t, doc)
	assertLightweightEditChangedFieldsForTest(t, lastEdit, []string{"name", "visible"})
	report := importFidelityReportFromNativeJSONForTest(t, doc)
	if report["updatedAt"] == "" {
		t.Fatalf("importFidelityReport.updatedAt missing: %+v", report)
	}
	byFrameID, ok := report["byFrameId"].(map[string]any)
	if !ok || byFrameID["frame-main"] == nil {
		t.Fatalf("importFidelityReport.byFrameId missing frame-main: %+v", report)
	}
}

func TestUpdateDesignLayerLightweightFillAndTextColor(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Color Edit Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	nativeJSON := contextDesignNativeJSON("Lightweight Color Edit Design")
	layers := nativeJSON["layers"].(map[string]any)
	layers["main-image"].(map[string]any)["style"] = map[string]any{"fills": []map[string]any{{"type": "solid", "color": map[string]any{"css": "#111111", "hex": "#111111"}}}}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)

	fillW := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-image", map[string]any{
		"revision_id": created.CurrentRevision.ID,
		"fill_color":  "#0f0",
	})
	if fillW.Code != http.StatusOK {
		t.Fatalf("UpdateDesignLayerLightweight fill_color: expected 200, got %d: %s", fillW.Code, fillW.Body.String())
	}
	var fillResp DesignFileDetailResponse
	if err := json.NewDecoder(fillW.Body).Decode(&fillResp); err != nil {
		t.Fatalf("decode fill edit response: %v", err)
	}
	doc := decodeDesignRevisionNativeJSONForTest(t, fillResp.CurrentRevision.NativeJSON)
	style := layerFromNativeJSONForTest(t, doc, "main-image")["style"].(map[string]any)
	fills := style["fills"].([]any)
	color := fills[0].(map[string]any)["color"].(map[string]any)
	if color["hex"] != "#00FF00" || color["css"] != "#00FF00" {
		t.Fatalf("fill color = %+v, want #00FF00", color)
	}
	assertLightweightEditChangedFieldsForTest(t, lastLightweightEditFromNativeJSONForTest(t, doc), []string{"fill_color"})

	textW := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-title", map[string]any{
		"revision_id": fillResp.CurrentRevision.ID,
		"text_color":  "#336699",
	})
	if textW.Code != http.StatusOK {
		t.Fatalf("UpdateDesignLayerLightweight text_color: expected 200, got %d: %s", textW.Code, textW.Body.String())
	}
	var textResp DesignFileDetailResponse
	if err := json.NewDecoder(textW.Body).Decode(&textResp); err != nil {
		t.Fatalf("decode text color edit response: %v", err)
	}
	doc = decodeDesignRevisionNativeJSONForTest(t, textResp.CurrentRevision.NativeJSON)
	textLayer := layerFromNativeJSONForTest(t, doc, "main-title")
	text := textLayer["text"].(map[string]any)
	textColor := text["color"].(map[string]any)
	if textColor["hex"] != "#336699" || textColor["css"] != "#336699" {
		t.Fatalf("text color = %+v, want #336699", textColor)
	}
	textStyle := textLayer["style"].(map[string]any)
	textFills := textStyle["fills"].([]any)
	textFillColor := textFills[0].(map[string]any)["color"].(map[string]any)
	if textFillColor["hex"] != "#336699" {
		t.Fatalf("text fill color = %+v, want #336699", textFillColor)
	}
	assertLightweightEditChangedFieldsForTest(t, lastLightweightEditFromNativeJSONForTest(t, doc), []string{"text_color"})
}

func TestUpdateDesignLayerLightweightStrokeColorAndWidth(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Stroke Edit Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	nativeJSON := contextDesignNativeJSON("Lightweight Stroke Edit Design")
	layers := nativeJSON["layers"].(map[string]any)
	layers["main-image"].(map[string]any)["style"] = map[string]any{"strokes": []map[string]any{{"color": map[string]any{"css": "#111111", "hex": "#111111"}, "width": float64(1)}}}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)

	w := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-image", map[string]any{
		"revision_id":  created.CurrentRevision.ID,
		"stroke_color": "#abc",
		"stroke_width": 2,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateDesignLayerLightweight stroke edit: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	doc := decodeDesignRevisionNativeJSONForTest(t, resp.CurrentRevision.NativeJSON)
	style := layerFromNativeJSONForTest(t, doc, "main-image")["style"].(map[string]any)
	strokes := style["strokes"].([]any)
	stroke := strokes[0].(map[string]any)
	color := stroke["color"].(map[string]any)
	if color["hex"] != "#AABBCC" || color["css"] != "#AABBCC" {
		t.Fatalf("stroke color = %+v, want #AABBCC", color)
	}
	if stroke["width"] != float64(2) {
		t.Fatalf("stroke width = %v, want 2", stroke["width"])
	}
	assertLightweightEditChangedFieldsForTest(t, lastLightweightEditFromNativeJSONForTest(t, doc), []string{"stroke_color", "stroke_width"})
}

func TestUpdateDesignLayerLightweightUndoLastEdit(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Undo Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Lightweight Undo Design"))

	editW := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-title", map[string]any{
		"revision_id": created.CurrentRevision.ID,
		"name":        "Edited title",
	})
	if editW.Code != http.StatusOK {
		t.Fatalf("edit expected 200, got %d: %s", editW.Code, editW.Body.String())
	}
	var editResp DesignFileDetailResponse
	if err := json.NewDecoder(editW.Body).Decode(&editResp); err != nil {
		t.Fatalf("decode edit response: %v", err)
	}

	undoW := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-title", map[string]any{
		"revision_id": editResp.CurrentRevision.ID,
		"undo_last":   true,
	})
	if undoW.Code != http.StatusOK {
		t.Fatalf("undo expected 200, got %d: %s", undoW.Code, undoW.Body.String())
	}
	var undoResp DesignFileDetailResponse
	if err := json.NewDecoder(undoW.Body).Decode(&undoResp); err != nil {
		t.Fatalf("decode undo response: %v", err)
	}
	doc := decodeDesignRevisionNativeJSONForTest(t, undoResp.CurrentRevision.NativeJSON)
	layer := layerFromNativeJSONForTest(t, doc, "main-title")
	if layer["name"] == "Edited title" {
		t.Fatalf("expected undo to restore previous name")
	}
	assertLightweightEditChangedFieldsForTest(t, lastLightweightEditFromNativeJSONForTest(t, doc), []string{"undo_last"})
}

func TestUpdateDesignLayerLightweightImageURL(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Image Replace Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	nativeJSON := contextDesignNativeJSON("Lightweight Image Replace Design")
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)

	w := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-image", map[string]any{
		"revision_id": created.CurrentRevision.ID,
		"image_url":   "https://example.com/replacement.png",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("image_url edit expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	doc := decodeDesignRevisionNativeJSONForTest(t, resp.CurrentRevision.NativeJSON)
	layer := layerFromNativeJSONForTest(t, doc, "main-image")
	image := layer["image"].(map[string]any)
	assetID := image["assetId"].(string)
	assets := doc["assets"].(map[string]any)
	asset := assets[assetID].(map[string]any)
	if asset["url"] != "https://example.com/replacement.png" {
		t.Fatalf("asset url = %v", asset["url"])
	}
	assertLightweightEditChangedFieldsForTest(t, lastLightweightEditFromNativeJSONForTest(t, doc), []string{"image_url"})
}

func TestUpdateDesignLayerLightweightRejectsMismatchedRevision(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Stale Revision Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Lightweight Stale Revision Design"))

	stale := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-title", map[string]any{
		"revision_id": "00000000-0000-0000-0000-000000000000",
		"text":        "Stale text",
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale lightweight edit: expected 409, got %d: %s", stale.Code, stale.Body.String())
	}
}

func TestUpdateDesignLayerLightweightRejectsTextEditOnNonTextLayer(t *testing.T) {
	created := createDesignFileForTest(t, "Lightweight Non Text Edit Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Lightweight Non Text Edit Design"))

	w := postDesignLayerLightweightEditForTest(t, created.File.ID, "main-image", map[string]any{
		"revision_id": created.CurrentRevision.ID,
		"text":        "Not allowed",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("text edit on non-text layer: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDesignRestoreTaskUsesCurrentRevisionAndStoresInput(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Task Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}

	input := map[string]any{
		"prompt": "restore the hero section",
		"selection": map[string]any{
			"frameId":  "frame-1",
			"layerIds": []string{"layer-1"},
		},
	}
	req := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input":   input,
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp DesignRestoreTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, resp.ID)
	})

	if resp.ID == "" {
		t.Fatal("expected task id")
	}
	if resp.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %s, want %s", resp.WorkspaceID, testWorkspaceID)
	}
	if resp.FileID != created.File.ID {
		t.Fatalf("file_id = %s, want %s", resp.FileID, created.File.ID)
	}
	if resp.RevisionID != created.CurrentRevision.ID {
		t.Fatalf("revision_id = %s, want current revision %s", resp.RevisionID, created.CurrentRevision.ID)
	}
	if resp.Status != "queued" {
		t.Fatalf("status = %q, want queued", resp.Status)
	}
	if resp.CreatedBy == nil || *resp.CreatedBy != testUserID {
		t.Fatalf("created_by = %v, want %s", resp.CreatedBy, testUserID)
	}

	var gotInput map[string]any
	if err := json.Unmarshal(resp.Input, &gotInput); err != nil {
		t.Fatalf("unmarshal task input: %v", err)
	}
	if gotInput["prompt"] != "restore the hero section" {
		t.Fatalf("input prompt = %v, want restore prompt", gotInput["prompt"])
	}
	selection, ok := gotInput["selection"].(map[string]any)
	if !ok {
		t.Fatalf("input selection type = %T", gotInput["selection"])
	}
	if selection["frameId"] != "frame-1" {
		t.Fatalf("input selection frameId = %v, want frame-1", selection["frameId"])
	}
}

func TestCreateDesignRestoreTaskAddsIssueTimelineComment(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Task Issue Timeline Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Restore Task Issue Timeline Project")
	issueID := createIssueForDesignTest(t, "Restore Task Issue Timeline", projectID)
	req := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id":  created.File.ID,
		"issue_id": issueID,
		"input": map[string]any{
			"version":       "1.0",
			"projectId":     projectID,
			"sourceIssueId": issueID,
			"purpose":       "frontend_restore",
			"items": []map[string]any{{
				"itemId":       "issue-timeline-frame",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"source":       "frame",
			}},
		},
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var commentCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND author_type = 'system' AND type = 'system' AND content LIKE '%设计稿还原任务已创建%'
	`, issueID).Scan(&commentCount); err != nil {
		t.Fatalf("count system comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("system comment count = %d, want 1", commentCount)
	}
}

func TestCreateDesignRestoreTaskReusesExistingIssueTask(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Task Reuse Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Restore Task Reuse Project")
	issueID := createIssueForDesignTest(t, "Restore Task Reuse Issue", projectID)
	body := map[string]any{
		"file_id":  created.File.ID,
		"issue_id": issueID,
		"input": map[string]any{
			"version":       "1.0",
			"projectId":     projectID,
			"sourceIssueId": issueID,
			"purpose":       "frontend_restore",
			"items": []map[string]any{{
				"itemId":       "reuse-frame",
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
		t.Fatalf("decode first task: %v", err)
	}

	secondW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(secondW, newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, body))
	if secondW.Code != http.StatusOK {
		t.Fatalf("second CreateDesignRestoreTask: expected 200 reuse, got %d: %s", secondW.Code, secondW.Body.String())
	}
	var second DesignRestoreTaskResponse
	if err := json.NewDecoder(secondW.Body).Decode(&second); err != nil {
		t.Fatalf("decode second task: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("reused task id = %s, want %s", second.ID, first.ID)
	}
	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_restore_task WHERE issue_id = $1`, issueID).Scan(&taskCount); err != nil {
		t.Fatalf("count restore tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("restore task count = %d, want 1", taskCount)
	}
}

func TestCreateDesignRestoreTaskDoesNotReuseFailedIssueTask(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Task Failed Retry Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Restore Task Failed Retry Project")
	issueID := createIssueForDesignTest(t, "Restore Task Failed Retry Issue", projectID)
	var failedTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_restore_task (workspace_id, file_id, revision_id, issue_id, status, input, result, created_by)
		VALUES ($1, $2, $3, $4, 'failed', '{}'::jsonb, '{}'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, created.File.ID, created.CurrentRevision.ID, issueID, testUserID).Scan(&failedTaskID); err != nil {
		t.Fatalf("insert failed restore task: %v", err)
	}
	body := map[string]any{
		"file_id":  created.File.ID,
		"issue_id": issueID,
		"input": map[string]any{
			"version":       "1.0",
			"projectId":     projectID,
			"sourceIssueId": issueID,
			"purpose":       "frontend_restore",
			"items": []map[string]any{{
				"itemId":       "retry-frame",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"source":       "frame",
			}},
		},
	}
	w := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(w, newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask retry: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	if createdTask.ID == failedTaskID {
		t.Fatalf("retry reused failed task %s", failedTaskID)
	}
}

func TestCreateDesignRestoreTaskDoesNotReuseCompletedIssueTask(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Task Completed Retry Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Restore Task Completed Retry Project")
	issueID := createIssueForDesignTest(t, "Restore Task Completed Retry Issue", projectID)
	var completedTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_restore_task (workspace_id, file_id, revision_id, issue_id, status, input, result, created_by)
		VALUES ($1, $2, $3, $4, 'completed', '{}'::jsonb, '{}'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, created.File.ID, created.CurrentRevision.ID, issueID, testUserID).Scan(&completedTaskID); err != nil {
		t.Fatalf("insert completed restore task: %v", err)
	}
	body := map[string]any{
		"file_id":  created.File.ID,
		"issue_id": issueID,
		"input": map[string]any{
			"version":       "1.0",
			"projectId":     projectID,
			"sourceIssueId": issueID,
			"purpose":       "frontend_restore",
			"items": []map[string]any{{
				"itemId":       "completed-retry-frame",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"source":       "frame",
			}},
		},
	}
	w := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(w, newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask retry: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	if createdTask.ID == completedTaskID {
		t.Fatalf("retry reused completed task %s", completedTaskID)
	}
}

func TestCreateDesignRestoreTaskDoesNotReuseStaleQueuedIssueTask(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Task Stale Revision Retry Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	projectID := createProjectForDesignTest(t, "Restore Task Stale Revision Retry Project")
	issueID := createIssueForDesignTest(t, "Restore Task Stale Revision Retry Issue", projectID)
	oldRevisionID := created.CurrentRevision.ID
	var newRevisionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_revision (file_id, workspace_id, revision_number, status, native_json, validation_errors, created_by)
		VALUES ($1, $2, 2, 'valid', $3::jsonb, '[]'::jsonb, $4)
		RETURNING id
	`, created.File.ID, testWorkspaceID, minimalDesignNativeJSON(created.File.Title), testUserID).Scan(&newRevisionID); err != nil {
		t.Fatalf("insert new revision: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET current_revision_id = $1 WHERE id = $2`, newRevisionID, created.File.ID); err != nil {
		t.Fatalf("update current revision: %v", err)
	}
	var queuedTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_restore_task (workspace_id, file_id, revision_id, issue_id, status, input, result, created_by)
		VALUES ($1, $2, $3, $4, 'queued', '{}'::jsonb, '{}'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, created.File.ID, oldRevisionID, issueID, testUserID).Scan(&queuedTaskID); err != nil {
		t.Fatalf("insert stale queued restore task: %v", err)
	}
	body := map[string]any{
		"file_id":     created.File.ID,
		"revision_id": newRevisionID,
		"issue_id":    issueID,
		"input": map[string]any{
			"version":       "1.0",
			"projectId":     projectID,
			"sourceIssueId": issueID,
			"purpose":       "frontend_restore",
			"items": []map[string]any{{
				"itemId":       "stale-queued-retry-frame",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   newRevisionID,
				"frameId":      "frame-main",
				"source":       "frame",
			}},
		},
	}
	w := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(w, newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask retry: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	if createdTask.ID == queuedTaskID {
		t.Fatalf("retry reused stale queued task %s", queuedTaskID)
	}
	if createdTask.RevisionID != newRevisionID {
		t.Fatalf("created task revision = %s, want %s", createdTask.RevisionID, newRevisionID)
	}
}

func TestCompactDesignRestoreAgentContextShrinksLargePayload(t *testing.T) {
	layers := map[string]any{}
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("layer-%03d", i)
		layers[id] = map[string]any{
			"id":       id,
			"name":     fmt.Sprintf("Layer %03d", i),
			"type":     "text",
			"x":        float64(i % 20),
			"y":        float64(i * 4),
			"width":    float64(120),
			"height":   float64(24),
			"frameId":  "frame-main",
			"parentId": "root",
			"text": map[string]any{
				"characters":    strings.Repeat("内容", 20),
				"fontFamily":    "PingFang SC",
				"fontSize":      float64(14),
				"fontWeight":    float64(400),
				"lineHeight":    "AUTO",
				"letterSpacing": float64(0),
				"color":         map[string]any{"hex": "#111111", "css": "rgba(17, 17, 17, 1)", "r": float64(17), "g": float64(17), "b": float64(17), "a": float64(1)},
			},
			"style": map[string]any{
				"fills": []any{map[string]any{"type": "solid", "color": map[string]any{"hex": "#FFFFFF", "css": "rgba(255,255,255,1)", "r": float64(255), "g": float64(255), "b": float64(255), "a": float64(1)}}},
			},
			"exportable": []any{map[string]any{"id": "slice-" + id, "assetId": "asset-" + id, "url": strings.Repeat("https://example.com/asset/", 5)}},
		}
	}
	context := map[string]any{
		"designFileId": "file-1",
		"revisionId":   "revision-1",
		"frame":        map[string]any{"id": "frame-main", "name": "Main", "width": float64(375), "height": float64(812), "children": []any{"layer-001"}},
		"rootLayerId":  "root",
		"layers":       layers,
		"colors":       make([]any, 500),
		"text":         make([]any, 500),
		"exportables":  make([]any, 500),
		"assets":       map[string]any{"asset-1": map[string]any{"id": "asset-1", "bytes": strings.Repeat("x", 100000), "url": "https://example.com/asset.png"}},
	}
	raw, err := json.Marshal(context)
	if err != nil {
		t.Fatalf("marshal raw context: %v", err)
	}
	compact := compactDesignRestoreAgentContext(context)
	encoded, err := json.Marshal(compact)
	if err != nil {
		t.Fatalf("marshal compact context: %v", err)
	}
	if len(encoded) >= len(raw)/2 {
		t.Fatalf("compact context size = %d, want less than half raw %d", len(encoded), len(raw))
	}
	if len(encoded) > 220000 {
		t.Fatalf("compact context size = %d, want <= 220000", len(encoded))
	}
	if _, ok := compact["compaction"]; !ok {
		t.Fatal("expected compaction metadata")
	}
}

func TestUIDesignDonePromotesFrontendIssue(t *testing.T) {
	created := createDesignFileForTest(t, "UI Done Restore Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("UI Done Restore Design"))
	projectID := createProjectForDesignTest(t, "UI Done Restore Project")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, created.File.ID); err != nil {
		t.Fatalf("attach design file to project: %v", err)
	}
	agentID := createHandlerTestAgent(t, "UI Done Frontend Agent", []byte("[]"))
	createParentW := httptest.NewRecorder()
	testHandler.CreateIssue(createParentW, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": "个人资料页", "status": "todo", "project_id": projectID}))
	if createParentW.Code != http.StatusCreated {
		t.Fatalf("create parent issue: expected 201, got %d: %s", createParentW.Code, createParentW.Body.String())
	}
	var parent IssueResponse
	if err := json.NewDecoder(createParentW.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent issue: %v", err)
	}
	createUIW := httptest.NewRecorder()
	testHandler.CreateIssue(createUIW, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": "UI设计", "status": "todo", "parent_issue_id": parent.ID}))
	if createUIW.Code != http.StatusCreated {
		t.Fatalf("create ui issue: expected 201, got %d: %s", createUIW.Code, createUIW.Body.String())
	}
	var uiIssue IssueResponse
	if err := json.NewDecoder(createUIW.Body).Decode(&uiIssue); err != nil {
		t.Fatalf("decode ui issue: %v", err)
	}
	createFEW := httptest.NewRecorder()
	testHandler.CreateIssue(createFEW, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": "前端开发", "status": "backlog", "parent_issue_id": parent.ID, "assignee_type": "agent", "assignee_id": agentID}))
	if createFEW.Code != http.StatusCreated {
		t.Fatalf("create frontend issue: expected 201, got %d: %s", createFEW.Code, createFEW.Body.String())
	}
	var frontendIssue IssueResponse
	if err := json.NewDecoder(createFEW.Body).Decode(&frontendIssue); err != nil {
		t.Fatalf("decode frontend issue: %v", err)
	}
	var restoreTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_restore_task (workspace_id, file_id, revision_id, issue_id, status, input, result, created_by)
		VALUES ($1, $2, $3, $4, 'completed', '{}'::jsonb, '{}'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, created.File.ID, created.CurrentRevision.ID, uiIssue.ID, testUserID).Scan(&restoreTaskID); err != nil {
		t.Fatalf("insert completed ui restore task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, restoreTaskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id IN ($1, $2, $3)`, parent.ID, uiIssue.ID, frontendIssue.ID)
	})
	updateW := httptest.NewRecorder()
	updateReq := withURLParam(newRequest("PUT", "/api/issues/"+uiIssue.ID, map[string]any{"status": "done"}), "id", uiIssue.ID)
	testHandler.UpdateIssue(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("UpdateIssue UI done: expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}
	var frontendStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, frontendIssue.ID).Scan(&frontendStatus); err != nil {
		t.Fatalf("load frontend issue status: %v", err)
	}
	if frontendStatus != "todo" {
		t.Fatalf("frontend issue status = %q, want todo", frontendStatus)
	}
	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_restore_task WHERE issue_id = $1`, frontendIssue.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count frontend restore tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("frontend restore task count = %d, want 0", taskCount)
	}
	var commentCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'system' AND content LIKE '%前端开发已进入待办%'`, frontendIssue.ID).Scan(&commentCount); err != nil {
		t.Fatalf("count frontend system comments: %v", err)
	}
	if commentCount != 1 {
		t.Fatalf("frontend system comment count = %d, want 1", commentCount)
	}
}

func TestGetDesignRestoreTaskReturnsTaskInWorkspace(t *testing.T) {
	created := createDesignFileForTest(t, "Get Restore Task Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"prompt": "get this task",
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	getReq := withURLParam(newRequest("GET", "/api/design-restore-tasks/"+createdTask.ID+"?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	getW := httptest.NewRecorder()
	testHandler.GetDesignRestoreTask(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GetDesignRestoreTask: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	var got DesignRestoreTaskResponse
	if err := json.NewDecoder(getW.Body).Decode(&got); err != nil {
		t.Fatalf("decode GetDesignRestoreTask: %v", err)
	}

	if got.ID != createdTask.ID {
		t.Fatalf("id = %s, want %s", got.ID, createdTask.ID)
	}
	if got.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %s, want %s", got.WorkspaceID, testWorkspaceID)
	}
	if got.FileID != createdTask.FileID {
		t.Fatalf("file_id = %s, want %s", got.FileID, createdTask.FileID)
	}
	if got.RevisionID != createdTask.RevisionID {
		t.Fatalf("revision_id = %s, want %s", got.RevisionID, createdTask.RevisionID)
	}
	if string(got.Input) != string(createdTask.Input) {
		t.Fatalf("input = %s, want %s", string(got.Input), string(createdTask.Input))
	}
}

func TestGetDesignRestoreTaskIncludesOfflineRuntimeExecutionStatus(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Task Offline Runtime Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"prompt": "diagnose this task",
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
		VALUES ($1, $2, 'offline restore runtime', 'local', 'opencode', 'offline', '', '{}'::jsonb, $3, now() - interval '10 minutes')
		RETURNING id
	`, testWorkspaceID, "restore-offline-"+createdTask.ID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert offline runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'Offline Restore Agent', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert offline agent: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	var agentTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, NULL, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID).Scan(&agentTaskID); err != nil {
		t.Fatalf("insert agent task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, agentTaskID)
	})

	if _, err := testPool.Exec(context.Background(), `
		UPDATE design_restore_task
		SET agent_task_id = $1, status = 'running', updated_at = now()
		WHERE id = $2
	`, agentTaskID, createdTask.ID); err != nil {
		t.Fatalf("link agent task: %v", err)
	}

	getReq := withURLParam(newRequest("GET", "/api/design-restore-tasks/"+createdTask.ID+"?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	getW := httptest.NewRecorder()
	testHandler.GetDesignRestoreTask(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GetDesignRestoreTask: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}

	var raw map[string]any
	if err := json.NewDecoder(getW.Body).Decode(&raw); err != nil {
		t.Fatalf("decode GetDesignRestoreTask: %v", err)
	}
	status, ok := raw["execution_status"].(map[string]any)
	if !ok {
		t.Fatalf("execution_status missing or invalid: %#v", raw["execution_status"])
	}
	if got := status["phase"]; got != "waiting_runtime" {
		t.Fatalf("phase = %#v, want waiting_runtime", got)
	}
	if got := status["reason"]; got != "runtime_offline" {
		t.Fatalf("reason = %#v, want runtime_offline", got)
	}
	if got := status["severity"]; got != "warning" {
		t.Fatalf("severity = %#v, want warning", got)
	}
	if got := status["runtime_status"]; got != "offline" {
		t.Fatalf("runtime_status = %#v, want offline", got)
	}
	if got := status["agent_task_status"]; got != "queued" {
		t.Fatalf("agent_task_status = %#v, want queued", got)
	}
}

func TestDesignRestoreExecutionStatusWarnsWhenRunningWithoutRecentOutput(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	row := designRestoreExecutionStatusSnapshot{
		AgentTaskID:        util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
		AgentTaskStatus:    pgtype.Text{String: "running", Valid: true},
		RuntimeID:          util.MustParseUUID("22222222-2222-2222-2222-222222222222"),
		RuntimeStatus:      pgtype.Text{String: "online", Valid: true},
		RuntimeLastSeenAt:  pgtype.Timestamptz{Time: now.Add(-30 * time.Second), Valid: true},
		AgentTaskStartedAt: pgtype.Timestamptz{Time: now.Add(-4 * time.Minute), Valid: true},
	}

	status := designRestoreExecutionStatusToResponse(row, now)

	if status.Phase != "running" {
		t.Fatalf("phase = %s, want running", status.Phase)
	}
	if status.Reason != "running_no_recent_output" {
		t.Fatalf("reason = %s, want running_no_recent_output", status.Reason)
	}
	if status.Severity != "warning" {
		t.Fatalf("severity = %s, want warning", status.Severity)
	}
}

func TestListDesignRestoreTasksReturnsWorkspaceTasks(t *testing.T) {
	created := createDesignFileForTest(t, "List Restore Task Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"version":   "1.0",
			"projectId": "project-list-test",
			"items":     []any{},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	listReq := newRequest("GET", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, nil)
	listW := httptest.NewRecorder()
	testHandler.ListDesignRestoreTasks(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignRestoreTasks: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var resp DesignRestoreTaskListResponse
	if err := json.NewDecoder(listW.Body).Decode(&resp); err != nil {
		t.Fatalf("decode ListDesignRestoreTasks: %v", err)
	}
	for _, task := range resp.Tasks {
		if task.ID == createdTask.ID {
			return
		}
	}
	t.Fatalf("expected listed restore tasks to include %s", createdTask.ID)
}

func TestDispatchDesignRestoreTaskCreatesAgentTask(t *testing.T) {
	created := createDesignFileForTest(t, "Dispatch Restore Task Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	nativeJSON := contextDesignNativeJSON("Dispatch Restore Task Design")
	frames := nativeJSON["frames"].([]map[string]any)
	frames[0]["source"] = map[string]any{"tool": "figma", "groupId": "4-189", "groupName": "Group 43"}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)
	agentID := createHandlerTestAgent(t, "Dispatch Restore Agent", []byte("[]"))
	projectID := createProjectForDesignTest(t, "Dispatch Restore Project")
	inputProjectID := createProjectForDesignTest(t, "Dispatch Restore Wrong Input Project")
	issueID := createIssueForDesignTest(t, "Dispatch Restore Issue", projectID)
	var designSystemProfileID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name, description,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES (
			$1, $2, $3, $4, 'Dispatch Restore UI 规范', '',
			'analyzed', true, '{"version":"agent-1.0","tokens":{"colors":[{"value":"#1677ff"}]}}'::jsonb, '[]'::jsonb, $5
		)
		RETURNING id
	`, testWorkspaceID, projectID, created.File.ID, created.CurrentRevision.ID, testUserID).Scan(&designSystemProfileID); err != nil {
		t.Fatalf("insert default design system profile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_system_profile WHERE id = $1`, designSystemProfileID)
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET status = 'backlog' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("set issue backlog: %v", err)
	}

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"version":   "1.0",
			"projectId": inputProjectID,
			"items": []map[string]any{{
				"itemId":       "dispatch-frame-1",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"source":       "frame",
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	req := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id":  agentID,
		"issue_id":  issueID,
		"prompt":    "restore this frame",
		"skip_plan": true,
	}), "id", createdTask.ID)
	w := httptest.NewRecorder()
	testHandler.DispatchDesignRestoreTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("DispatchDesignRestoreTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp DispatchDesignRestoreTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode DispatchDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.AgentTaskID)
	})
	if resp.AgentTaskID == "" {
		t.Fatal("expected agent task id")
	}
	if resp.Task.AgentTaskID == nil || *resp.Task.AgentTaskID != resp.AgentTaskID {
		t.Fatalf("restore task agent_task_id = %v, want %s", resp.Task.AgentTaskID, resp.AgentTaskID)
	}
	if resp.Task.Status != "queued" {
		t.Fatalf("restore task status = %q, want queued until daemon starts agent task", resp.Task.Status)
	}
	var issueStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if issueStatus != "in_progress" {
		t.Fatalf("issue status = %q, want in_progress", issueStatus)
	}
	var queuedContextRaw []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, resp.AgentTaskID).Scan(&queuedContextRaw); err != nil {
		t.Fatalf("load queued task context: %v", err)
	}
	var queuedContext map[string]any
	if err := json.Unmarshal(queuedContextRaw, &queuedContext); err != nil {
		t.Fatalf("decode queued task context: %v", err)
	}
	if queuedContext["type"] != service.DesignRestoreTaskContextType {
		t.Fatalf("queued context type = %v", queuedContext["type"])
	}
	if queuedContext["project_id"] != projectID {
		t.Fatalf("queued context project_id = %v, want %s", queuedContext["project_id"], projectID)
	}
	designSystem, ok := queuedContext["design_system"].(map[string]any)
	if !ok {
		t.Fatalf("queued context missing design_system: %#v", queuedContext)
	}
	if designSystem["id"] != designSystemProfileID || designSystem["status"] != "analyzed" {
		t.Fatalf("queued design_system identity = %#v", designSystem)
	}
	profile, ok := designSystem["profile"].(map[string]any)
	if !ok || profile["version"] != "agent-1.0" {
		t.Fatalf("queued design_system profile = %#v", designSystem["profile"])
	}
	restorePolicy, ok := queuedContext["restore_policy"].(map[string]any)
	if !ok || restorePolicy["restoreMode"] != "strict-structure" || restorePolicy["allowFullFramePreview"] != false {
		t.Fatalf("queued restore_policy = %#v, want strict-structure with full frame preview disabled", queuedContext["restore_policy"])
	}
	if allowedImageUse, _ := restorePolicy["allowedImageUse"].(string); !strings.Contains(allowedImageUse, "visible layer image/exported assets") || !strings.Contains(allowedImageUse, "full frame preview/thumbnail/full-frame slice forbidden") {
		t.Fatalf("queued allowedImageUse = %q, want visible layer assets allowed and full-frame assets forbidden", allowedImageUse)
	}
	outputPolicy, ok := queuedContext["output_policy"].(map[string]any)
	if !ok {
		t.Fatalf("queued output_policy = %#v, want object", queuedContext["output_policy"])
	}
	resultPolicy, ok := outputPolicy["result"].(map[string]any)
	if !ok || resultPolicy["artifactDocPath"] != "string" {
		t.Fatalf("queued output_policy.result = %#v, want artifactDocPath string contract", outputPolicy["result"])
	}
	itemContexts, ok := queuedContext["item_contexts"].([]any)
	if !ok || len(itemContexts) != 1 {
		t.Fatalf("queued item_contexts = %#v, want one item context", queuedContext["item_contexts"])
	}
	firstItemContext := itemContexts[0].(map[string]any)
	contextPayload := firstItemContext["context"].(map[string]any)
	frame := contextPayload["frame"].(map[string]any)
	if frame["id"] != "frame-main" {
		t.Fatalf("embedded frame context id = %v, want frame-main", frame["id"])
	}
	frameSource, ok := frame["source"].(map[string]any)
	if !ok || frameSource["groupName"] != "Group 43" {
		t.Fatalf("embedded frame source = %#v, want Figma group context", frame["source"])
	}
}

func TestDesignRestorePlanGenerateApproveDispatch(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Plan Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Restore Plan Design"))
	agentID := createHandlerTestAgent(t, "Restore Plan Agent", []byte("[]"))

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"version": "1.0",
			"items": []map[string]any{{
				"itemId":       "plan-frame-1",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"frameName":    "Main",
				"source":       "frame",
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	blockedReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID}), "id", createdTask.ID)
	blockedW := httptest.NewRecorder()
	testHandler.DispatchDesignRestoreTask(blockedW, blockedReq)
	if blockedW.Code != http.StatusConflict {
		t.Fatalf("Dispatch without plan: expected 409, got %d: %s", blockedW.Code, blockedW.Body.String())
	}

	generateReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/plan/generate?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	generateW := httptest.NewRecorder()
	testHandler.GenerateDesignRestorePlan(generateW, generateReq)
	if generateW.Code != http.StatusCreated {
		t.Fatalf("GenerateDesignRestorePlan: expected 201, got %d: %s", generateW.Code, generateW.Body.String())
	}
	var generatedPlan DesignRestorePlanResponse
	if err := json.NewDecoder(generateW.Body).Decode(&generatedPlan); err != nil {
		t.Fatalf("decode GenerateDesignRestorePlan: %v", err)
	}
	if generatedPlan.Status != "draft" || !strings.Contains(string(generatedPlan.Plan), "fengchenDoc/gallery-native-agent-test") {
		t.Fatalf("generated plan = %#v, want draft plan targeting fengchenDoc", generatedPlan)
	}

	approveReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/plan/approve?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	approveW := httptest.NewRecorder()
	testHandler.ApproveDesignRestorePlan(approveW, approveReq)
	if approveW.Code != http.StatusOK {
		t.Fatalf("ApproveDesignRestorePlan: expected 200, got %d: %s", approveW.Code, approveW.Body.String())
	}

	dispatchReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentID,
		"prompt":   "restore using approved plan",
	}), "id", createdTask.ID)
	dispatchW := httptest.NewRecorder()
	testHandler.DispatchDesignRestoreTask(dispatchW, dispatchReq)
	if dispatchW.Code != http.StatusCreated {
		t.Fatalf("DispatchDesignRestoreTask: expected 201, got %d: %s", dispatchW.Code, dispatchW.Body.String())
	}
	var dispatchResp DispatchDesignRestoreTaskResponse
	if err := json.NewDecoder(dispatchW.Body).Decode(&dispatchResp); err != nil {
		t.Fatalf("decode DispatchDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, dispatchResp.AgentTaskID)
	})

	var queuedContextRaw []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, dispatchResp.AgentTaskID).Scan(&queuedContextRaw); err != nil {
		t.Fatalf("load queued task context: %v", err)
	}
	var queuedContext map[string]any
	if err := json.Unmarshal(queuedContextRaw, &queuedContext); err != nil {
		t.Fatalf("decode queued context: %v", err)
	}
	if _, ok := queuedContext["restore_plan"].(map[string]any); !ok {
		t.Fatalf("queued context restore_plan = %#v, want object", queuedContext["restore_plan"])
	}
}

func TestGenerateDesignRestorePlanCreatesRepoAnalysisFromLocalDirectoryProject(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Plan Local Repo Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Restore Plan Local Repo Design"))
	agentID := createHandlerTestAgent(t, "Restore Plan Local Repo Agent", []byte("[]"))
	projectID := createProjectForDesignTest(t, "Restore Plan Local Repo Project")
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	repoRoot = strings.TrimSuffix(repoRoot, "/server/internal/handler")
	resourceRef, err := json.Marshal(localDirectoryRef{LocalPath: repoRoot, DaemonID: "restore-plan-test-daemon", Label: "Repository root"})
	if err != nil {
		t.Fatalf("marshal local directory ref: %v", err)
	}
	var resourceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'local_directory', $3::jsonb, 'Repository root', 0, $4)
		RETURNING id
	`, projectID, testWorkspaceID, resourceRef, testUserID).Scan(&resourceID); err != nil {
		t.Fatalf("insert project_resource: %v", err)
	}
	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"version":   "1.0",
			"projectId": projectID,
			"items": []map[string]any{{
				"itemId":       "plan-local-repo-frame-1",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"frameName":    "Main",
				"source":       "frame",
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	generateReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/plan/generate?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	generateW := httptest.NewRecorder()
	testHandler.GenerateDesignRestorePlan(generateW, generateReq)
	if generateW.Code != http.StatusCreated {
		t.Fatalf("GenerateDesignRestorePlan: expected 201, got %d: %s", generateW.Code, generateW.Body.String())
	}
	var generatedPlan DesignRestorePlanResponse
	if err := json.NewDecoder(generateW.Body).Decode(&generatedPlan); err != nil {
		t.Fatalf("decode GenerateDesignRestorePlan: %v", err)
	}
	var analysisCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM design_repo_analysis
		WHERE workspace_id = $1 AND project_id = $2 AND project_resource_id = $3 AND status = 'completed'
	`, testWorkspaceID, projectID, resourceID).Scan(&analysisCount); err != nil {
		t.Fatalf("count design repo analysis: %v", err)
	}
	if analysisCount != 1 {
		t.Fatalf("completed design_repo_analysis count = %d, want 1", analysisCount)
	}
	var plan map[string]any
	if err := json.Unmarshal(generatedPlan.Plan, &plan); err != nil {
		t.Fatalf("decode generated plan: %v", err)
	}
	repo := plan["repo"].(map[string]any)
	if repo["mode"] != "production_candidate" || repo["framework"] != "" || repo["language"] != "JavaScript" || repo["packageManager"] != "pnpm" {
		t.Fatalf("plan repo = %#v, want production_candidate JavaScript pnpm", repo)
	}
	execution := plan["execution"].(map[string]any)
	if execution["allowPrototypeHtml"] != false {
		t.Fatalf("execution.allowPrototypeHtml = %#v, want false", execution["allowPrototypeHtml"])
	}
	allowedPaths := execution["allowedPaths"].([]any)
	if len(allowedPaths) < 3 {
		t.Fatalf("execution.allowedPaths = %#v, want page/component/router paths", allowedPaths)
	}

	targets := plan["targets"].(map[string]any)
	selected := targets["selected"].(map[string]any)
	if selected["kind"] != "page_with_route_and_components" || !strings.Contains(selected["path"].(string), "design-restore") || selected["routePath"] == "" || selected["componentRoot"] == "" {
		t.Fatalf("selected target = %#v, want design-restore page route and components", selected)
	}
	if targets["needsUserSelection"] != false {
		t.Fatalf("targets.needsUserSelection = %#v, want false", targets["needsUserSelection"])
	}
	candidates := targets["candidates"].([]any)
	if len(candidates) == 0 {
		t.Fatal("expected target candidates")
	}
	foundFileCandidate := false
	for _, candidate := range candidates {
		candidateMap := candidate.(map[string]any)
		kind, _ := candidateMap["kind"].(string)
		path, _ := candidateMap["path"].(string)
		if strings.HasSuffix(kind, "_file") && strings.Contains(path, ".") {
			foundFileCandidate = true
			break
		}
	}
	if !foundFileCandidate {
		t.Fatalf("expected file-level target candidate, got %#v", candidates)
	}
	approveReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/plan/approve?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	approveW := httptest.NewRecorder()
	testHandler.ApproveDesignRestorePlan(approveW, approveReq)
	if approveW.Code != http.StatusOK {
		t.Fatalf("ApproveDesignRestorePlan with default page target: expected 200, got %d: %s", approveW.Code, approveW.Body.String())
	}
	targets["selected"] = nil
	targets["needsUserSelection"] = true
	brokenPlan, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal broken plan: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE design_restore_plan
		SET plan = $1::jsonb
		WHERE restore_task_id = $2 AND workspace_id = $3
	`, brokenPlan, createdTask.ID, testWorkspaceID); err != nil {
		t.Fatalf("break approved plan: %v", err)
	}
	dispatchReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentID,
		"prompt":   "restore using approved production plan",
	}), "id", createdTask.ID)
	dispatchW := httptest.NewRecorder()
	testHandler.DispatchDesignRestoreTask(dispatchW, dispatchReq)
	if dispatchW.Code != http.StatusConflict {
		t.Fatalf("DispatchDesignRestoreTask with invalid approved production plan: expected 409, got %d: %s", dispatchW.Code, dispatchW.Body.String())
	}
}

func TestGenerateDesignRestorePlanTargetsBusinessModuleFromParentIssue(t *testing.T) {
	created := createDesignFileForTest(t, "服务记录")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("服务记录"))
	projectID := createProjectForDesignTest(t, "Gallery Test")
	parentIssueID := createIssueForDesignTest(t, "服务记录开发", projectID)
	uiIssueID := createIssueForDesignTest(t, "UI设计", projectID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET parent_issue_id = $1
		WHERE id = $2
	`, parentIssueID, uiIssueID); err != nil {
		t.Fatalf("link child issue to parent: %v", err)
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	repoRoot = strings.TrimSuffix(repoRoot, "/server/internal/handler")
	resourceRef, err := json.Marshal(localDirectoryRef{LocalPath: repoRoot, DaemonID: "business-module-test-daemon", Label: "Repository root"})
	if err != nil {
		t.Fatalf("marshal local directory ref: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'local_directory', $3::jsonb, 'Repository root', 0, $4)
	`, projectID, testWorkspaceID, resourceRef, testUserID); err != nil {
		t.Fatalf("insert project_resource: %v", err)
	}

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id":  created.File.ID,
		"issue_id": uiIssueID,
		"input": map[string]any{
			"version":   "1.0",
			"projectId": projectID,
			"items": []map[string]any{{
				"itemId":       "business-module-frame-1",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"frameName":    "服务记录",
				"source":       "frame",
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	generateReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/plan/generate?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	generateW := httptest.NewRecorder()
	testHandler.GenerateDesignRestorePlan(generateW, generateReq)
	if generateW.Code != http.StatusCreated {
		t.Fatalf("GenerateDesignRestorePlan: expected 201, got %d: %s", generateW.Code, generateW.Body.String())
	}
	var generatedPlan DesignRestorePlanResponse
	if err := json.NewDecoder(generateW.Body).Decode(&generatedPlan); err != nil {
		t.Fatalf("decode GenerateDesignRestorePlan: %v", err)
	}
	var plan map[string]any
	if err := json.Unmarshal(generatedPlan.Plan, &plan); err != nil {
		t.Fatalf("decode generated plan: %v", err)
	}
	targets := plan["targets"].(map[string]any)
	selected := targets["selected"].(map[string]any)
	if selected["moduleSlug"] != "service-record" {
		t.Fatalf("selected.moduleSlug = %#v, want service-record; selected=%#v", selected["moduleSlug"], selected)
	}
	if selected["moduleName"] != "服务记录" {
		t.Fatalf("selected.moduleName = %#v, want 服务记录", selected["moduleName"])
	}
	for _, field := range []string{"path", "pagePath", "componentRoot", "routePath"} {
		value, _ := selected[field].(string)
		if !strings.Contains(value, "service-record") {
			t.Fatalf("selected.%s = %q, want service-record module target", field, value)
		}
		if strings.Contains(value, "design-restore") {
			t.Fatalf("selected.%s = %q, should not target design-restore sandbox", field, value)
		}
	}
	strategy := plan["targetStrategy"].(map[string]any)
	if strategy["mode"] != "business_module" || strategy["sourceIssueTitle"] != "服务记录开发" {
		t.Fatalf("targetStrategy = %#v, want business_module from parent issue", strategy)
	}
	execution := plan["execution"].(map[string]any)
	if execution["allowPrototypeHtml"] != false {
		t.Fatalf("execution.allowPrototypeHtml = %#v, want false", execution["allowPrototypeHtml"])
	}
	allowedPaths := execution["allowedPaths"].([]any)
	for _, path := range allowedPaths {
		if strings.Contains(path.(string), "design-restore") {
			t.Fatalf("allowedPaths = %#v, should not include design-restore sandbox", allowedPaths)
		}
	}
}

func TestGenerateDesignRestorePlanBuildsSemanticStructureFromFrameNames(t *testing.T) {
	created := createDesignFileForTest(t, "我的钱包")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSONWithFrameNamesForTest([]string{
		"01 钱包首页-已绑定支付宝",
		"01 钱包首页-未绑定支付宝",
		"04 提现-空金额",
		"04 提现-弹窗:确认提现",
		"04 提现-结果:提现申请已提交",
	}))
	projectID := createProjectForDesignTest(t, "Wallet Project")
	parentIssueID := createIssueForDesignTest(t, "我的钱包开发", projectID)
	uiIssueID := createIssueForDesignTest(t, "UI设计", projectID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET parent_issue_id = $1
		WHERE id = $2
	`, parentIssueID, uiIssueID); err != nil {
		t.Fatalf("link child issue to parent: %v", err)
	}

	items := []map[string]any{}
	for i, name := range []string{
		"01 钱包首页-已绑定支付宝",
		"01 钱包首页-未绑定支付宝",
		"04 提现-空金额",
		"04 提现-弹窗:确认提现",
		"04 提现-结果:提现申请已提交",
	} {
		items = append(items, map[string]any{
			"itemId":       fmt.Sprintf("wallet-frame-%d", i+1),
			"order":        i + 1,
			"designFileId": created.File.ID,
			"revisionId":   created.CurrentRevision.ID,
			"frameId":      fmt.Sprintf("frame-%d", i+1),
			"frameName":    name,
			"source":       "frame",
		})
	}
	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id":  created.File.ID,
		"issue_id": uiIssueID,
		"input": map[string]any{
			"version":   "1.0",
			"projectId": projectID,
			"items":     items,
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	generateReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/plan/generate?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	generateW := httptest.NewRecorder()
	testHandler.GenerateDesignRestorePlan(generateW, generateReq)
	if generateW.Code != http.StatusCreated {
		t.Fatalf("GenerateDesignRestorePlan: expected 201, got %d: %s", generateW.Code, generateW.Body.String())
	}
	var generatedPlan DesignRestorePlanResponse
	if err := json.NewDecoder(generateW.Body).Decode(&generatedPlan); err != nil {
		t.Fatalf("decode GenerateDesignRestorePlan: %v", err)
	}
	var plan map[string]any
	if err := json.Unmarshal(generatedPlan.Plan, &plan); err != nil {
		t.Fatalf("decode generated plan: %v", err)
	}
	structure := plan["designStructure"].(map[string]any)
	pages := structure["pages"].([]any)
	if len(pages) != 2 {
		t.Fatalf("pages = %#v, want two semantic pages", pages)
	}
	var withdraw map[string]any
	for _, rawPage := range pages {
		page := rawPage.(map[string]any)
		if page["pageName"] == "提现" {
			withdraw = page
		}
	}
	if withdraw == nil {
		t.Fatalf("withdraw page missing from pages: %#v", pages)
	}
	if len(withdraw["states"].([]any)) != 1 || len(withdraw["modals"].([]any)) != 1 || len(withdraw["resultStates"].([]any)) != 1 {
		t.Fatalf("withdraw semantic buckets = %#v", withdraw)
	}
	itemsBlock := plan["items"].([]any)
	modalItem := itemsBlock[3].(map[string]any)
	semantic := modalItem["semantic"].(map[string]any)
	if semantic["pageName"] != "提现" || semantic["kind"] != "modal" || semantic["label"] != "确认提现" {
		t.Fatalf("modal semantic = %#v", semantic)
	}
}

func TestGenerateDesignRestorePlanBuildsPageTargetsForDistinctPageNames(t *testing.T) {
	created := createDesignFileForTest(t, "我的钱包")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	frameNames := []string{
		"01 钱包首页-已绑支付宝",
		"02 管理提现账户-已认证",
		"02 管理提现账户-未认证",
		"03 绑定支付宝-空表单",
		"03 绑定支付宝-已授权",
		"04 提现-空金额",
		"04 提现-金额输入",
		"04 提现-弹窗:确认提现",
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSONWithFrameNamesForTest(frameNames))
	projectID := createProjectForDesignTest(t, "Wallet Project")
	createCompletedDesignRepoAnalysisForDesignTest(t, projectID, "Vue")
	parentIssueID := createIssueForDesignTest(t, "我的钱包", projectID)
	uiIssueID := createIssueForDesignTest(t, "UI设计", projectID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue
		SET parent_issue_id = $1
		WHERE id = $2
	`, parentIssueID, uiIssueID); err != nil {
		t.Fatalf("link child issue to parent: %v", err)
	}

	items := []map[string]any{}
	for i, name := range frameNames {
		items = append(items, map[string]any{
			"itemId":       fmt.Sprintf("wallet-page-frame-%d", i+1),
			"order":        i + 1,
			"designFileId": created.File.ID,
			"revisionId":   created.CurrentRevision.ID,
			"frameId":      fmt.Sprintf("frame-%d", i+1),
			"frameName":    name,
			"source":       "frame",
		})
	}
	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id":  created.File.ID,
		"issue_id": uiIssueID,
		"input": map[string]any{
			"version":   "1.0",
			"projectId": projectID,
			"items":     items,
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	generateReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/plan/generate?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	generateW := httptest.NewRecorder()
	testHandler.GenerateDesignRestorePlan(generateW, generateReq)
	if generateW.Code != http.StatusCreated {
		t.Fatalf("GenerateDesignRestorePlan: expected 201, got %d: %s", generateW.Code, generateW.Body.String())
	}
	var generatedPlan DesignRestorePlanResponse
	if err := json.NewDecoder(generateW.Body).Decode(&generatedPlan); err != nil {
		t.Fatalf("decode GenerateDesignRestorePlan: %v", err)
	}
	var plan map[string]any
	if err := json.Unmarshal(generatedPlan.Plan, &plan); err != nil {
		t.Fatalf("decode generated plan: %v", err)
	}

	targets := plan["targets"].(map[string]any)
	pageTargets, ok := targets["pageTargets"].([]any)
	if !ok {
		t.Fatalf("targets.pageTargets = %#v, want generated page targets", targets["pageTargets"])
	}
	if len(pageTargets) != 4 {
		t.Fatalf("pageTargets = %#v, want four business pages", pageTargets)
	}
	byName := map[string]map[string]any{}
	for _, rawTarget := range pageTargets {
		target := rawTarget.(map[string]any)
		byName[target["pageName"].(string)] = target
	}
	if byName["钱包首页"]["routePath"] != "/business-module" || byName["钱包首页"]["pagePath"] != "src/views/business-module/BusinessModuleView.vue" {
		t.Fatalf("wallet target = %#v", byName["钱包首页"])
	}
	if byName["管理提现账户"]["routePath"] != "/business-module/account-management" || byName["管理提现账户"]["pagePath"] != "src/views/business-module/AccountManagementView.vue" {
		t.Fatalf("account management target = %#v", byName["管理提现账户"])
	}
	if byName["绑定支付宝"]["routePath"] != "/business-module/bind-alipay" || byName["绑定支付宝"]["pagePath"] != "src/views/business-module/BindAlipayView.vue" {
		t.Fatalf("bind alipay target = %#v", byName["绑定支付宝"])
	}
	if byName["提现"]["routePath"] != "/business-module/withdraw" || byName["提现"]["pagePath"] != "src/views/business-module/WithdrawView.vue" {
		t.Fatalf("withdraw target = %#v", byName["提现"])
	}
	policy := targets["pageTargetPolicy"].(map[string]any)
	if policy["forbidTabsAcrossPageNames"] != true {
		t.Fatalf("pageTargetPolicy = %#v, want forbidTabsAcrossPageNames", policy)
	}

	planItems := plan["items"].([]any)
	accountItem := planItems[1].(map[string]any)
	if accountItem["targetPath"] != "src/views/business-module/AccountManagementView.vue" || accountItem["targetRoutePath"] != "/business-module/account-management" {
		t.Fatalf("account item target = %#v", accountItem)
	}
	withdrawModalItem := planItems[7].(map[string]any)
	if withdrawModalItem["targetPath"] != "src/views/business-module/WithdrawView.vue" || withdrawModalItem["targetRoutePath"] != "/business-module/withdraw" {
		t.Fatalf("withdraw modal item target = %#v", withdrawModalItem)
	}

	interactionFlow, ok := plan["interactionFlow"].(map[string]any)
	if !ok {
		t.Fatalf("interactionFlow = %#v, want generated page relationship contract", plan["interactionFlow"])
	}
	flowPolicy := interactionFlow["policy"].(map[string]any)
	if flowPolicy["queryParametersAreDebugOnly"] != true || flowPolicy["primaryPathRequiresUserInteractions"] != true {
		t.Fatalf("interactionFlow.policy = %#v, want interaction-first query-debug policy", flowPolicy)
	}
	transitions := interactionFlow["transitions"].([]any)
	if !designRestoreTransitionExistsForTest(transitions, "钱包首页", "提现账号管理", "管理提现账户", "route") {
		t.Fatalf("interactionFlow transitions missing wallet account-management route: %#v", transitions)
	}
	if !designRestoreTransitionExistsForTest(transitions, "钱包首页", "提现", "提现", "route") {
		t.Fatalf("interactionFlow transitions missing wallet withdraw route: %#v", transitions)
	}
	if !designRestoreTransitionExistsForTest(transitions, "管理提现账户", "立即绑定", "绑定支付宝", "route") {
		t.Fatalf("interactionFlow transitions missing account bind-alipay route: %#v", transitions)
	}
	if !designRestoreTransitionExistsForTest(transitions, "绑定支付宝", "确认绑定", "管理提现账户", "route") {
		t.Fatalf("interactionFlow transitions missing bind-confirm account route: %#v", transitions)
	}
	stateTransitions := interactionFlow["stateTransitions"].([]any)
	if !designRestoreStateTransitionExistsForTest(stateTransitions, "提现", "金额输入", "state") {
		t.Fatalf("interactionFlow stateTransitions missing withdraw amount state: %#v", stateTransitions)
	}
	if !designRestoreStateTransitionExistsForTest(stateTransitions, "提现", "确认提现", "modal") {
		t.Fatalf("interactionFlow stateTransitions missing withdraw confirm modal: %#v", stateTransitions)
	}

	artifacts, ok := plan["artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("artifacts = %#v, want UI restore artifact document contract", plan["artifacts"])
	}
	uiRestoreDocument, ok := artifacts["uiRestoreDocument"].(map[string]any)
	if !ok {
		t.Fatalf("artifacts.uiRestoreDocument = %#v, want document target", artifacts["uiRestoreDocument"])
	}
	if uiRestoreDocument["path"] != "docs/multica/ui-restore/"+createdTask.ID+".md" || uiRestoreDocument["handoffField"] != "artifactDocPath" {
		t.Fatalf("ui restore document artifact = %#v", uiRestoreDocument)
	}
	execution := plan["execution"].(map[string]any)
	if !designRestoreAnySliceContainsString(execution["allowedPaths"], "docs/multica/ui-restore") {
		t.Fatalf("execution.allowedPaths = %#v, want UI restore artifact doc path allowed", execution["allowedPaths"])
	}
}

func designRestoreAnySliceContainsString(values any, want string) bool {
	rawValues, ok := values.([]any)
	if !ok {
		return false
	}
	for _, raw := range rawValues {
		value, ok := raw.(string)
		if ok && value == want {
			return true
		}
	}
	return false
}

func designRestoreTransitionExistsForTest(transitions []any, fromPage string, triggerText string, toPage string, kind string) bool {
	for _, raw := range transitions {
		transition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if transition["fromPage"] == fromPage &&
			transition["triggerText"] == triggerText &&
			transition["toPage"] == toPage &&
			transition["kind"] == kind {
			return true
		}
	}
	return false
}

func designRestoreStateTransitionExistsForTest(transitions []any, pageName string, label string, kind string) bool {
	for _, raw := range transitions {
		transition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if transition["pageName"] == pageName &&
			transition["label"] == label &&
			transition["kind"] == kind {
			return true
		}
	}
	return false
}

func TestGenerateDesignRestorePlanIncludesLightweightRestorePackPolicies(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Pack Policy Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	nativeJSON := contextDesignNativeJSON("Restore Pack Policy Design")
	layers := nativeJSON["layers"].(map[string]any)
	layers["select-account"] = map[string]any{
		"id":      "select-account",
		"name":    "请选择提现账户",
		"type":    "text",
		"visible": true,
		"x":       24,
		"y":       128,
		"width":   180,
		"height":  24,
		"frameId": "frame-main",
		"text":    map[string]any{"characters": "请选择提现账户", "fontSize": 16},
	}
	layers["asset-card"] = map[string]any{
		"id":      "asset-card",
		"name":    "银行卡图标",
		"type":    "shape",
		"visible": true,
		"x":       24,
		"y":       180,
		"width":   48,
		"height":  48,
		"frameId": "frame-main",
		"image":   map[string]any{"assetId": "asset-card-image"},
	}
	nativeJSON["assets"].(map[string]any)["asset-card-image"] = map[string]any{"id": "asset-card-image", "kind": "image", "url": "https://static.example/card.png", "width": 48, "height": 48}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, nativeJSON)

	projectID := createProjectForDesignTest(t, "Restore Pack Policy Project")
	uiIssueID := createIssueForDesignTest(t, "UI设计", projectID)
	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id":  created.File.ID,
		"issue_id": uiIssueID,
		"input": map[string]any{
			"version":   "1.0",
			"projectId": projectID,
			"items": []map[string]any{{
				"itemId":       "restore-pack-frame",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"frameName":    "提现-金额输入",
				"source":       "frame",
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	generateReq := withURLParam(newRequest("POST", "/api/design-restore-tasks/"+createdTask.ID+"/plan/generate?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	generateW := httptest.NewRecorder()
	testHandler.GenerateDesignRestorePlan(generateW, generateReq)
	if generateW.Code != http.StatusCreated {
		t.Fatalf("GenerateDesignRestorePlan: expected 201, got %d: %s", generateW.Code, generateW.Body.String())
	}
	var generatedPlan DesignRestorePlanResponse
	if err := json.NewDecoder(generateW.Body).Decode(&generatedPlan); err != nil {
		t.Fatalf("decode GenerateDesignRestorePlan: %v", err)
	}
	var plan map[string]any
	if err := json.Unmarshal(generatedPlan.Plan, &plan); err != nil {
		t.Fatalf("decode generated plan: %v", err)
	}

	restorePack := plan["restorePack"].(map[string]any)
	if restorePack["mode"] != "lightweight" {
		t.Fatalf("restorePack mode = %#v, want lightweight", restorePack["mode"])
	}
	assetPolicy := restorePack["assetPolicy"].(map[string]any)
	if assetPolicy["priority"] != "render_visible_layer_assets" || assetPolicy["doNotRedrawExportedAssets"] != true {
		t.Fatalf("asset policy = %#v", assetPolicy)
	}
	interactionPolicy := restorePack["interactionPolicy"].(map[string]any)
	if interactionPolicy["selectLikeText"] != "use_project_select_or_popover" || interactionPolicy["inputLikeText"] != "use_project_input_or_form_control" {
		t.Fatalf("interaction policy = %#v", interactionPolicy)
	}
	noisePolicy := restorePack["noisePolicy"].(map[string]any)
	if noisePolicy["mode"] != "conservative" || noisePolicy["doNotDropVisibleAssets"] != true {
		t.Fatalf("noise policy = %#v", noisePolicy)
	}
	items := plan["items"].([]any)
	hints := items[0].(map[string]any)["restoreHints"].(map[string]any)
	if hints["assetLayerCount"].(float64) < 1 || hints["interactionCueCount"].(float64) < 1 {
		t.Fatalf("restore hints = %#v", hints)
	}
}

func TestDesignRestoreMappingsPersistFromAgentSummary(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Mapping Persist Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"version": "1.0",
			"items": []map[string]any{{
				"itemId":       "mapping-frame-1",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"source":       "frame",
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})
	task, err := testHandler.Queries.GetDesignRestoreTaskInWorkspace(context.Background(), db.GetDesignRestoreTaskInWorkspaceParams{ID: util.MustParseUUID(createdTask.ID), WorkspaceID: util.MustParseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatalf("load restore task: %v", err)
	}
	err = testHandler.replaceDesignRestoreMappingsFromSummary(context.Background(), task, designRestoreResultSummary{RestoreMapping: []map[string]any{{
		"source":     "main-title",
		"target":     "packages/views/designs/example.tsx",
		"targetKind": "file",
		"confidence": 0.91,
	}}})
	if err != nil {
		t.Fatalf("replaceDesignRestoreMappingsFromSummary: %v", err)
	}
	listReq := withURLParam(newRequest("GET", "/api/design-restore-tasks/"+createdTask.ID+"/mappings?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID)
	listW := httptest.NewRecorder()
	testHandler.ListDesignRestoreMappings(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignRestoreMappings: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var resp DesignRestoreMappingListResponse
	if err := json.NewDecoder(listW.Body).Decode(&resp); err != nil {
		t.Fatalf("decode mappings: %v", err)
	}
	if len(resp.Mappings) != 1 || resp.Mappings[0].LayerID != "main-title" || resp.Mappings[0].TargetPath != "packages/views/designs/example.tsx" {
		t.Fatalf("unexpected mappings: %+v", resp.Mappings)
	}
}

func TestGetDesignRestoreTaskItemContextFrameReturnsFrameContext(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Frame Context Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Restore Frame Context Design"))

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"version": "1",
			"items": []map[string]any{{
				"itemId":       "item-frame-main",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"frameName":    "Main Screen",
				"source":       "frame",
				"moduleKey":    "module-a",
				"stateKey":     "state-a",
				"slotKey":      "slot-a",
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	req := withDesignURLParams(newRequest("GET", "/api/design-restore-tasks/"+createdTask.ID+"/items/item-frame-main/context?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID, "itemId", "item-frame-main")
	w := httptest.NewRecorder()
	testHandler.GetDesignRestoreTaskItemContext(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetDesignRestoreTaskItemContext: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp DesignRestoreTaskItemContextResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode item context response: %v", err)
	}
	if resp.Task.ID != createdTask.ID || resp.Task.FileID != created.File.ID || resp.Task.RevisionID != created.CurrentRevision.ID {
		t.Fatalf("unexpected task metadata: %+v", resp.Task)
	}
	if resp.Item.ItemID != "item-frame-main" || resp.Item.DesignFileID != created.File.ID || resp.Item.RevisionID != created.CurrentRevision.ID || resp.Item.FrameID != "frame-main" || resp.Item.Source != "frame" {
		t.Fatalf("unexpected item metadata: %+v", resp.Item)
	}
	frame := resp.Context["frame"].(map[string]any)
	if frame["id"] != "frame-main" || frame["name"] != "Main Screen" {
		t.Fatalf("unexpected frame context frame: %+v", frame)
	}
	layers := resp.Context["layers"].(map[string]any)
	for _, id := range []string{"main-root", "main-title", "main-image", "main-offscreen"} {
		if _, ok := layers[id]; !ok {
			t.Fatalf("expected layer %s in frame context", id)
		}
	}
	if _, ok := layers["secondary-title"]; ok {
		t.Fatal("frame context included layer from another frame")
	}
}

func TestGetDesignRestoreTaskItemContextSelectionBoundsReturnsSelectionContext(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Selection Context Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	updateDesignRevisionNativeJSONForTest(t, created.CurrentRevision.ID, contextDesignNativeJSON("Restore Selection Context Design"))

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"version": "1",
			"items": []map[string]any{{
				"itemId":       "item-selection-main",
				"order":        1,
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-main",
				"frameName":    "Main Screen",
				"source":       "selection_bounds",
				"selectionBounds": map[string]any{
					"x": 35, "y": 35, "width": 230, "height": 80,
				},
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	req := withDesignURLParams(newRequest("GET", "/api/design-restore-tasks/"+createdTask.ID+"/items/item-selection-main/context?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID, "itemId", "item-selection-main")
	w := httptest.NewRecorder()
	testHandler.GetDesignRestoreTaskItemContext(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetDesignRestoreTaskItemContext: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp DesignRestoreTaskItemContextResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode item context response: %v", err)
	}
	if resp.Item.ItemID != "item-selection-main" || resp.Item.Source != "selection_bounds" {
		t.Fatalf("unexpected item metadata: %+v", resp.Item)
	}
	resolved := resp.Context["resolvedLayerIds"].([]any)
	if len(resolved) != 2 || resolved[0] != "main-root" || resolved[1] != "main-title" {
		t.Fatalf("resolvedLayerIds = %+v, want [main-root main-title]", resolved)
	}
	layers := resp.Context["layers"].(map[string]any)
	if _, ok := layers["main-title"]; !ok {
		t.Fatal("selection context should include intersecting main-title layer")
	}
	if _, ok := layers["main-image"]; ok {
		t.Fatal("selection context should not include non-intersecting main-image layer")
	}
}

func TestGetDesignRestoreTaskItemContextUnknownItemReturnsNotFound(t *testing.T) {
	created := createDesignFileForTest(t, "Restore Unknown Item Design")
	if created.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}

	createReq := newRequest("POST", "/api/design-restore-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"file_id": created.File.ID,
		"input": map[string]any{
			"version": "1",
			"items": []map[string]any{{
				"itemId":       "known-item",
				"designFileId": created.File.ID,
				"revisionId":   created.CurrentRevision.ID,
				"frameId":      "frame-1",
				"source":       "frame",
			}},
		},
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignRestoreTask(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateDesignRestoreTask: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var createdTask DesignRestoreTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&createdTask); err != nil {
		t.Fatalf("decode CreateDesignRestoreTask: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_restore_task WHERE id = $1`, createdTask.ID)
	})

	req := withDesignURLParams(newRequest("GET", "/api/design-restore-tasks/"+createdTask.ID+"/items/missing-item/context?workspace_id="+testWorkspaceID, nil), "id", createdTask.ID, "itemId", "missing-item")
	w := httptest.NewRecorder()
	testHandler.GetDesignRestoreTaskItemContext(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetDesignRestoreTaskItemContext unknown item: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListDesignFilesHidesManagedAssetSources(t *testing.T) {
	createLocalUIRestoreAgentForDesignTest(t)
	regular := createDesignFileForTest(t, "Visible Business Design")
	projectID := createProjectForDesignTest(t, "Managed Asset List Project")
	token := createPluginTokenForTest(t)
	req := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":              "Hidden UI Spec Source",
		"project_id":         projectID,
		"asset_type":         "design_system",
		"design_system_name": "Hidden UI Spec",
		"source_ref":         map[string]any{"provider": "figma", "source_key": "hidden-ui-spec"},
		"native_json":        minimalDesignNativeJSON("Hidden UI Spec Source"),
	})
	req.Header.Set("Authorization", "Bearer "+token)
	importW := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(importW, req)
	if importW.Code != http.StatusCreated {
		t.Fatalf("plugin import design system: expected 201, got %d: %s", importW.Code, importW.Body.String())
	}
	var imported struct {
		File DesignFileResponse `json:"file"`
	}
	if err := json.NewDecoder(importW.Body).Decode(&imported); err != nil {
		t.Fatalf("decode plugin import: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, imported.File.ID)
	})

	listReq := newRequest("GET", "/api/design-files?workspace_id="+testWorkspaceID, nil)
	listW := httptest.NewRecorder()
	testHandler.ListDesignFiles(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignFiles: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResp struct {
		DesignFiles []DesignFileResponse `json:"design_files"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode ListDesignFiles: %v", err)
	}
	foundRegular := false
	foundManagedSource := false
	for _, file := range listResp.DesignFiles {
		if file.ID == regular.File.ID {
			foundRegular = true
		}
		if file.ID == imported.File.ID {
			foundManagedSource = true
		}
	}
	if !foundRegular {
		t.Fatalf("regular design file %s should be visible", regular.File.ID)
	}
	if foundManagedSource {
		t.Fatalf("managed asset source file %s should not appear in regular design file list", imported.File.ID)
	}
}

func TestGetDesignFileRejectsInvalidID(t *testing.T) {
	req := withURLParam(newRequest("GET", "/api/design-files/not-a-uuid?workspace_id="+testWorkspaceID, nil), "id", "not-a-uuid")
	w := httptest.NewRecorder()
	testHandler.GetDesignFile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GetDesignFile invalid ID: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteDesignFile(t *testing.T) {
	created := createDesignFileForTest(t, "Delete Me Design")
	req := withURLParam(newRequest("DELETE", "/api/design-files/"+created.File.ID+"?workspace_id="+testWorkspaceID, nil), "id", created.File.ID)
	w := httptest.NewRecorder()
	testHandler.DeleteDesignFile(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteDesignFile: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	getReq := withURLParam(newRequest("GET", "/api/design-files/"+created.File.ID+"?workspace_id="+testWorkspaceID, nil), "id", created.File.ID)
	getW := httptest.NewRecorder()
	testHandler.GetDesignFile(getW, getReq)
	if getW.Code != http.StatusNotFound {
		t.Fatalf("GetDesignFile after delete: expected 404, got %d: %s", getW.Code, getW.Body.String())
	}
}

func TestDeleteDesignFileRejectsInvalidID(t *testing.T) {
	req := withURLParam(newRequest("DELETE", "/api/design-files/not-a-uuid?workspace_id="+testWorkspaceID, nil), "id", "not-a-uuid")
	w := httptest.NewRecorder()
	testHandler.DeleteDesignFile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DeleteDesignFile invalid ID: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func createFigmaImportCodeForTest(t *testing.T) string {
	t.Helper()
	req := newRequest("POST", "/api/design-files/figma-connections?workspace_id="+testWorkspaceID, nil)
	w := httptest.NewRecorder()
	testHandler.CreateFigmaImportConnection(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateFigmaImportConnection: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateFigmaImportConnectionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode figma import connection: %v", err)
	}
	if resp.Code == "" || resp.ExpiresAt == "" {
		t.Fatalf("expected code and expires_at, got %+v", resp)
	}
	return resp.Code
}

func importFigmaDesignForTest(t *testing.T, code string, title string, nativeJSON map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest("POST", "/api/design-files/imports/figma", map[string]any{
		"code":           code,
		"workspace_slug": handlerTestWorkspaceSlug,
		"title":          title,
		"description":    "Imported from Figma plugin",
		"source_ref":     map[string]any{"tool": "figma", "test": true},
		"native_json":    nativeJSON,
	})
	req.Header.Del("X-User-ID")
	req.Header.Del("X-Workspace-ID")
	w := httptest.NewRecorder()
	testHandler.ImportFigmaDesignFile(w, req)
	return w
}

func TestFigmaImportConnectionAndImport(t *testing.T) {
	code := createFigmaImportCodeForTest(t)
	w := importFigmaDesignForTest(t, code, "Figma Imported Design", minimalDesignNativeJSON("Figma Imported Design"))
	if w.Code != http.StatusCreated {
		t.Fatalf("ImportFigmaDesignFile: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if resp.File.CreatedBy == nil || *resp.File.CreatedBy != testUserID {
		t.Fatalf("created_by = %v, want %s", resp.File.CreatedBy, testUserID)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, resp.File.ID)
	})

	reuse := importFigmaDesignForTest(t, code, "Reused Code Design", minimalDesignNativeJSON("Reused Code Design"))
	if reuse.Code != http.StatusUnauthorized {
		t.Fatalf("reused code: expected 401, got %d: %s", reuse.Code, reuse.Body.String())
	}
}

func TestFigmaImportRejectsWorkspaceMismatch(t *testing.T) {
	code := createFigmaImportCodeForTest(t)
	req := newRequest("POST", "/api/design-files/imports/figma", map[string]any{
		"code":           code,
		"workspace_slug": "wrong-workspace",
		"title":          "Wrong Workspace",
		"native_json":    minimalDesignNativeJSON("Wrong Workspace"),
	})
	req.Header.Del("X-User-ID")
	req.Header.Del("X-Workspace-ID")
	w := httptest.NewRecorder()
	testHandler.ImportFigmaDesignFile(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("workspace mismatch: expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFigmaImportRejectsExpiredCode(t *testing.T) {
	code := "figma_expired_test_code"
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO design_import_code (workspace_id, user_id, provider, code_hash, expires_at)
		VALUES ($1, $2, 'figma', $3, $4)
	`, testWorkspaceID, testUserID, auth.HashToken(code), time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("insert expired code: %v", err)
	}
	w := importFigmaDesignForTest(t, code, "Expired Code Design", minimalDesignNativeJSON("Expired Code Design"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired code: expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFigmaImportInvalidNativeJSONDoesNotConsumeCode(t *testing.T) {
	code := createFigmaImportCodeForTest(t)
	invalid := importFigmaDesignForTest(t, code, "Invalid Figma Design", map[string]any{
		"version": "1.0",
		"file":    map[string]any{"title": "Invalid", "sourceType": "import"},
		"frames":  []map[string]any{{"id": "frame-1", "name": "Invalid", "rootLayerId": "missing", "width": 100, "height": 100}},
		"layers":  map[string]any{},
		"assets":  map[string]any{},
	})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid native json: expected 400, got %d: %s", invalid.Code, invalid.Body.String())
	}

	valid := importFigmaDesignForTest(t, code, "Valid After Invalid", minimalDesignNativeJSON("Valid After Invalid"))
	if valid.Code != http.StatusCreated {
		t.Fatalf("valid after invalid: expected 201, got %d: %s", valid.Code, valid.Body.String())
	}
}

func createPluginTokenForTest(t *testing.T) string {
	t.Helper()
	token := fmt.Sprintf("mfp_test_%d", time.Now().UnixNano())
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO design_plugin_token (provider, token_hash, token_prefix, user_id, workspace_id, scope, name)
		VALUES ('figma', $1, $2, $3, $4, 'design_import', 'Figma Plugin Test')
	`, auth.HashToken(token), token[:12], testUserID, testWorkspaceID)
	if err != nil {
		t.Fatalf("insert plugin token: %v", err)
	}
	return token
}

func TestFigmaPluginContextReturnsProjectFolders(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Plugin Context Project")
	folderID := createDesignFolderForTest(t, projectID, "Plugin Folder")
	design := createDesignFileForTest(t, "Plugin Context Existing Design")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1, folder_id = $2 WHERE id = $3`, projectID, folderID, design.File.ID); err != nil {
		t.Fatalf("attach design file to project folder: %v", err)
	}
	token := createPluginTokenForTest(t)
	req := newRequest("GET", "/api/design-plugin/figma/context", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	testHandler.GetFigmaPluginContext(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetFigmaPluginContext: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp FigmaPluginContextResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	for _, project := range resp.Projects {
		if project.ID == projectID {
			foundFolder := false
			for _, folder := range project.Folders {
				if folder.ID == folderID {
					foundFolder = true
					break
				}
			}
			if !foundFolder {
				t.Fatalf("context did not include folder %s under project %s", folderID, projectID)
			}
			for _, file := range project.DesignFiles {
				if file.ID == design.File.ID {
					if file.FolderID == nil || *file.FolderID != folderID {
						t.Fatalf("design file folder_id = %v, want %s", file.FolderID, folderID)
					}
					return
				}
			}
			t.Fatalf("context did not include design file %s under project %s", design.File.ID, projectID)
		}
	}
	t.Fatalf("context did not include project %s", projectID)
}

func TestFigmaPluginImportRequiresProject(t *testing.T) {
	token := createPluginTokenForTest(t)
	req := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":       "Plugin Import Without Project",
		"native_json": minimalDesignNativeJSON("Plugin Import Without Project"),
	})
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plugin import without project: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDesignFileRejectsEmbeddedBinaryNativeJSON(t *testing.T) {
	nativeJSON := minimalDesignNativeJSON("Embedded Binary Design")
	nativeJSON["assets"] = map[string]any{
		"asset-1": map[string]any{"id": "asset-1", "kind": "frame_preview", "url": "data:image/png;base64,AAAA"},
	}
	req := newRequest("POST", "/api/design-files?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Embedded Binary Design",
		"native_json": nativeJSON,
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignFile(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateDesignFile embedded binary: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFigmaPluginImportRejectsEmbeddedBinaryNativeJSON(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Plugin Binary Guard Project")
	token := createPluginTokenForTest(t)
	nativeJSON := minimalDesignNativeJSON("Plugin Binary Guard Design")
	nativeJSON["assets"] = map[string]any{
		"asset-1": map[string]any{"id": "asset-1", "kind": "frame_preview", "url": "https://static.example/frame.png", "bytes": []int{1, 2, 3}},
	}
	req := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":       "Plugin Binary Guard Design",
		"project_id":  projectID,
		"native_json": nativeJSON,
	})
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plugin import embedded binary: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFigmaPluginImportWithProjectAndFolder(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Plugin Import Project")
	folderID := createDesignFolderForTest(t, projectID, "Plugin Import Folder")
	token := createPluginTokenForTest(t)
	var beforeCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_file WHERE workspace_id = $1 AND project_id = $2 AND folder_id = $3`, testWorkspaceID, projectID, folderID).Scan(&beforeCount); err != nil {
		t.Fatalf("count design files before import: %v", err)
	}
	req := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":               "Plugin Import Request Title",
		"design_file_title":   "Plugin Import Design File Title",
		"project_id":          projectID,
		"folder_id":           folderID,
		"source_ref":          map[string]any{"provider": "figma"},
		"native_json":         minimalDesignNativeJSON("Plugin Import Design"),
		"publish_as_template": false,
	})
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("plugin import: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp DesignFileDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode plugin import: %v", err)
	}
	if resp.File.ProjectID == nil || *resp.File.ProjectID != projectID {
		t.Fatalf("project_id = %v, want %s", resp.File.ProjectID, projectID)
	}
	if resp.File.FolderID == nil || *resp.File.FolderID != folderID {
		t.Fatalf("folder_id = %v, want %s", resp.File.FolderID, folderID)
	}
	if resp.File.Title != "Plugin Import Design File Title" {
		t.Fatalf("title = %q, want Plugin Import Design File Title", resp.File.Title)
	}
	if resp.CurrentRevision == nil {
		t.Fatal("expected current revision")
	}
	doc := decodeDesignRevisionNativeJSONForTest(t, resp.CurrentRevision.NativeJSON)
	if report := importFidelityReportFromNativeJSONForTest(t, doc); report["byFrameId"] == nil {
		t.Fatalf("missing fidelity byFrameId: %+v", report)
	}
	var afterCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_file WHERE workspace_id = $1 AND project_id = $2 AND folder_id = $3`, testWorkspaceID, projectID, folderID).Scan(&afterCount); err != nil {
		t.Fatalf("count design files after import: %v", err)
	}
	if afterCount != beforeCount+1 {
		t.Fatalf("file count after import = %d, want %d", afterCount, beforeCount+1)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, resp.File.ID)
	})
}

func TestFigmaPluginImportTargetDesignFileMergesNewSourceNode(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Plugin Merge Project")
	folderID := createDesignFolderForTest(t, projectID, "Plugin Merge Folder")
	token := createPluginTokenForTest(t)

	initialReq := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":               "Plugin Merge Design",
		"design_file_title":   "Plugin Merge Design",
		"project_id":          projectID,
		"folder_id":           folderID,
		"source_ref":          map[string]any{"provider": "figma", "source_key": "merge-source"},
		"native_json":         figmaDesignNativeJSONWithSourceNodes("Plugin Merge Design", "1:1", "1:2", "1:3", "1:4"),
		"publish_as_template": false,
	})
	initialReq.Header.Set("Authorization", "Bearer "+token)
	initialW := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(initialW, initialReq)
	if initialW.Code != http.StatusCreated {
		t.Fatalf("initial plugin import: expected 201, got %d: %s", initialW.Code, initialW.Body.String())
	}
	var initial DesignFileDetailResponse
	if err := json.NewDecoder(initialW.Body).Decode(&initial); err != nil {
		t.Fatalf("decode initial plugin import: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, initial.File.ID)
	})
	if initial.CurrentRevision == nil || initial.CurrentRevision.RevisionNumber != 1 {
		t.Fatalf("initial revision = %+v, want number 1", initial.CurrentRevision)
	}
	if got := frameCountFromNativeJSONForTest(t, initial.CurrentRevision.NativeJSON); got != 4 {
		t.Fatalf("initial frame count = %d, want 4", got)
	}
	var beforeCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_file WHERE workspace_id = $1 AND project_id = $2 AND folder_id = $3`, testWorkspaceID, projectID, folderID).Scan(&beforeCount); err != nil {
		t.Fatalf("count design files before merge: %v", err)
	}

	mergeReq := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":                 "Plugin Merge Design New Frame",
		"project_id":            projectID,
		"folder_id":             folderID,
		"target_design_file_id": initial.File.ID,
		"source_ref":            map[string]any{"provider": "figma", "source_key": "merge-source"},
		"native_json":           figmaDesignNativeJSONWithSourceNodes("Plugin Merge Design New Frame", "1:5"),
		"publish_as_template":   false,
	})
	mergeReq.Header.Set("Authorization", "Bearer "+token)
	mergeW := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(mergeW, mergeReq)
	if mergeW.Code != http.StatusCreated {
		t.Fatalf("merge plugin import: expected 201, got %d: %s", mergeW.Code, mergeW.Body.String())
	}
	var merged DesignFileDetailResponse
	if err := json.NewDecoder(mergeW.Body).Decode(&merged); err != nil {
		t.Fatalf("decode merge plugin import: %v", err)
	}
	if merged.File.ID != initial.File.ID {
		t.Fatalf("merged file id = %s, want %s", merged.File.ID, initial.File.ID)
	}
	if merged.CurrentRevision == nil || merged.CurrentRevision.RevisionNumber != 2 {
		t.Fatalf("merged revision = %+v, want number 2", merged.CurrentRevision)
	}
	if got := frameCountFromNativeJSONForTest(t, merged.CurrentRevision.NativeJSON); got != 5 {
		t.Fatalf("merged frame count = %d, want 5", got)
	}
	mergedDoc := decodeDesignRevisionNativeJSONForTest(t, merged.CurrentRevision.NativeJSON)
	mergedFrames, ok := mergedDoc["frames"].([]any)
	if !ok {
		t.Fatalf("merged native_json frames type = %T", mergedDoc["frames"])
	}
	var foundNewFrameSource map[string]any
	for _, rawFrame := range mergedFrames {
		frame, ok := rawFrame.(map[string]any)
		if !ok || frame["sourceNodeId"] != "1:5" {
			continue
		}
		foundNewFrameSource, _ = frame["source"].(map[string]any)
		break
	}
	if foundNewFrameSource["groupName"] != "Group 43" {
		t.Fatalf("merged frame source groupName = %v, want Group 43", foundNewFrameSource["groupName"])
	}
	if report := importFidelityReportFromNativeJSONForTest(t, mergedDoc); report["byFrameId"] == nil {
		t.Fatalf("missing merged fidelity byFrameId: %+v", report)
	}
	var afterCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_file WHERE workspace_id = $1 AND project_id = $2 AND folder_id = $3`, testWorkspaceID, projectID, folderID).Scan(&afterCount); err != nil {
		t.Fatalf("count design files after merge: %v", err)
	}
	if afterCount != beforeCount {
		t.Fatalf("file count after merge = %d, want %d", afterCount, beforeCount)
	}
}

func TestFigmaPluginImportCanPublishTemplate(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Plugin Template Project")
	token := createPluginTokenForTest(t)
	templateKey := fmt.Sprintf("plugin-template-%d", time.Now().UnixNano())
	req := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":                "Plugin Template Design",
		"project_id":           projectID,
		"source_ref":           map[string]any{"provider": "figma", "source_key": templateKey},
		"native_json":          minimalDesignNativeJSON("Plugin Template Design"),
		"publish_as_template":  true,
		"template_library_key": "figma",
		"template_key":         templateKey,
		"template_name":        "Plugin Published Template",
		"template_category":    "figma",
	})
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("plugin import template: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		File     DesignFileResponse             `json:"file"`
		Template *DesignCatalogTemplateResponse `json:"template"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode plugin template import: %v", err)
	}
	if resp.Template == nil {
		t.Fatal("expected template response")
	}
	if resp.Template.Name != "Plugin Published Template" {
		t.Fatalf("template name = %q", resp.Template.Name)
	}
	var metadata map[string]any
	if err := json.Unmarshal(resp.Template.Metadata, &metadata); err != nil {
		t.Fatalf("decode template metadata: %v", err)
	}
	if _, ok := metadata["template_profile"].(map[string]any); !ok {
		t.Fatalf("expected plugin template metadata to include template_profile, got %+v", metadata)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, resp.File.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_template_library WHERE workspace_id = $1 AND key = 'figma'`, testWorkspaceID)
	})
}

func TestFigmaPluginImportCanPublishDesignSystem(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Plugin Design System Project")
	createLocalUIRestoreAgentForDesignTest(t)
	token := createPluginTokenForTest(t)
	req := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":                     "CRM UI Spec",
		"project_id":                projectID,
		"asset_type":                "design_system",
		"design_system_name":        "CRM 后台 UI 规范",
		"design_system_description": "CRM admin tokens and components",
		"source_ref":                map[string]any{"provider": "figma", "source_key": "crm-ui-spec"},
		"native_json":               minimalDesignNativeJSON("CRM UI Spec"),
	})
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("plugin import design system: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		File         DesignFileResponse           `json:"file"`
		DesignSystem *DesignSystemProfileResponse `json:"design_system"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode plugin design system import: %v", err)
	}
	if resp.DesignSystem == nil {
		t.Fatal("expected design_system response")
	}
	if resp.DesignSystem.Name != "CRM 后台 UI 规范" {
		t.Fatalf("design system name = %q", resp.DesignSystem.Name)
	}
	if resp.DesignSystem.Status != "analyzing" {
		t.Fatalf("uploaded design system status = %q, want analyzing", resp.DesignSystem.Status)
	}
	if resp.DesignSystem.ProjectID == nil || *resp.DesignSystem.ProjectID != projectID {
		t.Fatalf("design system project_id = %v, want %s", resp.DesignSystem.ProjectID, projectID)
	}
	var queuedTaskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id
		FROM agent_task_queue
		WHERE context->>'type' = $1
		  AND context->>'design_system_profile_id' = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, service.DesignSystemProfileAnalyzeContextType, resp.DesignSystem.ID).Scan(&queuedTaskID); err != nil {
		t.Fatalf("get queued design system analyze task: %v", err)
	}
	var defaultCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM design_system_profile
		WHERE workspace_id = $1 AND project_id = $2 AND is_default = true AND status = 'analyzed'
	`, testWorkspaceID, projectID).Scan(&defaultCount); err != nil {
		t.Fatalf("count default design system: %v", err)
	}
	if defaultCount != 0 {
		t.Fatalf("default design system count = %d, want 0 before agent analysis", defaultCount)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, queuedTaskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, resp.File.ID)
	})
}

func TestFigmaPluginImportTemplateRequiresProject(t *testing.T) {
	token := createPluginTokenForTest(t)
	templateKey := fmt.Sprintf("plugin-template-no-project-%d", time.Now().UnixNano())
	req := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
		"title":                "Plugin Template Design Without Project",
		"source_ref":           map[string]any{"provider": "figma", "source_key": templateKey},
		"native_json":          minimalDesignNativeJSON("Plugin Template Design Without Project"),
		"publish_as_template":  true,
		"template_library_key": "figma",
		"template_key":         templateKey,
		"template_name":        "Plugin Published Template Without Project",
		"template_category":    "figma",
	})
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	testHandler.ImportFigmaDesignWithPluginToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plugin import template without project: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFigmaPluginRepeatedImportWithoutTargetCreatesNewFile(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Plugin Version Project")
	folderID := createDesignFolderForTest(t, projectID, "Plugin Version Folder")
	token := createPluginTokenForTest(t)
	sourceRef := map[string]any{
		"tool":       "figma",
		"file_key":   "figma-file-1",
		"page_id":    "page-1",
		"scope":      "selected",
		"source_key": "figma:figma-file-1:page:page-1:scope:selected:nodes:1:2",
		"node_ids":   []string{"1:2"},
	}

	postImport := func(title string) DesignFileDetailResponse {
		req := newRequest("POST", "/api/design-plugin/figma/imports", map[string]any{
			"title":       title,
			"project_id":  projectID,
			"folder_id":   folderID,
			"source_ref":  sourceRef,
			"native_json": minimalDesignNativeJSON(title),
		})
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		testHandler.ImportFigmaDesignWithPluginToken(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("plugin import %s: expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var resp DesignFileDetailResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode plugin import %s: %v", title, err)
		}
		return resp
	}

	first := postImport("Plugin Version Design v1")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, first.File.ID)
	})
	second := postImport("Plugin Version Design v2")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_file WHERE id = $1`, second.File.ID)
	})

	if second.File.ID == first.File.ID {
		t.Fatalf("second upload file id = %s, want a new file when no target_design_file_id is provided", second.File.ID)
	}
	if first.CurrentRevision == nil || first.CurrentRevision.RevisionNumber != 1 {
		t.Fatalf("first revision = %+v, want number 1", first.CurrentRevision)
	}
	if second.CurrentRevision == nil || second.CurrentRevision.RevisionNumber != 1 {
		t.Fatalf("second revision = %+v, want number 1", second.CurrentRevision)
	}
	var fileCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM design_file
		WHERE workspace_id = $1 AND project_id = $2 AND folder_id = $3 AND source_ref->>'source_key' = $4
	`, testWorkspaceID, projectID, folderID, sourceRef["source_key"]).Scan(&fileCount); err != nil {
		t.Fatalf("count design files: %v", err)
	}
	if fileCount != 2 {
		t.Fatalf("file count = %d, want 2", fileCount)
	}
}

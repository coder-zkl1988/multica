package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/designimplementation"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const designImplementationContextSchemaV1 = "multica.design-implementation-context/v1"

type DesignImplementationRequest struct {
	RevisionID        string   `json:"revision_id"`
	FrameRefs         []string `json:"frame_refs"`
	ProjectResourceID string   `json:"project_resource_id"`
	IssueID           string   `json:"issue_id"`
}

type DesignImplementationPaths struct {
	Context           string `json:"context_path"`
	DesignManifest    string `json:"design_manifest_path"`
	DesignPackage     string `json:"design_package_path"`
	Scope             string `json:"scope_path"`
	RepositoryContext string `json:"repository_context_path"`
	Result            string `json:"result_path"`
}

type DesignImplementationSourceCapabilities struct {
	HasLayers       bool `json:"has_layers"`
	HasPrototype    bool `json:"has_prototype"`
	HasAssets       bool `json:"has_assets"`
	HasInteractions bool `json:"has_interactions"`
}

type DesignImplementationPackageDescriptor struct {
	Source           string         `json:"source"`
	ArchivePath      string         `json:"archive_path,omitempty"`
	ContentDigest    string         `json:"content_digest"`
	RestorePackScope map[string]any `json:"restore_pack_scope,omitempty"`
}

type DesignImplementationContextResponse struct {
	SchemaVersion            string                                 `json:"schema_version"`
	ImplementationRef        string                                 `json:"implementation_ref"`
	DesignRef                string                                 `json:"design_ref"`
	RevisionID               string                                 `json:"revision_id"`
	ContentDigest            string                                 `json:"content_digest"`
	FrameRefs                []string                               `json:"frame_refs"`
	ProjectID                string                                 `json:"project_id"`
	IssueID                  string                                 `json:"issue_id"`
	TaskID                   string                                 `json:"task_id,omitempty"`
	ProjectResourceID        string                                 `json:"project_resource_id"`
	DesignTitle              string                                 `json:"design_title"`
	Package                  *DesignImplementationPackageDescriptor `json:"package,omitempty"`
	SourceInstructions       []string                               `json:"source_instructions,omitempty"`
	VerificationTargets      []string                               `json:"verification_targets,omitempty"`
	DesignSystemDigest       string                                 `json:"design_system_digest,omitempty"`
	AllowedWritePaths        []string                               `json:"allowed_write_paths"`
	VerificationRequirements []string                               `json:"verification_requirements"`
	Paths                    DesignImplementationPaths              `json:"paths"`
	SourceCapabilities       DesignImplementationSourceCapabilities `json:"source_capabilities"`
}

type DesignImplementationPromptResponse struct {
	Prompt       string                              `json:"prompt"`
	MCPArguments map[string]any                      `json:"mcp_arguments"`
	Context      DesignImplementationContextResponse `json:"context"`
}

const designImplementationResultExample = `{
  "schema_version": "multica.design-implementation-result/v1",
  "design_ref": "copy exactly from context.json",
  "revision_id": "copy exactly from context.json",
  "repository_commit_before": "full pre-change commit hash",
  "status": "completed",
  "mappings": [{
    "frame_ref": "copy exactly from context.json",
    "target_files": ["relative/repository/path"],
    "target_components": [],
    "reused_components": [],
    "changed_routes": [],
    "reused_routes": []
  }],
  "commands": [{"command": "exact command", "status": "passed", "summary": "concise result"}],
  "preview_evidence": [{"frame_ref": "copy exactly from context.json", "status": "passed", "path": "bounded task-workdir preview evidence path", "summary": "rendered DOM and computed-layout checks"}],
  "blockers": [],
  "rollback_notes": []
}`

func (h *Handler) BuildDesignImplementationPrompt(w http.ResponseWriter, r *http.Request) {
	contextValue, request, repository, ok := h.resolveDesignImplementationRequest(w, r)
	if !ok {
		return
	}
	frameLines := make([]string, len(contextValue.FrameRefs))
	for i, frameRef := range contextValue.FrameRefs {
		frameLines[i] = "- " + frameRef
	}
	mcpArguments := map[string]any{
		"designRef": contextValue.DesignRef, "revisionId": contextValue.RevisionID,
		"frameRefs": contextValue.FrameRefs, "targetRepositoryId": contextValue.ProjectResourceID,
		"issueId": request.IssueID,
	}
	prompt := fmt.Sprintf("【任务】\n根据关联设计稿实现当前任务，优先复用目标仓库已有组件和页面结构。\n\n【设计稿】\n标题：%s\n固定版本：%s\n所选 Frame：\n%s\n目标仓库：%s\n\n【执行步骤】\n1. 调用 multica_design_get_implementation_context。任务身份由运行时绑定，不要手抄设计引用，参数使用空对象：\n```json\n{}\n```\n2. 读取目标仓库路由、组件、状态管理和样式规范。\n3. 根据 Implementation Context 完成实现。\n4. 运行约定验证，并把渲染后的 DOM、资源加载和计算布局检查写入任务工作目录中的文本或 JSON 证据文件。\n5. 写入 `.agent_context/design_implementation/result/implementation-result.json`，严格使用以下 `multica.design-implementation-result/v1` 字段，不得添加其他字段：\n```json\n%s\n```\n6. completed 结果的每个 Frame 都必须有 mapping 和 preview_evidence；每个 command 必须有 status 与 summary；preview_evidence 必须有实际存在的 path；partial/blocked/failed/cancelled 也必须写入结果。\n7. 调用 multica_design_validate_implementation_result，验证通过后只返回简短摘要；daemon 会直接收集已验证文件。\n\n【约束】\n禁止整图替代；禁止直接复制 Prototype；保留无关 dirty worktree。\n不要调用 `multica issue status` 或以任何方式修改当前 Issue 状态。\n不要调用 `multica issue get` 或 `multica issue comment add`；daemon 会保存结构化结果。\n不得创建本地提交、推送分支、创建 PR 或合并。\n不得创建或上传截图、录屏或 trace；视觉验收使用文本或 JSON 形式的 DOM 与计算布局证据。\n\n【输出】\n修改文件、复用组件、新增组件、检查结果、视觉验收和阻塞项。",
		contextValue.DesignTitle, contextValue.RevisionID, strings.Join(frameLines, "\n"), designImplementationRepositoryName(repository), designImplementationResultExample)
	prompt += "\n【可选真实预览】\n只有已验证、当前真实运行的实现预览地址存在时，才在对应 preview_evidence 条目中提供可选的 url 字段。url 必须是包含主机名且不含用户名或密码的 HTTP(S) 地址；不得编造地址，不得为了提供预览而自动部署。没有可用地址时省略 url，并在 summary 中如实说明。path 继续只记录任务工作目录内的相对证据文件路径，不得将本地路径或输入设计稿地址当作实现预览。\n"
	writeJSON(w, http.StatusOK, DesignImplementationPromptResponse{
		Prompt:       prompt,
		MCPArguments: mcpArguments,
		Context:      contextValue,
	})
}

func (h *Handler) GetDesignImplementationContext(w http.ResponseWriter, r *http.Request) {
	contextValue, _, _, ok := h.resolveDesignImplementationRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, contextValue)
}

func (h *Handler) resolveDesignImplementationRequest(w http.ResponseWriter, r *http.Request) (DesignImplementationContextResponse, DesignImplementationRequest, db.ProjectResource, bool) {
	workspaceID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	var request DesignImplementationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "implementation context request is invalid; select the design and repository again")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "implementation context request must contain one JSON object")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	now := time.Now()
	claim, err := parseDesignAssetRef(chi.URLParam(r, "designRef"), now)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "design_ref_invalid", "design reference is invalid or expired; select the design again")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	task, taskIdentity, taskOK := h.designImplementationTaskForIssue(r, request.IssueID, uuidToString(workspaceID))
	if claim.WorkspaceID != uuidToString(workspaceID) ||
		(r.Header.Get("X-Actor-Source") == "task_token" && (!taskOK || !designImplementationRequestMatchesTaskIdentity(chi.URLParam(r, "designRef"), claim, request, taskIdentity))) ||
		(r.Header.Get("X-Actor-Source") != "task_token" && claim.UserID != uuidToString(requesterID)) {
		writeProjectDesignSystemError(w, http.StatusForbidden, "forbidden", "design reference is not available to this user, workspace, or agent task")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	if request.RevisionID != claim.RevisionID {
		writeProjectDesignSystemError(w, http.StatusConflict, "revision_not_restorable", "requested revision does not match the frozen design reference")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	issue, ok := h.loadIssueForUser(w, r, strings.TrimSpace(request.IssueID))
	if !ok {
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	if !issue.ProjectID.Valid || uuidToString(issue.ProjectID) != claim.ProjectID {
		writeProjectDesignSystemError(w, http.StatusConflict, "project_mismatch", "issue and design must belong to the same project")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	repositoryID, err := parseDesignAssetClaimUUID(strings.TrimSpace(request.ProjectResourceID))
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "repository_not_found", "target repository was not found; select it again")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	repository, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{ID: repositoryID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusNotFound, "repository_not_found", "target repository was not found; select it again")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	if err != nil {
		writeDesignAssetResolveError(w, err)
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	if repository.ResourceType != projectResourceTypeGitHubRepo || uuidToString(repository.ProjectID) != claim.ProjectID {
		writeProjectDesignSystemError(w, http.StatusConflict, "project_mismatch", "target repository and design must belong to the same project")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	frames, err := h.resolveDesignImplementationFrames(r, claim)
	if err != nil {
		writeDesignAssetResolveError(w, err)
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	if err := validateDesignImplementationFrameRefs(claim, request.FrameRefs, frames); err != nil {
		writeDesignAssetResolveError(w, err)
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	title, designSystemDigest, sourceDocumentID, capabilities, err := h.designImplementationMetadata(r, claim)
	if err != nil {
		writeDesignAssetResolveError(w, err)
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	packageDescriptor := designImplementationPackageDescriptor(claim, sourceDocumentID, request.FrameRefs, frames)
	if packageDescriptor == nil {
		writeProjectDesignSystemError(w, http.StatusConflict, "implementation_scope_invalid", "selected pages cannot be materialized as one implementation package")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	taskID := ""
	if taskOK {
		taskID = uuidToString(task.ID)
	}
	implementationRef, err := designimplementation.MintReference(designimplementation.ReferenceClaim{
		WorkspaceID: uuidToString(workspaceID), ProjectID: claim.ProjectID, IssueID: request.IssueID,
		TaskID:            taskID,
		ProjectResourceID: request.ProjectResourceID, DesignRef: chi.URLParam(r, "designRef"),
		RevisionID: claim.RevisionID, ContentDigest: claim.ContentDigest, FrameRefs: append([]string(nil), request.FrameRefs...),
	}, now)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "implementation_reference_failed", "failed to bind the implementation context")
		return DesignImplementationContextResponse{}, DesignImplementationRequest{}, db.ProjectResource{}, false
	}
	return DesignImplementationContextResponse{
		SchemaVersion: designImplementationContextSchemaV1, ImplementationRef: implementationRef, DesignRef: chi.URLParam(r, "designRef"),
		RevisionID: claim.RevisionID, ContentDigest: claim.ContentDigest, FrameRefs: append([]string(nil), request.FrameRefs...),
		TaskID:    taskID,
		ProjectID: claim.ProjectID, IssueID: request.IssueID, ProjectResourceID: request.ProjectResourceID,
		DesignTitle: title, DesignSystemDigest: designSystemDigest, Package: packageDescriptor, AllowedWritePaths: []string{"."},
		VerificationRequirements: []string{"repository typecheck/tests/build as applicable", "real rendered preview for changed UI"},
		SourceInstructions:       designImplementationSourceInstructions(claim.Kind),
		VerificationTargets:      designImplementationVerificationTargets(claim.Kind),
		Paths: DesignImplementationPaths{
			Context:           ".agent_context/design_implementation/context.json",
			DesignManifest:    ".agent_context/design_implementation/design/package/manifest.json",
			DesignPackage:     ".agent_context/design_implementation/design/package",
			Scope:             ".agent_context/design_implementation/design/scope.json",
			RepositoryContext: ".agent_context/design_implementation/repository",
			Result:            ".agent_context/design_implementation/result/implementation-result.json",
		},
		SourceCapabilities: capabilities,
	}, request, repository, true
}

func (h *Handler) designImplementationTaskForIssue(r *http.Request, issueID, workspaceID string) (db.AgentTaskQueue, designimplementation.TaskIdentity, bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		return db.AgentTaskQueue{}, designimplementation.TaskIdentity{}, false
	}
	task, ok := h.taskFromRequestHeader(r)
	if !ok || !task.IssueID.Valid {
		return db.AgentTaskQueue{}, designimplementation.TaskIdentity{}, false
	}
	parsedIssueID, err := parseDesignAssetClaimUUID(strings.TrimSpace(issueID))
	if err != nil || task.IssueID != parsedIssueID {
		return db.AgentTaskQueue{}, designimplementation.TaskIdentity{}, false
	}
	identity, ok := h.designImplementationIdentityForTask(r.Context(), task, workspaceID)
	return task, identity, ok
}

func (h *Handler) designImplementationIdentityForTask(ctx context.Context, task db.AgentTaskQueue, workspaceID string) (designimplementation.TaskIdentity, bool) {
	if !task.TriggerCommentID.Valid {
		return designimplementation.TaskIdentity{}, false
	}
	workspaceUUID, err := parseDesignAssetClaimUUID(workspaceID)
	if err != nil {
		return designimplementation.TaskIdentity{}, false
	}
	comment, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: task.TriggerCommentID, WorkspaceID: workspaceUUID,
	})
	if err != nil || comment.IssueID != task.IssueID || !designimplementation.IsTask(commentDesignDeliveryContent(comment)) {
		return designimplementation.TaskIdentity{}, false
	}
	return designimplementation.ParseTaskIdentity(commentDesignDeliveryContent(comment))
}

func designImplementationRequestMatchesTaskIdentity(designRef string, claim designAssetRefClaim, request DesignImplementationRequest, identity designimplementation.TaskIdentity) bool {
	return identity.AssetID == claim.AssetID && identity.DesignRef == designRef &&
		identity.RevisionID == claim.RevisionID && identity.ContentDigest == claim.ContentDigest &&
		identity.ProjectResourceID == request.ProjectResourceID &&
		sameStringSlices(request.FrameRefs, identity.SelectedFrameRefs())
}

func sameStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func designImplementationPackageDescriptor(claim designAssetRefClaim, documentID string, frameRefs []string, availableFrames []DesignAssetFrameResponse) *DesignImplementationPackageDescriptor {
	switch claim.Kind {
	case "multica":
		if documentID == "" || claim.RevisionID == "" || claim.ContentDigest == "" {
			return nil
		}
		return &DesignImplementationPackageDescriptor{Source: "multica", ArchivePath: fmt.Sprintf("/api/design-documents/%s/revisions/%s/archive", documentID, claim.RevisionID), ContentDigest: claim.ContentDigest}
	case "figma":
		if len(frameRefs) != 1 || claim.AssetID == "" || claim.RevisionID == "" || claim.ContentDigest == "" {
			return nil
		}
		selection, err := parseDesignAssetFrameRef(frameRefs[0], time.Now())
		if err != nil || (selection.SelectionKind != "frame" && selection.SelectionKind != "figma_group") || selection.SelectionID == "" {
			return nil
		}
		scope := map[string]any{"version": "1.0", "kind": selection.SelectionKind, "designFileId": claim.AssetID, "revisionId": claim.RevisionID}
		if selection.SelectionKind == "frame" {
			scope["frameId"] = selection.SelectionID
		} else {
			var frameIDs []string
			for _, frame := range availableFrames {
				available, err := parseDesignAssetFrameRef(frame.FrameRef, time.Now())
				if err == nil && available.SelectionKind == "figma_group" && available.SelectionID == selection.SelectionID {
					frameIDs = append([]string(nil), frame.RestorePackGroupFrameIDs...)
					break
				}
			}
			if !uniqueDesignImplementationFrameIDs(frameIDs) {
				return nil
			}
			scope["groupId"] = selection.SelectionID
			scope["frameIds"] = frameIDs
			scope["frameCount"] = len(frameIDs)
		}
		return &DesignImplementationPackageDescriptor{Source: "figma", ContentDigest: claim.ContentDigest, RestorePackScope: scope}
	default:
		return nil
	}
}

func uniqueDesignImplementationFrameIDs(ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func (h *Handler) resolveDesignImplementationFrames(r *http.Request, claim designAssetRefClaim) ([]DesignAssetFrameResponse, error) {
	switch claim.Kind {
	case "figma":
		return h.resolveFigmaDesignAssetFrames(r, claim)
	case "multica":
		return h.resolveMulticaDesignAssetFrames(r, claim)
	default:
		return nil, designAssetResolveFailure(http.StatusBadRequest, "design_ref_invalid", "design reference is invalid; select the design again")
	}
}

func validateDesignImplementationFrameRefs(design designAssetRefClaim, refs []string, available []DesignAssetFrameResponse) error {
	if len(refs) == 0 || (design.Kind == "figma" && len(refs) != 1) {
		return designAssetResolveFailure(http.StatusBadRequest, "frame_ref_invalid", "select pages from one saved design; Figma accepts one exact frame or group")
	}
	availableSelections := make(map[string]struct{}, len(available))
	for _, frame := range available {
		claim, err := parseDesignAssetFrameRef(frame.FrameRef, time.Now())
		if err == nil {
			availableSelections[claim.SelectionKind+"\x00"+claim.SelectionID] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(refs))
	for _, raw := range refs {
		if raw == "" {
			return designAssetResolveFailure(http.StatusBadRequest, "frame_ref_invalid", "selected frame reference is invalid; select it again")
		}
		if _, duplicate := seen[raw]; duplicate {
			return designAssetResolveFailure(http.StatusBadRequest, "frame_ref_invalid", "selected frames must be unique")
		}
		seen[raw] = struct{}{}
		frame, err := parseDesignAssetFrameRef(raw, time.Now())
		if err != nil || frame.WorkspaceID != design.WorkspaceID || frame.ProjectID != design.ProjectID || frame.UserID != design.UserID ||
			frame.AssetID != design.AssetID || frame.RevisionID != design.RevisionID || frame.ContentDigest != design.ContentDigest {
			return designAssetResolveFailure(http.StatusBadRequest, "frame_ref_invalid", "selected frame does not belong to the frozen design revision")
		}
		if _, ok := availableSelections[frame.SelectionKind+"\x00"+frame.SelectionID]; !ok {
			return designAssetResolveFailure(http.StatusBadRequest, "frame_ref_invalid", "selected frame is no longer available in this revision")
		}
	}
	return nil
}

func (h *Handler) designImplementationMetadata(r *http.Request, claim designAssetRefClaim) (string, string, string, DesignImplementationSourceCapabilities, error) {
	workspaceID, err := parseDesignAssetClaimUUID(claim.WorkspaceID)
	if err != nil {
		return "", "", "", DesignImplementationSourceCapabilities{}, err
	}
	assetID, err := parseDesignAssetClaimUUID(claim.AssetID)
	if err != nil {
		return "", "", "", DesignImplementationSourceCapabilities{}, err
	}
	switch claim.Kind {
	case "figma":
		file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: assetID, WorkspaceID: workspaceID})
		return file.Title, "", "", DesignImplementationSourceCapabilities{HasLayers: true, HasAssets: true, HasInteractions: true}, err
	case "multica":
		document, err := h.Queries.GetDesignDocumentInWorkspace(r.Context(), db.GetDesignDocumentInWorkspaceParams{ID: assetID, WorkspaceID: workspaceID})
		if err != nil {
			return "", "", "", DesignImplementationSourceCapabilities{}, err
		}
		revisionID, err := parseDesignAssetClaimUUID(claim.RevisionID)
		if err != nil {
			return "", "", "", DesignImplementationSourceCapabilities{}, err
		}
		revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: workspaceID})
		return document.Title, textToString(revision.DesignSystemDigest), uuidToString(document.ID), DesignImplementationSourceCapabilities{HasPrototype: true, HasAssets: true, HasInteractions: true}, err
	default:
		return "", "", "", DesignImplementationSourceCapabilities{}, designAssetResolveFailure(http.StatusBadRequest, "design_ref_invalid", "design reference is invalid")
	}
}

func designImplementationSourceInstructions(kind string) []string {
	switch kind {
	case "multica":
		return []string{"Treat the Prototype as a structure, state, and interaction specification.", "Do not copy the Prototype HTML/CSS wholesale or embed it with an iframe or dangerouslySetInnerHTML.", "Implement selected page states, dialogs, and interactions in the target repository stack."}
	case "figma":
		return []string{"Treat the frozen Figma Restore Pack as selected visual, layout, text, asset, and interaction evidence.", "Reuse target repository components and do not infer unrelated frames or groups.", "Implement the selected frame or group in the target repository stack."}
	default:
		return nil
	}
}

func designImplementationVerificationTargets(kind string) []string {
	switch kind {
	case "multica":
		return []string{"verify selected page coverage against brief.json and coverage.json", "report a package gap when required coverage has no Prototype evidence"}
	case "figma":
		return []string{"verify selected Figma frame or group evidence against figma-restore-pack.json", "report missing selected-layer or asset evidence as a Restore Pack gap"}
	default:
		return nil
	}
}

func designImplementationRepositoryName(repository db.ProjectResource) string {
	if repository.Label.Valid && strings.TrimSpace(repository.Label.String) != "" {
		return repository.Label.String
	}
	return uuidToString(repository.ID)
}

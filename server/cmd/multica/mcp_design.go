package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designimplementation"
)

type designMCPAdapter struct {
	client  *cli.APIClient
	rootDir string
}

func designMCPToolDescriptors() []map[string]any {
	return []map[string]any{
		designMCPToolDescriptor("multica_design_get_restore_pack", "Get a server-built Restore Pack for a design restore scope."),
		designMCPToolDescriptor("multica_design_list_files", "List design files available in the configured workspace."),
		designMCPToolDescriptor("multica_design_list_frames", "List frames for one design file revision."),
		designMCPToolDescriptor("multica_design_list_groups", "List Figma groups for one design file revision."),
		designMCPToolDescriptor("multica_design_get_selection_context", "Get selected layer or bounds context for one design frame."),
		designMCPToolDescriptor("multica_design_get_ui_restore_artifact", "Return UI restore artifact path metadata for frontend implementation."),
		designMCPToolDescriptor("multica_design_get_implementation_context", "Materialize the frozen design implementation context using bounded repository-relative paths."),
		designMCPToolDescriptor("multica_design_validate_implementation_result", "Validate the ordinary Agent's implementation-result/v1 against the frozen implementation context."),
	}
}

func designMCPToolDescriptor(name string, description string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		},
	}
}

func (a *designMCPAdapter) callTool(ctx context.Context, name string, arguments map[string]any) (any, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("design MCP adapter is not configured")
	}
	switch name {
	case "multica_design_get_restore_pack":
		return a.getRestorePack(ctx, arguments)
	case "multica_design_list_files":
		return a.listFiles(ctx)
	case "multica_design_list_frames":
		return a.listFrames(ctx, arguments)
	case "multica_design_list_groups":
		return a.listGroups(ctx, arguments)
	case "multica_design_get_selection_context":
		return a.getSelectionContext(ctx, arguments)
	case "multica_design_get_ui_restore_artifact":
		return a.getUIRestoreArtifact(arguments), nil
	case "multica_design_get_implementation_context":
		return a.getImplementationContext(ctx, arguments)
	case "multica_design_validate_implementation_result":
		return a.validateImplementationResult()
	default:
		return nil, fmt.Errorf("unknown design MCP tool %q", name)
	}
}

func (a *designMCPAdapter) validateImplementationResult() (any, error) {
	root := a.rootDir
	if root == "" {
		root = "."
	}
	contextRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationContextPath)))
	if err != nil {
		return nil, fmt.Errorf("implementation_result_invalid: read implementation context: %w", err)
	}
	var contextValue designImplementationContextWire
	if err := json.Unmarshal(contextRaw, &contextValue); err != nil {
		return nil, fmt.Errorf("implementation_result_invalid: decode implementation context: %w", err)
	}
	resultRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationResultPath)))
	if err != nil {
		return nil, fmt.Errorf("implementation_result_invalid: read implementation result: %w", err)
	}
	result, err := designimplementation.ValidateJSONForContext(resultRaw, designimplementation.ExpectedIdentity{DesignRef: contextValue.DesignRef, RevisionID: contextValue.RevisionID, FrameRefs: contextValue.FrameRefs})
	if err != nil {
		return nil, err
	}
	return map[string]any{"schema_version": result.SchemaVersion, "status": result.Status, "result_path": designImplementationResultPath}, nil
}

const (
	designImplementationContextPath    = ".agent_context/design_implementation/context.json"
	designImplementationManifestPath   = ".agent_context/design_implementation/design/package/manifest.json"
	designImplementationPackagePath    = ".agent_context/design_implementation/design/package"
	designImplementationScopePath      = ".agent_context/design_implementation/design/scope.json"
	designImplementationRepositoryPath = ".agent_context/design_implementation/repository"
	designImplementationResultPath     = ".agent_context/design_implementation/result/implementation-result.json"
)

type designImplementationContextWire struct {
	SchemaVersion       string                                     `json:"schema_version"`
	ImplementationRef   string                                     `json:"implementation_ref"`
	DesignRef           string                                     `json:"design_ref"`
	RevisionID          string                                     `json:"revision_id"`
	ContentDigest       string                                     `json:"content_digest"`
	FrameRefs           []string                                   `json:"frame_refs"`
	ProjectID           string                                     `json:"project_id"`
	IssueID             string                                     `json:"issue_id"`
	TaskID              string                                     `json:"task_id"`
	ProjectResourceID   string                                     `json:"project_resource_id"`
	DesignTitle         string                                     `json:"design_title"`
	Package             *designImplementationPackageDescriptorWire `json:"package,omitempty"`
	SourceInstructions  []string                                   `json:"source_instructions,omitempty"`
	VerificationTargets []string                                   `json:"verification_targets,omitempty"`
	DesignSystemDigest  string                                     `json:"design_system_digest,omitempty"`
	AllowedWritePaths   []string                                   `json:"allowed_write_paths"`
	Verification        []string                                   `json:"verification_requirements"`
	Capabilities        map[string]any                             `json:"source_capabilities"`
}

type designImplementationPackageDescriptorWire struct {
	Source           string         `json:"source"`
	ArchivePath      string         `json:"archive_path"`
	ContentDigest    string         `json:"content_digest"`
	RestorePackScope map[string]any `json:"restore_pack_scope,omitempty"`
}

func (a *designMCPAdapter) getImplementationContext(ctx context.Context, arguments map[string]any) (any, error) {
	if bound, ok, err := a.taskBoundImplementationArguments(); err != nil {
		return nil, err
	} else if ok {
		arguments = bound
	}
	designRef := stringArgument(arguments, "designRef")
	revisionID := stringArgument(arguments, "revisionId")
	repositoryID := stringArgument(arguments, "targetRepositoryId")
	issueID := stringArgument(arguments, "issueId")
	frameRefs, err := stringSliceArgument(arguments, "frameRefs")
	if err != nil || designRef == "" || revisionID == "" || repositoryID == "" || issueID == "" || len(frameRefs) == 0 {
		return nil, fmt.Errorf("designRef, revisionId, frameRefs, targetRepositoryId, and issueId are required")
	}
	body := map[string]any{
		"revision_id": revisionID, "frame_refs": frameRefs,
		"project_resource_id": repositoryID, "issue_id": issueID,
	}
	var contextValue designImplementationContextWire
	endpoint := "/api/design-assets/" + url.PathEscape(designRef) + "/implementation-context"
	if err := a.client.PostJSON(ctx, endpoint, body, &contextValue); err != nil {
		return nil, mapDesignMCPAPIError(err)
	}
	if contextValue.SchemaVersion != "multica.design-implementation-context/v1" || contextValue.ImplementationRef == "" || contextValue.DesignRef != designRef ||
		contextValue.RevisionID != revisionID || contextValue.ProjectResourceID != repositoryID || contextValue.IssueID != issueID ||
		!equalStrings(contextValue.FrameRefs, frameRefs) {
		return nil, fmt.Errorf("design context response does not match the requested frozen task identity")
	}
	var packageFiles map[string][]byte
	if contextValue.Package != nil {
		switch contextValue.Package.Source {
		case "multica":
			if contextValue.Package.ArchivePath == "" || contextValue.Package.ContentDigest != contextValue.ContentDigest {
				return nil, fmt.Errorf("design_package_invalid: implementation package descriptor is invalid")
			}
			archive, err := a.client.DownloadFile(ctx, contextValue.Package.ArchivePath)
			if err != nil {
				return nil, fmt.Errorf("context_materialization_failed: download saved Multica design package: %w", err)
			}
			validated, files, err := designdocument.ReadBaseArchive(archive, contextValue.ContentDigest)
			if err != nil {
				return nil, fmt.Errorf("design_package_invalid: validate saved Multica design package: %w", err)
			}
			manifest, err := json.Marshal(validated.Manifest)
			if err != nil {
				return nil, fmt.Errorf("context_materialization_failed: encode saved Multica manifest: %w", err)
			}
			packageFiles = files
			packageFiles["manifest.json"] = append(manifest, '\n')
		case "figma":
			var err error
			packageFiles, err = a.figmaImplementationPackage(ctx, contextValue)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("design_package_invalid: unsupported implementation package source %q", contextValue.Package.Source)
		}
	} else if requiresPackageEvidence(contextValue.Capabilities) {
		return nil, fmt.Errorf("design_package_invalid: implementation context requires a package descriptor")
	}
	if err := materializeDesignImplementationContext(a.rootDir, contextValue, packageFiles); err != nil {
		return nil, fmt.Errorf("context_materialization_failed: %w", err)
	}
	return map[string]any{
		"schema_version":          contextValue.SchemaVersion,
		"context_path":            designImplementationContextPath,
		"design_manifest_path":    designImplementationManifestPath,
		"design_package_path":     designImplementationPackagePath,
		"scope_path":              designImplementationScopePath,
		"repository_context_path": designImplementationRepositoryPath,
		"result_path":             designImplementationResultPath,
		"source_capabilities":     contextValue.Capabilities,
	}, nil
}

func materializeDesignImplementationContext(rootDir string, contextValue designImplementationContextWire, packageFiles ...map[string][]byte) error {
	if rootDir == "" {
		rootDir = "."
	}
	repositoryRoot, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer repositoryRoot.Close()
	if err := ensureRootDirectory(repositoryRoot, ".agent_context"); err != nil {
		return err
	}
	agentRoot, err := repositoryRoot.OpenRoot(".agent_context")
	if err != nil {
		return err
	}
	defer agentRoot.Close()
	if err := requireRootDirectory(repositoryRoot, ".agent_context"); err != nil {
		return err
	}
	if err := rejectImplementationSymlinks(agentRoot); err != nil {
		return err
	}

	temporaryName := ".design_implementation-" + rand.Text()
	if err := agentRoot.Mkdir(temporaryName, 0o755); err != nil {
		return err
	}
	temporaryOwned := true
	defer func() {
		if temporaryOwned {
			_ = agentRoot.RemoveAll(temporaryName)
		}
	}()
	temporaryRoot, err := agentRoot.OpenRoot(temporaryName)
	if err != nil {
		return err
	}
	if err := requireRootDirectory(agentRoot, temporaryName); err != nil {
		_ = temporaryRoot.Close()
		return err
	}
	for _, directory := range []string{
		"design/package",
		"repository",
		"result",
	} {
		if err := temporaryRoot.MkdirAll(directory, 0o755); err != nil {
			_ = temporaryRoot.Close()
			return err
		}
	}
	projection := map[string]any{
		"schema_version": contextValue.SchemaVersion,
		"design_ref":     contextValue.DesignRef, "revision_id": contextValue.RevisionID,
		"content_digest": contextValue.ContentDigest, "title": contextValue.DesignTitle,
		"design_system_digest": contextValue.DesignSystemDigest, "source_capabilities": contextValue.Capabilities,
	}
	scope := map[string]any{
		"design_ref": contextValue.DesignRef, "revision_id": contextValue.RevisionID,
		"frame_refs": contextValue.FrameRefs, "project_id": contextValue.ProjectID,
		"issue_id": contextValue.IssueID, "task_id": contextValue.TaskID, "project_resource_id": contextValue.ProjectResourceID,
		"allowed_write_paths": contextValue.AllowedWritePaths, "verification_requirements": contextValue.Verification,
		"result_path": designImplementationResultPath, "result_schema": designimplementation.ResultSchemaV1,
	}
	for relative, value := range map[string]any{
		"context.json":                   contextValue,
		"design/context-projection.json": projection,
		"design/scope.json":              scope,
	} {
		if err := writeExclusiveRootJSON(temporaryRoot, relative, value); err != nil {
			_ = temporaryRoot.Close()
			return err
		}
	}
	if len(packageFiles) > 0 {
		for relative, contents := range packageFiles[0] {
			if !filepath.IsLocal(relative) {
				_ = temporaryRoot.Close()
				return fmt.Errorf("design package path %q is not local", relative)
			}
			if err := writeExclusiveRootFile(temporaryRoot, filepath.ToSlash(filepath.Join("design", "package", relative)), contents); err != nil {
				_ = temporaryRoot.Close()
				return err
			}
		}
	}
	if err := temporaryRoot.Close(); err != nil {
		return err
	}
	if err := replaceImplementationRoot(agentRoot, temporaryName, agentRoot.RemoveAll); err != nil {
		return err
	}
	temporaryOwned = false
	return nil
}

func (a *designMCPAdapter) figmaImplementationPackage(ctx context.Context, contextValue designImplementationContextWire) (map[string][]byte, error) {
	if contextValue.Package.ContentDigest != contextValue.ContentDigest || contextValue.Package.ArchivePath != "" {
		return nil, fmt.Errorf("design_package_invalid: implementation package descriptor is invalid")
	}
	scope, designFileID, err := validateFigmaRestorePackScope(contextValue.Package.RestorePackScope, contextValue.RevisionID)
	if err != nil {
		return nil, err
	}
	var pack map[string]any
	path := "/api/design-files/" + url.PathEscape(designFileID) + "/restore-pack"
	if err := a.client.PostJSON(ctx, path, map[string]any{"scope": scope}, &pack); err != nil {
		return nil, fmt.Errorf("context_materialization_failed: get frozen Figma Restore Pack: %w", mapDesignMCPAPIError(err))
	}
	if err := validateFigmaRestorePack(pack, scope, designFileID, contextValue.RevisionID, contextValue.ContentDigest, contextValue.Package.ContentDigest); err != nil {
		return nil, err
	}
	packJSON, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("context_materialization_failed: encode Figma Restore Pack: %w", err)
	}
	manifestJSON, err := json.MarshalIndent(map[string]any{
		"schema_version":    "multica.design-implementation-figma-package/v1",
		"source":            "figma",
		"content_digest":    contextValue.ContentDigest,
		"design_ref":        contextValue.DesignRef,
		"revision_id":       contextValue.RevisionID,
		"frame_refs":        contextValue.FrameRefs,
		"restore_pack_path": "figma-restore-pack.json",
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("context_materialization_failed: encode Figma package manifest: %w", err)
	}
	return map[string][]byte{
		"manifest.json":           append(manifestJSON, '\n'),
		"figma-restore-pack.json": append(packJSON, '\n'),
	}, nil
}

func validateFigmaRestorePackScope(scope map[string]any, revisionID string) (map[string]any, string, error) {
	if scope == nil || stringArgument(scope, "version") != "1.0" || stringArgument(scope, "revisionId") != revisionID {
		return nil, "", fmt.Errorf("design_package_invalid: Figma Restore Pack scope is invalid")
	}
	designFileID := stringArgument(scope, "designFileId")
	kind := stringArgument(scope, "kind")
	if designFileID == "" || (kind != "frame" && kind != "figma_group") {
		return nil, "", fmt.Errorf("design_package_invalid: Figma Restore Pack scope is invalid")
	}
	if kind == "frame" && stringArgument(scope, "frameId") == "" {
		return nil, "", fmt.Errorf("design_package_invalid: Figma Restore Pack scope is invalid")
	}
	if kind == "figma_group" {
		frameIDs, ok := uniqueStringArguments(scope, "frameIds")
		if stringArgument(scope, "groupId") == "" || !ok || integerArgument(scope, "frameCount") != len(frameIDs) {
			return nil, "", fmt.Errorf("design_package_invalid: Figma Restore Pack scope is invalid")
		}
	}
	return scope, designFileID, nil
}

func validateFigmaRestorePack(pack, scope map[string]any, designFileID, revisionID, contextDigest, descriptorDigest string) error {
	returnedScope := mapArgument(pack, "scope")
	if stringArgument(pack, "version") != "1.0" || stringArgument(mapArgument(pack, "designFile"), "id") != designFileID ||
		stringArgument(mapArgument(pack, "revision"), "id") != revisionID ||
		contextDigest == "" || descriptorDigest == "" || contextDigest != descriptorDigest || stringArgument(pack, "contentDigest") != contextDigest || stringArgument(pack, "contentDigest") != descriptorDigest ||
		stringArgument(returnedScope, "version") != "1.0" || stringArgument(returnedScope, "designFileId") != designFileID ||
		stringArgument(returnedScope, "revisionId") != revisionID || stringArgument(returnedScope, "kind") != stringArgument(scope, "kind") ||
		stringArgument(returnedScope, "frameId") != stringArgument(scope, "frameId") || stringArgument(returnedScope, "groupId") != stringArgument(scope, "groupId") {
		return fmt.Errorf("design_package_invalid: Figma Restore Pack does not match frozen implementation evidence")
	}
	frameIDs, err := validatedFigmaRestorePackFrameIDs(pack, designFileID, revisionID)
	if err != nil {
		return err
	}
	switch stringArgument(scope, "kind") {
	case "frame":
		if len(frameIDs) != 1 || frameIDs[0] != stringArgument(scope, "frameId") {
			return fmt.Errorf("design_package_invalid: Figma Restore Pack does not contain the selected frame")
		}
	case "figma_group":
		structure := mapArgument(pack, "designStructure")
		scopeFrameIDs, scopeOK := uniqueStringArguments(scope, "frameIds")
		structureFrameIDs, ok := uniqueStringArguments(structure, "frameIds")
		if !scopeOK || !ok || stringArgument(structure, "mode") != "figma_group" || stringArgument(structure, "groupId") != stringArgument(scope, "groupId") ||
			integerArgument(structure, "frameCount") != len(structureFrameIDs) || !sameStringArguments(scopeFrameIDs, structureFrameIDs) || !sameStringArguments(scopeFrameIDs, frameIDs) {
			return fmt.Errorf("design_package_invalid: Figma Restore Pack does not contain the selected group evidence")
		}
	default:
		return fmt.Errorf("design_package_invalid: Figma Restore Pack scope is invalid")
	}
	return nil
}

func validatedFigmaRestorePackFrameIDs(pack map[string]any, designFileID, revisionID string) ([]string, error) {
	frames, ok := pack["frames"].([]any)
	if !ok || len(frames) == 0 {
		return nil, fmt.Errorf("design_package_invalid: Figma Restore Pack has no selected frame evidence")
	}
	frameIDs := make([]string, 0, len(frames))
	seen := make(map[string]struct{}, len(frames))
	for _, rawFrame := range frames {
		frame, ok := rawFrame.(map[string]any)
		if !ok || stringArgument(frame, "designFileId") != designFileID || stringArgument(frame, "revisionId") != revisionID {
			return nil, fmt.Errorf("design_package_invalid: Figma Restore Pack frame evidence is invalid")
		}
		frameID := stringArgument(frame, "frameId")
		if frameID == "" {
			return nil, fmt.Errorf("design_package_invalid: Figma Restore Pack frame evidence is invalid")
		}
		if _, exists := seen[frameID]; exists {
			return nil, fmt.Errorf("design_package_invalid: Figma Restore Pack frame evidence is duplicated")
		}
		seen[frameID] = struct{}{}
		frameIDs = append(frameIDs, frameID)
	}
	return frameIDs, nil
}

func uniqueStringArguments(values map[string]any, key string) ([]string, bool) {
	raw, ok := values[key]
	if !ok {
		return nil, false
	}
	var items []any
	switch raw := raw.(type) {
	case []any:
		items = raw
	case []string:
		items = make([]any, len(raw))
		for i := range raw {
			items[i] = raw[i]
		}
	default:
		return nil, false
	}
	if len(items) == 0 {
		return nil, false
	}
	ids := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, rawID := range items {
		id, ok := rawID.(string)
		if !ok || id == "" {
			return nil, false
		}
		if _, exists := seen[id]; exists {
			return nil, false
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, true
}

func sameStringArguments(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func integerArgument(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case float64:
		if value == float64(int(value)) {
			return int(value)
		}
	case int:
		return value
	}
	return -1
}

func mapArgument(value map[string]any, key string) map[string]any {
	entry, _ := value[key].(map[string]any)
	return entry
}

func hasPrototype(capabilities map[string]any) bool {
	value, _ := capabilities["has_prototype"].(bool)
	return value
}

func requiresPackageEvidence(capabilities map[string]any) bool {
	return hasPrototype(capabilities) || hasLayers(capabilities)
}

func hasLayers(capabilities map[string]any) bool {
	value, _ := capabilities["has_layers"].(bool)
	return value
}

func writeExclusiveRootFile(root *os.Root, relative string, contents []byte) error {
	if err := root.MkdirAll(filepath.ToSlash(filepath.Dir(relative)), 0o755); err != nil {
		return err
	}
	file, err := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func ensureRootDirectory(root *os.Root, name string) error {
	err := requireRootDirectory(root, name)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := root.Mkdir(name, 0o755); err != nil {
		return err
	}
	return requireRootDirectory(root, name)
}

func requireRootDirectory(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s must be a non-symlink directory", name)
	}
	return nil
}

func rejectImplementationSymlinks(agentRoot *os.Root) error {
	if err := requireRootDirectory(agentRoot, "design_implementation"); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	implementationRoot, err := agentRoot.OpenRoot("design_implementation")
	if err != nil {
		return err
	}
	defer implementationRoot.Close()
	if err := requireRootDirectory(agentRoot, "design_implementation"); err != nil {
		return err
	}
	return fs.WalkDir(implementationRoot.FS(), ".", func(relative string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("design_implementation/%s must not be a symlink", relative)
		}
		return nil
	})
}

func writeExclusiveRootJSON(root *os.Root, relative string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	file, err := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func replaceImplementationRoot(agentRoot *os.Root, temporaryName string, removeAll func(string) error) error {
	if err := requireRootDirectory(agentRoot, "design_implementation"); errors.Is(err, fs.ErrNotExist) {
		return agentRoot.Rename(temporaryName, "design_implementation")
	} else if err != nil {
		return err
	}
	backupName := ".design_implementation-old-" + rand.Text()
	if err := agentRoot.Rename("design_implementation", backupName); err != nil {
		return err
	}
	if err := agentRoot.Rename(temporaryName, "design_implementation"); err != nil {
		restoreErr := agentRoot.Rename(backupName, "design_implementation")
		return errors.Join(err, restoreErr)
	}
	if err := removeAll(backupName); err != nil {
		cleanupErr := fmt.Errorf("remove previous implementation context: %w", err)
		failedNewName := ".design_implementation-failed-" + rand.Text()
		if moveErr := agentRoot.Rename("design_implementation", failedNewName); moveErr != nil {
			return errors.Join(cleanupErr, fmt.Errorf("move activated implementation context for rollback: %w", moveErr))
		}
		if restoreErr := agentRoot.Rename(backupName, "design_implementation"); restoreErr != nil {
			reinstateErr := agentRoot.Rename(failedNewName, "design_implementation")
			return errors.Join(
				cleanupErr,
				fmt.Errorf("restore previous implementation context: %w", restoreErr),
				wrapOptionalError("reinstate activated implementation context", reinstateErr),
			)
		}
		removeFailedNewErr := removeAll(failedNewName)
		return errors.Join(cleanupErr, wrapOptionalError("remove rolled-back implementation context", removeFailedNewErr))
	}
	return nil
}

func wrapOptionalError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func stringSliceArgument(arguments map[string]any, key string) ([]string, error) {
	raw, ok := arguments[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...), nil
	case []any:
		out := make([]string, len(values))
		for i, value := range values {
			var ok bool
			out[i], ok = value.(string)
			if !ok || out[i] == "" {
				return nil, fmt.Errorf("%s must contain non-empty strings", key)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be an array", key)
	}
}

func (a *designMCPAdapter) taskBoundImplementationArguments() (map[string]any, bool, error) {
	root := a.rootDir
	if root == "" {
		root = "."
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(execenv.TaskContextMarkerRelPath)))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read task context marker: %w", err)
	}
	var marker struct {
		ManagedBy            string                             `json:"managed_by"`
		TaskID               string                             `json:"task_id"`
		IssueID              string                             `json:"issue_id"`
		DesignImplementation *designimplementation.TaskIdentity `json:"design_implementation"`
	}
	if json.Unmarshal(raw, &marker) != nil || marker.ManagedBy != execenv.TaskContextMarkerManagedBy {
		return nil, false, errors.New("task context marker is invalid")
	}
	identity := marker.DesignImplementation
	if identity == nil {
		return nil, false, nil
	}
	frameRefs := identity.SelectedFrameRefs()
	if marker.TaskID == "" || marker.TaskID != os.Getenv("MULTICA_TASK_ID") || marker.IssueID == "" ||
		identity.DesignRef == "" || identity.RevisionID == "" || !uniqueStringSlice(frameRefs) || identity.ProjectResourceID == "" {
		return nil, false, errors.New("task-bound design implementation identity is invalid")
	}
	return map[string]any{
		"designRef": identity.DesignRef, "revisionId": identity.RevisionID,
		"frameRefs": frameRefs, "targetRepositoryId": identity.ProjectResourceID,
		"issueId": marker.IssueID,
	}, true, nil
}

func uniqueStringSlice(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return len(values) > 0
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (a *designMCPAdapter) getRestorePack(ctx context.Context, arguments map[string]any) (any, error) {
	scope, ok := arguments["scope"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("scope is required")
	}
	designFileID := stringArgument(scope, "designFileId")
	if designFileID == "" {
		return nil, fmt.Errorf("scope.designFileId is required")
	}
	body := map[string]any{"scope": scope}
	if detailLevel := stringArgument(arguments, "detailLevel"); detailLevel != "" {
		body["detailLevel"] = detailLevel
	}
	var out map[string]any
	path := "/api/design-files/" + url.PathEscape(designFileID) + "/restore-pack"
	if err := a.client.PostJSON(ctx, path, body, &out); err != nil {
		return nil, mapDesignMCPAPIError(err)
	}
	return out, nil
}

func (a *designMCPAdapter) listFiles(ctx context.Context) (any, error) {
	var out map[string]any
	if err := a.client.GetJSON(ctx, "/api/design-files", &out); err != nil {
		return nil, mapDesignMCPAPIError(err)
	}
	return out, nil
}

func (a *designMCPAdapter) listFrames(ctx context.Context, arguments map[string]any) (any, error) {
	designFileID := stringArgument(arguments, "designFileId")
	if designFileID == "" {
		return nil, fmt.Errorf("designFileId is required")
	}
	path := "/api/design-files/" + url.PathEscape(designFileID) + "/context"
	if revisionID := stringArgument(arguments, "revisionId"); revisionID != "" {
		path += "?revision_id=" + url.QueryEscape(revisionID)
	}
	var out map[string]any
	if err := a.client.GetJSON(ctx, path, &out); err != nil {
		return nil, mapDesignMCPAPIError(err)
	}
	return map[string]any{
		"designFileId": out["designFileId"],
		"revisionId":   out["revisionId"],
		"frames":       out["frames"],
	}, nil
}

func (a *designMCPAdapter) listGroups(ctx context.Context, arguments map[string]any) (any, error) {
	frames, err := a.listFrames(ctx, arguments)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"groups":  []any{},
		"frames":  frames,
		"message": "Group scope is available from Design Center copied scope JSON. Use multica_design_get_restore_pack with kind=figma_group when you have groupId/frameIds.",
	}, nil
}

func (a *designMCPAdapter) getSelectionContext(ctx context.Context, arguments map[string]any) (any, error) {
	designFileID := stringArgument(arguments, "designFileId")
	frameID := stringArgument(arguments, "frameId")
	if designFileID == "" {
		return nil, fmt.Errorf("designFileId is required")
	}
	if frameID == "" {
		return nil, fmt.Errorf("frameId is required")
	}
	body := map[string]any{}
	for _, key := range []string{"layerIds", "selectionBounds", "includeIntersectingLayers"} {
		if value, ok := arguments[key]; ok {
			body[key] = value
		}
	}
	path := "/api/design-files/" + url.PathEscape(designFileID) + "/frames/" + url.PathEscape(frameID) + "/selection-context"
	if revisionID := stringArgument(arguments, "revisionId"); revisionID != "" {
		path += "?revision_id=" + url.QueryEscape(revisionID)
	}
	var out map[string]any
	if err := a.client.PostJSON(ctx, path, body, &out); err != nil {
		return nil, mapDesignMCPAPIError(err)
	}
	return out, nil
}

func (a *designMCPAdapter) getUIRestoreArtifact(arguments map[string]any) any {
	artifactDocPath := stringArgument(arguments, "artifactDocPath")
	projectLocalPath := stringArgument(arguments, "projectLocalPath")
	return map[string]any{
		"artifactDocPath":  artifactDocPath,
		"projectLocalPath": projectLocalPath,
		"message":          "Read artifactDocPath from the local target repository and use it as the UI restore handoff document.",
	}
}

func stringArgument(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func mapDesignMCPAPIError(err error) error {
	var httpErr *cli.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == 401 {
		return fmt.Errorf("not_authenticated: run 'multica login'")
	}
	return err
}

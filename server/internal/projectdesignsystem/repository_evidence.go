package projectdesignsystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Repository evidence selection is adapted from Open Design v0.19.2
// (apps/daemon/src/tools-connectors-cli.ts), Apache-2.0. Product-specific
// Cherry/chat path boosts are intentionally omitted: every repository uses the
// same generic rules and the selected Agent makes the design decisions.
const (
	RepositoryEvidenceSchemaVersion = "multica.project-design-system-repository-evidence/v1"
	DefaultRepositoryEvidenceFiles  = 48
	MaxRepositoryEvidenceFiles      = 80
	maxRepositoryEvidenceFileBytes  = 120_000
	maxRepositoryEvidenceAssetBytes = 1_500_000
	maxRepositoryEvidenceTotalBytes = 12 << 20
	maxRepositoryEvidenceTreeFiles  = 200_000
)

type RepositoryEvidenceInput struct {
	RepositoryName      string `json:"repository_name"`
	RepositoryURL       string `json:"repository_url,omitempty"`
	RequestedRef        string `json:"requested_ref,omitempty"`
	ResolvedRef         string `json:"resolved_ref"`
	CommitSHA           string `json:"commit_sha"`
	CheckoutPath        string `json:"checkout_path"`
	InputSnapshotSHA256 string `json:"input_snapshot_sha256"`
	MaxFiles            int    `json:"-"`
}

type RepositoryEvidenceFile struct {
	Path         string `json:"path"`
	Kind         string `json:"kind"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	SnapshotPath string `json:"snapshot_path"`
}

type RepositoryEvidenceIndex struct {
	SchemaVersion       string                   `json:"schema_version"`
	RepositoryName      string                   `json:"repository_name"`
	RepositoryURL       string                   `json:"repository_url,omitempty"`
	RequestedRef        string                   `json:"requested_ref,omitempty"`
	ResolvedRef         string                   `json:"resolved_ref"`
	CommitSHA           string                   `json:"commit_sha"`
	CheckoutPath        string                   `json:"checkout_path"`
	InputSnapshotSHA256 string                   `json:"input_snapshot_sha256"`
	TreeFileCount       int                      `json:"tree_file_count"`
	SelectedFileCount   int                      `json:"selected_file_count"`
	SelectedBytes       int64                    `json:"selected_bytes"`
	Files               []RepositoryEvidenceFile `json:"files"`
	Buckets             map[string][]string      `json:"buckets"`
	Warnings            []string                 `json:"warnings"`
}

type RepositoryEvidenceBundle struct {
	Index RepositoryEvidenceIndex
	Files map[string][]byte
}

type repositoryEvidenceCandidate struct {
	path   string
	kind   string
	score  int
	size   int64
	binary bool
}

var (
	repositoryEvidenceNativeTokenPattern   = regexp.MustCompile(`(?i)(^|/)(color|colors|colour|colours|theme|themes|palette|palettes|typography|type|fonts?|spacing|sizing|metrics|dimens|tokens?|designsystem|design-system|design|styles?|styling|appearance|brand|branding)[a-z0-9_-]*\.(swift|kt|kts|java|dart|scala|cs|m|mm)$`)
	repositoryEvidenceReadmePattern        = regexp.MustCompile(`(?i)(^|/)readme\.(md|mdx|txt|rst)$`)
	repositoryEvidenceEditorDirPattern     = regexp.MustCompile(`(^|/)\.(vscode|zed|idea|fleet|zenflow|github|husky|gradle|vs|turbo|cache|devcontainer)/`)
	repositoryEvidenceGeneratedDirPattern  = regexp.MustCompile(`(^|/)(node_modules|vendor|dist|build|coverage|\.next|\.nuxt|\.git|out|target|storybook-static)/`)
	repositoryEvidenceLockPattern          = regexp.MustCompile(`(^|/)(package-lock\.json|pnpm-lock\.ya?ml|yarn\.lock|bun\.lockb)$`)
	repositoryEvidenceTestDirPattern       = regexp.MustCompile(`(^|/)(__tests__|__snapshots__|test|tests)/`)
	repositoryEvidenceTestFilePattern      = regexp.MustCompile(`\.(test|spec|bench)\.(tsx|ts|jsx|js)$`)
	repositoryEvidenceArchivePattern       = regexp.MustCompile(`\.(gif|avif|mp4|mov|zip|tar|gz|pdf)$`)
	repositoryEvidenceNativeSourcePattern  = regexp.MustCompile(`\.(swift|kt|kts|java|scala|go|rs|rb|py|php|cs|dart|vue|svelte|astro|ex|exs|elm|c|cc|cpp|cxx|h|hpp|hh|m|mm)$`)
	repositoryEvidenceManifestPattern      = regexp.MustCompile(`(^|/)(package\.json|pubspec\.yaml|build\.gradle(\.kts)?|pom\.xml|cargo\.toml|go\.mod)$`)
	repositoryEvidenceTokenFilePattern     = regexp.MustCompile(`(^|/)(tailwind|theme|themes?|themeprovider|tokens?|colors?|typography|design-system|design|constant|constants|style|styles)\.(config\.)?(ts|tsx|js|jsx|json|css|scss|less|md)$`)
	repositoryEvidenceGlobalStylePattern   = regexp.MustCompile(`(^|/)(globals?|index|style|styles|app|root)\.(css|scss|less)$`)
	repositoryEvidenceFontPattern          = regexp.MustCompile(`(^|/)(fonts?|assets?/fonts?|public/fonts?|resources/fonts?)/.*\.(ttf|otf|woff2?|css)$`)
	repositoryEvidenceContextDirPattern    = regexp.MustCompile(`/(context|providers?|theme|styles?|config|utils?)/`)
	repositoryEvidenceLayoutDirPattern     = regexp.MustCompile(`/(app|layout|layouts|shell|navigation|navbar|sidebar|routes?|screens?|pages?)/`)
	repositoryEvidenceComponentDirPattern  = regexp.MustCompile(`/(components?|ui|design-system|primitives?)/`)
	repositoryEvidenceComponentFilePattern = regexp.MustCompile(`(button|card|dialog|modal|input|form|nav|navbar|sidebar|table|badge|avatar|toast|menu|tabs|layout|shell|composer|message)\.(tsx|ts|jsx|js|css|scss|vue|svelte)$`)
	repositoryEvidenceEntryPattern         = regexp.MustCompile(`(^|/)(app|pages|src)/(layout|page|app|index|main)\.(tsx|ts|jsx|js|css|vue|svelte)$`)
	repositoryEvidenceTextPattern          = regexp.MustCompile(`\.(css|scss|less|tsx|ts|jsx|js|mjs|cjs|md|mdx|json|jsonc|svg|txt|rst|yaml|yml|toml|xml|swift|kt|kts|java|scala|go|rs|rb|py|php|cs|dart|vue|svelte|astro|ex|exs|elm|c|cc|cpp|cxx|h|hpp|hh|m|mm)$`)
	repositoryEvidenceFoundationPattern    = regexp.MustCompile(`(token|theme|color|palette|typography|font|spacing|radius|metric|dimen|style)`)
	repositoryEvidencePageDirPattern       = regexp.MustCompile(`/(pages?|screens?|routes?)/`)
	repositoryEvidenceShellDirPattern      = regexp.MustCompile(`/(layout|layouts|shell|navigation|navbar|sidebar)/`)
	repositoryEvidenceContextFilePattern   = regexp.MustCompile(`(^|/)(readme|package\.json|pubspec\.yaml|build\.gradle|pom\.xml|cargo\.toml|go\.mod)`)
	repositoryEvidenceAssetPattern         = regexp.MustCompile(`(^|/)(assets?|public|resources|build|fonts?)/.*(logo|icon|avatar|tray|brand|wordmark|mark|font)[^/]*\.(svg|png|jpe?g|webp|ico|ttf|otf|woff2?)$`)
	repositoryEvidenceBinaryPattern        = regexp.MustCompile(`\.(png|jpe?g|webp|ico|ttf|otf|woff2?)$`)
)

func CollectRepositoryEvidence(ctx context.Context, root string, input RepositoryEvidenceInput) (RepositoryEvidenceBundle, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return RepositoryEvidenceBundle{}, errors.New("repository evidence root is required")
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return RepositoryEvidenceBundle{}, errors.New("repository evidence root is not a safe directory")
	}
	maxFiles := input.MaxFiles
	if maxFiles == 0 {
		maxFiles = DefaultRepositoryEvidenceFiles
	}
	if maxFiles < 1 || maxFiles > MaxRepositoryEvidenceFiles {
		return RepositoryEvidenceBundle{}, fmt.Errorf("repository evidence max files must be between 1 and %d", MaxRepositoryEvidenceFiles)
	}
	if strings.TrimSpace(input.CommitSHA) == "" || strings.TrimSpace(input.ResolvedRef) == "" || strings.TrimSpace(input.CheckoutPath) == "" || strings.TrimSpace(input.InputSnapshotSHA256) == "" {
		return RepositoryEvidenceBundle{}, errors.New("repository evidence identity is incomplete")
	}

	paths := make([]string, 0, 4096)
	candidates := make([]repositoryEvidenceCandidate, 0, 256)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}
		normalized := strings.ToLower(relative)
		if entry.IsDir() {
			if shouldSkipRepositoryEvidencePath(normalized + "/") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || shouldSkipRepositoryEvidencePath(normalized) {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil || !fileInfo.Mode().IsRegular() {
			return nil
		}
		paths = append(paths, relative)
		if len(paths) > maxRepositoryEvidenceTreeFiles {
			return fmt.Errorf("repository evidence tree exceeds %d files", maxRepositoryEvidenceTreeFiles)
		}
		score := scoreRepositoryEvidenceFile(relative)
		if score <= 0 {
			return nil
		}
		binary := isRepositoryEvidenceBinaryAsset(normalized)
		limit := int64(maxRepositoryEvidenceFileBytes)
		if binary {
			limit = maxRepositoryEvidenceAssetBytes
		}
		if fileInfo.Size() <= 0 || fileInfo.Size() > limit {
			return nil
		}
		candidates = append(candidates, repositoryEvidenceCandidate{
			path: relative, kind: repositoryEvidenceKind(relative), score: score, size: fileInfo.Size(), binary: binary,
		})
		return nil
	})
	if err != nil {
		return RepositoryEvidenceBundle{}, fmt.Errorf("inventory repository evidence: %w", err)
	}
	sort.Strings(paths)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].path < candidates[j].path
	})
	candidates = preferRepositoryReadme(candidates, maxFiles)

	files := make(map[string][]byte, len(candidates)+3)
	selected := make([]RepositoryEvidenceFile, 0, len(candidates))
	buckets := map[string][]string{}
	warnings := make([]string, 0)
	var selectedBytes int64
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return RepositoryEvidenceBundle{}, err
		}
		if selectedBytes+candidate.size > maxRepositoryEvidenceTotalBytes {
			warnings = append(warnings, "Selected evidence reached the bounded byte limit; remaining ranked files stay available in the complete checkout and tree index.")
			break
		}
		absolute := filepath.Join(root, filepath.FromSlash(candidate.path))
		fileInfo, err := os.Lstat(absolute)
		if err != nil || fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() || fileInfo.Size() != candidate.size {
			continue
		}
		content, err := os.ReadFile(absolute)
		if err != nil {
			continue
		}
		snapshotPath := "files/" + candidate.path
		files[snapshotPath] = content
		selectedBytes += int64(len(content))
		selected = append(selected, RepositoryEvidenceFile{
			Path: candidate.path, Kind: candidate.kind, Bytes: int64(len(content)), SHA256: repositoryEvidenceSHA256(content), SnapshotPath: snapshotPath,
		})
		buckets[candidate.kind] = append(buckets[candidate.kind], candidate.path)
	}
	for _, values := range buckets {
		sort.Strings(values)
	}
	if len(selected) == 0 {
		return RepositoryEvidenceBundle{}, errors.New("repository contains no bounded design evidence")
	}

	index := RepositoryEvidenceIndex{
		SchemaVersion:       RepositoryEvidenceSchemaVersion,
		RepositoryName:      cleanRepositoryEvidenceText(input.RepositoryName),
		RepositoryURL:       sanitizeRepositoryEvidenceURL(input.RepositoryURL),
		RequestedRef:        cleanRepositoryEvidenceText(input.RequestedRef),
		ResolvedRef:         cleanRepositoryEvidenceText(input.ResolvedRef),
		CommitSHA:           strings.TrimSpace(input.CommitSHA),
		CheckoutPath:        filepath.ToSlash(strings.TrimSpace(input.CheckoutPath)),
		InputSnapshotSHA256: strings.TrimSpace(input.InputSnapshotSHA256),
		TreeFileCount:       len(paths), SelectedFileCount: len(selected), SelectedBytes: selectedBytes,
		Files: selected, Buckets: nonNilRepositoryEvidenceBuckets(buckets), Warnings: warnings,
	}
	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return RepositoryEvidenceBundle{}, err
	}
	files["index.json"] = indexJSON
	files["tree.txt"] = []byte(strings.Join(paths, "\n") + "\n")
	files["README.md"] = []byte(renderRepositoryEvidenceReadme(index))
	return RepositoryEvidenceBundle{Index: index, Files: files}, nil
}

func preferRepositoryReadme(values []repositoryEvidenceCandidate, limit int) []repositoryEvidenceCandidate {
	preferred := -1
	for index, value := range values {
		if !repositoryEvidenceReadmePattern.MatchString(value.path) {
			continue
		}
		if preferred == -1 || strings.Count(value.path, "/") < strings.Count(values[preferred].path, "/") ||
			(strings.Count(value.path, "/") == strings.Count(values[preferred].path, "/") && value.path < values[preferred].path) {
			preferred = index
		}
	}
	selected := append([]repositoryEvidenceCandidate(nil), values...)
	if len(selected) > limit {
		selected = selected[:limit]
	}
	if preferred == -1 {
		return selected
	}
	preferredValue := values[preferred]
	for _, value := range selected {
		if value.path == preferredValue.path {
			return selected
		}
	}
	return append([]repositoryEvidenceCandidate{preferredValue}, selected[:limit-1]...)
}

func shouldSkipRepositoryEvidencePath(normalized string) bool {
	if isRepositoryEvidenceAssetPath(normalized) {
		return false
	}
	if repositoryEvidenceEditorDirPattern.MatchString(normalized) {
		return true
	}
	if repositoryEvidenceGeneratedDirPattern.MatchString(normalized) {
		return true
	}
	if repositoryEvidenceLockPattern.MatchString(normalized) ||
		repositoryEvidenceTestDirPattern.MatchString(normalized) ||
		repositoryEvidenceTestFilePattern.MatchString(normalized) ||
		repositoryEvidenceArchivePattern.MatchString(normalized) {
		return true
	}
	base := strings.ToLower(filepath.Base(normalized))
	return strings.HasPrefix(base, ".env") || strings.Contains(base, "credential") || strings.Contains(base, "secret") ||
		strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".p12")
}

func scoreRepositoryEvidenceFile(relative string) int {
	normalized := strings.ToLower(filepath.ToSlash(relative))
	if shouldSkipRepositoryEvidencePath(normalized) {
		return -1
	}
	score := 0
	if repositoryEvidenceNativeTokenPattern.MatchString(normalized) {
		score += 95
	}
	if repositoryEvidenceNativeSourcePattern.MatchString(normalized) {
		score += 40
	}
	if repositoryEvidenceReadmePattern.MatchString(normalized) {
		score += 100
	}
	if repositoryEvidenceManifestPattern.MatchString(normalized) {
		score += 95
	}
	if repositoryEvidenceTokenFilePattern.MatchString(normalized) {
		score += 95
	}
	if repositoryEvidenceGlobalStylePattern.MatchString(normalized) {
		score += 88
	}
	if repositoryEvidenceFontPattern.MatchString(normalized) {
		score += 145
	}
	if isRepositoryEvidenceAssetPath(normalized) {
		score += 86
	}
	if repositoryEvidenceContextDirPattern.MatchString("/" + normalized) {
		score += 70
	}
	if repositoryEvidenceLayoutDirPattern.MatchString("/" + normalized) {
		score += 68
	}
	if repositoryEvidenceComponentDirPattern.MatchString("/" + normalized) {
		score += 65
	}
	if repositoryEvidenceComponentFilePattern.MatchString(normalized) {
		score += 58
	}
	if repositoryEvidenceEntryPattern.MatchString(normalized) {
		score += 45
	}
	if repositoryEvidenceTextPattern.MatchString(normalized) {
		score += 10
	}
	if isRepositoryEvidenceBinaryAsset(normalized) {
		score += 6
	}
	return score
}

func repositoryEvidenceKind(relative string) string {
	normalized := strings.ToLower(filepath.ToSlash(relative))
	switch {
	case repositoryEvidenceFoundationPattern.MatchString(normalized):
		return "foundation"
	case isRepositoryEvidenceAssetPath(normalized):
		return "asset"
	case repositoryEvidenceComponentDirPattern.MatchString("/" + normalized):
		return "component"
	case repositoryEvidencePageDirPattern.MatchString("/" + normalized):
		return "page-pattern"
	case repositoryEvidenceShellDirPattern.MatchString("/" + normalized):
		return "layout"
	case repositoryEvidenceContextFilePattern.MatchString(normalized):
		return "project-context"
	default:
		return "domain-extension"
	}
}

func isRepositoryEvidenceAssetPath(normalized string) bool {
	return repositoryEvidenceAssetPattern.MatchString(normalized)
}

func isRepositoryEvidenceBinaryAsset(normalized string) bool {
	return repositoryEvidenceBinaryPattern.MatchString(normalized)
}

func repositoryEvidenceSHA256(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func sanitizeRepositoryEvidenceURL(raw string) string {
	value := strings.TrimSpace(raw)
	if index := strings.Index(value, "?"); index >= 0 {
		value = value[:index]
	}
	if index := strings.Index(value, "#"); index >= 0 {
		value = value[:index]
	}
	if scheme := strings.Index(value, "://"); scheme >= 0 {
		if at := strings.Index(value[scheme+3:], "@"); at >= 0 {
			value = value[:scheme+3] + value[scheme+3+at+1:]
		}
	}
	return cleanRepositoryEvidenceText(value)
}

func cleanRepositoryEvidenceText(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\x00", ""), "\r", " "))
	if len(value) > 2048 {
		value = value[:2048]
	}
	return value
}

func nonNilRepositoryEvidenceBuckets(values map[string][]string) map[string][]string {
	if values == nil {
		return map[string][]string{}
	}
	return values
}

func renderRepositoryEvidenceReadme(index RepositoryEvidenceIndex) string {
	var b strings.Builder
	b.WriteString("# Repository design evidence\n\n")
	fmt.Fprintf(&b, "- Repository: %s\n- Resolved ref: %s\n- Commit: %s\n- Complete tree files: %d\n- Snapshotted evidence files: %d\n- Checkout: `%s`\n\n", index.RepositoryName, index.ResolvedRef, index.CommitSHA, index.TreeFileCount, index.SelectedFileCount, index.CheckoutPath)
	b.WriteString("Read `index.json` first, use `tree.txt` to understand the complete non-generated tree, then inspect the immutable snapshots under `files/`. The checkout remains available for evidence-backed spot checks. Do not infer design values from the repository name or URL.\n")
	return b.String()
}

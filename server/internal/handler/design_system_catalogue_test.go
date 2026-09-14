package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/designsystemcatalogue"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// designsystemcatalogueDigestForTest reads the digest the catalogue computed
// for a slug, so the test asserts the URL the handler composes without
// re-deriving the hash.
func designsystemcatalogueDigestForTest(t *testing.T, slug string) string {
	t.Helper()
	entries, err := designsystemcatalogue.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Slug == slug {
			return entry.ShowcaseDigest
		}
	}
	t.Fatalf("slug %q not in catalogue", slug)
	return ""
}

// The catalogue is embedded, so these exercise the real bundled packages
// rather than a fixture — the response the 官方 scope renders is what a
// reader sees here.
func TestListBuiltinDesignSystemsReturnsTheBundledCatalogue(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.ListBuiltinDesignSystems(w, httptest.NewRequest(http.MethodGet, "/api/design-systems/builtin", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		DesignSystems []BuiltinDesignSystemResponse `json:"design_systems"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.DesignSystems) < 100 {
		t.Fatalf("catalogue returned %d systems", len(body.DesignSystems))
	}
	for _, system := range body.DesignSystems {
		if system.Slug == "" || system.Name == "" {
			t.Fatalf("system without identity: %+v", system)
		}
	}
}

func TestGetBuiltinDesignSystemReturnsContentAndRejectsUnknown(t *testing.T) {
	list := httptest.NewRecorder()
	testHandler.ListBuiltinDesignSystems(list, httptest.NewRequest(http.MethodGet, "/api/design-systems/builtin", nil))
	var body struct {
		DesignSystems []BuiltinDesignSystemResponse `json:"design_systems"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil || len(body.DesignSystems) == 0 {
		t.Fatalf("list precondition failed: %v", err)
	}
	slug := body.DesignSystems[0].Slug

	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/api/design-systems/builtin/"+slug, nil), "slug", slug)
	testHandler.GetBuiltinDesignSystem(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var detail BuiltinDesignSystemDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Slug != slug || detail.TokensCSS == "" || detail.DesignMarkdown == "" {
		t.Fatalf("detail is incomplete: slug=%q tokens=%d design=%d",
			detail.Slug, len(detail.TokensCSS), len(detail.DesignMarkdown))
	}

	missing := httptest.NewRecorder()
	testHandler.GetBuiltinDesignSystem(missing,
		withURLParam(httptest.NewRequest(http.MethodGet, "/api/design-systems/builtin/nope", nil), "slug", "nope"))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown slug status = %d, want 404", missing.Code)
	}
}

// The list hands the client a digest-versioned showcase path, and the
// unauthenticated showcase route serves that document immutably, fenced by its
// own CSP: no scripts, no network, framed by any app origin. Any other digest
// or variant is a miss the browser must not remember.
func TestBuiltinDesignSystemShowcaseIsServedByDigest(t *testing.T) {
	list := httptest.NewRecorder()
	testHandler.ListBuiltinDesignSystems(list, httptest.NewRequest(http.MethodGet, "/api/design-systems/builtin", nil))
	var body struct {
		DesignSystems []BuiltinDesignSystemResponse `json:"design_systems"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var withShowcase *BuiltinDesignSystemResponse
	for i := range body.DesignSystems {
		if body.DesignSystems[i].ShowcaseURL != "" {
			withShowcase = &body.DesignSystems[i]
			break
		}
	}
	if withShowcase == nil {
		t.Fatal("no built-in system exposes a showcase")
	}
	digest := designsystemcatalogueDigestForTest(t, withShowcase.Slug)
	wantURL := "/api/design-systems/builtin/" + withShowcase.Slug + "/showcase/" + digest + "/light"
	if withShowcase.ShowcaseURL != wantURL {
		t.Fatalf("showcase_url = %q, want %q", withShowcase.ShowcaseURL, wantURL)
	}

	serve := func(slug, digest, variant string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := withURLParams(httptest.NewRequest(http.MethodGet, "/api/design-systems/builtin/"+slug+"/showcase/"+digest+"/"+variant, nil),
			"slug", slug, "digest", digest, "variant", variant)
		testHandler.GetBuiltinDesignSystemShowcase(recorder, request)
		return recorder
	}
	for _, variant := range []string{"light", "dark"} {
		recorder := serve(withShowcase.Slug, digest, variant)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", variant, recorder.Code, recorder.Body.String())
		}
		if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("%s: Content-Type = %q", variant, got)
		}
		if got := recorder.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
			t.Fatalf("%s: Cache-Control = %q, want immutable", variant, got)
		}
		csp := recorder.Header().Get("Content-Security-Policy")
		for _, directive := range []string{"default-src 'none'", "style-src 'unsafe-inline'", "frame-ancestors *", "sandbox"} {
			if !strings.Contains(csp, directive) {
				t.Fatalf("%s: CSP %q lacks %q", variant, csp, directive)
			}
		}
		if strings.Contains(csp, "script-src") {
			t.Fatalf("%s: showcase CSP admits scripts: %q", variant, csp)
		}
	}
	for _, tt := range []struct{ name, slug, digest, variant string }{
		{name: "stale digest", slug: withShowcase.Slug, digest: "000000000000", variant: "light"},
		{name: "unknown variant", slug: withShowcase.Slug, digest: digest, variant: "poster"},
		{name: "unknown slug", slug: "nope", digest: digest, variant: "light"},
	} {
		recorder := serve(tt.slug, tt.digest, tt.variant)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", tt.name, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s: Cache-Control = %q, want no-store", tt.name, got)
		}
	}
}

// A built-in system can be named as a style reference when a project creates
// its own system (DC-056): the snapshot inlines the package's design language
// and tokens so the agent's input stays frozen, unknown slugs are refused, and
// the count is capped because the content rides in the snapshot.
func TestResolveProjectDesignSystemReferencesInlinesBuiltinSystems(t *testing.T) {
	entries, err := designsystemcatalogue.List()
	if err != nil || len(entries) < 4 {
		t.Fatalf("catalogue precondition: %v (%d)", err, len(entries))
	}
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Builtin references")

	resolved, err := testHandler.resolveProjectDesignSystemReferences(ctx, workspaceID, projectID, []ProjectDesignSystemReferenceInput{
		{Kind: "builtin_design_system", Value: entries[0].Slug, Label: "参考风格"},
	})
	if err != nil {
		t.Fatalf("resolve builtin reference: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Value != entries[0].Slug || resolved[0].Title != entries[0].Name ||
		strings.TrimSpace(resolved[0].DesignMarkdown) == "" || strings.TrimSpace(resolved[0].TokensCSS) == "" {
		t.Fatalf("resolved = %+v", resolved)
	}

	var requestErr *projectDesignSystemRequestError
	if _, err := testHandler.resolveProjectDesignSystemReferences(ctx, workspaceID, projectID, []ProjectDesignSystemReferenceInput{
		{Kind: "builtin_design_system", Value: "no-such-system"},
	}); !errors.As(err, &requestErr) || requestErr.code != "reference_not_found" {
		t.Fatalf("unknown slug error = %v, want reference_not_found", err)
	}

	tooMany := make([]ProjectDesignSystemReferenceInput, 0, 4)
	for _, entry := range entries[:4] {
		tooMany = append(tooMany, ProjectDesignSystemReferenceInput{Kind: "builtin_design_system", Value: entry.Slug})
	}
	if _, err := testHandler.resolveProjectDesignSystemReferences(ctx, workspaceID, projectID, tooMany); !errors.As(err, &requestErr) || requestErr.code != "too_many_references" {
		t.Fatalf("four builtin references error = %v, want too_many_references", err)
	}
}

// A local_path reference names a directory on the machine that executes the
// task (the picked agent's own daemon). The server freezes the path verbatim
// without touching any filesystem, and only absolute paths — Unix or Windows,
// regardless of the server's own OS — are accepted.
func TestResolveProjectDesignSystemReferencesFreezesLocalPaths(t *testing.T) {
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Local path references")

	resolved, err := testHandler.resolveProjectDesignSystemReferences(ctx, workspaceID, projectID, []ProjectDesignSystemReferenceInput{
		{Kind: "local_path", Value: "  /Users/dev/brand-site  ", Label: "本地代码"},
		{Kind: "local_path", Value: `C:\dev\brand-site`},
	})
	if err != nil {
		t.Fatalf("resolve local_path references: %v", err)
	}
	if len(resolved) != 2 || resolved[0].Value != "/Users/dev/brand-site" || resolved[0].Label != "本地代码" || resolved[1].Value != `C:\dev\brand-site` {
		t.Fatalf("resolved = %+v", resolved)
	}

	var requestErr *projectDesignSystemRequestError
	for _, invalid := range []string{"", "relative/path", "./here", "~", "~/code"} {
		if _, err := testHandler.resolveProjectDesignSystemReferences(ctx, workspaceID, projectID, []ProjectDesignSystemReferenceInput{
			{Kind: "local_path", Value: invalid},
		}); !errors.As(err, &requestErr) || requestErr.code != "reference_invalid" {
			t.Fatalf("local_path %q error = %v, want reference_invalid", invalid, err)
		}
	}
}

// The workspace catalogue feeds the library's rows. OD's rows lead with the
// system's own summary and mark a system under adjustment; the entry must
// therefore carry the first line of the frozen brief and whether a draft
// package sits beside the saved one.
func TestListWorkspaceDesignSystemCatalogueCarriesSummaryAndDraftFlag(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Catalogue rows")
	system, err := db.New(testPool).CreateProjectDesignSystem(ctx, db.CreateProjectDesignSystemParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		ProjectID:     projectID,
		Name:          "看板视觉",
		Platform:      "web",
		InputSnapshot: []byte(`{"brief":"统一看板的产品视觉语言。\n第二行不进入摘要。","platform":"web"}`),
		CreatedBy:     parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_design_system_package WHERE design_system_id = $1`, system.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_design_system WHERE id = $1`, system.ID)
	})
	if _, err := testPool.Exec(ctx, `UPDATE project_design_system SET saved_at = now() WHERE id = $1`, system.ID); err != nil {
		t.Fatalf("mark saved: %v", err)
	}
	seedPackage := func(slot string) {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO project_design_system_package (
				design_system_id, slot, design_md, tokens_css, components_html, manifest, validation, integrity_sha256, render_status
			) VALUES ($1, $2, '# x', ':root{}', '<p></p>', '{}'::jsonb, '{}'::jsonb, 'x', 'passed')
		`, system.ID, slot); err != nil {
			t.Fatalf("seed %s package: %v", slot, err)
		}
	}
	seedPackage("saved")

	w := httptest.NewRecorder()
	testHandler.ListWorkspaceDesignSystemCatalogue(w, newRequest(http.MethodGet, "/api/project-design-systems/catalogue", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		DesignSystems []designSystemCatalogueEntry `json:"design_systems"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var entry *designSystemCatalogueEntry
	for i := range body.DesignSystems {
		if body.DesignSystems[i].ID == uuidToString(system.ID) {
			entry = &body.DesignSystems[i]
		}
	}
	if entry == nil {
		t.Fatalf("saved system missing from the catalogue: %+v", body.DesignSystems)
	}
	if entry.Summary != "统一看板的产品视觉语言。" {
		t.Fatalf("summary = %q, want the brief's first line", entry.Summary)
	}
	if entry.HasDraftPackage {
		t.Fatal("saved-only system reports a draft package")
	}
	if entry.OwnershipScope != "mine" {
		t.Fatalf("ownership scope = %q, want mine for the current requester", entry.OwnershipScope)
	}

	// A draft beside the saved package is OD's draft state: the flag flips.
	seedPackage("draft")
	w = httptest.NewRecorder()
	testHandler.ListWorkspaceDesignSystemCatalogue(w, newRequest(http.MethodGet, "/api/project-design-systems/catalogue", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for i := range body.DesignSystems {
		if body.DesignSystems[i].ID == uuidToString(system.ID) {
			entry = &body.DesignSystems[i]
		}
	}
	if entry == nil || !entry.HasDraftPackage {
		t.Fatalf("draft flag = %+v", entry)
	}
}

func TestCatalogueSummaryBoundsTheFirstLine(t *testing.T) {
	if got := catalogueSummary([]byte(`{"brief":"  \n第一行摘要\n第二行"}`)); got != "第一行摘要" {
		t.Fatalf("summary = %q", got)
	}
	if got := catalogueSummary([]byte(`{"brief":"` + strings.Repeat("长", 100) + `"}`)); len([]rune(got)) != 80 {
		t.Fatalf("long line not bounded: %d runes", len([]rune(got)))
	}
	if got := catalogueSummary([]byte(`{"brief":""}`)); got != "" {
		t.Fatalf("empty brief = %q", got)
	}
	if got := catalogueSummary(nil); got != "" {
		t.Fatalf("nil snapshot = %q", got)
	}
}

// The detail response carries the kit-view modules the library renders, and
// every artifact URL points at the digest-versioned showcase route that
// actually serves the page.
func TestGetBuiltinDesignSystemCarriesTheKitViewModules(t *testing.T) {
	w := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/api/design-systems/builtin/agentic", nil), "slug", "agentic")
	testHandler.GetBuiltinDesignSystem(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var detail BuiltinDesignSystemDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Title != "Design System Inspired by Agentic" || detail.Identity == "" {
		t.Fatalf("title/identity = %q / %q", detail.Title, detail.Identity)
	}
	if len(detail.Palette) != 7 || detail.Palette[0].Name != "Primary" || detail.Palette[0].Role != "accent" ||
		detail.Palette[0].Usage != "Token from style foundations." {
		t.Fatalf("palette = %+v", detail.Palette)
	}
	if detail.Typography.Display != "Playfair Display" || detail.Typography.Mono != "JetBrains Mono" || len(detail.Typography.Weights) != 9 {
		t.Fatalf("typography = %+v", detail.Typography)
	}
	if len(detail.LayoutGuidelines) == 0 || len(detail.TokenContract) != 6 || detail.TokenContract[0].Name != "colorPrimary" {
		t.Fatalf("layout/contract = %+v / %+v", detail.LayoutGuidelines, detail.TokenContract)
	}
	if len(detail.Artifacts) != 6 || detail.Artifacts[0].ID != "landing" || detail.Artifacts[0].Label != "Landing page" {
		t.Fatalf("artifacts = %+v", detail.Artifacts)
	}
	// The URL the response hands out must be servable as-is.
	digest := designsystemcatalogueDigestForTest(t, "agentic")
	wantPrefix := "/api/design-systems/builtin/agentic/showcase/" + digest + "/artifact-"
	if !strings.HasPrefix(detail.Artifacts[0].URL, wantPrefix) {
		t.Fatalf("artifact URL = %q, want prefix %q", detail.Artifacts[0].URL, wantPrefix)
	}
	serve := httptest.NewRecorder()
	request := withURLParams(httptest.NewRequest(http.MethodGet, detail.Artifacts[0].URL, nil),
		"slug", "agentic", "digest", digest, "variant", "artifact-landing")
	testHandler.GetBuiltinDesignSystemShowcase(serve, request)
	if serve.Code != http.StatusOK || !strings.Contains(serve.Body.String(), "Landing module") {
		t.Fatalf("artifact serve status = %d", serve.Code)
	}
}

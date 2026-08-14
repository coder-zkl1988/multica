package designdocument

import (
	"bytes"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"

	parse "github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
	"github.com/tdewolff/parse/v2/js"
	"golang.org/x/net/html"
)

func Audit(files map[string][]byte, index []FileEntry, targets []PreviewTarget, contentDigest string) AuditReport {
	return auditPackage(files, index, targets, contentDigest)
}

func auditPackage(files map[string][]byte, index []FileEntry, targets []PreviewTarget, contentDigest string) AuditReport {
	diagnostics := make([]Diagnostic, 0)
	for _, required := range []string{"brief.json", "prototype/index.html", "prototype/styles.css", "prototype/app.js", "coverage.json"} {
		if len(bytes.TrimSpace(files[required])) == 0 {
			diagnostics = append(diagnostics, diagnostic("artifact_missing", required, required+" must be present and non-empty"))
		}
	}
	var brief Brief
	if raw := files["brief.json"]; len(raw) != 0 {
		if err := decodeStrict(raw, &brief); err != nil {
			diagnostics = append(diagnostics, diagnostic("brief_invalid", "brief.json", "brief is invalid: "+err.Error()))
		} else {
			diagnostics = append(diagnostics, auditBrief(brief)...)
		}
	}
	var coverage Coverage
	if raw := files["coverage.json"]; len(raw) != 0 {
		if err := decodeStrict(raw, &coverage); err != nil {
			diagnostics = append(diagnostics, diagnostic("coverage_invalid", "coverage.json", "coverage is invalid: "+err.Error()))
		} else {
			diagnostics = append(diagnostics, auditCoverage(coverage, brief, targets)...)
		}
	}
	declared := make(map[string]FileEntry, len(index))
	for _, entry := range index {
		declared[entry.Path] = entry
	}
	for _, entry := range index {
		switch entry.MediaType {
		case "text/html; charset=utf-8":
			diagnostics = append(diagnostics, auditHTML(entry.Path, files[entry.Path], declared)...)
		case "text/css; charset=utf-8":
			diagnostics = append(diagnostics, auditCSS(entry.Path, files[entry.Path], declared)...)
		case "text/javascript; charset=utf-8":
			diagnostics = append(diagnostics, auditJS(entry.Path, files[entry.Path])...)
		}
	}
	return AuditReport{SchemaVersion: AuditSchema, Passed: len(diagnostics) == 0, ContentDigest: contentDigest, Diagnostics: diagnostics}
}

func auditBrief(brief Brief) []Diagnostic {
	d := make([]Diagnostic, 0)
	if !validText(brief.Goal) || !validText(brief.Summary) {
		d = append(d, diagnostic("brief_text_invalid", "brief.json", "goal and summary must be non-empty text"))
	}
	if brief.Requirements == nil || brief.Pages == nil || brief.Subpages == nil || brief.States == nil || brief.Overlays == nil || brief.Flows == nil || brief.Scenarios == nil || brief.Accessibility == nil || brief.NonGoals == nil {
		d = append(d, diagnostic("brief_shape_invalid", "brief.json", "all brief scope arrays must be present"))
	}
	if len(brief.Requirements) == 0 || len(brief.Pages) == 0 || len(brief.States) == 0 || len(brief.Overlays) == 0 || len(brief.Flows) == 0 || len(brief.Scenarios) == 0 || len(brief.Accessibility) == 0 || len(brief.NonGoals) == 0 {
		d = append(d, diagnostic("brief_scope_missing", "brief.json", "required brief scopes must be non-empty"))
	}
	seen := map[string]struct{}{}
	requirements, pages, states, overlays, flows := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	add := func(scope, id, name, description string, own map[string]struct{}) {
		if !stableID(id) || !validText(name) || !validText(description) {
			d = append(d, diagnostic("semantic_scope_invalid", "brief.json", scope+" requires a stable ID, name, and description"))
			return
		}
		if _, ok := seen[id]; ok {
			d = append(d, diagnostic("semantic_id_duplicate", "brief.json", "semantic IDs must be package-unique"))
			return
		}
		seen[id] = struct{}{}
		if own != nil {
			own[id] = struct{}{}
		}
	}
	for _, v := range brief.Requirements {
		add("requirement", v.ID, v.Name, v.Description, requirements)
	}
	for _, v := range brief.Pages {
		add("page", v.ID, v.Name, v.Description, pages)
	}
	for _, v := range brief.Subpages {
		add("subpage", v.ID, v.Name, v.Description, nil)
	}
	for _, v := range brief.States {
		add("state", v.ID, v.Name, v.Description, states)
	}
	for _, v := range brief.Overlays {
		add("overlay", v.ID, v.Name, v.Description, overlays)
	}
	for _, v := range brief.Blocks {
		add("block", v.ID, v.Name, v.Description, nil)
	}
	for _, v := range brief.Flows {
		add("flow", v.ID, v.Name, v.Description, flows)
	}
	for _, v := range brief.Scenarios {
		add("scenario", v.ID, v.Name, v.Description, nil)
	}
	for _, v := range brief.Accessibility {
		add("accessibility", v.ID, v.Name, v.Description, nil)
	}
	for _, v := range brief.NonGoals {
		add("non-goal", v.ID, v.Name, v.Description, nil)
	}
	check := func(code string, refs []string, values map[string]struct{}) {
		for _, id := range refs {
			if _, ok := values[id]; !ok {
				d = append(d, diagnostic(code, "brief.json", "reference "+id+" does not resolve"))
			}
		}
	}
	for _, v := range brief.Pages {
		check("requirement_reference_dangling", v.RequirementIDs, requirements)
	}
	for _, v := range brief.Subpages {
		check("page_reference_dangling", []string{v.PageID}, pages)
		check("requirement_reference_dangling", v.RequirementIDs, requirements)
	}
	for _, v := range append(append(append([]PageScope{}, brief.States...), brief.Overlays...), brief.Blocks...) {
		check("page_reference_dangling", []string{v.PageID}, pages)
		check("requirement_reference_dangling", v.RequirementIDs, requirements)
	}
	for _, v := range brief.Flows {
		check("page_reference_dangling", v.PageIDs, pages)
		check("state_reference_dangling", v.StateIDs, states)
		check("overlay_reference_dangling", v.OverlayIDs, overlays)
		check("requirement_reference_dangling", v.RequirementIDs, requirements)
	}
	for _, v := range brief.Scenarios {
		check("flow_reference_dangling", v.FlowIDs, flows)
	}
	return d
}

func auditCoverage(c Coverage, brief Brief, targets []PreviewTarget) []Diagnostic {
	d := make([]Diagnostic, 0)
	if c.Requirements == nil || c.Pages == nil || c.States == nil || c.Overlays == nil || c.Flows == nil || c.DesignSystem == nil || c.Interactions == nil || c.TemplateResidue == nil || c.Uncovered == nil {
		d = append(d, diagnostic("coverage_shape_invalid", "coverage.json", "all coverage arrays must be present"))
	}
	if len(c.DesignSystem) == 0 || len(c.Interactions) == 0 || len(c.TemplateResidue) == 0 {
		d = append(d, diagnostic("coverage_category_missing", "coverage.json", "all coverage categories must contain evidence"))
	}
	reqs, pages, states, overlays, flows, targetIDs := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, v := range brief.Requirements {
		reqs[v.ID] = struct{}{}
	}
	for _, v := range brief.Pages {
		pages[v.ID] = struct{}{}
	}
	for _, v := range brief.States {
		states[v.ID] = struct{}{}
	}
	for _, v := range brief.Overlays {
		overlays[v.ID] = struct{}{}
	}
	for _, v := range brief.Flows {
		flows[v.ID] = struct{}{}
	}
	for _, v := range targets {
		targetIDs[v.ID] = struct{}{}
	}
	seen := map[string]struct{}{}
	base := func(id, evidence string) {
		if !stableID(id) || !validText(evidence) {
			d = append(d, diagnostic("coverage_entry_invalid", "coverage.json", "coverage entries require stable IDs and evidence"))
			return
		}
		if _, ok := seen[id]; ok {
			d = append(d, diagnostic("coverage_id_duplicate", "coverage.json", "coverage IDs must be unique"))
		}
		seen[id] = struct{}{}
	}
	ref := func(code, id string, set map[string]struct{}) {
		if _, ok := set[id]; !ok {
			d = append(d, diagnostic(code, "coverage.json", "coverage reference "+id+" does not resolve"))
		}
	}
	covered := map[string]map[string]struct{}{
		"requirement": {}, "page": {}, "state": {}, "overlay": {}, "flow": {},
	}
	for _, v := range c.Requirements {
		base(v.ID, v.Evidence)
		ref("requirement_reference_dangling", v.RequirementID, reqs)
		covered["requirement"][v.RequirementID] = struct{}{}
	}
	for _, group := range []struct {
		kind   string
		values []ScopeCoverage
		field  func(ScopeCoverage) string
		set    map[string]struct{}
		code   string
	}{{"page", c.Pages, func(v ScopeCoverage) string { return v.PageID }, pages, "page_reference_dangling"}, {"state", c.States, func(v ScopeCoverage) string { return v.StateID }, states, "state_reference_dangling"}, {"overlay", c.Overlays, func(v ScopeCoverage) string { return v.OverlayID }, overlays, "overlay_reference_dangling"}, {"flow", c.Flows, func(v ScopeCoverage) string { return v.FlowID }, flows, "flow_reference_dangling"}} {
		for _, v := range group.values {
			base(v.ID, v.Evidence)
			semanticID := group.field(v)
			ref(group.code, semanticID, group.set)
			ref("preview_target_reference_dangling", v.TargetID, targetIDs)
			covered[group.kind][semanticID] = struct{}{}
		}
	}
	for _, v := range c.DesignSystem {
		base(v.ID, v.Evidence)
	}
	for _, v := range c.TemplateResidue {
		base(v.ID, v.Evidence)
	}
	for _, v := range c.Interactions {
		base(v.ID, v.Evidence)
		ref("preview_target_reference_dangling", v.TargetID, targetIDs)
	}
	uncovered := map[string]map[string]struct{}{
		"requirement": {}, "page": {}, "state": {}, "overlay": {}, "flow": {},
	}
	uncoveredRefs := map[string]struct {
		values map[string]struct{}
		code   string
	}{
		"requirement": {reqs, "requirement_reference_dangling"},
		"page":        {pages, "page_reference_dangling"},
		"state":       {states, "state_reference_dangling"},
		"overlay":     {overlays, "overlay_reference_dangling"},
		"flow":        {flows, "flow_reference_dangling"},
	}
	for _, v := range c.Uncovered {
		if !validText(v.Kind) || !stableID(v.ID) || !validText(v.Reason) {
			d = append(d, diagnostic("coverage_uncovered_invalid", "coverage.json", "uncovered entries require kind, ID, and reason"))
		}
		if _, ok := seen[v.ID]; ok {
			d = append(d, diagnostic("coverage_id_duplicate", "coverage.json", "coverage IDs must be unique"))
		}
		seen[v.ID] = struct{}{}
		definition, ok := uncoveredRefs[v.Kind]
		if !ok {
			d = append(d, diagnostic("coverage_uncovered_invalid", "coverage.json", "uncovered kind is not supported"))
			continue
		}
		ref(definition.code, v.ID, definition.values)
		uncovered[v.Kind][v.ID] = struct{}{}
	}
	checkComplete := func(kind string, ids []string) {
		for _, id := range ids {
			_, isCovered := covered[kind][id]
			_, isUncovered := uncovered[kind][id]
			if !isCovered && !isUncovered {
				d = append(d, diagnostic("coverage_scope_missing", "coverage.json", kind+" "+id+" must be covered or explicitly uncovered"))
			}
		}
	}
	requirementIDs, pageIDs, stateIDs, overlayIDs, flowIDs := make([]string, 0, len(brief.Requirements)), make([]string, 0, len(brief.Pages)), make([]string, 0, len(brief.States)), make([]string, 0, len(brief.Overlays)), make([]string, 0, len(brief.Flows))
	for _, v := range brief.Requirements {
		requirementIDs = append(requirementIDs, v.ID)
	}
	for _, v := range brief.Pages {
		pageIDs = append(pageIDs, v.ID)
	}
	for _, v := range brief.States {
		stateIDs = append(stateIDs, v.ID)
	}
	for _, v := range brief.Overlays {
		overlayIDs = append(overlayIDs, v.ID)
	}
	for _, v := range brief.Flows {
		flowIDs = append(flowIDs, v.ID)
	}
	checkComplete("requirement", requirementIDs)
	checkComplete("page", pageIDs)
	checkComplete("state", stateIDs)
	checkComplete("overlay", overlayIDs)
	checkComplete("flow", flowIDs)
	if !c.AgentSelfCheck.Completed || !validText(c.AgentSelfCheck.Notes) {
		d = append(d, diagnostic("agent_self_check_invalid", "coverage.json", "agent self-check must be completed with notes"))
	}
	return d
}

func auditHTML(name string, raw []byte, declared map[string]FileEntry) []Diagnostic {
	d := make([]Diagnostic, 0)
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return []Diagnostic{diagnostic("html_invalid", name, "HTML cannot be parsed")}
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			tag := strings.ToLower(node.Data)
			if tag == "form" {
				d = append(d, diagnostic("html_form_forbidden", name, "forms are not allowed in offline prototypes"))
			}
			if tag == "iframe" || tag == "object" || tag == "embed" || tag == "base" {
				d = append(d, diagnostic("html_element_forbidden", name, "embedded or base elements are not allowed"))
			}
			attrs := map[string]string{}
			for _, a := range node.Attr {
				attrs[strings.ToLower(a.Key)] = strings.TrimSpace(a.Val)
				if strings.HasPrefix(strings.ToLower(a.Key), "on") {
					d = append(d, diagnostic("html_event_handler_forbidden", name, "inline event handlers are not allowed"))
				}
			}
			for _, key := range []string{"action", "formaction", "ping", "srcdoc"} {
				if attrs[key] != "" {
					d = append(d, diagnostic("html_form_forbidden", name, "active form or navigation attributes are not allowed"))
				}
			}
			switch tag {
			case "script":
				if src := attrs["src"]; src != "" {
					d = append(d, auditLocalResource(name, src, "text/javascript; charset=utf-8", declared)...)
				}
			case "link":
				if !strings.EqualFold(attrs["rel"], "stylesheet") {
					d = append(d, diagnostic("html_link_forbidden", name, "only local stylesheets may be linked"))
				} else {
					d = append(d, auditLocalResource(name, attrs["href"], "text/css; charset=utf-8", declared)...)
				}
			case "img":
				if src := attrs["src"]; src != "" {
					d = append(d, auditLocalResource(name, src, "image/", declared)...)
				}
			case "input":
				if strings.EqualFold(attrs["type"], "image") && attrs["src"] != "" {
					d = append(d, auditLocalResource(name, attrs["src"], "image/", declared)...)
				}
			}
			if value := attrs["srcset"]; value != "" {
				d = append(d, auditSrcset(name, value, declared)...)
			}
			if value := attrs["poster"]; value != "" {
				d = append(d, auditLocalResource(name, value, "image/", declared)...)
			}
			if (tag == "audio" || tag == "video" || tag == "source" || tag == "track") && attrs["src"] != "" {
				d = append(d, auditLocalResource(name, attrs["src"], "", declared)...)
			}
			if tag == "meta" && strings.EqualFold(attrs["http-equiv"], "refresh") {
				d = append(d, diagnostic("html_url_unsafe", name, "meta refresh is not allowed"))
			}
			if href := attrs["href"]; href != "" && tag != "link" && !strings.HasPrefix(href, "#") {
				d = append(d, diagnostic("html_url_unsafe", name, "links must use package-local fragments"))
			}
			if style := attrs["style"]; style != "" {
				d = append(d, auditCSS(name, []byte(style), declared)...)
			}
			if tag == "style" && node.FirstChild != nil {
				d = append(d, auditCSS(name, []byte(node.FirstChild.Data), declared)...)
			}
			if tag == "script" && attrs["src"] == "" && node.FirstChild != nil {
				d = append(d, auditJS(name, []byte(node.FirstChild.Data))...)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return d
}

func auditCSS(name string, raw []byte, declared map[string]FileEntry) []Diagnostic {
	d := make([]Diagnostic, 0)
	lexer := css.NewLexer(parse.NewInputBytes(raw))
	resourceFunction := false
	for {
		tt, data := lexer.Next()
		if tt == css.ErrorToken {
			if err := lexer.Err(); err != nil && err != io.EOF {
				d = append(d, diagnostic("css_invalid", name, "CSS cannot be parsed"))
			}
			break
		}
		switch tt {
		case css.AtKeywordToken:
			if bytes.IndexByte(data, '\\') >= 0 {
				d = append(d, diagnostic("css_structural_escape_unsupported", name, "CSS escapes are not supported in structural tokens"))
			}
			if strings.EqualFold(string(data), "@import") {
				d = append(d, diagnostic("css_import_forbidden", name, "CSS imports are not allowed"))
			}
		case css.FunctionToken:
			if bytes.IndexByte(data, '\\') >= 0 {
				d = append(d, diagnostic("css_structural_escape_unsupported", name, "CSS escapes are not supported in structural tokens"))
			}
			resourceFunction = strings.EqualFold(strings.TrimSpace(string(data)), "image-set(")
		case css.URLToken:
			if bytes.IndexByte(data[:strings.IndexByte(string(data), '(')], '\\') >= 0 {
				d = append(d, diagnostic("css_structural_escape_unsupported", name, "CSS escapes are not supported in structural tokens"))
				continue
			}
			value, ok := cssURL(data)
			if !ok {
				d = append(d, diagnostic("css_url_unsafe", name, "CSS URL is invalid"))
			} else {
				for _, item := range auditLocalResource(name, value, "", declared) {
					if item.Code == "html_url_unsafe" {
						item.Code = "css_url_unsafe"
					}
					d = append(d, item)
				}
			}
		case css.BadURLToken, css.BadStringToken:
			d = append(d, diagnostic("css_invalid", name, "CSS contains an invalid token"))
		case css.StringToken:
			value := decodeCSSString(data)
			if resourceFunction {
				for _, item := range auditLocalResource(name, value, "image/", declared) {
					if item.Code == "html_url_unsafe" {
						item.Code = "css_url_unsafe"
					}
					d = append(d, item)
				}
			}
		case css.SemicolonToken, css.RightBraceToken, css.RightParenthesisToken:
			resourceFunction = false
		}
	}
	return d
}

func auditJS(name string, raw []byte) []Diagnostic {
	d := make([]Diagnostic, 0)
	lexer := js.NewLexer(parse.NewInputBytes(raw))
	tokens := make([]struct {
		tt   js.TokenType
		data string
	}, 0)
	for {
		tt, data := lexer.Next()
		if tt == js.ErrorToken {
			if err := lexer.Err(); err != nil && err != io.EOF {
				d = append(d, diagnostic("js_invalid", name, "JavaScript cannot be parsed"))
			}
			break
		}
		if tt == js.CommentToken || tt == js.CommentLineTerminatorToken || tt == js.LineTerminatorToken {
			continue
		}
		tokens = append(tokens, struct {
			tt   js.TokenType
			data string
		}{tt, string(data)})
	}
	for i, t := range tokens {
		if t.tt == js.ImportToken {
			d = append(d, diagnostic("js_network_forbidden", name, "dynamic or static imports are not allowed"))
		}
		if js.IsIdentifier(t.tt) {
			plain := decodeJSIdentifier(t.data)
			if strings.Contains(t.data, "\\") {
				d = append(d, diagnostic("js_identifier_escape_unsupported", name, "JavaScript identifier escapes are not supported"))
				continue
			}
			if forbiddenJSName(plain) {
				d = append(d, diagnostic("js_network_forbidden", name, "network-capable JavaScript APIs are not allowed"))
			}
		}
		if t.tt == js.StringToken {
			value, ok := decodeJSString(t.data)
			if !ok {
				continue
			}
			d = append(d, auditJSString(name, value)...)
			if i > 0 && tokens[i-1].tt == js.OpenBracketToken && nextToken(tokens, i) == js.CloseBracketToken && forbiddenJSName(value) {
				d = append(d, diagnostic("js_network_forbidden", name, "network-capable JavaScript APIs are not allowed"))
			}
		}
		if t.tt == js.TemplateToken || t.tt == js.TemplateStartToken || t.tt == js.TemplateMiddleToken || t.tt == js.TemplateEndToken {
			value := templateStaticPart(t.tt, t.data)
			d = append(d, auditJSString(name, value)...)
			if t.tt == js.TemplateToken && i > 0 && tokens[i-1].tt == js.OpenBracketToken && nextToken(tokens, i) == js.CloseBracketToken && forbiddenJSName(value) {
				d = append(d, diagnostic("js_network_forbidden", name, "network-capable JavaScript APIs are not allowed"))
			}
		}
	}
	return d
}

func auditLocalResource(base, reference, want string, declared map[string]FileEntry) []Diagnostic {
	if reference == "" {
		return []Diagnostic{diagnostic("resource_path_unsafe", base, "resource path is empty")}
	}
	if strings.HasPrefix(reference, "#") {
		return nil
	}
	clean := reference
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	if strings.Contains(reference, "\\") || strings.HasPrefix(reference, "/") || strings.HasPrefix(reference, "~") || strings.Contains(reference, ":") || strings.HasPrefix(reference, "//") {
		return []Diagnostic{diagnostic("html_url_unsafe", base, "external and absolute resource URLs are not allowed")}
	}
	resolved := path.Clean(path.Join(path.Dir(base), clean))
	if strings.HasPrefix(resolved, "../") || resolved == ".." {
		return []Diagnostic{diagnostic("resource_path_unsafe", base, "resource path leaves the package")}
	}
	entry, ok := declared[resolved]
	if !ok {
		return []Diagnostic{diagnostic("resource_path_unsafe", base, "resource path is not declared in the package")}
	}
	if want != "" && !strings.HasPrefix(entry.MediaType, want) {
		return []Diagnostic{diagnostic("resource_type_invalid", base, "resource has an invalid media type")}
	}
	return nil
}

func auditSrcset(name, value string, declared map[string]FileEntry) []Diagnostic {
	d := make([]Diagnostic, 0)
	for _, candidate := range strings.Split(value, ",") {
		fields := strings.Fields(candidate)
		if len(fields) == 0 || len(fields) > 2 {
			d = append(d, diagnostic("resource_path_unsafe", name, "srcset is invalid"))
			continue
		}
		d = append(d, auditLocalResource(name, fields[0], "image/", declared)...)
	}
	return d
}

func cssURL(raw []byte) (string, bool) {
	s := strings.TrimSpace(string(raw))
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return "", false
	}
	s = strings.TrimSpace(s[open+1 : len(s)-1])
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return decodeCSSString([]byte(s)), true
	}
	return s, s != ""
}
func nextToken(tokens []struct {
	tt   js.TokenType
	data string
}, i int) js.TokenType {
	if i+1 < len(tokens) {
		return tokens[i+1].tt
	}
	return js.ErrorToken
}
func forbiddenJSName(v string) bool {
	switch v {
	case "fetch", "XMLHttpRequest", "WebSocket", "EventSource", "serviceWorker", "sendBeacon", "Image":
		return true
	}
	return false
}
func decodeJSIdentifier(v string) string { return strings.TrimSpace(v) }
func absoluteLocalPath(v string) bool {
	return strings.HasPrefix(v, "~/") || strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\\`) ||
		(len(v) >= 3 && ((v[0] >= 'a' && v[0] <= 'z') || (v[0] >= 'A' && v[0] <= 'Z')) && v[1] == ':' && (v[2] == '/' || v[2] == '\\'))
}

func auditJSString(name, value string) []Diagnostic {
	d := make([]Diagnostic, 0)
	if absoluteLocalPath(value) {
		d = append(d, diagnostic("absolute_path_forbidden", name, "absolute and home-relative paths are not allowed"))
	}
	if secretLiteral(value) {
		d = append(d, diagnostic("secret_pattern_forbidden", name, "credential-like values are not allowed"))
	}
	if externalURL(value) {
		d = append(d, diagnostic("external_url_forbidden", name, "external URLs are not allowed"))
	}
	if strings.HasPrefix(value, "/api/") {
		d = append(d, diagnostic("api_path_forbidden", name, "real API paths are not allowed"))
	}
	return d
}

func templateStaticPart(token js.TokenType, value string) string {
	switch token {
	case js.TemplateToken:
		return strings.TrimSuffix(strings.TrimPrefix(value, "`"), "`")
	case js.TemplateStartToken:
		return strings.TrimSuffix(strings.TrimPrefix(value, "`"), "${")
	case js.TemplateMiddleToken:
		return strings.TrimSuffix(strings.TrimPrefix(value, "}"), "${")
	case js.TemplateEndToken:
		return strings.TrimSuffix(strings.TrimPrefix(value, "}"), "`")
	}
	return ""
}

func decodeJSString(value string) (string, bool) {
	if len(value) < 2 {
		return "", false
	}
	if value[0] == '\'' && value[len(value)-1] == '\'' {
		value = `"` + strings.ReplaceAll(strings.ReplaceAll(value[1:len(value)-1], `\'`, `'`), `"`, `\"`) + `"`
	}
	decoded, err := strconv.Unquote(value)
	return decoded, err == nil
}

func decodeCSSString(data []byte) string {
	value := strings.TrimSpace(string(data))
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		value = value[1 : len(value)-1]
	}
	var decoded strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '\\' {
			decoded.WriteByte(value[i])
			i++
			continue
		}
		i++
		if i == len(value) {
			break
		}
		if digit, ok := cssHexDigit(value[i]); ok {
			code := uint32(digit)
			digits := 1
			i++
			for i < len(value) && digits < 6 {
				digit, ok = cssHexDigit(value[i])
				if !ok {
					break
				}
				code = code*16 + uint32(digit)
				digits++
				i++
			}
			if i < len(value) && cssWhitespace(value[i]) {
				i++
			}
			decoded.WriteRune(rune(code))
			continue
		}
		decoded.WriteByte(value[i])
		i++
	}
	return decoded.String()
}

func cssHexDigit(value byte) (byte, bool) {
	switch {
	case '0' <= value && value <= '9':
		return value - '0', true
	case 'a' <= value && value <= 'f':
		return value - 'a' + 10, true
	case 'A' <= value && value <= 'F':
		return value - 'A' + 10, true
	}
	return 0, false
}
func cssWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}
func externalURL(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	return strings.HasPrefix(value, "//") || (err == nil && parsed.Scheme != "")
}
func secretLiteral(v string) bool {
	lower := strings.ToLower(v)
	return strings.HasPrefix(lower, "sk-") || strings.HasPrefix(lower, "ghp_") || strings.HasPrefix(lower, "xoxb-") || strings.Contains(lower, "-----begin private key-----")
}
func stableID(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}
func diagnostic(code, filePath, message string) Diagnostic {
	return Diagnostic{Code: code, Severity: SeverityError, Path: filePath, Message: message}
}

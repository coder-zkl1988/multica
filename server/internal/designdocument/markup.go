package designdocument

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// forbiddenMarkupElements are elements that embed a foreign browsing context or
// run declarative animation against an external reference. <script>, <link>,
// <style> and <form> are deliberately absent: a design document prototype needs
// local scripts, local stylesheets and local form validation.
var forbiddenMarkupElements = map[string]struct{}{
	"animate": {}, "animatemotion": {}, "animatetransform": {}, "base": {},
	"discard": {}, "embed": {}, "foreignobject": {}, "frame": {}, "frameset": {},
	"iframe": {}, "object": {}, "portal": {}, "set": {},
}

// forbiddenMarkupAttributes carry a URL or a directive the package cannot own.
var forbiddenMarkupAttributes = map[string]struct{}{
	"background": {}, "cite": {}, "crossorigin": {}, "data": {}, "formaction": {},
	"http-equiv": {}, "integrity": {}, "manifest": {}, "ping": {}, "poster": {},
	"srcdoc": {}, "srcset": {},
}

// scriptTypes a prototype may declare. Anything else, in particular importmap
// and speculationrules, could map a name onto a remote URL.
var allowedScriptTypes = map[string]struct{}{
	"": {}, "module": {}, "text/javascript": {}, "application/javascript": {},
}

// linkRelations a prototype may declare. Every relation that only exists to
// warm up or fetch a remote origin is absent.
var allowedLinkRelations = map[string]struct{}{
	"stylesheet": {}, "icon": {}, "shortcut icon": {},
}

// mediaSourceElements load a package asset through their src attribute.
var mediaSourceElements = map[string]struct{}{
	"img": {}, "audio": {}, "video": {}, "source": {}, "track": {},
}

type markupAudit struct {
	path        string
	artifacts   map[string]ArtifactIndexEntry
	diagnostics []Diagnostic
	// deck marks the prototype entry of a deck document. It exempts the
	// speaker-notes JSON block and widens the script navigation rule for the
	// deck host channel.
	deck bool
}

// auditMarkup parses one prototype document with a real HTML parser and checks
// every element and attribute that could reach outside the package. deck marks
// the prototype entry of a deck document.
func auditMarkup(source []byte, basePath string, deck bool, artifacts map[string]ArtifactIndexEntry) []Diagnostic {
	document, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return []Diagnostic{errorDiagnostic("prototype_html_invalid", basePath, "prototype page is not valid HTML")}
	}
	audit := &markupAudit{path: basePath, artifacts: artifacts, diagnostics: make([]Diagnostic, 0), deck: deck}
	audit.walk(document)
	return audit.diagnostics
}

func (audit *markupAudit) walk(node *html.Node) {
	if node.Type == html.ElementNode {
		tag := strings.ToLower(node.Data)
		if _, forbidden := forbiddenMarkupElements[tag]; forbidden {
			audit.report("prototype_html_forbidden_element", "Element <"+tag+"> is not allowed")
		}
		switch tag {
		case "script":
			audit.checkScript(node)
		case "link":
			audit.checkLink(node)
		case "style":
			audit.diagnostics = append(audit.diagnostics, auditStyle(elementText(node), audit.path, false, audit.artifacts)...)
		}
		audit.checkAttributes(node, tag)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		audit.walk(child)
	}
}

func (audit *markupAudit) checkAttributes(node *html.Node, tag string) {
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		value := strings.TrimSpace(attribute.Val)
		switch {
		case strings.HasPrefix(key, "on"):
			// Inline handlers are not auditable script contexts and are blocked
			// by any reasonable Preview CSP. Prototype behaviour belongs in a
			// <script> block or a package-local .js file.
			audit.report("prototype_inline_handler", "HTML event attributes are not allowed; use a package-local script")
		case forbiddenMarkupAttribute(key):
			audit.report("prototype_html_forbidden_attribute", "Attribute "+key+" is not allowed")
		case key == "style":
			audit.diagnostics = append(audit.diagnostics, auditStyle(value, audit.path, true, audit.artifacts)...)
		case key == "action":
			// A form may exist for local validation, but it may not post
			// anywhere. Only an empty or fragment action is accepted.
			if value != "" && value != "#" {
				audit.report("prototype_html_url_unsafe", "form actions must stay inside the prototype")
			}
		case key == "href" && tag != "link":
			audit.checkDocumentLink(value)
		case key == "src" && tag != "script":
			audit.checkMediaSource(value, tag)
		case key == "xlink:href":
			if value != "" && !strings.HasPrefix(value, "#") {
				audit.report("prototype_html_url_unsafe", "xlink references must be fragment-only")
			}
		}
	}
}

func forbiddenMarkupAttribute(key string) bool {
	_, forbidden := forbiddenMarkupAttributes[key]
	return forbidden
}

// checkDocumentLink accepts a fragment, or a link to another prototype page in
// this package. Everything else leaves the package.
func (audit *markupAudit) checkDocumentLink(value string) {
	if value == "" {
		return
	}
	resolved, ok := resolveLocalResource(value, audit.path)
	if !ok {
		audit.report("prototype_html_url_unsafe", "links must target a fragment or another prototype page")
		return
	}
	if strings.HasPrefix(resolved, "#") {
		return
	}
	entry, exists := audit.artifacts[resolved]
	if !exists || (entry.Role != "prototype_entry" && entry.Role != "prototype_page") {
		audit.report("prototype_html_url_unsafe", "links must target a fragment or another prototype page")
	}
}

func (audit *markupAudit) checkMediaSource(value, tag string) {
	if _, allowed := mediaSourceElements[tag]; !allowed {
		audit.report("prototype_html_url_unsafe", "only media elements may load a package asset through src")
		return
	}
	resolved, ok := resolveLocalResource(value, audit.path)
	if !ok || strings.HasPrefix(resolved, "#") {
		audit.report("prototype_html_url_unsafe", "media resources must be package-local assets")
		return
	}
	if entry, exists := audit.artifacts[resolved]; !exists || entry.Role != "asset" {
		audit.report("prototype_html_url_unsafe", "media resources must be package-local assets")
	}
}

func (audit *markupAudit) checkScript(node *html.Node) {
	source, hasSource := attributeValue(node, "src")
	scriptType, _ := attributeValue(node, "type")
	// The deck speaker-notes block is JSON data, not executable script, and the
	// deck contract requires exactly this node. Every other application/json
	// script stays forbidden below.
	if audit.deck && !hasSource && speakerNotesScript(node) {
		return
	}
	if _, allowed := allowedScriptTypes[strings.ToLower(scriptType)]; !allowed {
		audit.report("prototype_script_type_forbidden", "script type "+scriptType+" is not allowed")
		return
	}
	inline := elementText(node)
	if !hasSource {
		if strings.TrimSpace(inline) == "" {
			return
		}
		// A deck's framework runtime announces itself with the ready literal.
		// Byte-frozen platform code is exempt from the generic script audit
		// (it is shipped, not authored); anything claiming to be it but
		// drifting from the skeleton is a contract violation, reported as
		// drift rather than as confusing value-flow errors.
		if audit.deck && strings.Contains(inline, "od:deck-ready") {
			if IsDeckFrameworkScript(inline) {
				return
			}
			audit.report("deck_framework_drift", "the deck framework script must stay byte-identical to the skeleton; only the marked slots may change")
			return
		}
		audit.diagnostics = append(audit.diagnostics, auditScript([]byte(inline), audit.path, audit.deck, audit.artifacts)...)
		return
	}
	if strings.TrimSpace(inline) != "" {
		audit.report("prototype_script_ambiguous", "a script element cannot have both a src and inline code")
	}
	resolved, ok := resolveLocalResource(source, audit.path)
	if !ok || strings.HasPrefix(resolved, "#") || !prototypeScriptPath(resolved) {
		audit.report("prototype_script_external", "scripts must be package-local prototype scripts")
		return
	}
	if entry, exists := audit.artifacts[resolved]; !exists || entry.Role != "prototype_script" {
		audit.report("prototype_script_external", "scripts must be package-local prototype scripts")
	}
}

// speakerNotesScript reports whether node is the deck speaker-notes JSON block.
// The id and the JSON type are both required, so an id-only lookalike stays
// subject to the script policy.
func speakerNotesScript(node *html.Node) bool {
	return strings.EqualFold(attributeText(node, "id"), "speaker-notes") &&
		strings.EqualFold(attributeText(node, "type"), "application/json")
}

func (audit *markupAudit) checkLink(node *html.Node) {
	relation := strings.ToLower(strings.Join(strings.Fields(attributeText(node, "rel")), " "))
	if _, allowed := allowedLinkRelations[relation]; !allowed {
		audit.report("prototype_link_relation_forbidden", "link relation "+relation+" is not allowed")
		return
	}
	href := attributeText(node, "href")
	resolved, ok := resolveLocalResource(href, audit.path)
	if !ok || strings.HasPrefix(resolved, "#") {
		audit.report("prototype_stylesheet_external", "linked resources must be package-local")
		return
	}
	entry, exists := audit.artifacts[resolved]
	if !exists {
		audit.report("prototype_stylesheet_external", "linked resources must be package-local")
		return
	}
	if relation == "stylesheet" && entry.Role != "prototype_style" {
		audit.report("prototype_stylesheet_external", "stylesheet links must target a package-local prototype stylesheet")
		return
	}
	if relation != "stylesheet" && entry.Role != "asset" {
		audit.report("prototype_stylesheet_external", "icon links must target a package-local asset")
	}
}

func (audit *markupAudit) report(code, message string) {
	audit.diagnostics = append(audit.diagnostics, errorDiagnostic(code, audit.path, message))
}

func attributeValue(node *html.Node, key string) (string, bool) {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, key) {
			return strings.TrimSpace(attribute.Val), true
		}
	}
	return "", false
}

func attributeText(node *html.Node, key string) string {
	value, _ := attributeValue(node, key)
	return value
}

func elementText(node *html.Node) string {
	var text strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
	}
	return text.String()
}

// auditSVGAsset rejects active content inside an SVG asset. An SVG is served as
// an image here, but it is still a document that can carry script, animation
// and external references.
func auditSVGAsset(name string, raw []byte, artifacts map[string]ArtifactIndexEntry) []Diagnostic {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	diagnostics := make([]Diagnostic, 0)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return append(diagnostics, errorDiagnostic("asset_svg_invalid", name, "SVG asset is invalid XML"))
		}
		switch value := token.(type) {
		case xml.Directive, xml.ProcInst:
			diagnostics = append(diagnostics, errorDiagnostic("asset_svg_unsafe", name, "SVG directives and processing instructions are not allowed"))
		case xml.StartElement:
			tag := strings.ToLower(value.Name.Local)
			switch tag {
			case "animate", "animatemotion", "animatetransform", "discard", "foreignobject", "iframe", "script", "set", "style":
				diagnostics = append(diagnostics, errorDiagnostic("asset_svg_unsafe", name, "active SVG content is not allowed"))
			}
			for _, attribute := range value.Attr {
				key := strings.ToLower(attribute.Name.Local)
				switch {
				case strings.HasPrefix(key, "on"):
					diagnostics = append(diagnostics, errorDiagnostic("asset_svg_unsafe", name, "SVG event attributes are not allowed"))
				case (key == "href" || key == "src") && !strings.HasPrefix(strings.TrimSpace(attribute.Value), "#"):
					diagnostics = append(diagnostics, errorDiagnostic("asset_svg_unsafe", name, "SVG external resources are not allowed"))
				case key == "style":
					diagnostics = append(diagnostics, auditStyle(attribute.Value, name, true, artifacts)...)
				}
			}
		}
	}
	return diagnostics
}

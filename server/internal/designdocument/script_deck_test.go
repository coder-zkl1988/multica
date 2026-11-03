package designdocument

import (
	"strings"
	"testing"
)

// The deck host channel is the only navigation exception a deck gets:
// window.parent.postMessage. Everything else stays rejected.
func TestAuditScriptDeckPostMessageException(t *testing.T) {
	tests := []struct {
		name   string
		script string
		deck   bool
		code   string
	}{
		{
			name:   "deck posts to window.parent",
			script: `window.parent.postMessage({ type: "od:deck-ready" }, "*");`,
			deck:   true,
		},
		{
			name:   "same call in a non-deck prototype",
			script: `window.parent.postMessage({ type: "od:deck-ready" }, "*");`,
			deck:   false,
			code:   "prototype_script_navigation_forbidden",
		},
		{
			name:   "parent location in a deck",
			script: `window.parent.location = "orders.html";`,
			deck:   true,
			code:   "prototype_script_navigation_forbidden",
		},
		{
			name:   "parent open in a deck",
			script: `window.parent.open("orders.html");`,
			deck:   true,
			code:   "prototype_script_navigation_forbidden",
		},
		{
			name:   "parent document in a deck",
			script: `window.parent.document.body.textContent = "orders";`,
			deck:   true,
			code:   "prototype_script_navigation_forbidden",
		},
		{
			name:   "bare parent in a deck",
			script: `parent.focus();`,
			deck:   true,
			code:   "prototype_script_navigation_forbidden",
		},
		{
			name:   "top in a deck",
			script: `top.location = "orders.html";`,
			deck:   true,
			code:   "prototype_script_navigation_forbidden",
		},
		{
			name:   "frames in a deck",
			script: `frames[0].location = "orders.html";`,
			deck:   true,
			code:   "prototype_script_navigation_forbidden",
		},
		{
			name:   "opener in a deck",
			script: `opener.postMessage("x", "*");`,
			deck:   true,
			code:   "prototype_script_navigation_forbidden",
		},
		{
			name:   "bare location in a deck",
			script: `location.reload();`,
			deck:   true,
			code:   "prototype_script_navigation_forbidden",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := auditScript([]byte(tt.script), prototypeEntryPath, tt.deck, map[string]ArtifactIndexEntry{})
			codes := make([]string, 0, len(diagnostics))
			for _, diagnostic := range diagnostics {
				codes = append(codes, diagnostic.Code)
			}
			if tt.code == "" {
				if len(diagnostics) != 0 {
					t.Fatalf("diagnostics = %v, want none", codes)
				}
				return
			}
			if !strings.Contains(strings.Join(codes, ","), tt.code) {
				t.Fatalf("diagnostics = %v, want %q", codes, tt.code)
			}
		})
	}
}

// The speaker-notes block is data, not script: a deck exempts it from the
// script type policy, every other application/json script stays forbidden, and
// a non-deck prototype keeps forbidding the notes block too.
func TestAuditMarkupSpeakerNotesScriptExemption(t *testing.T) {
	notes := `<script type="application/json" id="speaker-notes">[]</script>`
	other := `<script type="application/json" id="deck-data">[]</script>`
	tests := []struct {
		name    string
		deck    bool
		scripts string
		want    int
	}{
		{name: "deck speaker notes", deck: true, scripts: notes, want: 0},
		{name: "deck notes and other json script", deck: true, scripts: notes + other, want: 1},
		{name: "deck other json script only", deck: true, scripts: other, want: 1},
		{name: "non-deck notes script", deck: false, scripts: notes, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := []byte(`<!doctype html><html data-od-deck-protocol="1"><body>` + tt.scripts + `</body></html>`)
			diagnostics := auditMarkup(source, prototypeEntryPath, tt.deck, map[string]ArtifactIndexEntry{})
			forbidden := 0
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "prototype_script_type_forbidden" {
					forbidden++
				}
			}
			if forbidden != tt.want {
				t.Fatalf("prototype_script_type_forbidden count = %d, want %d (%+v)", forbidden, tt.want, diagnostics)
			}
		})
	}
}

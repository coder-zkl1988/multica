package designdocument

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestDeckSkeletonSelfConsistency locks the shipped skeleton to the contract
// the rest of DC-068 consumes: it is a deck document, its slides project, its
// notes block aligns one entry per slide, the required ids and protocol
// literals are present, and the framework style matches its allowlist digest.
func TestDeckSkeletonSelfConsistency(t *testing.T) {
	skeleton := []byte(DeckSkeletonHTML)
	if !IsDeckDocument(skeleton) {
		t.Fatal("DeckSkeletonHTML is not recognised as a deck document")
	}

	slides, err := DeckSlides(skeleton)
	if err != nil {
		t.Fatalf("DeckSlides(DeckSkeletonHTML) error = %v", err)
	}
	if len(slides) < 1 {
		t.Fatalf("DeckSlides(DeckSkeletonHTML) returned %d slides, want at least 1", len(slides))
	}
	if slides[0].Index != 1 {
		t.Errorf("first slide Index = %d, want 1", slides[0].Index)
	}
	if slides[0].Label != "01 Title" {
		t.Errorf("first slide Label = %q, want %q", slides[0].Label, "01 Title")
	}
	for _, slide := range slides {
		if !DeckScreenLabelPattern.MatchString(slide.Label) {
			t.Errorf("slide %d label %q does not follow the data-screen-label convention", slide.Index, slide.Label)
		}
	}

	notes, err := ExtractSpeakerNotes(DeckSkeletonHTML, len(slides))
	if err != nil {
		t.Fatalf("ExtractSpeakerNotes(DeckSkeletonHTML) error = %v", err)
	}
	if len(notes) != len(slides) {
		t.Fatalf("ExtractSpeakerNotes length = %d, want %d (one per slide)", len(notes), len(slides))
	}
	for index, note := range notes {
		if note != "" {
			t.Errorf("notes[%d] = %q, want empty for the skeleton's empty block", index, note)
		}
	}

	for _, id := range []string{"deck-stage", "deck-prev", "deck-next", "deck-cur", "deck-total"} {
		if !strings.Contains(DeckSkeletonHTML, `id="`+id+`"`) {
			t.Errorf("skeleton is missing id=%q", id)
		}
	}
	for _, literal := range []string{
		DeckProtocolReadyMessageType,
		DeckProtocolStateMessageType,
		DeckProtocolNavigateMessageType,
	} {
		if !strings.Contains(DeckSkeletonHTML, literal) {
			t.Errorf("skeleton is missing protocol literal %q", literal)
		}
	}
	if !strings.Contains(DeckSkeletonHTML, "@media print") {
		t.Error("skeleton is missing the @media print rules")
	}

	// The three documented deviations from upstream.
	if !strings.Contains(DeckSkeletonHTML, "'multica.deck.index'") {
		t.Error("skeleton does not use the fixed multica.deck.index storage key")
	}
	if strings.Contains(DeckSkeletonHTML, "location.pathname") {
		t.Error("skeleton still derives the storage key from location.pathname")
	}
	if !strings.Contains(DeckSkeletonHTML, "window.parent.postMessage") {
		t.Error("skeleton does not post to window.parent")
	}
	if !strings.Contains(DeckSkeletonHTML, "'"+DeckProtocolEscapeMessageType+"'") {
		t.Errorf("skeleton does not send %q", DeckProtocolEscapeMessageType)
	}
	if !strings.Contains(DeckSkeletonHTML, `protocolVersion: 1`) {
		t.Error("skeleton escape message does not carry protocolVersion: 1")
	}

	sum := sha256.Sum256([]byte(DeckFrameworkStyleV1))
	digest := hex.EncodeToString(sum[:])
	if digest != DeckFrameworkStyleSHA256V1 {
		t.Fatalf("framework style digest = %s, want %s", digest, DeckFrameworkStyleSHA256V1)
	}
	if !slices.Contains(DeckFrameworkStyleSHA256, digest) {
		t.Fatalf("framework style digest %s is not in the allowlist", digest)
	}
}

// TestDeckSkeletonFrameworkStyleGolden proves the framework <style> inside the
// skeleton is byte-identical to the frozen upstream style, so a future edit to
// either side fails here instead of drifting silently.
func TestDeckSkeletonFrameworkStyleGolden(t *testing.T) {
	document, err := html.Parse(strings.NewReader(DeckSkeletonHTML))
	if err != nil {
		t.Fatalf("parse skeleton: %v", err)
	}
	style := findFirstElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "style"
	})
	if style == nil {
		t.Fatal("skeleton has no style element")
	}
	if got := elementText(style); got != DeckFrameworkStyleV1 {
		t.Fatalf("skeleton framework <style> differs from DeckFrameworkStyleV1 (got %d bytes, want %d)", len(got), len(DeckFrameworkStyleV1))
	}
}

// TestDeckSkeletonStructure checks the skeleton with the HTML parser: exactly
// one active slide, at least one section.slide, all five required ids, and the
// navigation chrome outside the scaled stage.
func TestDeckSkeletonStructure(t *testing.T) {
	document, err := html.Parse(strings.NewReader(DeckSkeletonHTML))
	if err != nil {
		t.Fatalf("parse skeleton: %v", err)
	}
	var slideSections, activeSections int
	ids := make(map[string]bool)
	var stage *html.Node
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if id := attributeText(node, "id"); id != "" {
				ids[id] = true
				if id == "deck-stage" {
					stage = node
				}
			}
			if node.Data == "section" && hasClass(node, "slide") {
				slideSections++
				if hasClass(node, "active") {
					activeSections++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)

	if slideSections < 1 {
		t.Fatalf("section.slide count = %d, want at least 1", slideSections)
	}
	if activeSections != 1 {
		t.Fatalf("section.slide.active count = %d, want exactly 1", activeSections)
	}
	for _, id := range []string{"deck-stage", "deck-prev", "deck-next", "deck-cur", "deck-total"} {
		if !ids[id] {
			t.Errorf("missing id %q", id)
		}
	}
	if stage == nil {
		t.Fatal("missing #deck-stage")
	}
	for _, id := range []string{"deck-prev", "deck-next", "deck-cur", "deck-total"} {
		if findFirstElement(stage, func(node *html.Node) bool { return attributeText(node, "id") == id }) != nil {
			t.Errorf("#%s is inside #deck-stage, want it outside the scaled stage", id)
		}
	}
}

func TestDeckIsDeckDocument(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "skeleton", source: DeckSkeletonHTML, want: true},
		{name: "double quoted", source: `<html data-od-deck-protocol="1"><body></body></html>`, want: true},
		{name: "single quoted", source: `<html data-od-deck-protocol='1'><body></body></html>`, want: true},
		{name: "unquoted", source: `<html data-od-deck-protocol=1><body></body></html>`, want: true},
		{name: "whitespace around equals", source: "<html\n  data-od-deck-protocol = \"1\" ><body></body></html>", want: true},
		{name: "uppercase attribute", source: `<HTML DATA-OD-DECK-PROTOCOL="1"><BODY></BODY></HTML>`, want: true},
		{name: "marker on body", source: `<html><body data-od-deck-protocol="1"></body></html>`, want: false},
		{name: "marker on section", source: `<html><body><section data-od-deck-protocol="1"></section></body></html>`, want: false},
		{name: "marker inside comment", source: `<html><body><!-- <html data-od-deck-protocol="1"> --></body></html>`, want: false},
		{name: "marker inside script text", source: `<html><body><script>var marker = 'data-od-deck-protocol="1"';</script></body></html>`, want: false},
		{name: "wrong value", source: `<html data-od-deck-protocol="2"><body></body></html>`, want: false},
		{name: "empty", source: ``, want: false},
		{name: "garbage", source: "\x00\x01 not html at all <", want: false},
		{name: "plain text", source: `data-od-deck-protocol="1"`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsDeckDocument([]byte(test.source)); got != test.want {
				t.Fatalf("IsDeckDocument() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDeckSlidesProjection(t *testing.T) {
	source := `<!doctype html>
<html data-od-deck-protocol="1"><body>
  <section class="slide" data-screen-label="00 outside">outside</section>
  <div class="deck-stage" id="deck-stage">
    <section class="hero slide active" id="cover" data-screen-label="01 Cover"></section>
    <section class="slide">
      <section class="slide" data-screen-label="99 Nested"></section>
    </section>
    <section class="slide" data-screen-label="03·Agenda"></section>
    <section data-screen-label="04 Not a slide"></section>
  </div>
</body></html>`
	slides, err := DeckSlides([]byte(source))
	if err != nil {
		t.Fatalf("DeckSlides() error = %v", err)
	}
	want := []DeckSlide{
		{Index: 1, Label: "01 Cover", ID: "cover"},
		{Index: 2, Label: "", ID: ""},
		{Index: 3, Label: "03·Agenda", ID: ""},
	}
	if len(slides) != len(want) {
		t.Fatalf("len(slides) = %d, want %d: %+v", len(slides), len(want), slides)
	}
	for index := range want {
		if slides[index] != want[index] {
			t.Errorf("slides[%d] = %+v, want %+v", index, slides[index], want[index])
		}
	}
}

func TestDeckSlidesWithoutStage(t *testing.T) {
	source := `<html data-od-deck-protocol="1"><body>
  <section class="slide" data-screen-label="01 One"></section>
  <section class="slide" data-screen-label="02 Two"></section>
</body></html>`
	slides, err := DeckSlides([]byte(source))
	if err != nil {
		t.Fatalf("DeckSlides() error = %v", err)
	}
	if len(slides) != 2 {
		t.Fatalf("len(slides) = %d, want 2", len(slides))
	}
	if slides[1].Index != 2 || slides[1].Label != "02 Two" {
		t.Errorf("slides[1] = %+v, want index 2 label %q", slides[1], "02 Two")
	}
}

func TestDeckExtractSpeakerNotes(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		slideCount int
		want       []string
		wantErr    error
	}{
		{
			name:       "json array",
			source:     speakerNotesDocument(`["one", "two"]`),
			slideCount: 2,
			want:       []string{"one", "two"},
		},
		{
			name:       "notes object",
			source:     speakerNotesDocument(`{"notes": ["a", "b"], "version": 1}`),
			slideCount: 2,
			want:       []string{"a", "b"},
		},
		{
			name:       "fewer entries than slides padded",
			source:     speakerNotesDocument(`["only"]`),
			slideCount: 3,
			want:       []string{"only", "", ""},
		},
		{
			name:       "extra entries preserved",
			source:     speakerNotesDocument(`["a", "b", "c"]`),
			slideCount: 1,
			want:       []string{"a", "b", "c"},
		},
		{
			name:       "null and non-string items normalized",
			source:     speakerNotesDocument(`[null, 2, true, {"a": 1}, ["x"], "  spaced  "]`),
			slideCount: 6,
			want:       []string{"", "2", "true", `{"a":1}`, `["x"]`, "spaced"},
		},
		{
			name:       "empty array pads",
			source:     speakerNotesDocument(`[]`),
			slideCount: 2,
			want:       []string{"", ""},
		},
		{
			name:       "empty body pads",
			source:     speakerNotesDocument(``),
			slideCount: 2,
			want:       []string{"", ""},
		},
		{
			name:       "null payload pads",
			source:     speakerNotesDocument(`null`),
			slideCount: 1,
			want:       []string{""},
		},
		{
			name:    "invalid json",
			source:  speakerNotesDocument(`[`),
			wantErr: ErrSpeakerNotesInvalid,
		},
		{
			name:    "wrong top-level shape",
			source:  speakerNotesDocument(`"nope"`),
			wantErr: ErrSpeakerNotesInvalid,
		},
		{
			name:    "object without notes",
			source:  speakerNotesDocument(`{"items": []}`),
			wantErr: ErrSpeakerNotesInvalid,
		},
		{
			name:    "notes not an array",
			source:  speakerNotesDocument(`{"notes": "nope"}`),
			wantErr: ErrSpeakerNotesInvalid,
		},
		{
			name:    "missing block",
			source:  `<html><body><p>no notes</p></body></html>`,
			wantErr: ErrSpeakerNotesNotFound,
		},
		{
			name:    "block inside comment",
			source:  `<html><body><!-- <script id="speaker-notes">["x"]</script> --></body></html>`,
			wantErr: ErrSpeakerNotesNotFound,
		},
		{
			name:    "other script id",
			source:  speakerNotesDocumentWithID("deck-data", "[]"),
			wantErr: ErrSpeakerNotesNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notes, err := ExtractSpeakerNotes(test.source, test.slideCount)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(notes, test.want) {
				t.Fatalf("notes = %q, want %q", notes, test.want)
			}
		})
	}
}

func TestDeckSpeakerNotesErrorSentinelsAreDistinct(t *testing.T) {
	_, missingErr := ExtractSpeakerNotes(`<html><body></body></html>`, 1)
	_, invalidErr := ExtractSpeakerNotes(speakerNotesDocument(`[`), 1)
	if errors.Is(missingErr, ErrSpeakerNotesInvalid) {
		t.Error("missing block error is also classified as an invalid payload")
	}
	if errors.Is(invalidErr, ErrSpeakerNotesNotFound) {
		t.Error("invalid payload error is also classified as a missing block")
	}
}

func TestDeckNormalizeSpeakerNotes(t *testing.T) {
	tests := []struct {
		name       string
		notes      []string
		slideCount int
		want       []string
	}{
		{name: "nil pads", notes: nil, slideCount: 3, want: []string{"", "", ""}},
		{name: "short pads", notes: []string{"a"}, slideCount: 3, want: []string{"a", "", ""}},
		{name: "long preserved", notes: []string{"a", "b", "c"}, slideCount: 1, want: []string{"a", "b", "c"}},
		{name: "empty and zero", notes: []string{}, slideCount: 0, want: []string{}},
		{name: "negative slide count", notes: []string{"a"}, slideCount: -1, want: []string{"a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeSpeakerNotes(test.notes, test.slideCount); !slices.Equal(got, test.want) {
				t.Fatalf("NormalizeSpeakerNotes() = %q, want %q", got, test.want)
			}
		})
	}
}

func speakerNotesDocument(body string) string {
	return speakerNotesDocumentWithID("speaker-notes", body)
}

func speakerNotesDocumentWithID(id, body string) string {
	return `<!doctype html><html data-od-deck-protocol="1"><body>` +
		`<script type="application/json" id="` + id + `">` + body + `</script>` +
		`</body></html>`
}

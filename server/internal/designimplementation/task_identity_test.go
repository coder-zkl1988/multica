package designimplementation

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestTaskIdentityReadsNewMultiPageAndHistoricalSinglePageMarkers(t *testing.T) {
	t.Parallel()

	for name, identity := range map[string]TaskIdentity{
		"multi page": {
			AssetID: "asset-1", DesignRef: "design-1", RevisionID: "revision-1",
			ContentDigest: "sha256:digest", FrameRefs: []string{"page-1", "page-2"}, ProjectResourceID: "repository-1",
		},
		"historical single page": {
			AssetID: "asset-1", DesignRef: "design-1", RevisionID: "revision-1",
			ContentDigest: "sha256:digest", FrameRef: "page-1", ProjectResourceID: "repository-1",
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(identity)
			if err != nil {
				t.Fatal(err)
			}
			parsed, ok := ParseTaskIdentity(TaskTrigger + "\n" + TaskMarkerPrefix + url.PathEscape(string(raw)) + " -->")
			if !ok || !sameStrings(parsed.SelectedFrameRefs(), identity.SelectedFrameRefs()) {
				t.Fatalf("parsed identity = %+v, ok = %v", parsed, ok)
			}
		})
	}
}

func TestTaskIdentityRejectsDuplicatePages(t *testing.T) {
	t.Parallel()
	identity := TaskIdentity{
		AssetID: "asset-1", DesignRef: "design-1", RevisionID: "revision-1",
		ContentDigest: "sha256:digest", FrameRefs: []string{"page-1", "page-1"}, ProjectResourceID: "repository-1",
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ParseTaskIdentity(TaskMarkerPrefix + url.PathEscape(string(raw)) + " -->"); ok {
		t.Fatal("duplicate page identity was accepted")
	}
}

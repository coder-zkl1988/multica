package designimplementation

import (
	"encoding/json"
	"net/url"
	"strings"
)

const TaskMarkerPrefix = "<!-- multica-design-implementation:"
const TaskTrigger = "【Design Center 设计稿一键还原】"

type TaskIdentity struct {
	AssetID           string   `json:"assetId"`
	DesignRef         string   `json:"designRef"`
	RevisionID        string   `json:"revisionId"`
	ContentDigest     string   `json:"contentDigest"`
	FrameRefs         []string `json:"frameRefs,omitempty"`
	FrameRef          string   `json:"frameRef,omitempty"` // Historical v1 marker reader; new markers write frameRefs.
	ProjectResourceID string   `json:"projectResourceId"`
}

func ParseTaskIdentity(content string) (TaskIdentity, bool) {
	start := strings.Index(content, TaskMarkerPrefix)
	if start < 0 {
		return TaskIdentity{}, false
	}
	valueStart := start + len(TaskMarkerPrefix)
	end := strings.Index(content[valueStart:], "-->")
	if end < 0 {
		return TaskIdentity{}, false
	}
	decoded, err := url.PathUnescape(strings.TrimSpace(content[valueStart : valueStart+end]))
	if err != nil {
		return TaskIdentity{}, false
	}
	var identity TaskIdentity
	if json.Unmarshal([]byte(decoded), &identity) != nil || identity.AssetID == "" || identity.DesignRef == "" ||
		identity.RevisionID == "" || identity.ContentDigest == "" || identity.ProjectResourceID == "" ||
		!uniqueNonEmpty(identity.SelectedFrameRefs()) {
		return TaskIdentity{}, false
	}
	return identity, true
}

func IsTask(content string) bool {
	return strings.Contains(content, TaskTrigger)
}

func ClaimMatchesTaskIdentity(claim ReferenceClaim, identity TaskIdentity) bool {
	return claim.ProjectResourceID == identity.ProjectResourceID && claim.DesignRef == identity.DesignRef &&
		claim.RevisionID == identity.RevisionID && claim.ContentDigest == identity.ContentDigest &&
		sameStrings(claim.FrameRefs, identity.SelectedFrameRefs())
}

func (identity TaskIdentity) SelectedFrameRefs() []string {
	if len(identity.FrameRefs) > 0 {
		return identity.FrameRefs
	}
	if identity.FrameRef != "" {
		return []string{identity.FrameRef}
	}
	return nil
}

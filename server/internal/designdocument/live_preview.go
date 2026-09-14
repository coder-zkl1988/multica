package designdocument

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"strings"
)

const LivePreviewMaxBytes = 4 << 20
const LivePreviewMaxFiles = 256

// LivePreview is an unapproved, read-only snapshot, never a document revision.
type LivePreview struct {
	EntryPath string            `json:"entry_path"`
	Files     map[string][]byte `json:"files"`
}

func LivePreviewPathAllowed(name string) bool {
	if name != path.Clean(name) || strings.ContainsAny(name, "\\\x00") || (!strings.HasPrefix(name, "prototype/") && !strings.HasPrefix(name, "assets/")) {
		return false
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".css", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".woff", ".woff2", ".ttf":
		return true
	default:
		return false
	}
}

func (p LivePreview) Validate() error {
	if len(p.Files) == 0 || len(p.Files) > LivePreviewMaxFiles {
		return errors.New("invalid preview file count")
	}
	if !strings.HasPrefix(p.EntryPath, "prototype/") || path.Ext(p.EntryPath) != ".html" || len(p.Files[p.EntryPath]) == 0 {
		return errors.New("preview entry is missing")
	}
	total := 0
	for name, data := range p.Files {
		if !LivePreviewPathAllowed(name) {
			return errors.New("invalid preview file path")
		}
		total += len(data)
		if total > LivePreviewMaxBytes {
			return errors.New("preview exceeds size limit")
		}
	}
	return nil
}

func (p LivePreview) Digest() (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

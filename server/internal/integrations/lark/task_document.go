package lark

import (
	"context"
	"errors"
	"strings"
)

// TaskDocumentReader exposes a bounded, credential-free document read to an
// already-authorized Feishu chat task. Credentials stay server-side; callers
// provide only the docx token extracted from a link in their current topic.
type TaskDocumentReader interface {
	ReadDocxText(context.Context, InstallationCredentials, string) (DocxText, error)
}

// DocxText is a stable plain-text projection for agent review. It preserves
// document order and headings; unsupported rich media is omitted rather than
// guessed. The block snapshot remains revision-consistent.
type DocxText struct {
	DocumentID string `json:"document_id"`
	Revision   int64  `json:"revision"`
	Text       string `json:"text"`
}

func (c *httpAPIClient) ReadDocxText(ctx context.Context, creds InstallationCredentials, docID string) (DocxText, error) {
	snapshot, err := c.prdSnapshot(ctx, creds, docID)
	if err != nil {
		return DocxText{}, err
	}
	root, ok := snapshot.Blocks[docID]
	if !ok {
		return DocxText{}, errors.New("docx root block is unavailable")
	}
	var out strings.Builder
	seen := make(map[string]bool, len(snapshot.Blocks))
	var walk func(string) error
	walk = func(id string) error {
		if seen[id] {
			return errors.New("docx block tree contains a cycle")
		}
		seen[id] = true
		block, ok := snapshot.Blocks[id]
		if !ok {
			return errors.New("docx block tree references a missing block")
		}
		if heading, ok := block.heading(); ok && strings.TrimSpace(heading) != "" {
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(strings.Repeat("#", block.Type-2))
			out.WriteByte(' ')
			out.WriteString(heading)
			out.WriteByte('\n')
		} else if text, ok := block.plainText(); ok && strings.TrimSpace(text) != "" {
			out.WriteString(text)
			out.WriteByte('\n')
		}
		for _, child := range block.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range root.Children {
		if err := walk(child); err != nil {
			return DocxText{}, err
		}
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return DocxText{}, errors.New("docx contains no readable text")
	}
	return DocxText{DocumentID: docID, Revision: snapshot.Revision, Text: text}, nil
}

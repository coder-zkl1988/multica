package lark

import (
	"context"
	"strings"
	"testing"
)

func TestReadDocxTextPreservesDocumentOrder(t *testing.T) {
	f, client := newPRDDocumentFake(t)
	reader, ok := client.(TaskDocumentReader)
	if !ok {
		t.Fatal("HTTP client does not expose task document reader")
	}
	document, err := reader.ReadDocxText(context.Background(), testCreds(), "copy1")
	if err != nil {
		t.Fatal(err)
	}
	if document.DocumentID != "copy1" || document.Revision != f.revision {
		t.Fatalf("document identity mismatch: %+v", document)
	}
	want := []string{"# 需求背景", "【待确认】原模板提示", "## 项目目标", "# 附录"}
	position := -1
	for _, text := range want {
		next := strings.Index(document.Text, text)
		if next <= position {
			t.Fatalf("%q missing or out of order in %q", text, document.Text)
		}
		position = next
	}
}

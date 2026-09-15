package handler

import "testing"

func TestChatDocxID(t *testing.T) {
	for _, raw := range []string{
		"https://soyoung.feishu.cn/docx/XL6UdYjLIo2tBcxlCYTcKjZjnAe",
		"https://tenant.larksuite.com/docx/doc_token-1#section",
	} {
		if id, ok := chatDocxID(raw); !ok || id == "" {
			t.Fatalf("valid docx URL rejected: %q", raw)
		}
	}
	for _, raw := range []string{
		"http://soyoung.feishu.cn/docx/XL6UdYjLIo2tBcxlCYTcKjZjnAe",
		"https://user@soyoung.feishu.cn/docx/token",
		"https://soyoung.feishu.cn:443/docx/token",
		"https://soyoung.feishu.cn/wiki/token",
		"https://soyoung.feishu.cn/docx/token?redirect=https://evil.invalid",
		"https://soyoung.feishu.cn/docx/a/b",
	} {
		if _, ok := chatDocxID(raw); ok {
			t.Fatalf("unsafe docx URL accepted: %q", raw)
		}
	}
}

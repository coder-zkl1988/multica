package handler

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestTaskTokenExpiry(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	request := httptest.NewRequest("POST", "/api/daemon/tasks/claim", nil)

	if got := taskTokenExpiry(request, now); !got.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("unbounded expiry = %v", got)
	}
	parentExpiry := now.Add(45 * time.Minute)
	request.Header.Set("X-Auth-Expires-At", parentExpiry.Format(time.RFC3339))
	if got := taskTokenExpiry(request, now); !got.Equal(parentExpiry) {
		t.Fatalf("parent-bounded expiry = %v, want %v", got, parentExpiry)
	}
	request.Header.Set("X-Auth-Expires-At", now.Add(48*time.Hour).Format(time.RFC3339))
	if got := taskTokenExpiry(request, now); !got.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("24-hour bounded expiry = %v", got)
	}
}

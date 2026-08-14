package main

import (
	"strings"
	"testing"
	"time"
)

func TestRunUpdateRejectsNonPositiveDownloadTimeout(t *testing.T) {
	orig := updateDownloadTimeout
	updateDownloadTimeout = 0
	t.Cleanup(func() { updateDownloadTimeout = orig })

	err := runUpdate(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "download timeout must be greater than zero") {
		t.Fatalf("runUpdate error = %v, want download timeout validation", err)
	}
}

func TestUpdateCommandRegistersDownloadTimeoutFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("download-timeout")
	if flag == nil {
		t.Fatal("updateCmd is missing --download-timeout")
	}
	if got := flag.DefValue; got != (120 * time.Second).String() {
		t.Fatalf("--download-timeout default = %q, want %q", got, (120 * time.Second).String())
	}
}

func TestShouldUpdateCLI(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "older sso build", current: "v0.4.23-sso.9", latest: "v0.4.23-sso.10", want: true},
		{name: "same version", current: "v0.4.23-sso.10", latest: "v0.4.23-sso.10", want: false},
		{name: "newer current version", current: "v0.4.23-sso.10", latest: "v0.4.23-sso.9", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUpdateCLI(tt.current, tt.latest); got != tt.want {
				t.Fatalf("shouldUpdateCLI(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

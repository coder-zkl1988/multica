package service

import "testing"

func TestNormalizePMOOwnerEmail(t *testing.T) {
	tests := []struct {
		name, externalID, want string
	}{
		{"bare account", "yanmeichen", "yanmeichen@soyoung.com"},
		{"trim and lowercase email", " YanMeiChen@Soyoung.com ", "yanmeichen@soyoung.com"},
		{"safe punctuation", "yan.mei_chen-1", "yan.mei_chen-1@soyoung.com"},
		{"empty", "   ", ""},
		{"display name is not guessed", "严美辰", ""},
		{"spaces are invalid", "yan mei chen", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePMOOwnerEmail(tt.externalID); got != tt.want {
				t.Fatalf("normalizePMOOwnerEmail(%q) = %q, want %q", tt.externalID, got, tt.want)
			}
		})
	}
}

func TestMatchPMOAssigneeMappingsKeepsExplicitAndUsesOriginalExternalIDKey(t *testing.T) {
	owners := map[string]*PMOExternalOwner{
		"YanMeiChen":           {ExternalID: "YanMeiChen"},
		"manual-owner":         {ExternalID: "manual-owner"},
		"yanmeichenyanmeichen": {ExternalID: "yanmeichenyanmeichen"},
		"严美辰":                  {ExternalID: "严美辰"},
	}
	memberEmailToUserID := map[string]string{
		"yanmeichen@soyoung.com": "user-a",
	}
	existing := map[string]string{
		"manual-owner": "user-b",
	}

	got := matchPMOAssigneeMappings(owners, memberEmailToUserID, existing)

	if got["YanMeiChen"] != "user-a" {
		t.Fatalf("mappings[%q] = %q, want user-a", "YanMeiChen", got["YanMeiChen"])
	}
	if got["manual-owner"] != "user-b" {
		t.Fatalf("mappings[%q] = %q, want user-b", "manual-owner", got["manual-owner"])
	}
	if _, ok := got["yanmeichenyanmeichen"]; ok {
		t.Fatalf("bad duplicated value must not be mapped, got %+v", got)
	}
	if _, ok := got["严美辰"]; ok {
		t.Fatalf("display name must not be guessed, got %+v", got)
	}
}

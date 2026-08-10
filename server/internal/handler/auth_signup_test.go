package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newTestHandler(cfg Config) *Handler {
	return &Handler{cfg: cfg}
}

func TestSignupGating(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		email   string
		isNew   bool
		wantErr bool
	}{
		{"allow_signup_true_new", Config{AllowSignup: true}, "a@x.com", true, false},
		{"allow_signup_false_new", Config{AllowSignup: false}, "a@x.com", true, true},
		{"allow_signup_false_existing", Config{AllowSignup: false}, "a@x.com", false, false},
		{"domain_allowlist_match", Config{AllowedEmailDomains: []string{"company.com"}}, "user@company.com", true, false},
		{"domain_allowlist_mismatch", Config{AllowedEmailDomains: []string{"company.com"}}, "user@other.com", true, true},
		{"email_allowlist_match", Config{AllowedEmails: []string{"boss@x.com"}}, "boss@x.com", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newTestHandler(tt.cfg).checkSignupAllowed(tt.email, tt.isNew)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestFindOrCreateUserGating(t *testing.T) {
	t.Run("new_user_blocked", func(t *testing.T) {
		h := newTestHandler(Config{})
		h.Queries = db.New(&mockDB{getUserErr: pgx.ErrNoRows})

		_, isNew, err := h.findOrCreateUser(context.Background(), "new@blocked.com")
		if err == nil || isNew || !strings.Contains(err.Error(), "registration is disabled") {
			t.Fatalf("findOrCreateUser() = isNew %v, err %v", isNew, err)
		}
	})

	t.Run("existing_user_allowed", func(t *testing.T) {
		h := newTestHandler(Config{})
		h.Queries = db.New(&mockDB{})

		_, isNew, err := h.findOrCreateUser(context.Background(), "existing@test.com")
		if err != nil || isNew {
			t.Fatalf("findOrCreateUser() = isNew %v, err %v", isNew, err)
		}
	})
}

func TestFindOrCreateUserRejectsServiceAccount(t *testing.T) {
	const email = "legacy-auth-service-account@multica.ai"
	ctx := context.Background()
	_, err := testPool.Exec(ctx, `
		INSERT INTO "user" (name, email, account_kind)
		VALUES ('Service Account', $1, 'service')
		ON CONFLICT (email) DO UPDATE SET account_kind = 'service'
	`, email)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email) })

	_, _, err = testHandler.findOrCreateUser(ctx, email)
	if err == nil || !strings.Contains(err.Error(), "service account") {
		t.Fatalf("findOrCreateUser() error = %v", err)
	}
}

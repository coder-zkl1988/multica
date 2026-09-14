package designimplementation

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

const (
	ReferenceSchemaV1 = "multica.design-implementation-reference/v1"
	referencePrefix   = "implementation_v1_"
	referenceLifetime = 24 * time.Hour
)

var ErrReferenceInvalid = errors.New("design implementation reference is invalid")

type ReferenceClaim struct {
	SchemaVersion     string   `json:"schema_version"`
	WorkspaceID       string   `json:"workspace_id"`
	ProjectID         string   `json:"project_id"`
	IssueID           string   `json:"issue_id"`
	TaskID            string   `json:"task_id,omitempty"`
	ProjectResourceID string   `json:"project_resource_id"`
	DesignRef         string   `json:"design_ref"`
	RevisionID        string   `json:"revision_id"`
	ContentDigest     string   `json:"content_digest"`
	FrameRefs         []string `json:"frame_refs"`
	ExpiresAt         int64    `json:"expires_at"`
}

func MintReference(claim ReferenceClaim, now time.Time) (string, error) {
	claim.SchemaVersion = ReferenceSchemaV1
	claim.ExpiresAt = now.Add(referenceLifetime).Unix()
	if err := validateReferenceClaim(claim, now); err != nil {
		return "", err
	}
	plaintext, err := json.Marshal(claim)
	if err != nil {
		return "", err
	}
	aead, err := referenceAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nil, nonce, plaintext, []byte(referencePrefix))
	return referencePrefix + base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func OpenReference(raw string, now time.Time) (ReferenceClaim, error) {
	if !strings.HasPrefix(raw, referencePrefix) {
		return ReferenceClaim{}, ErrReferenceInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, referencePrefix))
	if err != nil {
		return ReferenceClaim{}, ErrReferenceInvalid
	}
	aead, err := referenceAEAD()
	if err != nil || len(payload) < aead.NonceSize() {
		return ReferenceClaim{}, ErrReferenceInvalid
	}
	plaintext, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], []byte(referencePrefix))
	if err != nil {
		return ReferenceClaim{}, ErrReferenceInvalid
	}
	var claim ReferenceClaim
	if err := json.Unmarshal(plaintext, &claim); err != nil || validateReferenceClaim(claim, now) != nil {
		return ReferenceClaim{}, ErrReferenceInvalid
	}
	return claim, nil
}

func validateReferenceClaim(claim ReferenceClaim, now time.Time) error {
	if claim.SchemaVersion != ReferenceSchemaV1 || claim.ExpiresAt <= now.Unix() ||
		strings.TrimSpace(claim.WorkspaceID) == "" || strings.TrimSpace(claim.ProjectID) == "" ||
		strings.TrimSpace(claim.IssueID) == "" || strings.TrimSpace(claim.ProjectResourceID) == "" ||
		strings.TrimSpace(claim.DesignRef) == "" || strings.TrimSpace(claim.RevisionID) == "" ||
		strings.TrimSpace(claim.ContentDigest) == "" || !uniqueNonEmpty(claim.FrameRefs) {
		return ErrReferenceInvalid
	}
	return nil
}

func referenceAEAD() (cipher.AEAD, error) {
	secret := strings.TrimSpace(os.Getenv("MULTICA_DESIGN_ASSET_REF_KEY"))
	if secret == "" {
		secret = string(auth.JWTSecret())
	}
	if secret == "" {
		return nil, errors.New("design implementation reference signing key is not configured")
	}
	key := sha256.Sum256([]byte("multica-design-implementation-reference-v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func uniqueNonEmpty(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

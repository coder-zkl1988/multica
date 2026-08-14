package designdocument

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/multica-ai/multica/server/internal/storage"
)

type ArchiveReference struct {
	ObjectKey     string `json:"object_key"`
	ContentDigest string `json:"content_digest"`
}

func ArchiveObjectKey(binding Binding, contentDigest string) (string, error) {
	if err := validateBinding(binding); err != nil {
		return "", err
	}
	if !validDigest(contentDigest) {
		return "", errors.New("design document content digest is invalid")
	}
	segments := []string{binding.WorkspaceID, binding.ProjectID, binding.DocumentID, binding.RevisionID}
	for _, segment := range segments {
		if segment == "." || segment == ".." || strings.ContainsAny(segment, `:/\`) {
			return "", errors.New("design document object key identity is invalid")
		}
	}
	return fmt.Sprintf("design-documents/%s/%s/%s/%s/%s.zip",
		binding.WorkspaceID, binding.ProjectID, binding.DocumentID, binding.RevisionID,
		strings.TrimPrefix(contentDigest, "sha256:")), nil
}

func UploadArchive(ctx context.Context, store storage.Storage, archive []byte, expected Binding) (ArchiveReference, error) {
	if store == nil {
		return ArchiveReference{}, errors.New("design document archive storage is nil")
	}
	validated, err := ValidateArchive(archive, expected)
	if err != nil {
		return ArchiveReference{}, err
	}
	key, err := ArchiveObjectKey(expected, validated.Manifest.ContentDigest)
	if err != nil {
		return ArchiveReference{}, err
	}
	digestHex := strings.TrimPrefix(validated.Manifest.ContentDigest, "sha256:")
	if _, err := store.Upload(ctx, key, archive, "application/zip", digestHex+".zip"); err != nil {
		return ArchiveReference{}, fmt.Errorf("upload design document archive: %w", err)
	}
	return ArchiveReference{ObjectKey: key, ContentDigest: validated.Manifest.ContentDigest}, nil
}

func LoadArchive(ctx context.Context, store storage.Storage, key string, expected Binding) ([]byte, ValidatedPackage, error) {
	if store == nil {
		return nil, ValidatedPackage{}, errors.New("design document archive storage is nil")
	}
	reader, err := store.GetReader(ctx, key)
	if err != nil {
		return nil, ValidatedPackage{}, fmt.Errorf("get design document archive: %w", err)
	}
	if reader == nil {
		return nil, ValidatedPackage{}, errors.New("get design document archive: storage returned a nil reader")
	}
	archive, readErr := io.ReadAll(io.LimitReader(reader, maxArchiveBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return nil, ValidatedPackage{}, fmt.Errorf("read design document archive: %w", errors.Join(readErr, closeErr))
	}
	if int64(len(archive)) > maxArchiveBytes {
		return nil, ValidatedPackage{}, errors.New("design document archive exceeds the read limit")
	}
	validated, err := ValidateArchive(archive, expected)
	if err != nil {
		return nil, ValidatedPackage{}, err
	}
	return archive, validated, nil
}

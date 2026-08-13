package designdocument

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestUploadArchiveValidatesBeforeUsingStableObjectKey(t *testing.T) {
	binding := validBinding()
	collected, err := CollectDirectory(copyFixture(t), binding)
	if err != nil {
		t.Fatal(err)
	}
	store := &archiveStoreFake{}

	first, err := UploadArchive(context.Background(), store, collected.Archive, binding)
	if err != nil {
		t.Fatal(err)
	}
	second, err := UploadArchive(context.Background(), store, collected.Archive, binding)
	if err != nil {
		t.Fatal(err)
	}
	digestHex := strings.TrimPrefix(collected.Manifest.ContentDigest, "sha256:")
	wantKey := "design-documents/ws-1/project-1/doc-1/rev-1/" + digestHex + ".zip"
	if first != (ArchiveReference{ObjectKey: wantKey, ContentDigest: collected.Manifest.ContentDigest}) {
		t.Fatalf("reference = %#v", first)
	}
	if second != first || len(store.uploads) != 2 {
		t.Fatalf("retry = %#v, uploads = %d", second, len(store.uploads))
	}
	for _, upload := range store.uploads {
		if upload.key != wantKey || upload.contentType != "application/zip" || upload.filename != digestHex+".zip" || !bytes.Equal(upload.data, collected.Archive) {
			t.Fatalf("unexpected upload: %#v", upload)
		}
	}
	assertNoArchiveDelete(t, store)
}

func TestUploadArchiveRejectsInvalidInputWithoutReference(t *testing.T) {
	binding := validBinding()
	collected, err := CollectDirectory(copyFixture(t), binding)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		archive []byte
		binding Binding
	}{
		{name: "invalid archive", archive: []byte("not a zip"), binding: binding},
		{name: "binding mismatch", archive: collected.Archive, binding: func() Binding { v := binding; v.RevisionID = "rev-2"; return v }()},
		{name: "digest mismatch", archive: func() []byte {
			files := unzip(t, collected.Archive)
			mutateJSONBytes(t, files, "manifest.json", func(v map[string]any) { v["content_digest"] = sha('f') })
			return zipFiles(t, files)
		}(), binding: binding},
		{name: "unsafe key segment", archive: func() []byte {
			unsafeBinding := binding
			unsafeBinding.WorkspaceID = "ws/other"
			packageWithUnsafeBinding, collectErr := CollectDirectory(copyFixture(t), unsafeBinding)
			if collectErr != nil {
				t.Fatal(collectErr)
			}
			return packageWithUnsafeBinding.Archive
		}(), binding: func() Binding { v := binding; v.WorkspaceID = "ws/other"; return v }()},
		{name: "colon in key segment", archive: func() []byte {
			unsafeBinding := binding
			unsafeBinding.WorkspaceID = "ws:1"
			packageWithUnsafeBinding, collectErr := CollectDirectory(copyFixture(t), unsafeBinding)
			if collectErr != nil {
				t.Fatal(collectErr)
			}
			return packageWithUnsafeBinding.Archive
		}(), binding: func() Binding { v := binding; v.WorkspaceID = "ws:1"; return v }()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &archiveStoreFake{}
			got, err := UploadArchive(context.Background(), store, tt.archive, tt.binding)
			if err == nil || got != (ArchiveReference{}) {
				t.Fatalf("reference = %#v, error = %v", got, err)
			}
			if len(store.uploads) != 0 {
				t.Fatalf("invalid archive was uploaded: %#v", store.uploads)
			}
			assertNoArchiveDelete(t, store)
		})
	}

	if got, err := UploadArchive(context.Background(), nil, collected.Archive, binding); err == nil || got != (ArchiveReference{}) {
		t.Fatalf("nil store reference = %#v, error = %v", got, err)
	}

	store := &archiveStoreFake{uploadErr: errors.New("upload failed")}
	if got, err := UploadArchive(context.Background(), store, collected.Archive, binding); !errors.Is(err, store.uploadErr) || got != (ArchiveReference{}) {
		t.Fatalf("failed upload reference = %#v, error = %v", got, err)
	}
	assertNoArchiveDelete(t, store)
}

func TestLoadArchiveReadsBoundedAndRevalidates(t *testing.T) {
	binding := validBinding()
	collected, err := CollectDirectory(copyFixture(t), binding)
	if err != nil {
		t.Fatal(err)
	}
	store := &archiveStoreFake{reader: io.NopCloser(bytes.NewReader(collected.Archive))}

	raw, validated, err := LoadArchive(context.Background(), store, "design-documents/archive.zip", binding)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, collected.Archive) || validated.Manifest.ContentDigest != collected.Manifest.ContentDigest {
		t.Fatalf("loaded archive or manifest differs")
	}
	if store.getKey != "design-documents/archive.zip" {
		t.Fatalf("GetReader key = %q", store.getKey)
	}
	assertNoArchiveDelete(t, store)
}

func TestLoadArchiveRejectsStorageAndArchiveFailures(t *testing.T) {
	binding := validBinding()
	collected, err := CollectDirectory(copyFixture(t), binding)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), collected.Archive...)
	tampered[len(tampered)/2] ^= 0xff
	readFailure := errors.New("read failed")
	closeFailure := errors.New("close failed")
	getFailure := errors.New("get failed")
	tests := []struct {
		name  string
		store *archiveStoreFake
		want  error
	}{
		{name: "get", store: &archiveStoreFake{getErr: getFailure}, want: getFailure},
		{name: "read", store: &archiveStoreFake{reader: &scriptedArchiveReader{readErr: readFailure}}, want: readFailure},
		{name: "close", store: &archiveStoreFake{reader: &scriptedArchiveReader{data: collected.Archive, closeErr: closeFailure}}, want: closeFailure},
		{name: "oversize", store: &archiveStoreFake{reader: &scriptedArchiveReader{remaining: maxArchiveBytes + 1}}},
		{name: "tampered", store: &archiveStoreFake{reader: io.NopCloser(bytes.NewReader(tampered))}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, validated, err := LoadArchive(context.Background(), tt.store, "archive.zip", binding)
			if err == nil || raw != nil || validated.Manifest.SchemaVersion != "" || validated.Audit.SchemaVersion != "" {
				t.Fatalf("raw bytes = %d, validated = %#v, error = %v", len(raw), validated, err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			assertNoArchiveDelete(t, tt.store)
		})
	}

	if raw, validated, err := LoadArchive(context.Background(), nil, "archive.zip", binding); err == nil || raw != nil || validated.Manifest.SchemaVersion != "" || validated.Audit.SchemaVersion != "" {
		t.Fatalf("nil store raw = %d, validated = %#v, error = %v", len(raw), validated, err)
	}
}

type archiveUpload struct {
	key, contentType, filename string
	data                       []byte
}

type archiveStoreFake struct {
	uploads                     []archiveUpload
	uploadErr, getErr           error
	reader                      io.ReadCloser
	getKey                      string
	deleteCalls, deleteKeyCalls int
}

func (f *archiveStoreFake) Upload(_ context.Context, key string, data []byte, contentType, filename string) (string, error) {
	f.uploads = append(f.uploads, archiveUpload{key: key, data: append([]byte(nil), data...), contentType: contentType, filename: filename})
	return "", f.uploadErr
}
func (f *archiveStoreFake) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	f.getKey = key
	return f.reader, f.getErr
}
func (f *archiveStoreFake) Delete(context.Context, string)             { f.deleteCalls++ }
func (f *archiveStoreFake) DeleteObject(context.Context, string) error { f.deleteCalls++; return nil }
func (f *archiveStoreFake) DeleteKeys(context.Context, []string)       { f.deleteKeyCalls++ }
func (f *archiveStoreFake) KeyFromURL(string) string                   { return "" }
func (f *archiveStoreFake) ObjectURL(string) string                    { return "" }
func (f *archiveStoreFake) CdnDomain() string                          { return "" }

func assertNoArchiveDelete(t *testing.T, store *archiveStoreFake) {
	t.Helper()
	if store.deleteCalls != 0 || store.deleteKeyCalls != 0 {
		t.Fatalf("storage delete called: single=%d batch=%d", store.deleteCalls, store.deleteKeyCalls)
	}
}

type scriptedArchiveReader struct {
	data      []byte
	remaining int64
	readErr   error
	closeErr  error
}

func (r *scriptedArchiveReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	if r.remaining > 0 {
		n := int64(len(p))
		if n > r.remaining {
			n = r.remaining
		}
		for i := int64(0); i < n; i++ {
			p[i] = 0
		}
		r.remaining -= n
		return int(n), nil
	}
	if r.readErr != nil {
		err := r.readErr
		r.readErr = nil
		return 0, err
	}
	return 0, io.EOF
}
func (r *scriptedArchiveReader) Close() error { return r.closeErr }

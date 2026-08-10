package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	qiniuauth "github.com/qiniu/go-sdk/v7/auth"
	qiniustorage "github.com/qiniu/go-sdk/v7/storage"
)

const (
	defaultQiniuBucket     = "static"
	defaultQiniuCDNBaseURL = "https://static.soyoung.com"
	defaultQiniuKeyPrefix  = "sy-design"
)

type qiniuConfig struct {
	AccessKey  string
	SecretKey  string
	Bucket     string
	CDNBaseURL string
	KeyPrefix  string
}

type qiniuUploadRequest struct {
	UploadToken string
	Key         string
	Data        []byte
	ContentType string
}

type qiniuUploader interface {
	Upload(ctx context.Context, request qiniuUploadRequest) error
}

type qiniuDeleter interface {
	Delete(ctx context.Context, bucket, key string) error
}

type qiniuSDKUploader struct {
	uploader *qiniustorage.FormUploader
}

func (u *qiniuSDKUploader) Upload(ctx context.Context, request qiniuUploadRequest) error {
	var response qiniustorage.PutRet
	return u.uploader.Put(
		ctx,
		&response,
		request.UploadToken,
		request.Key,
		bytes.NewReader(request.Data),
		int64(len(request.Data)),
		&qiniustorage.PutExtra{MimeType: request.ContentType},
	)
}

type qiniuSDKDeleter struct {
	manager *qiniustorage.BucketManager
}

func (d *qiniuSDKDeleter) Delete(_ context.Context, bucket, key string) error {
	return d.manager.Delete(bucket, key)
}

type QiniuStorage struct {
	credentials *qiniuauth.Credentials
	bucket      string
	cdnBaseURL  string
	keyPrefix   string
	uploader    qiniuUploader
	deleter     qiniuDeleter
	httpClient  *http.Client
}

// NewQiniuStorageFromEnv configures Qiniu Kodo for Figma design assets.
// It returns nil when Qiniu credentials are not configured.
//
// Environment variables:
//   - QINIU_ACCESS_KEY / QINIU_SECRET_KEY (required)
//   - QINIU_BUCKET (default: "static")
//   - QINIU_CDN_BASE_URL (default: "https://static.soyoung.com")
//   - QINIU_KEY_PREFIX (default: "sy-design")
func NewQiniuStorageFromEnv() *QiniuStorage {
	accessKey := strings.TrimSpace(os.Getenv("QINIU_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("QINIU_SECRET_KEY"))
	if accessKey == "" && secretKey == "" {
		return nil
	}
	if accessKey == "" || secretKey == "" {
		slog.Error("qiniu design asset storage disabled: both QINIU_ACCESS_KEY and QINIU_SECRET_KEY are required")
		return nil
	}

	config := qiniuConfig{
		AccessKey:  accessKey,
		SecretKey:  secretKey,
		Bucket:     os.Getenv("QINIU_BUCKET"),
		CDNBaseURL: os.Getenv("QINIU_CDN_BASE_URL"),
		KeyPrefix:  os.Getenv("QINIU_KEY_PREFIX"),
	}
	config = normalizeQiniuConfig(config)
	credentials := qiniuauth.New(config.AccessKey, config.SecretKey)
	sdkConfig := &qiniustorage.Config{
		Zone:          &qiniustorage.Zone_z0,
		UseHTTPS:      true,
		UseCdnDomains: false,
	}
	store := newQiniuStorage(
		config,
		&qiniuSDKUploader{uploader: qiniustorage.NewFormUploader(sdkConfig)},
		&qiniuSDKDeleter{manager: qiniustorage.NewBucketManager(credentials, sdkConfig)},
		http.DefaultClient,
	)
	slog.Info("qiniu design asset storage initialized", "bucket", store.bucket, "cdn_base_url", store.cdnBaseURL, "key_prefix", store.keyPrefix)
	return store
}

func newQiniuStorage(config qiniuConfig, uploader qiniuUploader, deleter qiniuDeleter, httpClient *http.Client) *QiniuStorage {
	config = normalizeQiniuConfig(config)
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &QiniuStorage{
		credentials: qiniuauth.New(config.AccessKey, config.SecretKey),
		bucket:      config.Bucket,
		cdnBaseURL:  config.CDNBaseURL,
		keyPrefix:   config.KeyPrefix,
		uploader:    uploader,
		deleter:     deleter,
		httpClient:  httpClient,
	}
}

func normalizeQiniuConfig(config qiniuConfig) qiniuConfig {
	config.AccessKey = strings.TrimSpace(config.AccessKey)
	config.SecretKey = strings.TrimSpace(config.SecretKey)
	config.Bucket = strings.TrimSpace(config.Bucket)
	if config.Bucket == "" {
		config.Bucket = defaultQiniuBucket
	}
	config.CDNBaseURL = strings.TrimRight(strings.TrimSpace(config.CDNBaseURL), "/")
	if config.CDNBaseURL == "" {
		config.CDNBaseURL = defaultQiniuCDNBaseURL
	}
	config.KeyPrefix = strings.Trim(strings.TrimSpace(config.KeyPrefix), "/")
	if config.KeyPrefix == "" {
		config.KeyPrefix = defaultQiniuKeyPrefix
	}
	return config
}

func (s *QiniuStorage) Upload(ctx context.Context, key string, data []byte, contentType string, _ string) (string, error) {
	objectKey := s.objectKey(key)
	if objectKey == "" {
		return "", fmt.Errorf("qiniu Upload: empty key")
	}
	if s.uploader == nil {
		return "", fmt.Errorf("qiniu Upload: uploader is not configured")
	}
	policy := qiniustorage.PutPolicy{
		Scope:      s.bucket + ":" + objectKey,
		Expires:    120,
		DetectMime: 1,
	}
	request := qiniuUploadRequest{
		UploadToken: policy.UploadToken(s.credentials),
		Key:         objectKey,
		Data:        data,
		ContentType: contentType,
	}
	if err := s.uploader.Upload(ctx, request); err != nil {
		return "", fmt.Errorf("qiniu Upload: %w", err)
	}
	return s.objectURL(objectKey), nil
}

func (s *QiniuStorage) Delete(ctx context.Context, key string) {
	objectKey := s.objectKey(key)
	if objectKey == "" || s.deleter == nil {
		return
	}
	if err := s.deleter.Delete(ctx, s.bucket, objectKey); err != nil {
		slog.Error("qiniu Delete failed", "key", objectKey, "error", err)
	}
}

// DeleteObject is Delete with the error surfaced so callers can retry failed
// cleanup instead of assuming the object was removed.
func (s *QiniuStorage) DeleteObject(ctx context.Context, key string) error {
	objectKey := s.objectKey(key)
	if objectKey == "" {
		return nil
	}
	if s.deleter == nil {
		return fmt.Errorf("qiniu DeleteObject: deleter is not configured")
	}
	if err := s.deleter.Delete(ctx, s.bucket, objectKey); err != nil {
		return fmt.Errorf("qiniu DeleteObject: %w", err)
	}
	return nil
}

func (s *QiniuStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *QiniuStorage) KeyFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err == nil && parsed.Path != "" {
		if key, unescapeErr := url.PathUnescape(strings.TrimPrefix(parsed.Path, "/")); unescapeErr == nil {
			return key
		}
		return strings.TrimPrefix(parsed.Path, "/")
	}
	return strings.TrimPrefix(strings.TrimSpace(rawURL), "/")
}

// ObjectURL returns the CDN URL that Upload would return for key.
func (s *QiniuStorage) ObjectURL(key string) string {
	objectKey := s.objectKey(key)
	if objectKey == "" {
		return ""
	}
	return s.objectURL(objectKey)
}

func (s *QiniuStorage) CdnDomain() string {
	parsed, err := url.Parse(s.cdnBaseURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func (s *QiniuStorage) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	objectKey := s.objectKey(key)
	if objectKey == "" {
		return nil, fmt.Errorf("qiniu GetReader: empty key")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(objectKey), nil)
	if err != nil {
		return nil, fmt.Errorf("qiniu GetReader: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qiniu GetReader: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		resp.Body.Close()
		return nil, fmt.Errorf("qiniu GetReader: CDN returned %s", resp.Status)
	}
	return resp.Body, nil
}

func (s *QiniuStorage) objectKey(key string) string {
	key = strings.Trim(strings.TrimSpace(key), "/")
	if key == "" {
		return ""
	}
	if key == s.keyPrefix || strings.HasPrefix(key, s.keyPrefix+"/") {
		return key
	}
	return s.keyPrefix + "/" + key
}

func (s *QiniuStorage) objectURL(key string) string {
	return s.cdnBaseURL + "/" + strings.TrimPrefix(key, "/")
}

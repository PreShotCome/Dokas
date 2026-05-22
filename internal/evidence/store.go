package evidence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store abstracts where evidence PDFs live. LocalStore (filesystem) is the
// dev/CI default; S3Store is an S3-compatible bucket (AWS S3 or Cloudflare
// R2), the production backend, ideally configured with Object Lock.
//
// Stored content is opaque ciphertext — the evidence Service encrypts before
// Put and decrypts after ReadAll, so a Store never sees plaintext.
type Store interface {
	// Put writes content for a drill and returns an opaque key (a file path
	// for LocalStore, an object key for S3Store).
	Put(ctx context.Context, drillID string, content []byte) (key string, err error)
	// ReadAll returns the content previously stored under key.
	ReadAll(ctx context.Context, key string) ([]byte, error)
	// Delete removes the object. The retention sweeper calls this only after
	// retain_until has passed; under S3 Object Lock a delete before then is
	// refused by the bucket itself.
	Delete(ctx context.Context, key string) error
}

// LocalStore writes evidence under a base directory. The key is the file
// path, so it round-trips through the drills.evidence_path column unchanged.
type LocalStore struct {
	dir string
}

func NewLocalStore(dir string) *LocalStore { return &LocalStore{dir: dir} }

func (s *LocalStore) Put(_ context.Context, drillID string, content []byte) (string, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", fmt.Errorf("evidence dir: %w", err)
	}
	path := filepath.Join(s.dir, drillID+".pdf")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("write evidence: %w", err)
	}
	return path, nil
}

func (s *LocalStore) ReadAll(_ context.Context, key string) ([]byte, error) {
	return os.ReadFile(key)
}

func (s *LocalStore) Delete(_ context.Context, key string) error {
	err := os.Remove(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// S3Store stores evidence in an S3-compatible bucket. It signs every request
// with AWS Signature Version 4 (see sigv4.go) over net/http — no SDK. For
// production the bucket should have Object Lock enabled with a default
// COMPLIANCE retention, so every object is immutable until it expires.
type S3Store struct {
	bucket    string
	region    string
	endpoint  string // base URL, no trailing slash
	accessKey string
	secretKey string
	http      *http.Client
}

// NewS3Store builds an S3-compatible store. endpoint may be empty for AWS S3
// (the regional endpoint is derived); for Cloudflare R2 or MinIO pass the
// account/cluster endpoint explicitly. R2 expects region "auto".
func NewS3Store(bucket, region, endpoint, accessKey, secretKey string) *S3Store {
	if endpoint == "" {
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	return &S3Store{
		bucket:    bucket,
		region:    region,
		endpoint:  strings.TrimRight(endpoint, "/"),
		accessKey: accessKey,
		secretKey: secretKey,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *S3Store) objectURL(key string) string {
	return s.endpoint + "/" + s.bucket + "/" + key
}

func (s *S3Store) Put(ctx context.Context, drillID string, content []byte) (string, error) {
	key := drillID + ".pdf"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key),
		bytes.NewReader(content))
	if err != nil {
		return "", err
	}
	req.ContentLength = int64(len(content))
	req.Header.Set("Content-Type", "application/octet-stream")
	hash := sha256Hex(content)
	req.Header.Set("X-Amz-Content-Sha256", hash)
	signV4(req, hash, s.accessKey, s.secretKey, s.region, "s3", time.Now())
	if _, err := s.do(req); err != nil {
		return "", err
	}
	return key, nil
}

func (s *S3Store) ReadAll(ctx context.Context, key string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Amz-Content-Sha256", emptyPayloadHash)
	signV4(req, emptyPayloadHash, s.accessKey, s.secretKey, s.region, "s3", time.Now())
	return s.do(req)
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Amz-Content-Sha256", emptyPayloadHash)
	signV4(req, emptyPayloadHash, s.accessKey, s.secretKey, s.region, "s3", time.Now())
	_, err = s.do(req)
	return err
}

// do executes a signed request and returns the body. A 404 is treated as
// "already gone" (nil, nil); other 4xx/5xx become errors.
func (s *S3Store) do(req *http.Request) ([]byte, error) {
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("evidence s3: %s: %w", req.Method, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		snippet := string(body)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return nil, fmt.Errorf("evidence s3: %s %s: %s", req.Method, resp.Status, snippet)
	}
	return body, nil
}

// Compile-time interface guards.
var (
	_ Store = (*LocalStore)(nil)
	_ Store = (*S3Store)(nil)
)

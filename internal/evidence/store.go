package evidence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrNotImplemented is returned by the S3 store stub.
var ErrNotImplemented = errors.New("evidence: store not implemented")

// Store abstracts where evidence PDFs live. LocalStore (filesystem) is the
// dev/CI default; S3Store is the production Object-Lock-backed bucket,
// stubbed in this phase. Retention is enforced by the retention sweeper in
// the app layer, not the backend, so both stores honour the same policy.
type Store interface {
	// Put writes content for a drill and returns an opaque key (a path for
	// LocalStore, an object key for S3Store).
	Put(ctx context.Context, drillID string, content []byte) (key string, err error)
	// Open returns a reader for a previously stored key.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// ReadAll is the convenience used by signing + verification.
	ReadAll(ctx context.Context, key string) ([]byte, error)
	// Delete removes the object. The retention sweeper calls this only after
	// confirming retain_until has passed.
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

func (s *LocalStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(key)
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

// S3Store is the production evidence backend: an S3 bucket with Object Lock
// in compliance mode. Wiring is deferred — every method returns
// ErrNotImplemented so a misconfiguration is obvious rather than silent.
type S3Store struct {
	// Bucket, Region, retention config — populated when the real store lands.
}

func NewS3Store() *S3Store { return &S3Store{} }

func (s *S3Store) Put(context.Context, string, []byte) (string, error) {
	return "", ErrNotImplemented
}
func (s *S3Store) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, ErrNotImplemented
}
func (s *S3Store) ReadAll(context.Context, string) ([]byte, error) {
	return nil, ErrNotImplemented
}
func (s *S3Store) Delete(context.Context, string) error { return ErrNotImplemented }

// Compile-time interface guards.
var (
	_ Store = (*LocalStore)(nil)
	_ Store = (*S3Store)(nil)
)

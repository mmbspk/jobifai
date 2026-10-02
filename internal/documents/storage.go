package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BlobStore persists opaque bytes at durable keys (local filesystem adapter).
type BlobStore interface {
	PutAtomic(key string, r io.Reader) (sha256Hex string, size int64, err error)
	Read(key string) ([]byte, error)
	Open(key string) (io.ReadCloser, error)
}

type LocalBlobStore struct {
	Root string
}

func NewLocalBlobStore(root string) (*LocalBlobStore, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &LocalBlobStore{Root: root}, nil
}

func (s *LocalBlobStore) abs(key string) (string, error) {
	if key == "" || filepath.IsAbs(key) || key != filepath.Clean(key) {
		return "", fmt.Errorf("invalid storage key")
	}
	base, err := filepath.Abs(s.Root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(s.Root, key))
	if err != nil {
		return "", err
	}
	if !filepath.HasPrefix(target, base+string(filepath.Separator)) && target != base {
		return "", fmt.Errorf("storage key escapes root")
	}
	return target, nil
}

func (s *LocalBlobStore) PutAtomic(key string, r io.Reader) (string, int64, error) {
	target, err := s.abs(key)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return "", 0, err
	}
	tmp := target + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func (s *LocalBlobStore) Read(key string) ([]byte, error) {
	target, err := s.abs(key)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(target)
}

func (s *LocalBlobStore) Open(key string) (io.ReadCloser, error) {
	target, err := s.abs(key)
	if err != nil {
		return nil, err
	}
	return os.Open(target)
}

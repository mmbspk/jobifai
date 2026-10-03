package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var ErrBlobNotFound = errors.New("blob not found")

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

func pathContainedInBase(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	if !pathContainedInBase(base, target) {
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
	tmp, err := os.CreateTemp(filepath.Dir(target), ".blob-"+uuid.NewString()+".tmp")
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmp.Name()
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), r)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, closeErr
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func (s *LocalBlobStore) Read(key string) ([]byte, error) {
	target, err := s.abs(key)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrBlobNotFound, key)
		}
		return nil, err
	}
	return data, nil
}

func (s *LocalBlobStore) Open(key string) (io.ReadCloser, error) {
	target, err := s.abs(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrBlobNotFound, key)
		}
		return nil, err
	}
	return f, nil
}

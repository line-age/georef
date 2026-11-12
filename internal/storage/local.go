package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

type localStore struct {
	root string
}

// NewLocalStore returns a Store that writes files under root.
// Directory layout: <root>/sheets/<id>/original and <root>/sheets/<id>/pyramid.tif
func NewLocalStore(root string) Store {
	return &localStore{root: root}
}

func (s *localStore) SaveOriginalScan(_ context.Context, id string, r io.Reader) error {
	return atomicWrite(s.scanPath(id), r)
}

func (s *localStore) OpenOriginalScan(_ context.Context, id string) (io.ReadCloser, error) {
	return os.Open(s.scanPath(id))
}

func (s *localStore) OriginalScanPath(_ context.Context, id string) (string, error) {
	return s.scanPath(id), nil
}

func (s *localStore) SavePyramidalTIFF(_ context.Context, id string, r io.Reader) error {
	return atomicWrite(s.tiffPath(id), r)
}

func (s *localStore) PyramidalTIFFPath(_ context.Context, id string) (string, error) {
	return s.tiffPath(id), nil
}

func (s *localStore) scanPath(id string) string {
	return filepath.Join(s.root, "sheets", id, "original")
}

func (s *localStore) tiffPath(id string) string {
	return filepath.Join(s.root, "sheets", id, "pyramid.tif")
}

// atomicWrite writes r to dst using a temp file in the same directory followed
// by a rename. On any error the temp file is removed and dst is left untouched.
func atomicWrite(dst string, r io.Reader) (retErr error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		if retErr != nil {
			os.Remove(tmp.Name()) //nolint:errcheck
		}
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), dst)
}

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

func (store *localStore) SaveOriginalScan(_ context.Context, id string, reader io.Reader) (string, error) {
	filePath := store.scanPath(id)
	return filePath, atomicWrite(filePath, reader)
}

func (store *localStore) OpenOriginalScan(_ context.Context, id string) (io.ReadCloser, error) {
	return os.Open(store.scanPath(id))
}

func (store *localStore) OriginalScanPath(_ context.Context, id string) (string, error) {
	return store.scanPath(id), nil
}

func (store *localStore) SavePyramidalTIFF(_ context.Context, id string, reader io.Reader) (string, error) {
	filePath := store.tiffPath(id)
	return filePath, atomicWrite(filePath, reader)
}

func (store *localStore) PyramidalTIFFPath(_ context.Context, id string) (string, error) {
	return store.tiffPath(id), nil
}

func (store *localStore) scanPath(id string) string {
	return filepath.Join(store.root, "sheets", id, "original")
}

func (store *localStore) tiffPath(id string) string {
	return filepath.Join(store.root, "sheets", id, "pyramid.tif")
}

// atomicWrite writes reader to destPath using a temp file in the same directory
// followed by a rename. On any error the temp file is removed and destPath is left untouched.
func atomicWrite(destPath string, reader io.Reader) (retErr error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(destPath), ".tmp-")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		if retErr != nil {
			os.Remove(tempPath) //nolint:errcheck
		}
	}()

	if _, err := io.Copy(tempFile, reader); err != nil {
		tempFile.Close() //nolint:errcheck
		return err
	}
	if err := tempFile.Sync(); err != nil {
		tempFile.Close() //nolint:errcheck
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	return os.Rename(tempPath, destPath)
}

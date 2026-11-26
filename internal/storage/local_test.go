package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/line-age/georef/internal/storage"
)

func TestLocalStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := storage.NewLocalStore(root)
	ctx := context.Background()
	id := "01JEMKR9VXYZ"

	t.Run("save_and_open_original_scan", func(t *testing.T) {
		t.Parallel()
		content := "original scan bytes"

		if _, err := store.SaveOriginalScan(ctx, id, strings.NewReader(content)); err != nil {
			t.Fatalf("SaveOriginalScan: %v", err)
		}

		rc, err := store.OpenOriginalScan(ctx, id)
		if err != nil {
			t.Fatalf("OpenOriginalScan: %v", err)
		}
		defer rc.Close()

		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != content {
			t.Fatalf("content mismatch: got %q, want %q", got, content)
		}
	})

	t.Run("save_pyramidal_tiff_and_get_path", func(t *testing.T) {
		t.Parallel()
		content := []byte{0x49, 0x49, 0x2A, 0x00} // TIFF little-endian magic bytes

		if _, err := store.SavePyramidalTIFF(ctx, id, bytes.NewReader(content)); err != nil {
			t.Fatalf("SavePyramidalTIFF: %v", err)
		}

		path, err := store.PyramidalTIFFPath(ctx, id)
		if err != nil {
			t.Fatalf("PyramidalTIFFPath: %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read path %q: %v", path, err)
		}
		if !bytes.Equal(got, content) {
			t.Fatalf("content mismatch at %q", path)
		}
	})

	t.Run("save_creates_directories", func(t *testing.T) {
		t.Parallel()
		newID := "01JEMKR9VNEW"

		if _, err := store.SaveOriginalScan(ctx, newID, strings.NewReader("x")); err != nil {
			t.Fatalf("SaveOriginalScan with new id: %v", err)
		}

		dir := filepath.Join(root, "sheets", newID)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("expected directory %q to exist: %v", dir, err)
		}
	})

	t.Run("atomic_write_leaves_no_temp_files", func(t *testing.T) {
		t.Parallel()
		atomicID := "01JEMKR9VATM"

		if _, err := store.SaveOriginalScan(ctx, atomicID, strings.NewReader("data")); err != nil {
			t.Fatalf("SaveOriginalScan: %v", err)
		}

		dir := filepath.Join(root, "sheets", atomicID)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".tmp-") {
				t.Errorf("temp file left behind: %s", e.Name())
			}
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 file, got %d", len(entries))
		}
	})

	t.Run("open_missing_scan_returns_error", func(t *testing.T) {
		t.Parallel()

		_, err := store.OpenOriginalScan(ctx, "does-not-exist")
		if err == nil {
			t.Fatal("expected error opening missing scan, got nil")
		}
		if !os.IsNotExist(err) {
			t.Fatalf("expected os.IsNotExist error, got: %v", err)
		}
	})
}

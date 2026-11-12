package storage

import (
	"context"
	"io"
)

// Store persists and retrieves the two files that belong to a sheet:
// the original scan as uploaded, and the pyramidal TIFF built from it.
// The filesystem and S3 backends both satisfy this interface.
type Store interface {
	// SaveOriginalScan streams r to storage as the original scan for the given sheet ID.
	// The write is atomic: either the whole file lands or nothing does.
	// It returns the path at which the file was stored.
	SaveOriginalScan(ctx context.Context, id string, r io.Reader) (string, error)

	// OpenOriginalScan returns a reader for the original scan of the given sheet ID.
	// The caller must close the returned ReadCloser.
	OpenOriginalScan(ctx context.Context, id string) (io.ReadCloser, error)

	// OriginalScanPath returns the filesystem path or S3 URI of the original scan.
	// Stored in the sheet record so operators can locate the file directly.
	OriginalScanPath(ctx context.Context, id string) (string, error)

	// SavePyramidalTIFF streams r to storage as the pyramidal TIFF for the given sheet ID.
	// The write is atomic: either the whole file lands or nothing does.
	SavePyramidalTIFF(ctx context.Context, id string, r io.Reader) error

	// PyramidalTIFFPath returns the filesystem path or S3 URI that the image server
	// uses to read the pyramidal TIFF directly. The format depends on the backend.
	PyramidalTIFFPath(ctx context.Context, id string) (string, error)
}

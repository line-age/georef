package model

import "time"

// Status represents the processing lifecycle of a sheet.
type Status string

const (
	StatusProcessing Status = "processing"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
	StatusDeleted    Status = "deleted"
)

// Sheet is the central entity of the image service. It owns a scanned map image
// from the moment it is uploaded until it is served as IIIF tiles.
type Sheet struct {
	ID string // ULID; stable forever — embedded in every IIIF URL.

	// Descriptive metadata supplied by the uploader.
	Title       string
	Institution string
	SourceURL   string
	RightsNote  string

	// Lifecycle state. Transitions: processing → ready | failed; failed → processing (retry).
	// Soft-delete sets status to deleted; the row and ID are never removed.
	Status       Status
	ErrorMessage string // non-empty only when Status == StatusFailed

	// Original file — recorded at upload, never modified.
	OriginalChecksum string // SHA-256 hex
	OriginalSize     int64  // bytes
	OriginalPath     string // storage-relative path, e.g. sheets/{id}/original

	// Pyramid file — written by the worker on successful conversion.
	PyramidPath string // storage-relative path, e.g. sheets/{id}/pyramid.tif; empty until ready

	// Nullable: not yet confirmed as required. See docs/model.md.
	UploaderGitHubID *int64

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time // non-nil when Status == StatusDeleted
}

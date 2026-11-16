package api

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
)

const (
	msgInvalidForm     = "invalid multipart form"
	msgFileTooLarge    = "file too large"
	msgTitleRequired   = "title is required"
	msgFileRequired    = "file is required"
	msgFileTypeInvalid = "unsupported file type; accepted: image/tiff, image/jpeg"
)

// allowedContentTypes lists the file types vips can convert to a pyramidal TIFF.
var allowedContentTypes = map[string]bool{
	"image/tiff": true,
	"image/jpeg": true,
}

// uploadRequest holds the parsed and validated fields from a POST /sheets multipart form.
type uploadRequest struct {
	Title       string
	Institution string
	SourceURL   string
	RightsNote  string
	File        multipart.File
}

// parseUploadRequest extracts the multipart form fields into an uploadRequest.
// Returns an error if the form is malformed or the file field is missing.
// If the body exceeds the size limit set by MaxBytesReader, the error wraps
// *http.MaxBytesError so the caller can return 413.
func parseUploadRequest(request *http.Request, memoryLimit int64) (*uploadRequest, error) {
	if err := request.ParseMultipartForm(memoryLimit); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, err // preserve type; caller checks with errors.As
		}
		return nil, errors.New(msgInvalidForm)
	}
	file, _, err := request.FormFile("file")
	if err != nil {
		return nil, errors.New(msgFileRequired)
	}
	return &uploadRequest{
		Title:       request.FormValue("title"),
		Institution: request.FormValue("institution"),
		SourceURL:   request.FormValue("source_url"),
		RightsNote:  request.FormValue("rights_note"),
		File:        file,
	}, nil
}

// validate checks that all required fields are present and that the uploaded
// file's content is an accepted type.
func (form *uploadRequest) validate() error {
	if err := form.validateRequiredFields(); err != nil {
		return err
	}
	return form.validateFileContentType()
}

// validateRequiredFields returns an error when any mandatory form field is missing.
func (form *uploadRequest) validateRequiredFields() error {
	if form.Title == "" {
		return errors.New(msgTitleRequired)
	}
	return nil
}

// validateFileContentType reads the first 512 bytes of the uploaded file and
// returns an error if the detected MIME type is not in allowedContentTypes.
// It rewinds the file cursor to the start before returning, so callers can
// read the full file contents afterwards.
func (form *uploadRequest) validateFileContentType() error {
	contentTypeProbeBytes := make([]byte, 512)
	bytesRead, readErr := form.File.Read(contentTypeProbeBytes)
	if _, err := form.File.Seek(0, io.SeekStart); err != nil {
		return errors.New(msgInvalidForm)
	}
	if readErr != nil && readErr != io.EOF {
		return errors.New(msgInvalidForm)
	}

	detectedContentType := detectContentType(contentTypeProbeBytes[:bytesRead])
	if !allowedContentTypes[detectedContentType] {
		return errors.New(msgFileTypeInvalid)
	}
	return nil
}

// detectContentType sniffs the MIME type of a file from its leading bytes.
// http.DetectContentType does not recognise TIFF, so TIFF magic is checked first.
func detectContentType(data []byte) string {
	if isTIFF(data) {
		return "image/tiff"
	}
	return http.DetectContentType(data)
}

// isTIFF reports whether data starts with a TIFF magic number.
// TIFF files start with either "II\x2A\x00" (little-endian) or "MM\x00\x2A" (big-endian).
func isTIFF(data []byte) bool {
	return len(data) >= 4 &&
		((data[0] == 0x49 && data[1] == 0x49 && data[2] == 0x2A && data[3] == 0x00) ||
			(data[0] == 0x4D && data[1] == 0x4D && data[2] == 0x00 && data[3] == 0x2A))
}

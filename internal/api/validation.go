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
func parseUploadRequest(request *http.Request) (*uploadRequest, error) {
	if err := request.ParseMultipartForm(multipartMemoryLimit); err != nil {
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

// validate checks that all required fields are present and that the file's
// actual content matches an accepted type. It reads the first 512 bytes rather
// than trusting the client-supplied Content-Type header.
func (form *uploadRequest) validate() error {
	if form.Title == "" {
		return errors.New(msgTitleRequired)
	}

	contentTypeProbeBytes := make([]byte, 512)
	bytesRead, _ := form.File.Read(contentTypeProbeBytes)
	if _, err := form.File.Seek(0, io.SeekStart); err != nil {
		return errors.New(msgInvalidForm)
	}

	detectedContentType := http.DetectContentType(contentTypeProbeBytes[:bytesRead])
	if !allowedContentTypes[detectedContentType] {
		return errors.New(msgFileTypeInvalid)
	}
	return nil
}

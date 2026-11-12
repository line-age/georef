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

// allowedMIMETypes lists the file types vips can convert to a pyramidal TIFF.
var allowedMIMETypes = map[string]bool{
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
func parseUploadRequest(r *http.Request) (*uploadRequest, error) {
	if err := r.ParseMultipartForm(multipartMaxMem); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, err // preserve type; caller checks with errors.As
		}
		return nil, errors.New(msgInvalidForm)
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, errors.New(msgFileRequired)
	}
	return &uploadRequest{
		Title:       r.FormValue("title"),
		Institution: r.FormValue("institution"),
		SourceURL:   r.FormValue("source_url"),
		RightsNote:  r.FormValue("rights_note"),
		File:        file,
	}, nil
}

// validate checks that all required fields are present and that the file's
// actual content matches an accepted type. It sniffs the first 512 bytes rather
// than trusting the client-supplied Content-Type header.
func (req *uploadRequest) validate() error {
	if req.Title == "" {
		return errors.New(msgTitleRequired)
	}

	sniff := make([]byte, 512)
	n, _ := req.File.Read(sniff)
	if _, err := req.File.Seek(0, io.SeekStart); err != nil {
		return errors.New(msgInvalidForm)
	}

	detected := http.DetectContentType(sniff[:n])
	if !allowedMIMETypes[detected] {
		return errors.New(msgFileTypeInvalid)
	}
	return nil
}

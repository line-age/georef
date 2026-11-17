// Package proxy implements a streaming reverse proxy to the IIIF image server.
// It handles two request types: info.json (buffered, with @id rewrite) and
// tiles (streamed without buffering). Both reuse a pooled HTTP connection.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config holds tuneable parameters for the Proxy transport.
type Config struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	RequestTimeout      time.Duration
	MaxInfoBodySize     int64 // maximum bytes read from an info.json response body
}

// ErrStreamInterrupted is returned by ServeTile when the 200 OK response headers
// have already been sent to the client but the tile body stream was cut short.
// Callers must not write another response after receiving this error.
type ErrStreamInterrupted struct{ Cause error }

func (e ErrStreamInterrupted) Error() string { return "stream interrupted: " + e.Cause.Error() }
func (e ErrStreamInterrupted) Unwrap() error { return e.Cause }

// passedHeaders are upstream response headers forwarded to the client unchanged.
var passedHeaders = []string{
	"Content-Type", "Cache-Control", "ETag", "Last-Modified", "Expires",
}

// Proxy is a streaming reverse proxy to the internal IIIF image server.
// A single Proxy is safe for concurrent use.
type Proxy struct {
	client          *http.Client
	maxInfoBodySize int64
}

// New returns a Proxy backed by a connection-pooled HTTP client configured by cfg.
func New(cfg Config) *Proxy {
	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
	}
	return &Proxy{
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.RequestTimeout,
		},
		maxInfoBodySize: cfg.MaxInfoBodySize,
	}
}

// ServeInfoJSON fetches info.json from upstreamURL, rewrites the @id field
// to publicIIIFBaseURL (the external URL the client will use to request tiles),
// then writes the modified document to w.
// cacheControl, when non-empty, overrides the upstream Cache-Control header
// (e.g. "public" for open sheets, "private" when read-login is on).
func (imageProxy *Proxy) ServeInfoJSON(w http.ResponseWriter, r *http.Request, upstreamURL, publicIIIFBaseURL, cacheControl string) error {
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		return err
	}

	upstreamResp, err := imageProxy.client.Do(upstreamReq)
	if err != nil {
		return err
	}
	defer upstreamResp.Body.Close()

	if upstreamResp.StatusCode != http.StatusOK {
		return fmt.Errorf("image server returned %d", upstreamResp.StatusCode)
	}

	var doc map[string]any
	if err := json.NewDecoder(io.LimitReader(upstreamResp.Body, imageProxy.maxInfoBodySize)).Decode(&doc); err != nil {
		return err
	}
	doc["@id"] = publicIIIFBaseURL

	rewritten, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	copyResponseHeaders(w, upstreamResp)
	if cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(rewritten) //nolint:errcheck
	return nil
}

// ServeTile streams the upstream tile response to w without buffering the body.
// cacheControl, when non-empty, overrides the upstream Cache-Control header.
func (imageProxy *Proxy) ServeTile(w http.ResponseWriter, r *http.Request, upstreamURL, cacheControl string) error {
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		return err
	}

	upstreamResp, err := imageProxy.client.Do(upstreamReq)
	if err != nil {
		return err
	}
	defer upstreamResp.Body.Close()

	if upstreamResp.StatusCode != http.StatusOK {
		return fmt.Errorf("image server returned %d", upstreamResp.StatusCode)
	}

	copyResponseHeaders(w, upstreamResp)
	if cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, upstreamResp.Body); err != nil {
		return ErrStreamInterrupted{Cause: err}
	}
	return nil
}

// FetchDimensions fetches info.json from infoURL and returns the image width and height.
// Used by the manifest handler to populate canvas dimensions.
func (imageProxy *Proxy) FetchDimensions(ctx context.Context, infoURL string) (width, height int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		return 0, 0, err
	}

	resp, err := imageProxy.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("image server returned %d", resp.StatusCode)
	}

	var info struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, imageProxy.maxInfoBodySize)).Decode(&info); err != nil {
		return 0, 0, err
	}
	return info.Width, info.Height, nil
}

// copyResponseHeaders forwards selected upstream response headers to w.
func copyResponseHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, name := range passedHeaders {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
}

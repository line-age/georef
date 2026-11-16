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

const (
	maxIdleConns        = 100
	maxIdleConnsPerHost = 20
	idleConnTimeout     = 90 * time.Second

	// Tiles can be large and slow connections are common; don't time out short.
	defaultTimeout = 2 * time.Minute
)

// passedHeaders are upstream response headers forwarded to the client unchanged.
var passedHeaders = []string{
	"Content-Type", "Cache-Control", "ETag", "Last-Modified", "Expires",
}

// Proxy is a streaming reverse proxy to the internal IIIF image server.
// A single Proxy is safe for concurrent use.
type Proxy struct {
	client *http.Client
}

// New returns a Proxy backed by a connection-pooled HTTP client.
func New() *Proxy {
	transport := &http.Transport{
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdleConnsPerHost,
		IdleConnTimeout:     idleConnTimeout,
	}
	return &Proxy{
		client: &http.Client{
			Transport: transport,
			Timeout:   defaultTimeout,
		},
	}
}

// ServeInfoJSON fetches info.json from upstreamURL, rewrites the @id field
// to publicIIIFBaseURL (the external URL the client will use to request tiles),
// then writes the modified document to w.
func (proxy *Proxy) ServeInfoJSON(w http.ResponseWriter, r *http.Request, upstreamURL, publicIIIFBaseURL string) error {
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		return err
	}

	upstreamResp, err := proxy.client.Do(upstreamReq)
	if err != nil {
		return err
	}
	defer upstreamResp.Body.Close()

	if upstreamResp.StatusCode != http.StatusOK {
		return fmt.Errorf("image server returned %d", upstreamResp.StatusCode)
	}

	var doc map[string]any
	if err := json.NewDecoder(upstreamResp.Body).Decode(&doc); err != nil {
		return err
	}
	doc["@id"] = publicIIIFBaseURL

	rewritten, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	copyResponseHeaders(w, upstreamResp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(rewritten) //nolint:errcheck
	return nil
}

// ServeTile streams the upstream tile response to w without buffering the body.
func (proxy *Proxy) ServeTile(w http.ResponseWriter, r *http.Request, upstreamURL string) error {
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		return err
	}

	upstreamResp, err := proxy.client.Do(upstreamReq)
	if err != nil {
		return err
	}
	defer upstreamResp.Body.Close()

	copyResponseHeaders(w, upstreamResp)
	w.WriteHeader(upstreamResp.StatusCode)
	io.Copy(w, upstreamResp.Body) //nolint:errcheck
	return nil
}

// FetchDimensions fetches info.json from infoURL and returns the image width and height.
// Used by the manifest handler to populate canvas dimensions.
func (proxy *Proxy) FetchDimensions(ctx context.Context, infoURL string) (width, height int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		return 0, 0, err
	}

	resp, err := proxy.client.Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
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

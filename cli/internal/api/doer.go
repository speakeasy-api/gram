package api

import (
	"net/http"
	"strconv"
	"time"
)

// contentLengthPreservingTransport wraps an http.RoundTripper and ensures that
// Content-Length headers are preserved by setting req.ContentLength from the
// header value before the request is sent. This is necessary because Go's
// http.Client automatically removes Content-Length headers when the body is
// an io.Reader, but we need to explicitly set the length for upload endpoints.
type contentLengthPreservingTransport struct {
	transport http.RoundTripper
}

func (t *contentLengthPreservingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// If Content-Length header is set but req.ContentLength is not,
	// copy the header value to req.ContentLength so it's preserved
	if req.ContentLength <= 0 {
		if cl := req.Header.Get("Content-Length"); cl != "" {
			if length, err := strconv.ParseInt(cl, 10, 64); err == nil && length > 0 {
				req.ContentLength = length
			}
		}
	}
	return t.transport.RoundTrip(req)
}

// goaSharedHTTPClient is a singleton HTTP client for the CLI. Backend API
// should use this client as its main transport.
var goaSharedHTTPClient = &http.Client{
	Timeout: 10 * time.Minute,
	Transport: &contentLengthPreservingTransport{
		transport: &http.Transport{
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	},
}

// SharedHTTPClient returns the shared HTTP client used across the CLI.
func SharedHTTPClient() *http.Client {
	return goaSharedHTTPClient
}

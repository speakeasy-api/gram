package proxy

import (
	"context"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// McpSessionIDHeader is the session header defined by the MCP Streamable
	// HTTP transport.
	McpSessionIDHeader = "Mcp-Session-Id"
)

// isSkippedRequestHeader returns true for headers that should never be
// forwarded verbatim from the user request to the remote MCP server:
// hop-by-hop headers (RFC 7230 § 6.1), Host (set by the HTTP client from the
// remote URL), and Authorization (carries the Gram API key, which is not
// valid upstream).
func isSkippedRequestHeader(name string) bool {
	switch strings.ToLower(name) {
	case
		"authorization",
		"connection",
		"content-length",
		"host",
		"keep-alive",
		"proxy-authenticate",
		"proxy-authorization",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade":
		return true
	}
	return false
}

// isSkippedResponseHeader returns true for headers that should not be relayed
// from the remote MCP server to the user. The net/http client collapses
// hop-by-hop handling itself, but Content-Length is recomputed by the
// ResponseWriter, and Transfer-Encoding would double-encode.
func isSkippedResponseHeader(name string) bool {
	switch strings.ToLower(name) {
	case
		"connection",
		"content-length",
		"keep-alive",
		"proxy-authenticate",
		"proxy-authorization",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade":
		return true
	}
	return false
}

// applyResponseHeaders copies headers from the upstream MCP server response onto w,
// filtering through [isSkippedResponseHeader] so hop-by-hop and
// transport-managed headers are not forwarded. Multi-value headers are
// preserved by appending each value individually rather than joining.
//
// Callers ([writeResponse], [Proxy.relaySSEStream]) must invoke this before
// [http.ResponseWriter.WriteHeader]; once the status line is written, header
// mutations are silently dropped.
func applyResponseHeaders(w http.ResponseWriter, remoteResp *http.Response) {
	for name, values := range remoteResp.Header {
		if isSkippedResponseHeader(name) {
			continue
		}
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
}

// applyRequestHeaders populates the upstream request headers by copying forward-safe
// headers from the user request and overlaying the configured static and
// pass-through headers. Configured headers win on conflict.
func (p *Proxy) applyRequestHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	for name, values := range userReq.Header {
		if isSkippedRequestHeader(name) {
			continue
		}
		for _, v := range values {
			remoteReq.Header.Add(name, v)
		}
	}

	for _, h := range p.Headers {
		value, err := h.Resolve(userReq)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "missing required header for remote mcp server").Log(ctx, p.Logger)
		}
		if value == "" {
			remoteReq.Header.Del(h.Name)
			continue
		}
		remoteReq.Header.Set(h.Name, value)
	}

	return nil
}

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
// hop-by-hop headers (RFC 7230 § 6.1) and Host (set by the HTTP client
// from the remote URL).
//
// Accept-Encoding is dropped so the Go transport owns content-encoding
// negotiation: when the client forwards its own Accept-Encoding the transport
// declines to transparently decompress the response, leaving a gzipped body
// that fails JSON-RPC decode and bypasses response interception. Letting the
// transport add and unwind its own encoding keeps the buffered body decodable.
//
// Origin, Referer, and Cookie are browser-only headers that carry the
// dashboard's own origin and session, not the caller's intent toward the
// upstream. They are meaningless upstream and actively harmful: MCP servers
// that implement the spec's DNS-rebinding protection validate Origin and
// reject a request whose Origin isn't their own (e.g. Langfuse 403s a
// forwarded "Origin: https://<dashboard>"), and forwarding Cookie would leak
// the gram_session to an arbitrary upstream. Drop them so requests proxied
// from the dashboard match those from a headless MCP client.
//
// Authorization is end-to-end and is handled separately by
// [Proxy.applyRequestHeaders] based on [Proxy.AuthorizationOverride].
func isSkippedRequestHeader(name string) bool {
	switch strings.ToLower(name) {
	case
		"accept-encoding",
		"authorization",
		"connection",
		"content-length",
		"cookie",
		"host",
		"keep-alive",
		"origin",
		"proxy-authenticate",
		"proxy-authorization",
		"referer",
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
		"upgrade",
		// Internal gateway→gram-server tunnel diagnostics
		// (wire.HeaderTunnelError). The retry policy consumes it from the
		// upstream response object; external MCP clients must not see it.
		"x-gram-tunnel-error":
		return true
	}
	return false
}

// applyResponseHeaders copies headers from the upstream MCP server response onto w,
// filtering through [isSkippedResponseHeader] so hop-by-hop and
// transport-managed headers are not forwarded. Multi-value headers are
// preserved by appending each value individually rather than joining.
//
// When the upstream rejected the request (401/403) and wwwAuthenticate is
// non-empty, it replaces the upstream's WWW-Authenticate. The upstream
// challenge names the upstream's own protected-resource metadata, which a
// spec-following MCP client must reject — it doesn't match the URL the
// client connected to — and which otherwise misdirects its re-auth at the
// upstream's authorization server instead of this server's.
//
// Callers ([writeResponse], [Proxy.relaySSEStream]) must invoke this before
// [http.ResponseWriter.WriteHeader]; once the status line is written, header
// mutations are silently dropped.
func applyResponseHeaders(w http.ResponseWriter, remoteResp *http.Response, wwwAuthenticate string) {
	replaceChallenge := wwwAuthenticate != "" &&
		(remoteResp.StatusCode == http.StatusUnauthorized || remoteResp.StatusCode == http.StatusForbidden)
	for name, values := range remoteResp.Header {
		if isSkippedResponseHeader(name) {
			continue
		}
		if replaceChallenge && strings.EqualFold(name, "WWW-Authenticate") {
			continue
		}
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
	if replaceChallenge {
		w.Header().Set("WWW-Authenticate", wwwAuthenticate)
	}
}

// applyRequestHeaders populates the upstream request headers by copying forward-safe
// headers from the user request and overlaying the configured static and
// pass-through headers. Configured headers win on conflict.
//
// The user's Authorization header is always dropped — Gram-issued
// credentials (API keys, Gram-managed OAuth tokens, chat-session JWTs)
// are not meaningful upstream. When [Proxy.AuthorizationOverride] is
// non-empty, the proxy emits its own "Authorization: Bearer <override>"
// upstream; configured headers may further override that.
func (p *Proxy) applyRequestHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	for name, values := range userReq.Header {
		if isSkippedRequestHeader(name) {
			continue
		}
		for _, v := range values {
			remoteReq.Header.Add(name, v)
		}
	}

	if p.AuthorizationOverride != "" {
		remoteReq.Header.Set("Authorization", "Bearer "+p.AuthorizationOverride)
	}

	for _, h := range p.Headers {
		value, err := h.Resolve(userReq)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "missing required header for remote mcp server").LogError(ctx, p.Logger)
		}
		if value == "" {
			remoteReq.Header.Del(h.Name)
			continue
		}
		remoteReq.Header.Set(h.Name, value)
	}

	// Strip last so configured headers can't reintroduce Accept-Encoding after
	// the user-header filter: the Go transport must own content-encoding
	// negotiation, otherwise a gzipped upstream body reaches readJSONRPCBody
	// undecoded and bypasses response interception.
	remoteReq.Header.Del("Accept-Encoding")

	return nil
}

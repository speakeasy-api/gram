package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
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
//
// The MCP 2026-07-28 standard request headers (Mcp-Method, Mcp-Name, and the
// Mcp-Param-{Name} family) must never be added here, and configured headers
// never override them ([Proxy.applyRequestHeaders]). The specification
// requires an intermediary to forward them, including any Mcp-Param-{Name}
// header it does not recognize, and the upstream validates them against the
// body; stripping one would make a conforming request fail upstream.
func isSkippedRequestHeader(name string) bool {
	if mcpauthz.ReservedHeader(name) {
		return true
	}
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
		// Sec-Fetch-* describe the dashboard's own fetch, not the caller's
		// intent toward the upstream, and are dropped for the same reason as
		// Origin above. Forwarding them is doubly wrong now that Speakeasy enforces
		// the same protection inbound: "cross-site" 403s any upstream running
		// net/http.CrossOriginProtection, while "same-origin" would falsely
		// satisfy that upstream's check on Speakeasy's behalf.
		"sec-fetch-dest",
		"sec-fetch-mode",
		"sec-fetch-site",
		"sec-fetch-user",
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
	if mcpauthz.ReservedHeader(name) {
		return true
	}
	// The CORS middleware owns the browser-facing policy. An upstream's own
	// Access-Control-* values would sit beside it, and a browser rejects a
	// response carrying two Access-Control-Allow-Origin values.
	if strings.HasPrefix(strings.ToLower(name), "access-control-") {
		return true
	}
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
		"x-gram-tunnel-error",
		// Internal gateway→gram-server agent-session report
		// (wire.HeaderTunnelAgentSession); external MCP clients must not see it.
		"x-gram-tunnel-agent-session":
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
	// Marks the access log's gram.http.response.external attribute so relayed
	// upstream statuses (including 5xx) are distinguishable from Speakeasy faults.
	w.Header().Set(constants.HeaderProxiedResponse, "1")
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

// stripConfiguredCredentials removes every credential this proxy attaches on
// a project's behalf, for use when a redirect leaves the origin the
// credentials were configured for.
//
// net/http drops Authorization and Cookie itself, but only when the redirect
// leaves the initial hostname: it keeps them across a subdomain, a port
// change, and a downgrade to another scheme, and it knows nothing about
// configured headers. Without this an upstream could redirect to a host it
// controls and collect a project's API key.
func (p *Proxy) stripConfiguredCredentials(header http.Header) {
	header.Del("Authorization")
	header.Del("Cookie")
	mcpauthz.Strip(header)

	for _, h := range p.Headers {
		if h.Name != "" {
			header.Del(h.Name)
		}
		// The inbound header a pass-through reads from is forwarded verbatim
		// as well, so it has to go with the header it populates.
		if h.ValueFromRequestHeader != "" {
			header.Del(h.ValueFromRequestHeader)
		}
	}
}

// applyRequestHeaders populates the upstream request headers by copying forward-safe
// headers from the user request and overlaying the configured static and
// pass-through headers. Configured headers win on conflict.
//
// The user's Authorization header is always dropped — Speakeasy-issued
// credentials (API keys, Speakeasy-managed OAuth tokens, chat-session JWTs)
// are not meaningful upstream. When [Proxy.AuthorizationOverride] is
// non-empty, the proxy emits its own "Authorization: Bearer <override>"
// upstream after configured headers are resolved so per-user identity wins a
// legacy conflict with a static Authorization credential.
//
// Under [HeaderPolicyRemote] the other Speakeasy credential, session, caller
// assertion and tunnel transport headers ([IsProtectedInboundHeader]) are
// dropped from the copy as well, so a caller's API key or chat session never
// reaches an arbitrary upstream.
func (p *Proxy) applyRequestHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	tunneled := p.HeaderPolicy == HeaderPolicyTunneled
	for name, values := range userReq.Header {
		if isSkippedRequestHeader(name) {
			continue
		}
		if !tunneled && IsProtectedInboundHeader(name) {
			continue
		}
		for _, v := range values {
			remoteReq.Header.Add(name, v)
		}
	}

	if tunneled {
		if err := p.applyTunneledConfiguredHeaders(ctx, userReq, remoteReq); err != nil {
			return err
		}
	} else if err := p.applyRemoteConfiguredHeaders(ctx, userReq, remoteReq); err != nil {
		return err
	}

	if p.AuthorizationOverride != "" {
		remoteReq.Header.Set("Authorization", "Bearer "+p.AuthorizationOverride)
	}

	// Strip last so configured headers can't reintroduce Accept-Encoding after
	// the user-header filter: the Go transport must own content-encoding
	// negotiation, otherwise a gzipped upstream body reaches readJSONRPCBody
	// undecoded and bypasses response interception.
	remoteReq.Header.Del("Accept-Encoding")
	mcpauthz.Strip(remoteReq.Header)
	if p.CallerAssertion != nil {
		assertion, err := p.CallerAssertion(ctx)
		if err != nil {
			return oops.E(oops.CodeUnauthorized, err, "could not establish tunnel caller identity").LogWarn(ctx, p.Logger)
		}
		if assertion != "" {
			remoteReq.Header.Set(mcpauthz.Header, assertion)
		}
	}

	return nil
}

// applyTunneledConfiguredHeaders overlays configured headers under
// [HeaderPolicyTunneled].
func (p *Proxy) applyTunneledConfiguredHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	for _, h := range p.Headers {
		if mcpauthz.ReservedHeader(h.Name) || mcpauthz.ReservedHeader(h.ValueFromRequestHeader) {
			continue
		}
		// A configured header must not set or delete a standard MCP request
		// header: the client's value is forwarded untouched, as the
		// specification requires of an intermediary.
		if httpheaders.IsStandardMCPRequestHeader(h.Name) {
			continue
		}
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
	return nil
}

// applyRemoteConfiguredHeaders overlays configured headers under
// [HeaderPolicyRemote]. Every stored row is re-validated here, so a row written
// before the policy existed, or by another writer, cannot read a protected
// inbound header or claim a reserved name.
//
// A row that fails is suppressed when optional: its destination is cleared so
// a value the client sent under that name does not stand in for it. A required
// row that fails rejects the request with an error naming the header, never
// its value.
func (p *Proxy) applyRemoteConfiguredHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	for _, h := range p.Headers {
		// Configuration does not own the caller assertion or the client's
		// standard MCP request headers, which an intermediary must forward
		// untouched, so a row naming one is ignored.
		if mcpauthz.ReservedHeader(h.Name) || httpheaders.IsStandardMCPRequestHeader(strings.ReplaceAll(h.Name, "_", "-")) {
			continue
		}
		// A resolved upstream token owns Authorization. The configured row it
		// shadows is not sent, so it cannot impose a requirement of its own.
		if p.AuthorizationOverride != "" && headerKey(h.Name) == "authorization" {
			continue
		}

		err := checkStoredRemoteHeader(h)
		var value string
		if err == nil {
			value, err = h.Resolve(userReq)
		}
		if err == nil && value != "" {
			if verr := ValidateHeaderValue(value); verr != nil {
				err = fmt.Errorf("header %q: %w", h.Name, verr)
			}
		}
		if err != nil {
			if h.IsRequired {
				return oops.E(oops.CodeBadRequest, err, "%s", remoteHeaderFailureMessage(h, err)).LogWarn(ctx, p.Logger)
			}
			p.Logger.WarnContext(ctx, "skip invalid configured header for remote mcp server", attr.SlogError(err))
			value = ""
		}
		if value == "" {
			if checkStoredRemoteName(h.Name) == nil {
				remoteReq.Header.Del(h.Name)
			}
			continue
		}
		remoteReq.Header.Set(h.Name, value)
	}
	return nil
}

// remoteHeaderFailureMessage is the client-facing explanation for a required
// configured header that cannot be sent to a remote MCP server.
func remoteHeaderFailureMessage(h ConfiguredHeader, err error) string {
	switch {
	case errors.Is(err, ErrProtectedSource):
		return fmt.Sprintf("required header %q for remote mcp server cannot be populated from request header %q: Speakeasy does not forward caller credentials or Speakeasy headers upstream. Send the upstream credential in a separate request header, store a static credential, or configure upstream OAuth where the server supports it", h.Name, h.ValueFromRequestHeader)
	case errors.Is(err, ErrReservedHeader):
		return fmt.Sprintf("required header %q cannot be configured on a remote mcp server: change or remove it in the server's settings", h.Name)
	case errors.Is(err, ErrInvalidHeaderName), errors.Is(err, ErrInvalidHeaderValue):
		return fmt.Sprintf("required header %q for remote mcp server is not a valid HTTP header: change or remove it in the server's settings", h.Name)
	default:
		return "missing required header for remote mcp server"
	}
}

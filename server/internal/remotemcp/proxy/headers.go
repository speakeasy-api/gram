package proxy

import (
	"context"
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

	for _, h := range p.RoutingHeaders {
		if h.Name != "" {
			header.Del(h.Name)
		}
	}
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
	for _, h := range p.EnvironmentHeaders {
		if h.Name != "" {
			deleteFoldedHeader(header, h.Name)
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
func (p *Proxy) applyRequestHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	tunneled := p.HeaderPolicy == HeaderPolicyTunneled
	for name, values := range userReq.Header {
		if isSkippedRequestHeader(name) {
			continue
		}
		if tunneled && IsProtectedInboundHeader(name) {
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
	if err := p.applyEnvironmentHeaders(ctx, remoteReq); err != nil {
		return err
	}

	// Routing headers are Speakeasy's own transport state. They are applied
	// after configured headers so no configuration can displace them, and
	// they own every spelling of their name an upstream might fold together,
	// so a caller's Mcp_Session_Id cannot sit beside the pinned backend
	// Mcp-Session-Id.
	for _, h := range p.RoutingHeaders {
		value, err := h.Resolve(userReq)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "missing required header for remote mcp server").LogError(ctx, p.Logger)
		}
		deleteFoldedHeader(remoteReq.Header, h.Name)
		if value == "" {
			continue
		}
		remoteReq.Header.Set(h.Name, value)
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

// applyRemoteConfiguredHeaders overlays configured headers under
// [HeaderPolicyRemote].
func (p *Proxy) applyRemoteConfiguredHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	for _, h := range p.Headers {
		if p.shadowedByEnvironment(h.Name) {
			continue
		}
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

// applyTunneledConfiguredHeaders overlays configured headers under
// [HeaderPolicyTunneled]. Every stored row is re-validated here, so a row that
// predates the policy or came from another writer cannot claim a reserved
// name or read a protected inbound header.
//
// An optional row that fails is suppressed: its destination is cleared, so a
// value the client sent under that name does not stand in for it, unless the
// destination itself is protected, which leaves the client's protocol field or
// Speakeasy's routing field alone. A required row that fails rejects the
// request. Errors and logs name the header, never its value.
func (p *Proxy) applyTunneledConfiguredHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	for _, h := range p.Headers {
		if p.shadowedByEnvironment(h.Name) {
			continue
		}
		// A resolved upstream token owns Authorization. The configured row it
		// shadows is not sent, so it cannot impose a requirement of its own.
		if p.AuthorizationOverride != "" && headerKey(h.Name) == "authorization" {
			continue
		}

		if err := checkStoredTunneledHeader(h); err != nil {
			if h.IsRequired {
				return oops.E(oops.CodeBadRequest, err, "invalid required header for tunneled mcp server").LogWarn(ctx, p.Logger)
			}
			p.Logger.WarnContext(ctx, "skip invalid configured header for tunneled mcp server", attr.SlogError(err))
			if name, nerr := NormalizeHeaderName(h.Name); nerr == nil && !isReservedTunneledDestination(name) {
				deleteFoldedHeader(remoteReq.Header, name)
			}
			continue
		}

		value, err := h.Resolve(userReq)
		if err == nil && value != "" {
			if verr := ValidateHeaderValue(value); verr != nil {
				if h.IsRequired {
					err = fmt.Errorf("header %q: %w", h.Name, verr)
				}
				value = ""
			}
		}
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "missing required header for tunneled mcp server").LogWarn(ctx, p.Logger)
		}
		// A configured header owns its name in every spelling an upstream
		// might fold together, so the caller's alias cannot stand beside it.
		deleteFoldedHeader(remoteReq.Header, h.Name)
		if value == "" {
			continue
		}
		remoteReq.Header.Set(h.Name, value)
	}
	return nil
}

// shadowedByEnvironment reports whether an environment header replaces the
// configured row named name. A replaced row is not resolved at all, so a
// requirement it would impose, such as a required pass-through, is met by the
// environment's value instead.
func (p *Proxy) shadowedByEnvironment(name string) bool {
	for _, h := range p.EnvironmentHeaders {
		if headerKey(h.Name) == headerKey(name) {
			return true
		}
	}
	return false
}

// applyEnvironmentHeaders overlays the headers mapped from the MCP server's
// linked environment under either policy. Each one owns every spelling of its
// name, so neither a client field nor a source row folding to the same key
// can stand beside it. The rows were validated when the environment was
// loaded; they are checked again here so a row from any other writer still
// meets the strict policy, and a failure refuses the request rather than
// falling back to another value for the header.
func (p *Proxy) applyEnvironmentHeaders(ctx context.Context, remoteReq *http.Request) error {
	for _, h := range p.EnvironmentHeaders {
		// A resolved upstream token owns Authorization.
		if p.AuthorizationOverride != "" && headerKey(h.Name) == "authorization" {
			continue
		}
		if err := checkStoredTunneledHeader(h); err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid environment header for mcp server").LogWarn(ctx, p.Logger)
		}
		if h.ValueFromRequestHeader != "" || strings.Trim(h.StaticValue, " \t") == "" {
			return oops.E(oops.CodeBadRequest, fmt.Errorf("%w: header %q has no static value", ErrInvalidEnvironmentHeader, h.Name), "invalid environment header for mcp server").LogWarn(ctx, p.Logger)
		}
		deleteFoldedHeader(remoteReq.Header, h.Name)
		remoteReq.Header.Set(h.Name, h.StaticValue)
	}
	return nil
}

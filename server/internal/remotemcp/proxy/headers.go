package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/net/http/httpguts"

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

// HeaderPolicy selects how strictly a proxy treats operator-configured headers
// and the client headers it copies upstream.
type HeaderPolicy string

const (
	// HeaderPolicyRemote (the zero value) is applied to remote MCP servers:
	//   - client headers: Speakeasy credentials, sessions, caller assertions and
	//     tunnel transport fields ([IsProtectedInboundHeader]) are not copied
	//     upstream;
	//   - configured headers: may not read those headers as their source, may
	//     not set Set-Cookie/Proxy-Authorization or a request-sourced Cookie,
	//     and are re-validated on every request (an invalid optional row is
	//     dropped, an invalid required row fails the request with 400).
	HeaderPolicyRemote HeaderPolicy = ""

	// HeaderPolicyTunneled is the strict policy for tunneled MCP servers.
	// Speakeasy credentials, tunnel transport fields, the rest of the internal
	// X-Gram-* family, Authorization and caller assertions never reach the
	// upstream as a copied client header or as the source of a configured
	// header ([IsProtectedTunneledInboundHeader]), and configured headers may
	// not claim transport, framing or MCP protocol names. The tunnel's own
	// routing fields travel as [Proxy.RoutingHeaders], outside this policy.
	HeaderPolicyTunneled HeaderPolicy = "tunneled"
)

// ErrInvalidHeaderName reports a configured header name that is not a valid
// HTTP field name.
var ErrInvalidHeaderName = errors.New("invalid header name")

// ErrInvalidHeaderValue reports a configured header value containing bytes
// that cannot appear in an HTTP field value.
var ErrInvalidHeaderValue = errors.New("invalid header value")

// ErrReservedHeader reports a configured header whose name or source is
// reserved by the policy.
var ErrReservedHeader = errors.New("reserved header")

// ErrProtectedSource reports a configured header that reads a protected inbound
// header. It always accompanies [ErrReservedHeader].
var ErrProtectedSource = errors.New("protected source")

// ErrProtectedDestination reports a configured header that would send a
// request-sourced value under a protected name. It always accompanies
// [ErrReservedHeader].
var ErrProtectedDestination = errors.New("protected destination")

// NormalizeHeaderName validates raw as an HTTP field name and returns its
// canonical form. Control bytes anywhere in raw are rejected before any
// trimming, so a name carrying CR, LF or a tab is never quietly repaired into
// a valid one. Leading and trailing ASCII spaces are trimmed.
func NormalizeHeaderName(raw string) (string, error) {
	for i := range len(raw) {
		if c := raw[i]; c < 0x20 || c == 0x7f {
			return "", fmt.Errorf("%w: contains a control character", ErrInvalidHeaderName)
		}
	}
	name := strings.Trim(raw, " ")
	if !httpguts.ValidHeaderFieldName(name) {
		return "", fmt.Errorf("%w: %q is not an HTTP field name", ErrInvalidHeaderName, name)
	}
	return http.CanonicalHeaderKey(name), nil
}

// ValidateHeaderValue reports whether v may be sent as an HTTP field value.
// Horizontal tab is the only control character allowed.
func ValidateHeaderValue(v string) error {
	for i := range len(v) {
		if c := v[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return fmt.Errorf("%w: contains a control character", ErrInvalidHeaderValue)
		}
	}
	if !httpguts.ValidHeaderFieldValue(v) {
		return ErrInvalidHeaderValue
	}
	return nil
}

// headerKey folds a header name for policy matching. Underscores are treated
// as dashes because some upstream servers read X_Foo as X-Foo.
func headerKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", "-"))
}

// speakeasyHeaderPrefix covers Speakeasy's own request headers: API keys,
// dashboard and chat sessions, project selection and consent transport state.
const speakeasyHeaderPrefix = "gram-"

// speakeasyAIHeaderPrefix covers the documented Speakeasy-AI-* names for the
// same credentials and sessions. The public listener renames them to the
// older names before handlers run, but not every route installs that
// middleware, so the policy matches both spellings itself.
const speakeasyAIHeaderPrefix = "speakeasy-ai-"

// tunnelHeaderPrefix covers the tunnel transport family exchanged between
// Speakeasy, the tunnel gateway and the tunnel agent.
const tunnelHeaderPrefix = "x-gram-tunnel-"

// IsProtectedInboundHeader reports whether a header on the inbound request
// carries a Speakeasy credential, session, caller assertion or tunnel
// transport field. Under [HeaderPolicyRemote] such a header is never copied
// upstream and never used as the source of a configured header.
//
// Authorization is not listed: the copy already drops it, and a configured
// header may forward the caller's own upstream credential from it.
func IsProtectedInboundHeader(name string) bool {
	if mcpauthz.ReservedHeader(name) {
		return true
	}
	key := headerKey(name)
	if strings.HasPrefix(key, speakeasyHeaderPrefix) || strings.HasPrefix(key, speakeasyAIHeaderPrefix) || strings.HasPrefix(key, tunnelHeaderPrefix) {
		return true
	}
	switch key {
	case
		"proxy-authorization",
		"cookie",
		"set-cookie",
		"x-gram-agent-version":
		return true
	}
	return false
}

// internalHeaderPrefix covers Speakeasy's internal X-Gram-* family: the
// tunnel transport fields exchanged with the gateway and agent, the agent
// version, and internal request controls such as scope overrides.
const internalHeaderPrefix = "x-gram-"

// IsProtectedTunneledInboundHeader reports whether a header on the inbound
// request is protected under [HeaderPolicyTunneled]: everything
// [IsProtectedInboundHeader] protects, plus the rest of Speakeasy's internal
// X-Gram-* family and Authorization. Such a header is never copied to a
// tunneled upstream and never used as the source of one of its configured
// headers. Authorization may still be configured as a destination.
//
// The remote set stays narrower on purpose: remote servers may forward the
// caller's own upstream credential from Authorization and may read
// non-transport X-Gram-* headers such as X-Gram-Owner.
func IsProtectedTunneledInboundHeader(name string) bool {
	if IsProtectedInboundHeader(name) {
		return true
	}
	key := headerKey(name)
	return key == "authorization" || strings.HasPrefix(key, internalHeaderPrefix)
}

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
//
// Protected client headers are dropped from the copy under both policies:
// [IsProtectedInboundHeader] for [HeaderPolicyRemote] and
// [IsProtectedTunneledInboundHeader] for [HeaderPolicyTunneled]. Environment
// headers then replace configured rows, and Speakeasy's routing headers are
// applied last so nothing configured can displace them.
func (p *Proxy) applyRequestHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	tunneled := p.HeaderPolicy == HeaderPolicyTunneled
	for name, values := range userReq.Header {
		if isSkippedRequestHeader(name) {
			continue
		}
		if tunneled && IsProtectedTunneledInboundHeader(name) {
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

	// The caller's User-Agent is forwarded as is; only a missing one gets ours.
	if remoteReq.Header.Get("User-Agent") == "" {
		remoteReq.Header.Set("User-Agent", constants.UserAgent)
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

// applyRemoteConfiguredHeaders overlays configured headers under
// [HeaderPolicyRemote], re-validating every stored row so one written before
// the policy existed cannot bypass it. A failing optional row is suppressed;
// a failing required row rejects the request, naming the header, never its
// value.
func (p *Proxy) applyRemoteConfiguredHeaders(ctx context.Context, userReq *http.Request, remoteReq *http.Request) error {
	for _, h := range p.Headers {
		if p.shadowedByEnvironment(h.Name) {
			continue
		}
		if isUnownedHeader(h.Name) {
			continue
		}
		// A resolved upstream token owns Authorization. The configured row it
		// shadows is not sent, so it cannot impose a requirement of its own.
		if p.AuthorizationOverride != "" && headerKey(h.Name) == "authorization" {
			continue
		}

		value, err := resolveRemoteHeader(h, userReq)
		if err != nil {
			if h.IsRequired {
				return oops.E(oops.CodeBadRequest, err, "%s", remoteHeaderFailureMessage(h, err)).LogWarn(ctx, p.Logger)
			}
			p.logWithIdentity(ctx, slog.LevelWarn, "skip invalid configured header for remote mcp server", attr.SlogRemoteMCPConfiguredHeaderName(h.Name), attr.SlogError(err))
		}
		clearRemoteDestination(remoteReq.Header, h.Name)
		if value != "" {
			remoteReq.Header.Set(h.Name, value)
		}
	}
	return nil
}

// clearRemoteDestination removes client values under the name of a
// configured header, so they cannot stand in for it or sit beside it.
// Every spelling an upstream may fold together goes, matched by the trimmed
// name the client would send. Unowned headers are never touched.
func clearRemoteDestination(header http.Header, stored string) {
	name, err := NormalizeHeaderName(stored)
	if err != nil {
		return
	}
	if isUnownedHeader(name) {
		return
	}
	key := headerKey(name)
	for present := range header {
		if headerKey(present) == key {
			delete(header, present)
		}
	}
}

// isUnownedHeader reports whether name belongs to the caller assertion or the
// client's standard MCP request headers, which configuration never sets or
// clears: an intermediary must forward the latter untouched.
func isUnownedHeader(name string) bool {
	return mcpauthz.ReservedHeader(name) || httpheaders.IsStandardMCPRequestHeader(strings.ReplaceAll(name, "_", "-"))
}

// CheckRemoteHeader validates a configured header against
// [HeaderPolicyRemote]. Names are matched case-insensitively, with
// underscores read as dashes.
func CheckRemoteHeader(h ConfiguredHeader) error {
	if h.ValueFromRequestHeader != "" && IsProtectedInboundHeader(h.ValueFromRequestHeader) {
		return fmt.Errorf("%w: %w: %q cannot be forwarded to a remote MCP server", ErrReservedHeader, ErrProtectedSource, h.ValueFromRequestHeader)
	}
	if isReservedRemoteDestination(h) {
		return fmt.Errorf("%w: %q cannot be configured on a remote MCP server", ErrReservedHeader, h.Name)
	}
	if h.ValueFromRequestHeader != "" && IsProtectedInboundHeader(h.Name) {
		return fmt.Errorf("%w: %w: %q cannot be populated from a request header", ErrReservedHeader, ErrProtectedDestination, h.Name)
	}
	if h.StaticValue != "" {
		if err := ValidateHeaderValue(h.StaticValue); err != nil {
			return err
		}
	}
	return nil
}

// isReservedRemoteDestination reports whether a configured header may not be
// sent under its name to a remote upstream. Cookie is allowed only as a static
// operator credential, never populated from the inbound request.
func isReservedRemoteDestination(h ConfiguredHeader) bool {
	if isUnownedHeader(h.Name) {
		return true
	}
	switch headerKey(h.Name) {
	case "set-cookie", "proxy-authorization":
		return true
	case "cookie":
		return h.ValueFromRequestHeader != ""
	}
	return false
}

// checkStoredRemoteHeader re-validates a stored configured header at request
// time. Any casing is accepted, but surrounding whitespace is not: the name is
// sent and later removed exactly as stored.
func checkStoredRemoteHeader(h ConfiguredHeader) error {
	names := []string{h.Name}
	if h.ValueFromRequestHeader != "" {
		names = append(names, h.ValueFromRequestHeader)
	}
	for _, raw := range names {
		name, err := NormalizeHeaderName(raw)
		if err != nil {
			return err
		}
		if !strings.EqualFold(name, raw) {
			return fmt.Errorf("%w: %q has surrounding whitespace", ErrInvalidHeaderName, raw)
		}
	}
	return CheckRemoteHeader(h)
}

// resolveRemoteHeader validates a stored header and resolves the value to send.
func resolveRemoteHeader(h ConfiguredHeader, userReq *http.Request) (string, error) {
	if err := checkStoredRemoteHeader(h); err != nil {
		return "", err
	}
	value, err := h.Resolve(userReq)
	if err != nil || value == "" {
		return "", err
	}
	if err := ValidateHeaderValue(value); err != nil {
		return "", fmt.Errorf("header %q: %w", h.Name, err)
	}
	return value, nil
}

// remoteHeaderFailureMessage is the client-facing explanation for a required
// configured header that cannot be sent to a remote MCP server.
func remoteHeaderFailureMessage(h ConfiguredHeader, err error) string {
	switch {
	case errors.Is(err, ErrProtectedSource):
		return fmt.Sprintf("required header %q cannot read request header %q: Speakeasy credentials and cookies are never forwarded to remote MCP servers", h.Name, h.ValueFromRequestHeader)
	case errors.Is(err, ErrProtectedDestination):
		return fmt.Sprintf("required header %q cannot be populated from a request header: it is a Speakeasy header", h.Name)
	case errors.Is(err, ErrReservedHeader):
		return fmt.Sprintf("required header %q cannot be configured on a remote mcp server: change or remove it in the server's settings", h.Name)
	case errors.Is(err, ErrInvalidHeaderName), errors.Is(err, ErrInvalidHeaderValue):
		return fmt.Sprintf("required header %q for remote mcp server is not a valid HTTP header: change or remove it in the server's settings", h.Name)
	default:
		return "missing required header for remote mcp server"
	}
}

// isReservedTunneledDestination reports whether a configured header may not
// be sent under name to a tunneled upstream. Authorization is allowed: a
// static service credential is a legitimate configuration, and a resolved
// upstream token still takes precedence over it.
func isReservedTunneledDestination(name string) bool {
	if httpheaders.IsStandardMCPRequestHeader(strings.ReplaceAll(name, "_", "-")) {
		return true
	}
	key := headerKey(name)
	if key == "authorization" {
		return false
	}
	if IsProtectedTunneledInboundHeader(name) {
		return true
	}
	switch key {
	case
		// Hop-by-hop and framing fields belong to the HTTP transport.
		"connection",
		"keep-alive",
		"proxy-authenticate",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade",
		"content-length",
		"host",
		"accept-encoding",
		// MCP Streamable HTTP session and resumption fields.
		"mcp-session-id",
		"last-event-id":
		return true
	}
	return false
}

// CheckTunneledHeader validates a configured header against
// [HeaderPolicyTunneled]. h.Name and h.ValueFromRequestHeader must already be
// normalized with [NormalizeHeaderName]; a static value is checked with
// [ValidateHeaderValue].
func CheckTunneledHeader(h ConfiguredHeader) error {
	if isReservedTunneledDestination(h.Name) {
		return fmt.Errorf("%w: %q cannot be configured on a tunneled MCP server", ErrReservedHeader, h.Name)
	}
	if h.ValueFromRequestHeader != "" && IsProtectedTunneledInboundHeader(h.ValueFromRequestHeader) {
		return fmt.Errorf("%w: %w: %q cannot be forwarded to a tunneled MCP server", ErrReservedHeader, ErrProtectedSource, h.ValueFromRequestHeader)
	}
	if h.StaticValue != "" {
		if err := ValidateHeaderValue(h.StaticValue); err != nil {
			return err
		}
	}
	return nil
}

// checkStoredTunneledHeader re-validates a stored configured header at request
// time, including its name and source syntax, so a row written before the
// policy existed, or by another writer, cannot bypass it.
func checkStoredTunneledHeader(h ConfiguredHeader) error {
	name, err := NormalizeHeaderName(h.Name)
	if err != nil {
		return err
	}
	if name != h.Name {
		return fmt.Errorf("%w: %q is not in canonical form", ErrInvalidHeaderName, h.Name)
	}
	if h.ValueFromRequestHeader != "" {
		if _, err := NormalizeHeaderName(h.ValueFromRequestHeader); err != nil {
			return err
		}
	}
	return CheckTunneledHeader(h)
}

// deleteFoldedHeader removes every field in header whose name folds to the
// same key as name, so an inbound X_Tenant cannot sit beside a configured
// X-Tenant at an upstream that reads the two as one.
func deleteFoldedHeader(header http.Header, name string) {
	key := headerKey(name)
	for existing := range header {
		if headerKey(existing) == key {
			delete(header, existing)
		}
	}
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

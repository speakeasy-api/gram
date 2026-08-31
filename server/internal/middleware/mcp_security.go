package middleware

import (
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// MCPSecurity enforces the MCP Streamable HTTP transport's browser-facing
// security requirements on the MCP JSON-RPC endpoints: Origin validation
// (2025-11-25 and 2026-07-28 both make this a MUST, answered with 403) and
// rejection of the un-preflighted Content-Type values that would otherwise
// route around it.
//
// Origin validation delegates to net/http.CrossOriginProtection, which decides
// cross-origin-ness from Sec-Fetch-Site (sent by every browser since 2023 and
// not settable from JavaScript), falling back to comparing the Origin host
// with Host. Crucially it allows requests carrying neither header, so native
// MCP clients — Claude Desktop, Cursor, CLI tools — pass untouched and no
// per-server origin configuration is needed. Note that the pinned
// modelcontextprotocol/go-sdk v1.7.0 does NOT do this for you: its
// StreamableHTTPOptions.CrossOriginProtection defaults to nil and is only
// populated when MCPGODEBUG=enableoriginverification=1 (streamable.go:243),
// which this deployment does not set — so /platform-mcp, which is served by
// that handler, depends on this middleware too.
//
// Two categories of legitimate cross-origin browser traffic are exempt:
//
//   - Gram Elements, embedded on customer domains. Those requests carry a
//     chat-session token whose audience claim names the embedding origin;
//     chatSessionsCORS validates Origin against that claim and marks the
//     request trusted (see markChatSessionOriginTrusted). The audience claim,
//     not this middleware, is the trusted-origin mechanism for Elements.
//   - The Gram first-party origins passed as trustedOrigins. The dashboard's
//     MCP inspection tabs connect to a customer's *custom domain*, which is
//     cross-site from app.getgram.ai and cannot be rebased onto the Gram
//     origin — mcp_endpoint rows are looked up by (slug, custom_domain_id),
//     so the same slug does not resolve on the platform host. Trusting our own
//     origin costs nothing an attacker could use: MCP auth reads only
//     Authorization and Gram-Chat-Session, never cookies, so a request forged
//     from app.getgram.ai carries no ambient credential to replay.
//
// Registration must come after CORSMiddleware so this runs downstream of
// chatSessionsCORS and can observe the trust marker it sets. Non-MCP routes
// are untouched; OPTIONS preflights never reach here for /mcp because
// chatSessionsCORS answers them itself.
func MCPSecurity(logger *slog.Logger, trustedOrigins []string) (func(http.Handler) http.Handler, error) {
	protection := http.NewCrossOriginProtection()
	for _, origin := range trustedOrigins {
		if origin == "" {
			continue
		}
		normalized, err := normalizeOrigin(origin)
		if err != nil {
			return nil, err
		}
		if err := protection.AddTrustedOrigin(normalized); err != nil {
			return nil, fmt.Errorf("add trusted origin %q: %w", normalized, err)
		}
	}

	logger = logger.With(attr.SlogComponent("mcp-security"))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMCPJSONRPCEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			if !chatSessionOriginTrusted(r.Context()) {
				if err := protection.Check(originCheckProbe(r)); err != nil {
					logMCPSecurityRejection(r, logger, "cross_origin", err.Error())
					http.Error(w, "forbidden: cross-origin request rejected", http.StatusForbidden)
					return
				}
			}

			if r.Method == http.MethodPost && !isJSONRequest(r.Header.Get("Content-Type")) {
				logMCPSecurityRejection(r, logger, "content_type", r.Header.Get("Content-Type"))
				http.Error(w, "Content-Type must be 'application/json'", http.StatusUnsupportedMediaType)
				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

// originCheckProbe returns the request CrossOriginProtection should judge.
//
// Check exempts GET and HEAD as "safe methods", on the reasoning that a safe
// method performs no state change. That does not hold for MCP: GET on a
// Streamable HTTP endpoint opens the standalone SSE stream, and against a
// proxy-backed server it establishes a session upstream. The spec's Origin
// MUST is not scoped to a method either, so a cross-site GET is exactly the
// DNS-rebinding shape the requirement exists to refuse.
//
// Check reads only the method, Sec-Fetch-Site, Origin, and Host, so presenting
// GET and HEAD under POST semantics applies the identical origin logic without
// reimplementing it. The copy is shallow and the headers are read-only in
// Check. OPTIONS is left alone: a preflight must be answerable, and one never
// reaches this middleware anyway because CORSMiddleware and chatSessionsCORS
// both answer OPTIONS before calling next.
func originCheckProbe(r *http.Request) *http.Request {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		probe := *r
		probe.Method = http.MethodPost
		return &probe
	default:
		return r
	}
}

// normalizeOrigin reduces a configured URL to the bare scheme://host form
// AddTrustedOrigin demands. server-url and site-url are ordinary URL flags
// everywhere else in the server and a trailing slash is legal in them, but
// AddTrustedOrigin rejects any path at all — including "/" — so passing one
// through unnormalized would refuse to boot the server.
func normalizeOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse trusted origin %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("trusted origin %q needs a scheme and host", raw)
	}

	// AddTrustedOrigin compares the configured string to the Origin header
	// byte for byte, and a browser sends a lowercased host with the scheme's
	// default port omitted. A flag set to "https://APP.getgram.ai" or
	// "https://app.getgram.ai:443" would otherwise register an origin nothing
	// can ever match, silently dropping the exemption.
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Host)
	if (scheme == "https" && strings.HasSuffix(host, ":443")) ||
		(scheme == "http" && strings.HasSuffix(host, ":80")) {
		host = host[:strings.LastIndex(host, ":")]
	}

	return scheme + "://" + host, nil
}

// isJSONRequest reports whether a Content-Type header names application/json,
// ignoring parameters such as "; charset=utf-8".
//
// This matches modelcontextprotocol/go-sdk, whose Streamable HTTP handler
// rejects any POST whose base media type is not application/json with 415
// (streamable.go:388). That handler already serves Gram's own /platform-mcp
// endpoint, so anything laxer here would leave the hosted MCP surfaces more
// permissive than one we already ship.
//
// An absent or unparseable header fails closed. Every conforming MCP client
// sends application/json (the go-sdk client sets it unconditionally), so a
// request without it is not a client this transport supports. Rejecting it
// also closes the CORS-simple-request bypass, where a JSON-RPC body sent as
// text/plain reaches a handler having never been preflighted.
func isJSONRequest(header string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}

// logMCPSecurityRejection records a rejection with enough context to tell an
// attack apart from a first-party client that lost its exemption. Enforcement
// ships without a staged rollout, so this log is the only signal available for
// answering "did we break someone" — keep it on every rejection path.
func logMCPSecurityRejection(r *http.Request, logger *slog.Logger, reason, detail string) {
	logger.InfoContext(r.Context(), "rejected mcp request",
		attr.SlogReason(reason),
		attr.SlogErrorMessage(detail),
		attr.SlogHTTPRequestMethod(r.Method),
		attr.SlogURLOriginal(r.URL.Path),
		attr.SlogHTTPRequestHeaderOrigin(r.Header.Get("Origin")),
		attr.SlogHTTPRequestHeaderSecFetchSite(r.Header.Get("Sec-Fetch-Site")),
		attr.SlogHTTPRequestHeaderContentType(r.Header.Get("Content-Type")),
		attr.SlogHostName(r.Host),
		attr.SlogHTTPRequestHeaderUserAgent(r.Header.Get("User-Agent")),
	)
}

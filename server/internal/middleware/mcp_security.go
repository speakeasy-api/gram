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

// simpleRequestContentTypes are the three media types a browser can send on a
// cross-origin POST without triggering a CORS preflight (the Fetch spec's
// "CORS-safelisted request-header" set for Content-Type). A JSON-RPC body sent
// under one of these reaches an MCP handler having never been preflighted, so
// on the surfaces whose only protection was the preflight it bypassed the
// check entirely.
//
// This is deliberately a denylist of the bypass vector rather than an
// allowlist of application/json. Requiring application/json would reject
// conforming non-browser callers for no real gain: `curl -d @body.json` with
// no -H sends application/x-www-form-urlencoded, and a bare POST with no
// Content-Type at all is common in hand-rolled integrations.
//
// An absent Content-Type is allowed even though a browser CAN produce one
// un-preflighted (fetch with a Uint8Array or a type-less Blob body sends no
// Content-Type). That case is covered by the Origin check above, which is the
// real control here; this check is defence in depth for the pre-Sec-Fetch-Site
// browsers that the Origin check falls back to Host comparison for. Rejecting
// an absent header would break real clients to close nothing.
var simpleRequestContentTypes = map[string]struct{}{
	"text/plain":                        {},
	"multipart/form-data":               {},
	"application/x-www-form-urlencoded": {},
}

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
				if err := protection.Check(r); err != nil {
					logMCPSecurityRejection(r, logger, "cross_origin", err.Error())
					http.Error(w, "forbidden: cross-origin request rejected", http.StatusForbidden)
					return
				}
			}

			if r.Method == http.MethodPost {
				if mediaType, ok := disallowedRequestMediaType(r.Header.Get("Content-Type")); ok {
					logMCPSecurityRejection(r, logger, "content_type", mediaType)
					http.Error(w, "unsupported media type: send MCP requests as application/json", http.StatusUnsupportedMediaType)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}, nil
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
	return parsed.Scheme + "://" + parsed.Host, nil
}

// disallowedRequestMediaType reports whether a Content-Type header names one
// of the CORS-simple media types, returning the media type for telemetry. An
// empty header is allowed (see simpleRequestContentTypes).
//
// A header Go cannot parse falls back to the bare type token rather than being
// waved through. Fetch's CORS-safelist check uses a more lenient MIME parser
// than mime.ParseMediaType, so values Go rejects — "text/plain;;",
// "multipart/form-data; boundary=", a duplicated charset parameter — are still
// sent un-preflighted by a browser and would otherwise walk straight past this
// check.
func disallowedRequestMediaType(header string) (string, bool) {
	if strings.TrimSpace(header) == "" {
		return "", false
	}

	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		bare, _, _ := strings.Cut(header, ";")
		mediaType = strings.ToLower(strings.TrimSpace(bare))
	}

	_, disallowed := simpleRequestContentTypes[mediaType]
	return mediaType, disallowed
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

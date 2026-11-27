package mcp

import (
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// unmatchedMCPMetadataMessage answers a discovery request for an MCP URL that
// matches no route. Nothing is published there, whether or not the server
// exists, so the message names the path rather than the server.
const unmatchedMCPMetadataMessage = "oauth metadata not found: no OAuth or OpenID metadata is published at this URL"

// unmatchedMCPTransportMessage answers an MCP request to a URL that matches no
// route, such as a server URL with an extra path segment or an empty slug.
// It depends only on the shape of the path, never on whether a server exists,
// so it may differ from publicMCPNotFoundMessage without revealing anything.
const unmatchedMCPTransportMessage = "mcp server not found: no MCP server is served at this URL. Check that the URL has no extra path segment, or copy the server's current URL from the MCP page in the Speakeasy dashboard and update your MCP client configuration"

// unmatchedMCPRequestKind classifies a request for an MCP URL that no route
// matched by what an MCP client sent it for.
type unmatchedMCPRequestKind string

const (
	// unmatchedMCPRequestOther is any request an MCP client would not send.
	// It keeps the plain 404.
	unmatchedMCPRequestOther unmatchedMCPRequestKind = "other"

	// unmatchedMCPRequestDiscovery is an OAuth or OpenID metadata lookup for
	// an MCP server URL, either appended to it or inserted before its path.
	unmatchedMCPRequestDiscovery unmatchedMCPRequestKind = "discovery"

	// unmatchedMCPRequestTransport is a Streamable HTTP request: a JSON body
	// POST, or any request carrying the MCP protocol version or session
	// header.
	unmatchedMCPRequestTransport unmatchedMCPRequestKind = "transport"
)

// unmatchedMCPDiscoveryPaths are the metadata documents MCP clients look up
// for an MCP server URL.
var unmatchedMCPDiscoveryPaths = []string{
	wellknown.OAuthProtectedResourcePath,
	wellknown.OAuthAuthorizationServerPath,
	wellknown.OpenIDConfigurationPath,
}

// NotFoundHandler answers requests no route matched. For an MCP URL, a
// request shaped like an MCP client's gets a JSON 404 the client can parse:
// discovery lookups get the HTTP error shape, transport requests a JSON-RPC
// not-found. Every other request keeps the plain 404.
//
// Install it as the not-found handler of every mux that MCP routes are
// attached to. It runs inside the mux, after its middleware.
func NotFoundHandler(logger *slog.Logger) http.HandlerFunc {
	discovery := oops.ErrHandle(logger, func(http.ResponseWriter, *http.Request) error {
		return oops.E(oops.CodeNotFound, nil, unmatchedMCPMetadataMessage)
	})
	transport := oops.MCPErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		cause := oops.E(oops.CodeNotFound, nil, unmatchedMCPTransportMessage)
		if r.Method != http.MethodPost {
			return cause
		}
		return rejectMissingMCPTarget(w, r, logger, mcpversions.SupportedHostedToolset(), cause)
	})

	return func(w http.ResponseWriter, r *http.Request) {
		switch classifyUnmatchedMCPRequest(r) {
		case unmatchedMCPRequestDiscovery:
			discovery.ServeHTTP(w, r)
		case unmatchedMCPRequestTransport:
			transport.ServeHTTP(w, r)
		case unmatchedMCPRequestOther:
			http.NotFound(w, r)
		}
	}
}

func classifyUnmatchedMCPRequest(r *http.Request) unmatchedMCPRequestKind {
	mcpPath := "/" + mcproute.MCP.String()
	isDiscoveryMethod := r.Method == http.MethodGet || r.Method == http.MethodHead

	// RFC 9728 and RFC 8414 insert the well-known segment before the server
	// URL's path, so a lookup for /mcp/<slug>/ arrives as
	// /.well-known/oauth-protected-resource/mcp/<slug>/.
	for _, discoveryPath := range unmatchedMCPDiscoveryPaths {
		if isPathOrBelow(r.URL.Path, discoveryPath+mcpPath) {
			if isDiscoveryMethod {
				return unmatchedMCPRequestDiscovery
			}
			return unmatchedMCPRequestOther
		}
	}

	if !isPathOrBelow(r.URL.Path, mcpPath) {
		return unmatchedMCPRequestOther
	}

	if isDiscoveryMethod {
		cleaned := path.Clean(r.URL.Path)
		for _, discoveryPath := range unmatchedMCPDiscoveryPaths {
			if strings.HasSuffix(cleaned, discoveryPath) {
				return unmatchedMCPRequestDiscovery
			}
		}
	}

	if r.Header.Get(mcpversions.HTTPHeader) != "" || r.Header.Get(proxy.McpSessionIDHeader) != "" {
		return unmatchedMCPRequestTransport
	}

	if r.Method == http.MethodPost {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err == nil && mediaType == "application/json" {
			return unmatchedMCPRequestTransport
		}
	}

	return unmatchedMCPRequestOther
}

// isPathOrBelow reports whether urlPath is prefix itself or a path beneath it,
// so /mcp matches /mcp and /mcp/a but not /mcpextra.
func isPathOrBelow(urlPath, prefix string) bool {
	return urlPath == prefix || strings.HasPrefix(urlPath, prefix+"/")
}

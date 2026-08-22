package mcp

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// routeUpstreamToken selects the single Authorization value a proxied MCP
// backend forwards. These cover the pure selection branches the DB-backed
// resolver tests cannot reach cheaply: qualified routing by the credential's
// grant-time RFC 8707 resource, and the fail-closed ambiguity paths.

func TestRouteUpstreamToken_EmptyMapReturnsEmpty(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), nil, "https://upstream.example.com/mcp")
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestRouteUpstreamToken_SingleEntryReturnsToken(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "upstream-token", Resource: "", RemoteSessionClientID: uuid.New()},
	}, "https://upstream.example.com/mcp")
	require.NoError(t, err)
	require.Equal(t, "upstream-token", token)
}

func TestRouteUpstreamToken_SingleEntryResourceMismatchLogsDebug(t *testing.T) {
	t.Parallel()

	// A lone credential is forwarded even when its recorded resource disagrees
	// with the backend's, but the disagreement must leave a debug-level trace
	// so a future strict-matching tightening can be sized from logs.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	token, err := routeUpstreamToken(t.Context(), logger, map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "upstream-token", Resource: "https://other.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://upstream.example.com/mcp")
	require.NoError(t, err)
	require.Equal(t, "upstream-token", token, "the lone credential must still be forwarded")
	require.Contains(t, buf.String(), "recorded resource does not match")

	// A matching resource (modulo trailing slash) must stay silent.
	buf.Reset()
	token, err = routeUpstreamToken(t.Context(), logger, map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "upstream-token", Resource: "https://upstream.example.com/mcp/", RemoteSessionClientID: uuid.New()},
	}, "https://upstream.example.com/mcp")
	require.NoError(t, err)
	require.Equal(t, "upstream-token", token)
	require.Empty(t, buf.String())
}

func TestRouteUpstreamToken_MultipleEntriesRoutesByResource(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://b.example.com/mcp")
	require.NoError(t, err)
	require.Equal(t, "token-b", token)
}

func TestRouteUpstreamToken_ResourceMatchIgnoresTrailingSlash(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp/", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://a.example.com/mcp")
	require.NoError(t, err)
	require.Equal(t, "token-a", token)
}

func TestRouteUpstreamToken_MultipleEntriesNoResourceFailsClosed(t *testing.T) {
	t.Parallel()

	// Tunneled backends record no upstream resource; with more than one
	// candidate credential there is nothing to route by.
	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "")
	require.Error(t, err)
	require.Empty(t, token)
}

func TestRouteUpstreamToken_MultipleEntriesNoMatchFailsClosed(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://b.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://c.example.com/mcp")
	require.Error(t, err)
	require.Empty(t, token)
}

func TestRouteUpstreamToken_DuplicateResourceFailsClosed(t *testing.T) {
	t.Parallel()

	token, err := routeUpstreamToken(t.Context(), testenv.NewLogger(t), map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
	}, "https://a.example.com/mcp")
	require.Error(t, err)
	require.Empty(t, token)
}

func TestTunnelGatewayURL_NormalizesAcceptedAddrs(t *testing.T) {
	t.Parallel()

	for addr, want := range map[string]string{
		"10.0.0.5:8090":                        "http://10.0.0.5:8090",       // host:port defaults to http
		"tunnel-gateway:8090":                  "http://tunnel-gateway:8090", // dns host:port defaults to http
		"http://tunnel-gateway:8090":           "http://tunnel-gateway:8090",
		"https://tunnel-gateway.internal:8443": "https://tunnel-gateway.internal:8443",
	} {
		got, err := tunnelrouting.GatewayURL(addr)
		require.NoError(t, err, "addr %q", addr)
		require.Equal(t, want, got, "addr %q", addr)
	}
}

func TestTunnelGatewayURL_RejectsInvalidAddrs(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{
		"https:///missing-host",
		"http:tunnel-gateway:8090", // opaque URL
		"http://:8090",             // empty hostname
		"ftp://tunnel-gateway:8090",
	} {
		_, err := tunnelrouting.GatewayURL(addr)
		require.Error(t, err, "addr %q", addr)
	}
}

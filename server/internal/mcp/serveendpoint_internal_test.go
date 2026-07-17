package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
)

// singleUpstreamToken collapses the per-remote-issuer token map for the
// single-Authorization remote-MCP backend. These cover the branches that the
// DB-backed resolver tests cannot reach while the one_per_issuer index still
// caps a user_session_issuer at one remote client.

func TestSingleUpstreamToken_EmptyMapReturnsEmpty(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(nil)
	require.NoError(t, err)
	require.Empty(t, token)
}

func TestSingleUpstreamToken_SingleEntryReturnsToken(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(map[uuid.UUID]string{uuid.New(): "upstream-token"})
	require.NoError(t, err)
	require.Equal(t, "upstream-token", token)
}

func TestSingleUpstreamToken_MultipleEntriesFailsClosed(t *testing.T) {
	t.Parallel()

	token, err := singleUpstreamToken(map[uuid.UUID]string{
		uuid.New(): "token-a",
		uuid.New(): "token-b",
	})
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

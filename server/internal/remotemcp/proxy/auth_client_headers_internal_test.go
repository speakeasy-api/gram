package proxy

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestIsCredentialHeader(t *testing.T) {
	t.Parallel()

	require.True(t, IsCredentialHeader("authorization", false, false))
	require.True(t, IsCredentialHeader(" Authorization", false, false))
	require.True(t, IsCredentialHeader("X-Api-Key", true, false), "a secret value is a credential")
	require.True(t, IsCredentialHeader("X-Token", false, true), "a forwarded value is a credential")
	require.False(t, IsCredentialHeader("X-Workspace", false, false))
}

func TestMergeAuthClientHeadersRemoteClientWins(t *testing.T) {
	t.Parallel()

	server := []ConfiguredHeader{
		{Name: "X-Workspace", StaticValue: "server-ws"},
		{Name: "X-Region", StaticValue: "eu"},
	}
	client := []ConfiguredHeader{
		{Name: "x_workspace", StaticValue: "client-ws"},
		{Name: "Authorization", StaticValue: "Basic client"},
	}
	merged := MergeAuthClientHeaders(HeaderPolicyRemote, server, client)
	require.Equal(t, []ConfiguredHeader{
		{Name: "X-Region", StaticValue: "eu"},
		{Name: "x_workspace", StaticValue: "client-ws"},
		{Name: "Authorization", StaticValue: "Basic client"},
	}, merged)
}

func TestMergeAuthClientHeadersTunneledRoutingWins(t *testing.T) {
	t.Parallel()

	routing := []ConfiguredHeader{{Name: "X-Tunnel-ID", StaticValue: "tunnel"}}
	client := []ConfiguredHeader{
		{Name: "X-Tunnel-Id", StaticValue: "spoofed"},
		{Name: "X-Api-Key", StaticValue: "client-key"},
	}
	merged := MergeAuthClientHeaders(HeaderPolicyTunneled, routing, client)
	require.Equal(t, []ConfiguredHeader{
		{Name: "X-Api-Key", StaticValue: "client-key"},
		{Name: "X-Tunnel-ID", StaticValue: "tunnel"},
	}, merged)
}

func TestApplyRequestHeadersAuthClientOrder(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	userReq.Header.Set("Authorization", "Bearer speakeasy-session")
	userReq.Header.Set("X-Caller-Key", "caller-key")
	server := []ConfiguredHeader{{Name: "X-Workspace", StaticValue: "server-ws"}}
	client := []ConfiguredHeader{
		{Name: "X-Workspace", StaticValue: "client-ws"},
		{Name: "Authorization", StaticValue: "Basic client-static"},
		{Name: "X-Api-Key", ValueFromRequestHeader: "X-Caller-Key"},
	}

	t.Run("client headers follow the server's and the bearer comes last", func(t *testing.T) {
		t.Parallel()
		remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
		p := &Proxy{Logger: testenv.NewLogger(t), Headers: MergeAuthClientHeaders(HeaderPolicyRemote, server, client), AuthorizationOverride: "resolved-bearer"}
		require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
		require.Equal(t, "client-ws", remoteReq.Header.Get("X-Workspace"))
		require.Equal(t, "Bearer resolved-bearer", remoteReq.Header.Get("Authorization"), "the bearer cannot be overridden")
		require.Equal(t, "caller-key", remoteReq.Header.Get("X-Api-Key"))
	})

	t.Run("a static client sends its own Authorization and never the caller's", func(t *testing.T) {
		t.Parallel()
		remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
		p := &Proxy{Logger: testenv.NewLogger(t), Headers: MergeAuthClientHeaders(HeaderPolicyRemote, server, client), AuthorizationOverride: ""}
		require.NoError(t, p.applyRequestHeaders(t.Context(), userReq, remoteReq))
		require.Equal(t, "Basic client-static", remoteReq.Header.Get("Authorization"))
	})

	t.Run("no-auth sends no auth material", func(t *testing.T) {
		t.Parallel()
		remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
		// Only the Speakeasy session reaches this request: no client header
		// is configured to carry anything else.
		sessionOnly := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
		sessionOnly.Header.Set("Authorization", "Bearer speakeasy-session")
		p := &Proxy{Logger: testenv.NewLogger(t), Headers: MergeAuthClientHeaders(HeaderPolicyRemote, server, nil), AuthorizationOverride: ""}
		require.NoError(t, p.applyRequestHeaders(t.Context(), sessionOnly, remoteReq))
		require.Empty(t, remoteReq.Header.Get("Authorization"))
		require.Empty(t, remoteReq.Header.Get("X-Api-Key"))
		require.Equal(t, []string{"User-Agent", "X-Workspace"}, headerNames(remoteReq.Header), "only the server's non-credential header is added")
		require.Equal(t, "server-ws", remoteReq.Header.Get("X-Workspace"))
	})
}

func headerNames(h http.Header) []string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

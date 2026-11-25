package mcp

import (
	"testing"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
)

// legacyRouteBaseChallenge is the cached shape from before route bases were typed.
type legacyRouteBaseChallenge struct {
	ID       string `json:"id"`
	Endpoint struct {
		McpSlug   string `json:"mcp_slug"`
		RouteBase string `json:"route_base,omitempty"`
	} `json:"endpoint"`
}

func TestAuthnChallengeStateDecodesLegacyRouteBase(t *testing.T) {
	t.Parallel()

	codec := redisCache.New(&redisCache.Options{})
	for _, tc := range []struct {
		cached string
		want   mcproute.Base
		ok     bool
	}{
		{cached: "", want: mcproute.MCP, ok: true},
		{cached: "mcp", want: mcproute.MCP, ok: true},
		{cached: "mcp/gateway", want: mcproute.OrganizationGateway, ok: true},
		{cached: "x/mcp", want: "", ok: false},
	} {
		var legacy legacyRouteBaseChallenge
		legacy.ID = "challenge"
		legacy.Endpoint.McpSlug = "my-server"
		legacy.Endpoint.RouteBase = tc.cached
		raw, err := codec.Marshal(legacy)
		require.NoError(t, err)

		var state AuthnChallengeState
		require.NoError(t, codec.Unmarshal(raw, &state), tc.cached)
		require.Equal(t, "my-server", state.Endpoint.McpSlug)
		require.Equal(t, mcproute.Base(tc.cached), state.Endpoint.RouteBase)
		got, ok := state.Endpoint.RouteBase.Resolve()
		require.Equal(t, tc.ok, ok, tc.cached)
		require.Equal(t, tc.want, got, tc.cached)
	}
}

func TestEndpointRefRouteBaseEncodesAsPlainString(t *testing.T) {
	t.Parallel()

	codec := redisCache.New(&redisCache.Options{})
	typed, err := codec.Marshal(EndpointRef{McpSlug: "my-server", RouteBase: mcproute.OrganizationGateway})
	require.NoError(t, err)

	var legacy struct {
		McpSlug   string `json:"mcp_slug"`
		RouteBase string `json:"route_base,omitempty"`
	}
	require.NoError(t, codec.Unmarshal(typed, &legacy))
	require.Equal(t, "mcp/gateway", legacy.RouteBase)
}

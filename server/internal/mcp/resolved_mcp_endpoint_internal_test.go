package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateRef_Matches(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		Slug:      "my-server",
		RouteBase: "x/mcp",
	}
	require.NoError(t, endpoint.ValidateRef(EndpointRef{
		McpSlug:   "my-server",
		RouteBase: "x/mcp",
	}))
}

// A challenge minted on one route surface must not be resumable on the other:
// the RFC 9207 `iss` is built from the resolved endpoint's RouteBase, so a
// cross-surface resume would emit an issuer differing from the one the client
// recorded at mint time.
func TestValidateRef_RejectsCrossSurfaceResume(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		Slug:      "my-server",
		RouteBase: "mcp",
	}
	err := endpoint.ValidateRef(EndpointRef{
		McpSlug:   "my-server",
		RouteBase: "x/mcp",
	})
	require.ErrorIs(t, err, errToolsetEndpointMismatch)
}

// States minted before EndpointRef.RouteBase existed carry an empty value,
// which is treated as "mcp".
func TestValidateRef_LegacyEmptyRouteBaseMeansMcp(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		Slug:      "my-server",
		RouteBase: "mcp",
	}
	require.NoError(t, endpoint.ValidateRef(EndpointRef{
		McpSlug: "my-server",
	}))

	xmcpEndpoint := &ResolvedMcpEndpoint{
		Slug:      "my-server",
		RouteBase: "x/mcp",
	}
	require.ErrorIs(t, xmcpEndpoint.ValidateRef(EndpointRef{
		McpSlug: "my-server",
	}), errToolsetEndpointMismatch)
}

func TestValidateRef_RejectsCustomDomainMismatch(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		Slug:           "my-server",
		CustomDomainID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		RouteBase:      "mcp",
	}
	err := endpoint.ValidateRef(EndpointRef{
		McpSlug:        "my-server",
		CustomDomainID: uuid.NullUUID{},
		RouteBase:      "mcp",
	})
	require.ErrorIs(t, err, errToolsetEndpointMismatch)
}

package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestValidateRef_Matches(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		Slug:      "my-server",
		RouteBase: "mcp",
	}
	require.NoError(t, endpoint.ValidateRef(EndpointRef{
		McpSlug:   "my-server",
		RouteBase: "mcp",
	}))
}

// Cached challenges from the retired runtime surface must not resume through
// the canonical route.
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

func TestBuildResolvedMcpEndpointByRef_RejectsRetiredRouteAsNotFound(t *testing.T) {
	t.Parallel()

	_, err := (&Service{}).buildResolvedMcpEndpointByRef(t.Context(), EndpointRef{
		McpSlug:   "my-server",
		RouteBase: "x/mcp",
	})
	require.ErrorIs(t, err, errToolsetEndpointMismatch)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
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

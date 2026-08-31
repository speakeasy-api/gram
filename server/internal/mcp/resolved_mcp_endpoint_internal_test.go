package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestValidateGrantRejectsOriginChange(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	endpoint := &ResolvedMcpEndpoint{
		Slug:                "my-server",
		RouteBase:           "mcp",
		UserSessionIssuerID: issuerID,
	}
	err := endpoint.ValidateGrant(EndpointRef{
		McpSlug: "my-server",
		BaseURL: "https://custom.example.com",
	}, issuerID, "https://platform.example.com")
	require.ErrorIs(t, err, errToolsetEndpointMismatch)
}

func TestValidateGrantAllowsLegacyMissingOrigin(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	endpoint := &ResolvedMcpEndpoint{
		Slug:                "my-server",
		RouteBase:           "mcp",
		UserSessionIssuerID: issuerID,
	}
	require.NoError(t, endpoint.ValidateGrant(EndpointRef{McpSlug: "my-server"}, issuerID, "https://platform.example.com"))
}

func TestValidateChallengeRejectsIssuerChange(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		Slug:                "my-server",
		RouteBase:           "mcp",
		UserSessionIssuerID: uuid.New(),
	}
	err := endpoint.ValidateChallenge(EndpointRef{McpSlug: "my-server"}, uuid.New())
	require.ErrorIs(t, err, errToolsetEndpointMismatch)
}

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
func TestValidateRefRejectsVisibilityChange(t *testing.T) {
	t.Parallel()

	wasPublic := true
	endpoint := &ResolvedMcpEndpoint{Slug: "my-server", RouteBase: "mcp", IsPublic: false}
	err := endpoint.ValidateRef(EndpointRef{McpSlug: "my-server", IsPublic: &wasPublic})
	require.ErrorIs(t, err, errToolsetEndpointMismatch)
}

func TestValidateRefAllowsLegacyMissingVisibility(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{Slug: "my-server", RouteBase: "mcp", IsPublic: false}
	require.NoError(t, endpoint.ValidateRef(EndpointRef{McpSlug: "my-server"}))
}

func TestValidateRefAllowsLegacyMissingBackendIDs(t *testing.T) {
	t.Parallel()

	endpoint := &ResolvedMcpEndpoint{
		Slug:        "my-server",
		RouteBase:   "mcp",
		McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}
	require.NoError(t, endpoint.ValidateRef(EndpointRef{McpSlug: "my-server"}))
}

func TestValidateRefRejectsBackendSwap(t *testing.T) {
	t.Parallel()

	serverID := uuid.New()
	endpoint := &ResolvedMcpEndpoint{
		Slug:        "my-server",
		RouteBase:   "mcp",
		McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
	}
	err := endpoint.ValidateRef(EndpointRef{
		McpSlug:     "my-server",
		McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	})
	require.ErrorIs(t, err, errToolsetEndpointMismatch)
}

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

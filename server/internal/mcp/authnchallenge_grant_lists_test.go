package mcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// The grants an endpoint advertises and the grants a self-registering client
// may claim are separate lists. jwt-bearer is claimable because the ID-JAG
// exchange authenticates a registered client, yet an endpoint without ID-JAG
// must not advertise it. Were the metadata built from the registration list,
// this endpoint would advertise a grant it cannot serve.
func TestAuthorizationServerMetadata_AdvertisesIndependentlyOfRegistrableGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	require.Contains(t, usersessions.RegistrableGrantTypes, oauthwire.GrantTypeJWTBearer)

	metadata := fetchASMetadata(t, ti, toolset.McpSlug.String)
	advertised, ok := metadata["grant_types_supported"].([]any)
	require.True(t, ok, "metadata must carry grant_types_supported: %v", metadata)
	require.ElementsMatch(t, []any{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}, advertised)
}

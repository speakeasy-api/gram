package urn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestUserSessionIssuerMCPServersRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewUserSessionIssuerMCPServers(id)

	require.Equal(t, "user_session_issuer_mcp_servers:55555555-5555-5555-5555-555555555555", original.String())

	parsed, err := urn.ParseUserSessionIssuerMCPServers(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)
}

func TestUserSessionIssuerMCPServersRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		// The issuer's own urn names the issuer, not its MCP servers.
		"user_session_issuer:55555555-5555-5555-5555-555555555555",
		"user_session_issuer_mcp_servers:not-a-uuid",
		"user_session_issuer_mcp_servers:00000000-0000-0000-0000-000000000000",
		"user_session_issuer_mcp_servers:55555555-5555-5555-5555-555555555555:extra",
	} {
		_, err := urn.ParseUserSessionIssuerMCPServers(value)
		require.ErrorIs(t, err, urn.ErrInvalid, value)
	}
}

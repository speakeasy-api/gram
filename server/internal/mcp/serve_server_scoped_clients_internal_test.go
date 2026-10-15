// Credential resolution for a direct endpoint is scoped to that server's own
// provider — to nothing at all when it has none — while a legacy toolset
// endpoint has no server identity to scope by and stays issuer-wide (AIM-577).

package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestServerScopedProvider(t *testing.T) {
	t.Parallel()

	serverID := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	providerID := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	unset := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	provider, serverScoped := serverScopedProvider(&ResolvedMcpEndpoint{McpServerID: serverID, RemoteSessionIssuerID: providerID})
	require.True(t, serverScoped)
	require.Equal(t, providerID, provider)

	// A real server with no provider is scoped to nothing, not widened to its
	// user session issuer: the clients bound there are its siblings'.
	provider, serverScoped = serverScopedProvider(&ResolvedMcpEndpoint{McpServerID: serverID, RemoteSessionIssuerID: unset})
	require.True(t, serverScoped)
	require.False(t, provider.Valid)

	// A legacy toolset endpoint has no server row to name a provider.
	_, serverScoped = serverScopedProvider(&ResolvedMcpEndpoint{McpServerID: unset, RemoteSessionIssuerID: unset})
	require.False(t, serverScoped)
}

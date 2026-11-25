package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestUnofferedTypedLoginKeepsAuthorizationOutcome(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, errTypedLoginNotOffered, remotesessions.ErrTypedAuthorizationDenied)
	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, remotesessions.TypedAuthorizationHappened(errTypedLoginNotOffered))
}

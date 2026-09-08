package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestEvaluateConsentConnectAccessSkipsPublicAndUnboundEndpoints(t *testing.T) {
	t.Parallel()

	service := &Service{}
	subject := urn.NewUserSubject("user-1")

	public, _, err := service.evaluateConsentConnectAccess(t.Context(), &ResolvedMcpEndpoint{
		IsPublic:    true,
		Slug:        "public-server",
		McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}, subject, "challenge", "client")
	require.NoError(t, err)
	require.False(t, public.Denied)

	unbound, _, err := service.evaluateConsentConnectAccess(t.Context(), &ResolvedMcpEndpoint{
		IsPublic: false,
		Slug:     "unbound-server",
	}, subject, "challenge", "client")
	require.NoError(t, err)
	require.False(t, unbound.Denied)
}

func TestEvaluateConsentConnectAccessDeniesAnonymousOnPrivate(t *testing.T) {
	t.Parallel()

	service := &Service{}
	denied, _, err := service.evaluateConsentConnectAccess(t.Context(), &ResolvedMcpEndpoint{
		IsPublic:    false,
		Slug:        "private-server",
		McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}, urn.NewAnonymousSubject("anon-session"), "challenge", "client")
	require.NoError(t, err)
	require.True(t, denied.Denied)
	require.False(t, denied.CanRequest)
	require.Equal(t, "private-server", denied.ServerName)
}

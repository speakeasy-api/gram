package remotemcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:     "Authorization",
				IsSecret: new(true),
				Value:    new("Bearer token123"),
			},
		},
	})
	require.NoError(t, err)

	result, err := ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, created.ID, result.ID)
	require.Equal(t, "https://mcp.example.com", result.URL)
	require.Equal(t, "streamable-http", result.TransportType)
	require.Len(t, result.Headers, 1)
	require.Equal(t, "Authorization", result.Headers[0].Name)
	require.True(t, result.Headers[0].IsSecret)
	require.NotNil(t, result.Headers[0].Value)
	require.Contains(t, *result.Headers[0].Value, "*")
}

func TestGetServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

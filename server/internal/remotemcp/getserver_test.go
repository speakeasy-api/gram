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
		ID:               &created.ID,
		Slug:             nil,
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

	missing := uuid.NewString()
	_, err := ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               &missing,
		Slug:             nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetServer_BySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://api.example.com/mcp",
		TransportType:    "streamable-http",
		Headers:          []*gen.HeaderInput{},
	})
	require.NoError(t, err)
	require.NotNil(t, created.Slug)

	result, err := ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               nil,
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, result.ID)
}

func TestGetServer_BySlug_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               nil,
		Slug:             new("nonexistent-slug-aaaa"),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetServer_NeitherIDNorSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               nil,
		Slug:             nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetServer_BothIDAndSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	_, err := ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               &id,
		Slug:             new("some-slug-aaaa"),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

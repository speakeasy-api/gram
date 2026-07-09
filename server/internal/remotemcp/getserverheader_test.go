package remotemcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func TestGetServerHeader_RedactsSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)
	created := createSecretHeader(t, ctx, ti, server.ID, "Authorization", "Bearer token123")

	header, err := ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Equal(t, created.ID, header.ID)
	require.Equal(t, "Authorization", header.Name)
	require.True(t, header.IsSecret)
	require.NotNil(t, header.Value)
	require.Equal(t, "***", *header.Value)
}

func TestGetServerHeader_NonSecretValueVisible(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Tenant", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("acme")
	}))
	require.NoError(t, err)

	header, err := ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, header.IsSecret)
	require.NotNil(t, header.Value)
	require.Equal(t, "acme", *header.Value)
}

func TestGetServerHeader_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A header id from another project must not resolve.
func TestGetServerHeader_OtherProjectNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	otherServer := seedOtherProjectServer(t, ctx, ti)

	otherHeader := remotemcptest.SeedHeader(t, ctx, ti.conn, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      otherServer.ID,
		ProjectID:              otherServer.ProjectID,
		Name:                   "X-Other",
		Description:            pgtype.Text{String: "", Valid: false},
		IsRequired:             false,
		IsSecret:               false,
		Value:                  conv.ToPGText("other-value"),
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})

	_, err := ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{
		ID:               otherHeader.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetServerHeader_DeletedNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	_, err = ti.service.GetServerHeader(ctx, &gen.GetServerHeaderPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

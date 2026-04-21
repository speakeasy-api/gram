package remotemcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
		Headers: []*gen.HeaderInput{
			{
				Name:     "X-API-Key",
				IsSecret: new(true),
				Value:    new("secret-key"),
			},
		},
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Verify get returns not found
	_, err = ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Deleting a non-existent server should succeed silently
	err := ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

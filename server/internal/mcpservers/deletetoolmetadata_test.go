package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteToolMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
			{ToolName: "beta", Title: nil, ReadOnlyHint: nil, DestructiveHint: new(true), IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	beforeUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)

	err = ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "alpha",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeUpdates+1, afterUpdates)

	listed, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Tools, 1)
	require.Equal(t, "beta", listed.Tools[0].ToolName)

	// The deleted entry is still visible with include_deleted.
	includeDeleted := true
	withDeleted, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   &includeDeleted,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, withDeleted.Tools, 2)

	// Deleting the same tool again reports not-found.
	err = ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "alpha",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteToolMetadata_ServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
		McpServerID:      uuid.NewString(),
		ToolName:         "alpha",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteToolMetadata_RBAC_DeniedWithoutGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	deniedCtx := withExactAuthzGrants(t, ctx, ti.conn)
	err := ti.service.DeleteToolMetadata(deniedCtx, &gen.DeleteToolMetadataPayload{
		McpServerID:      uuid.NewString(),
		ToolName:         "alpha",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Names are stored trimmed, so a padded name must find its entry rather than
// reporting a misleading 404.
func TestDeleteToolMetadata_TrimsToolName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	err = ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "  alpha  ",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, listed.Tools)
}

func TestDeleteToolMetadata_RejectsEmptyToolName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	err := ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "   ",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestDeleteToolMetadata_RejectsToolsetBackedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createToolsetBackedMcpServer(t, ctx, ti)

	err := ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "alpha",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

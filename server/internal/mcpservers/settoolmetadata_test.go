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

func TestSetToolMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	beforeUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)

	updated, err := ti.service.SetToolMetadata(ctx, &gen.SetToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "alpha",
		Title:            new("Alpha"),
		ReadOnlyHint:     nil,
		DestructiveHint:  new(true),
		IdempotentHint:   nil,
		OpenWorldHint:    new(true),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	require.Equal(t, serverID, updated.McpServerID)
	require.Equal(t, "alpha", updated.ToolName)
	require.Equal(t, new("Alpha"), updated.Title)
	require.Equal(t, new(true), updated.DestructiveHint)
	require.Equal(t, new(true), updated.OpenWorldHint)
	require.Nil(t, updated.ReadOnlyHint, "full-record replace clears omitted hints")
	require.Nil(t, updated.IdempotentHint)
	require.Nil(t, updated.DeletedAt)

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
	require.Equal(t, new("Alpha"), listed.Tools[0].Title)
	require.Equal(t, new(true), listed.Tools[0].DestructiveHint)
	require.Equal(t, new(true), listed.Tools[0].OpenWorldHint)
	require.Nil(t, listed.Tools[0].ReadOnlyHint)
}

func TestSetToolMetadata_ToolNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.SetToolMetadata(ctx, &gen.SetToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "missing",
		Title:            nil,
		ReadOnlyHint:     new(true),
		DestructiveHint:  nil,
		IdempotentHint:   nil,
		OpenWorldHint:    nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetToolMetadata_ServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.SetToolMetadata(ctx, &gen.SetToolMetadataPayload{
		McpServerID:      uuid.NewString(),
		ToolName:         "alpha",
		Title:            nil,
		ReadOnlyHint:     new(true),
		DestructiveHint:  nil,
		IdempotentHint:   nil,
		OpenWorldHint:    nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetToolMetadata_RBAC_DeniedWithoutGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	deniedCtx := withExactAuthzGrants(t, ctx, ti.conn)
	_, err := ti.service.SetToolMetadata(deniedCtx, &gen.SetToolMetadataPayload{
		McpServerID:      uuid.NewString(),
		ToolName:         "alpha",
		Title:            nil,
		ReadOnlyHint:     new(true),
		DestructiveHint:  nil,
		IdempotentHint:   nil,
		OpenWorldHint:    nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

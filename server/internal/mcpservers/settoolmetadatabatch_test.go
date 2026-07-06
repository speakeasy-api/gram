package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// createRemoteBackedMcpServer creates an mcp_servers row backed by a seeded
// remote MCP server and returns its ID, for tool metadata tests.
func createRemoteBackedMcpServer(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "tool metadata server " + uuid.NewString(),
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	return created.ID
}

func TestSetToolMetadataBatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	beforeUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)

	result, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "list_items", Title: new("List items"), ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
			{ToolName: "delete_item", Title: nil, ReadOnlyHint: nil, DestructiveHint: new(true), IdempotentHint: new(true), OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Tools, 2)
	require.Equal(t, 0, result.Retired)

	byName := map[string]*types.ToolMetadata{}
	for _, tool := range result.Tools {
		byName[tool.ToolName] = tool
		require.Equal(t, serverID, tool.McpServerID)
		require.NotEmpty(t, tool.CreatedAt)
		require.NotEmpty(t, tool.UpdatedAt)
		require.Nil(t, tool.DeletedAt)
	}
	require.Equal(t, new("List items"), byName["list_items"].Title)
	require.Equal(t, new(true), byName["list_items"].ReadOnlyHint)
	require.Nil(t, byName["list_items"].DestructiveHint)
	require.Nil(t, byName["delete_item"].Title)
	require.Nil(t, byName["delete_item"].ReadOnlyHint)
	require.Equal(t, new(true), byName["delete_item"].DestructiveHint)
	require.Equal(t, new(true), byName["delete_item"].IdempotentHint)
	require.Nil(t, byName["delete_item"].OpenWorldHint)

	afterUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeUpdates+1, afterUpdates, "two tools written produce one collection-level entry")
}

func TestSetToolMetadataBatch_UpsertsAndRetires(t *testing.T) {
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

	// alpha changes hints, beta is absent (retired), gamma is new.
	result, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: nil, DestructiveHint: new(true), IdempotentHint: nil, OpenWorldHint: nil},
			{ToolName: "gamma", Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Len(t, result.Tools, 2)
	require.Equal(t, 1, result.Retired)

	// gamma created, alpha updated and beta retired all land in one entry.
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
	require.Len(t, listed.Tools, 2)
	require.Equal(t, "alpha", listed.Tools[0].ToolName)
	require.Nil(t, listed.Tools[0].ReadOnlyHint)
	require.Equal(t, new(true), listed.Tools[0].DestructiveHint)
	require.Equal(t, "gamma", listed.Tools[1].ToolName)
	require.Nil(t, listed.Tools[1].ReadOnlyHint)
	require.Nil(t, listed.Tools[1].DestructiveHint)
	require.Nil(t, listed.Tools[1].IdempotentHint)
	require.Nil(t, listed.Tools[1].OpenWorldHint)
}

func TestSetToolMetadataBatch_NoOpUpsertSkipsUpdateEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	tools := []*gen.ToolMetadataForm{
		{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
	}

	_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID:      serverID,
		Tools:            tools,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	beforeUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)

	result, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID:      serverID,
		Tools:            tools,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.Retired)

	afterUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeUpdates, afterUpdates, "unchanged hints emit no update event")
}

func TestSetToolMetadataBatch_EmptyBatchRetiresAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
			{ToolName: "beta", Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	result, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID:      serverID,
		Tools:            []*gen.ToolMetadataForm{},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Tools)
	require.Equal(t, 2, result.Retired)

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

func TestSetToolMetadataBatch_RejectsDuplicateToolName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
			{ToolName: "alpha", Title: nil, ReadOnlyHint: nil, DestructiveHint: new(true), IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetToolMetadataBatch_RejectsEmptyToolName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "   ", Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetToolMetadataBatch_ServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID:      uuid.NewString(),
		Tools:            []*gen.ToolMetadataForm{},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetToolMetadataBatch_RejectsToolsetBackedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetID := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "toolset backed server",
		EnvironmentID:     nil,
		RemoteMcpServerID: nil,
		ToolsetID:         &toolsetID,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	_, err = ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: created.ID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestSetToolMetadataBatch_RBAC_DeniedWithoutGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	deniedCtx := withExactAuthzGrants(t, ctx, ti.conn)
	_, err := ti.service.SetToolMetadataBatch(deniedCtx, &gen.SetToolMetadataBatchPayload{
		McpServerID:      uuid.NewString(),
		Tools:            []*gen.ToolMetadataForm{},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSetToolMetadataBatch_RBAC_AllowedWithProjectGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	// A project-scoped grant covers every server in the project: wildcard
	// resource, narrowed by the project_id dimension.
	grantedCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.Grant{
		Scope: authz.ScopeMCPWrite,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    authCtx.ProjectID.String(),
		},
	})
	result, err := ti.service.SetToolMetadataBatch(grantedCtx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
}

// The check is scoped to the MCP server, so a grant naming one server must
// authorize writes to that server and no other.
func TestSetToolMetadataBatch_RBAC_ServerScopedGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)
	otherServerID := createRemoteBackedMcpServer(t, ctx, ti)

	grantedCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, serverID))

	tools := []*gen.ToolMetadataForm{
		{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
	}

	result, err := ti.service.SetToolMetadataBatch(grantedCtx, &gen.SetToolMetadataBatchPayload{
		McpServerID:      serverID,
		Tools:            tools,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)

	_, err = ti.service.SetToolMetadataBatch(grantedCtx, &gen.SetToolMetadataBatchPayload{
		McpServerID:      otherServerID,
		Tools:            tools,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

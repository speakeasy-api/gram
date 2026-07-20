package mcpservers_test

import (
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

func TestAddToolMetadataBatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	beforeUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)

	result, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "list_items", Title: new("List items"), ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
			{ToolName: "delete_item", Title: nil, ReadOnlyHint: new(false), DestructiveHint: new(true), IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Tools, 2)

	byName := map[string]*types.ToolMetadata{}
	for _, tool := range result.Tools {
		byName[tool.ToolName] = tool
		require.Equal(t, serverID, tool.McpServerID)
		require.Nil(t, tool.DeletedAt)
	}
	require.Equal(t, new("List items"), byName["list_items"].Title)
	require.Equal(t, new(true), byName["list_items"].ReadOnlyHint)
	require.Nil(t, byName["list_items"].DestructiveHint)
	// false and unset are distinct stored states.
	require.Equal(t, new(false), byName["delete_item"].ReadOnlyHint)
	require.Equal(t, new(true), byName["delete_item"].DestructiveHint)
	require.Nil(t, byName["delete_item"].IdempotentHint)

	afterUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeUpdates+1, afterUpdates, "one collection-level entry for the batch")
}

// A batch naming an already-stored tool is rejected outright, and none of the
// batch lands — not even the tools that would not have collided.
func TestAddToolMetadataBatch_ConflictInsertsNothing(t *testing.T) {
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

	_, err = ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "beta", Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
			{ToolName: "alpha", Title: nil, ReadOnlyHint: nil, DestructiveHint: new(true), IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "alpha")

	listed, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Tools, 1, "beta must not have landed alongside the conflict")
	require.Equal(t, "alpha", listed.Tools[0].ToolName)
	require.Equal(t, new(true), listed.Tools[0].ReadOnlyHint, "stored alpha is untouched")
	require.Nil(t, listed.Tools[0].DestructiveHint)
}

// The partial unique index covers only live rows, so a tool whose sole prior
// row is a tombstone is recorded again as a fresh entry.
func TestAddToolMetadataBatch_DeletedToolInsertsFresh(t *testing.T) {
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
		ToolName:         "alpha",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	result, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: nil, DestructiveHint: new(true), IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
	require.Equal(t, new(true), result.Tools[0].DestructiveHint)
	require.Nil(t, result.Tools[0].ReadOnlyHint)

	listed, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Tools, 1)
}

// Nothing is ever deleted: stored tools the payload omits survive untouched.
func TestAddToolMetadataBatch_LeavesAbsentToolsUntouched(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: new("Alpha"), ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "beta", Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
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
	require.Len(t, listed.Tools, 2)
	require.Equal(t, "alpha", listed.Tools[0].ToolName)
	require.Equal(t, new("Alpha"), listed.Tools[0].Title, "alpha survives untouched")
	require.Equal(t, new(true), listed.Tools[0].ReadOnlyHint)
	require.Equal(t, "beta", listed.Tools[1].ToolName)
}

// The empty batch is a no-op here, where the same shape deletes everything on
// the authoritative path.
func TestAddToolMetadataBatch_EmptyBatchIsNoOp(t *testing.T) {
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

	beforeUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)

	result, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      serverID,
		Tools:            []*gen.ToolMetadataForm{},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Tools)

	listed, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Tools, 1, "the empty batch deletes nothing")

	afterUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeUpdates, afterUpdates, "a no-op emits no audit entry")
}

func TestAddToolMetadataBatch_RejectsDuplicateToolNameInPayload(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	// A name repeated within one payload is a malformed request, distinct from
	// the conflict raised when a tool is already stored.
	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
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

func TestAddToolMetadataBatch_RejectsEmptyToolName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
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

func TestAddToolMetadataBatch_ServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      uuid.NewString(),
		Tools:            []*gen.ToolMetadataForm{},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAddToolMetadataBatch_RejectsToolsetBackedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetID := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "toolset backed server " + uuid.NewString(),
		EnvironmentID:     nil,
		RemoteMcpServerID: nil,
		ToolsetID:         &toolsetID,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	_, err = ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
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

func TestAddToolMetadataBatch_RBAC_DeniedWithoutGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	deniedCtx := withExactAuthzGrants(t, ctx, ti.conn)
	_, err := ti.service.AddToolMetadataBatch(deniedCtx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      uuid.NewString(),
		Tools:            []*gen.ToolMetadataForm{},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestAddToolMetadataBatch_RBAC_AllowedWithProjectGrant(t *testing.T) {
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
	result, err := ti.service.AddToolMetadataBatch(grantedCtx, &gen.AddToolMetadataBatchPayload{
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
func TestAddToolMetadataBatch_RBAC_ServerScopedGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)
	otherServerID := createRemoteBackedMcpServer(t, ctx, ti)

	grantedCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, serverID))

	tools := []*gen.ToolMetadataForm{
		{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
	}

	result, err := ti.service.AddToolMetadataBatch(grantedCtx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      serverID,
		Tools:            tools,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)

	_, err = ti.service.AddToolMetadataBatch(grantedCtx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      otherServerID,
		Tools:            tools,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

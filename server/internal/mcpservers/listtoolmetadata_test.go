package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListToolMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "zeta", Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: new(true)},
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Len(t, result.Tools, 2)
	require.Equal(t, "alpha", result.Tools[0].ToolName, "results ordered by tool name")
	require.Equal(t, "zeta", result.Tools[1].ToolName)
	require.Equal(t, new(true), result.Tools[0].ReadOnlyHint)
	require.Equal(t, new(true), result.Tools[1].OpenWorldHint)
}

func TestListToolMetadata_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	result, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Tools)
}

func TestListToolMetadata_IncludeDeleted(t *testing.T) {
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

	// Retire beta by re-submitting the batch without it.
	_, err = ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "alpha", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	activeOnly, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, activeOnly.Tools, 1)
	require.Equal(t, "alpha", activeOnly.Tools[0].ToolName)

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
	require.Equal(t, "alpha", withDeleted.Tools[0].ToolName)
	require.Nil(t, withDeleted.Tools[0].DeletedAt)
	require.Equal(t, "beta", withDeleted.Tools[1].ToolName)
	require.NotNil(t, withDeleted.Tools[1].DeletedAt)
}

func TestListToolMetadata_ServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      uuid.NewString(),
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListToolMetadata_RBAC_DeniedWithoutGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	deniedCtx := withExactAuthzGrants(t, ctx, ti.conn)
	_, err := ti.service.ListToolMetadata(deniedCtx, &gen.ListToolMetadataPayload{
		McpServerID:      uuid.NewString(),
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListToolMetadata_RBAC_AllowedWithProjectGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	// A project-scoped grant covers every server in the project: wildcard
	// resource, narrowed by the project_id dimension.
	grantedCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.Grant{
		Scope: authz.ScopeMCPRead,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    authCtx.ProjectID.String(),
		},
	})
	result, err := ti.service.ListToolMetadata(grantedCtx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Tools)
}

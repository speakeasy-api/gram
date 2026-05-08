package mcpendpoints_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListMcpEndpoints_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{
		McpServerID:      nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.McpEndpoints)
}

func TestListMcpEndpoints_Multiple(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	for _, name := range []string{"alpha", "beta"} {
		_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			CustomDomainID:   nil,
			McpServerID:      mcpServerID,
			Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-" + name),
		})
		require.NoError(t, err)
	}

	result, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{
		McpServerID:      nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpEndpoints, 2)
}

func TestListMcpEndpoints_FilteredByFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendA := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	frontendB := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	for _, tc := range []struct {
		frontend, suffix string
	}{
		{frontendA, "a-one"},
		{frontendA, "a-two"},
		{frontendB, "b-one"},
	} {
		_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			CustomDomainID:   nil,
			McpServerID:      tc.frontend,
			Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-" + tc.suffix),
		})
		require.NoError(t, err)
	}

	// Filter to frontend A: expect only its two slugs.
	result, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{
		McpServerID:      &frontendA,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpEndpoints, 2)
	for _, s := range result.McpEndpoints {
		require.Equal(t, frontendA, s.McpServerID)
	}
}

func TestListMcpEndpoints_FilteredByFrontend_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Frontend exists but has no slugs; filter returns empty without error.
	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	result, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{
		McpServerID:      &mcpServerID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.McpEndpoints)
}

func TestListMcpEndpoints_FilteredByFrontend_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bad := "not-a-uuid"
	_, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{
		McpServerID:      &bad,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListMcpEndpoints_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{
		McpServerID:      nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

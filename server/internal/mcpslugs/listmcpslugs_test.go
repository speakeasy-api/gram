package mcpslugs_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_slugs"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListMcpSlugs_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.ListMcpSlugs(ctx, &gen.ListMcpSlugsPayload{
		McpFrontendID:    nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.McpSlugs)
}

func TestListMcpSlugs_Multiple(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()

	for _, name := range []string{"alpha", "beta"} {
		_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			CustomDomainID:   nil,
			McpFrontendID:    frontendID,
			Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-" + name),
		})
		require.NoError(t, err)
	}

	result, err := ti.service.ListMcpSlugs(ctx, &gen.ListMcpSlugsPayload{
		McpFrontendID:    nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpSlugs, 2)
}

func TestListMcpSlugs_FilteredByFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendA := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()
	frontendB := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()

	for _, tc := range []struct {
		frontend, suffix string
	}{
		{frontendA, "a-one"},
		{frontendA, "a-two"},
		{frontendB, "b-one"},
	} {
		_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			CustomDomainID:   nil,
			McpFrontendID:    tc.frontend,
			Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-" + tc.suffix),
		})
		require.NoError(t, err)
	}

	// Filter to frontend A: expect only its two slugs.
	result, err := ti.service.ListMcpSlugs(ctx, &gen.ListMcpSlugsPayload{
		McpFrontendID:    &frontendA,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpSlugs, 2)
	for _, s := range result.McpSlugs {
		require.Equal(t, frontendA, s.McpFrontendID)
	}
}

func TestListMcpSlugs_FilteredByFrontend_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Frontend exists but has no slugs; filter returns empty without error.
	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()

	result, err := ti.service.ListMcpSlugs(ctx, &gen.ListMcpSlugsPayload{
		McpFrontendID:    &frontendID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.McpSlugs)
}

func TestListMcpSlugs_FilteredByFrontend_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bad := "not-a-uuid"
	_, err := ti.service.ListMcpSlugs(ctx, &gen.ListMcpSlugsPayload{
		McpFrontendID:    &bad,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListMcpSlugs_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListMcpSlugs(ctx, &gen.ListMcpSlugsPayload{
		McpFrontendID:    nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

package mcpslugs_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_slugs"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateMcpSlug_PlatformDomainWithOrgPrefix(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpSlugCreate)
	require.NoError(t, err)

	slug := authCtx.OrganizationSlug + "-example"
	result, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slug),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)
	require.Equal(t, frontendID, result.McpFrontendID)
	require.Equal(t, slug, string(result.Slug))
	require.Nil(t, result.CustomDomainID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpSlugCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateMcpSlug_PlatformDomainRejectsUnprefixedSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString("some-unrelated-slug"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpSlug_InvalidFrontendID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    "not-a-uuid",
		Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-example"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateMcpSlug_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-example"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

package mcpslugs_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_slugs"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
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

func TestCreateMcpSlug_RejectsCrossTenantMcpFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Frontend lives in a different project in the same org.
	otherFrontendID := seedOtherProjectMcpFrontend(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    otherFrontendID,
		Slug:             types.McpSlugString(authCtx.OrganizationSlug + "-leak"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpSlug_ConflictOnDuplicateSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slugVal := authCtx.OrganizationSlug + "-taken"

	_, err := ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slugVal),
	})
	require.NoError(t, err)

	// Second create with the same (NULL custom_domain_id, slug) must conflict.
	_, err = ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slugVal),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateMcpSlug_ConflictOnDuplicateSlugWithCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendID := seedMcpFrontend(t, ctx, ti.conn, *authCtx.ProjectID).String()

	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "custom-" + uuid.NewString() + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	customDomainID := domain.ID.String()

	slugVal := "taken"

	_, err = ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   &customDomainID,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slugVal),
	})
	require.NoError(t, err)

	// Second create with the same (custom_domain_id, slug) must conflict.
	_, err = ti.service.CreateMcpSlug(ctx, &gen.CreateMcpSlugPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   &customDomainID,
		McpFrontendID:    frontendID,
		Slug:             types.McpSlugString(slugVal),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

package mcpendpoints_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateMcpEndpoint_PlatformDomainWithOrgPrefix(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)

	slug := authCtx.OrganizationSlug + "-example"
	result, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)
	require.Equal(t, mcpServerID, result.McpServerID)
	require.Equal(t, slug, string(result.Slug))
	require.Nil(t, result.CustomDomainID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateMcpEndpoint_PlatformDomainRejectsUnprefixedSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug("some-unrelated-slug"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpEndpoint_InvalidFrontendID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      "not-a-uuid",
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-example"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateMcpEndpoint_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-example"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateMcpEndpoint_RejectsCrossTenantMcpFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Frontend lives in a different project in the same org.
	otherFrontendID := seedOtherProjectMcpFrontend(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      otherFrontendID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-leak"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCreateMcpEndpoint_ConflictOnDuplicateSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slugVal := authCtx.OrganizationSlug + "-taken"

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	require.NoError(t, err)

	// Second create with the same (NULL custom_domain_id, slug) must conflict.
	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateMcpEndpoint_ConflictOnDuplicateSlugWithCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "custom-" + uuid.NewString() + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	customDomainID := domain.ID.String()

	slugVal := "taken"

	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   &customDomainID,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	require.NoError(t, err)

	// Second create with the same (custom_domain_id, slug) must conflict.
	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   &customDomainID,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slugVal),
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

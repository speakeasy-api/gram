package mcpendpoints_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateMcpEndpoint_FullReplace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	frontendA := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	frontendB := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      frontendA,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-original"),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		CustomDomainID:   nil,
		McpServerID:      frontendB,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-renamed"),
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, frontendB, updated.McpServerID)
	require.Equal(t, authCtx.OrganizationSlug+"-renamed", string(updated.Slug))

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpEndpointUpdate)
	require.NoError(t, err)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)
}

func TestUpdateMcpEndpoint_RootSlugRenameRetainsMapping(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "endpoint-rename.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	serverID := seedMcpServerWithVisibility(t, ctx, ti.conn, *authCtx.ProjectID, "public")
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID:    serverID,
		Slug:           "before",
	})
	require.NoError(t, err)
	require.NoError(t, customdomainsrepo.New(ti.conn).SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  endpoint.ID,
		CustomDomainID: domain.ID,
	}))

	updated, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		ID:             endpoint.ID.String(),
		CustomDomainID: new(domain.ID.String()),
		McpServerID:    serverID.String(),
		Slug:           types.McpEndpointSlug("after"),
	})
	require.NoError(t, err)
	require.True(t, updated.IsDomainRoot)

	route, err := customdomainsrepo.New(ti.conn).GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.Equal(t, "after", route.RootSlug)
	require.Equal(t, endpoint.ID, route.RootMcpEndpointID)
}

func TestUpdateMcpEndpoint_RootMoveAndDisabledServerClearMapping(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "endpoint-clear.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	activeServerID := seedMcpServerWithVisibility(t, ctx, ti.conn, *authCtx.ProjectID, "public")
	disabledServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID:    activeServerID,
		Slug:           "root",
	})
	require.NoError(t, err)
	require.NoError(t, customdomainsrepo.New(ti.conn).SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  endpoint.ID,
		CustomDomainID: domain.ID,
	}))
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		ID:             endpoint.ID.String(),
		CustomDomainID: new(domain.ID.String()),
		McpServerID:    disabledServerID.String(),
		Slug:           types.McpEndpointSlug("root"),
	})
	require.NoError(t, err)
	require.False(t, updated.IsDomainRoot)

	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+1, afterAuditCount)

	// Re-select and move to the platform domain; the marker must not transfer.
	require.NoError(t, customdomainsrepo.New(ti.conn).SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  endpoint.ID,
		CustomDomainID: domain.ID,
	}))
	updated, err = ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		ID:             endpoint.ID.String(),
		CustomDomainID: nil,
		McpServerID:    activeServerID.String(),
		Slug:           types.McpEndpointSlug(authCtx.OrganizationSlug + "-moved"),
	})
	require.NoError(t, err)
	require.False(t, updated.IsDomainRoot)
	require.Nil(t, updated.CustomDomainID)
}

func TestUpdateMcpEndpoint_PlatformDomainRejectsUnprefixedSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-base"),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug("bad-prefix"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpdateMcpEndpoint_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	_, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               uuid.NewString(),
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-whatever"),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateMcpEndpoint_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               uuid.NewString(),
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-whatever"),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpdateMcpEndpoint_RejectsCrossTenantMcpFrontend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ownFrontendID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      ownFrontendID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-legit"),
	})
	require.NoError(t, err)

	otherFrontendID := seedOtherProjectMcpFrontend(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err = ti.service.UpdateMcpEndpoint(ctx, &gen.UpdateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               created.ID,
		CustomDomainID:   nil,
		McpServerID:      otherFrontendID,
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-legit"),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

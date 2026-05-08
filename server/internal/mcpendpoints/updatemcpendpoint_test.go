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

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
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteMcpEndpoint(t *testing.T) {
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
		McpServerID:      conv.PtrEmpty(mcpServerID),
		Slug:             types.McpEndpointSlug(authCtx.OrganizationSlug + "-delete-me"),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)

	err = ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpEndpointDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Subsequent get returns not-found.
	id := created.ID
	_, err = ti.service.GetMcpEndpoint(ctx, &gen.GetMcpEndpointPayload{
		ID:               &id,
		CustomDomainID:   nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpEndpoint_RootAutoClearsAndAuditsMapping(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "endpoint-delete-root.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	serverID := seedMcpServerWithVisibility(t, ctx, ti.conn, *authCtx.ProjectID, "public")
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID:    uuid.NullUUID{UUID: serverID, Valid: true},
		Slug:           "root",
	})
	require.NoError(t, err)
	require.NoError(t, customdomainsrepo.New(ti.conn).SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  endpoint.ID,
		CustomDomainID: domain.ID,
	}))
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)

	err = ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{ID: endpoint.ID.String()})
	require.NoError(t, err)

	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+1, afterAuditCount)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, endpoint.ID.String(), beforeSnapshot["RootMcpEndpointID"])
	require.Nil(t, afterSnapshot["RootMcpEndpointID"])
	route, err := customdomainsrepo.New(ti.conn).GetCustomDomainRouteConfig(ctx, domain.ID)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, route.RootMcpEndpointID)
	require.Empty(t, route.RootSlug)
}

func TestDeleteMcpEndpoint_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteMcpEndpoint_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	err := ti.service.DeleteMcpEndpoint(ctx, &gen.DeleteMcpEndpointPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

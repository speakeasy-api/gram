package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

func TestUpdateMcpServer_DisableClearsRootAndReenableDoesNotRestore(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, remoteID, domainID, endpointID := createPublicServerWithRoot(t, ctx, ti, "disable-root")
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:                server.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)
	requireRootCleared(t, ctx, ti, domainID, endpointID)
	requireLatestServerRootAutoClearAudit(t, ctx, ti, endpointID)

	afterDisableAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+1, afterDisableAuditCount)

	_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		ID:                server.ID,
		Name:              nil,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	requireRootCleared(t, ctx, ti, domainID, endpointID)
	afterReenableAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	require.Equal(t, afterDisableAuditCount, afterReenableAuditCount)
}

func TestDeleteMcpServer_ClearsRootMappingWithEndpointCascade(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, _, domainID, endpointID := createPublicServerWithRoot(t, ctx, ti, "delete-root")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)

	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: server.ID})
	require.NoError(t, err)
	route, err := customdomainsrepo.New(ti.conn).GetCustomDomainRouteConfig(ctx, domainID)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, route.RootMcpEndpointID)
	_, err = mcpendpointsrepo.New(ti.conn).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	requireLatestServerRootAutoClearAudit(t, ctx, ti, endpointID)

	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+1, afterAuditCount)
}

func createPublicServerWithRoot(t *testing.T, ctx context.Context, ti *testInstance, slug string) (*types.McpServer, string, uuid.UUID, uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	server, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:              "root lifecycle " + slug,
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         slug + ".example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	endpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID:    uuid.MustParse(server.ID),
		Slug:           slug,
	})
	require.NoError(t, err)
	require.NoError(t, customdomainsrepo.New(ti.conn).SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  endpoint.ID,
		CustomDomainID: domain.ID,
	}))
	return server, remoteID, domain.ID, endpoint.ID
}

func requireRootCleared(t *testing.T, ctx context.Context, ti *testInstance, domainID, endpointID uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	route, err := customdomainsrepo.New(ti.conn).GetCustomDomainRouteConfig(ctx, domainID)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, route.RootMcpEndpointID)
	endpoint, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.False(t, endpoint.IsDomainRoot.Valid && endpoint.IsDomainRoot.Bool)
}

func requireLatestServerRootAutoClearAudit(t *testing.T, ctx context.Context, ti *testInstance, endpointID uuid.UUID) {
	t.Helper()

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, endpointID.String(), beforeSnapshot["RootMcpEndpointID"])
	require.Nil(t, afterSnapshot["RootMcpEndpointID"])
}

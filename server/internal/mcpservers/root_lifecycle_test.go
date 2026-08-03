package mcpservers_test

import (
	"context"
	"testing"
	"time"

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
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestUpdateMcpServer_DisableSerializesWithRootSelection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		existingRoot bool
	}{
		{name: "existing root reapply", existingRoot: true},
		{name: "non-root selection", existingRoot: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			server, remoteID, domainID, endpointID := createPublicServerWithRoot(t, ctx, ti, "disable-race-"+uuid.NewString()[:8])
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			if !tc.existingRoot {
				require.NoError(t, customdomainsrepo.New(ti.conn).ClearRootMcpEndpoint(ctx, domainID))
			}

			setterTx := testenv.BeginTx(t, ctx, ti.conn)
			domainRepo := customdomainsrepo.New(setterTx)
			_, err := domainRepo.LockCustomDomainByID(ctx, domainID)
			require.NoError(t, err)
			_, err = domainRepo.LockRootMcpEndpointSelection(ctx, customdomainsrepo.LockRootMcpEndpointSelectionParams{
				CustomDomainID: domainID,
				McpEndpointID:  uuid.NullUUID{UUID: endpointID, Valid: true},
			})
			require.NoError(t, err)
			_, err = domainRepo.GetEligibleRootMcpEndpoint(ctx, customdomainsrepo.GetEligibleRootMcpEndpointParams{
				McpEndpointID:  endpointID,
				CustomDomainID: domainID,
				OrganizationID: authCtx.ActiveOrganizationID,
			})
			require.NoError(t, err)
			require.NoError(t, domainRepo.ClearRootMcpEndpoint(ctx, domainID))
			require.NoError(t, domainRepo.SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
				McpEndpointID:  endpointID,
				CustomDomainID: domainID,
			}))

			disabled := make(chan error, 1)
			go func() {
				_, updateErr := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
					ID:                server.ID,
					Name:              nil,
					EnvironmentID:     nil,
					RemoteMcpServerID: &remoteID,
					ToolsetID:         nil,
					Visibility:        types.McpServerVisibility("disabled"),
				})
				disabled <- updateErr
			}()

			select {
			case updateErr := <-disabled:
				require.Failf(t, "disable returned before root selection committed", "error: %v", updateErr)
			case <-time.After(100 * time.Millisecond):
			}

			require.NoError(t, setterTx.Commit(ctx))
			select {
			case updateErr := <-disabled:
				require.NoError(t, updateErr)
			case <-time.After(5 * time.Second):
				require.Fail(t, "disable deadlocked with root selection")
			}
			requireRootCleared(t, ctx, ti, domainID, endpointID)
		})
	}
}

func TestUpdateMcpServer_DisableClearsNewlyCommittedRoot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	server, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		Name:              "new root disable race",
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("public"),
	})
	require.NoError(t, err)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "new-root-disable-race.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)

	setterTx := testenv.BeginTx(t, ctx, ti.conn)
	endpoint, err := mcpendpointsrepo.New(setterTx).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID:    uuid.MustParse(server.ID),
		Slug:           "new-root",
	})
	require.NoError(t, err)
	domainRepo := customdomainsrepo.New(setterTx)
	_, err = domainRepo.LockCustomDomainByID(ctx, domain.ID)
	require.NoError(t, err)
	_, err = domainRepo.LockRootMcpEndpointSelection(ctx, customdomainsrepo.LockRootMcpEndpointSelectionParams{
		CustomDomainID: domain.ID,
		McpEndpointID:  uuid.NullUUID{UUID: endpoint.ID, Valid: true},
	})
	require.NoError(t, err)
	_, err = domainRepo.GetEligibleRootMcpEndpoint(ctx, customdomainsrepo.GetEligibleRootMcpEndpointParams{
		McpEndpointID:  endpoint.ID,
		CustomDomainID: domain.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.NoError(t, domainRepo.SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  endpoint.ID,
		CustomDomainID: domain.ID,
	}))

	disabled := make(chan error, 1)
	go func() {
		_, updateErr := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
			ID:                server.ID,
			Name:              nil,
			EnvironmentID:     nil,
			RemoteMcpServerID: &remoteID,
			ToolsetID:         nil,
			Visibility:        types.McpServerVisibility("disabled"),
		})
		disabled <- updateErr
	}()

	select {
	case updateErr := <-disabled:
		require.Failf(t, "disable returned before new root committed", "error: %v", updateErr)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, setterTx.Commit(ctx))
	select {
	case updateErr := <-disabled:
		require.NoError(t, updateErr)
	case <-time.After(5 * time.Second):
		require.Fail(t, "disable deadlocked with new root selection")
	}
	requireRootCleared(t, ctx, ti, domain.ID, endpoint.ID)
	requireLatestServerRootAutoClearAudit(t, ctx, ti, endpoint.ID)
}

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

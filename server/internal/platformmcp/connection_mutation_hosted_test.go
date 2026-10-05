package platformmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type unusedConnectionEndpointWriter struct{}

func (unusedConnectionEndpointWriter) CreateMcpEndpointInTransaction(context.Context, pgx.Tx, mcpendpoints.CreateMcpEndpointInTransactionInput) (*types.McpEndpoint, error) {
	return nil, errors.New("unexpected address mutation")
}

func (unusedConnectionEndpointWriter) UpdateMcpEndpointAddressInTransaction(context.Context, pgx.Tx, mcpendpoints.UpdateMcpEndpointAddressInput) (*types.McpEndpoint, []uuid.UUID, error) {
	return nil, nil, errors.New("unexpected address mutation")
}

type allowConnectionNetworkAccess struct{}

func (allowConnectionNetworkAccess) PrepareNetworkAccess(context.Context, networkaccess.EligibilityInput) (networkaccess.AdmissionFinalizer, error) {
	return networkaccess.NewAdmissionFinalizer(func(context.Context, pgx.Tx) error { return nil }), nil
}

func TestSetMCPNetworkAccessHostedToolsetUsesCanonicalPolicy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_hosted_network_access")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	orgSlug := projectOrganizationSlug(ctx, conn, principal.OrganizationID)
	require.NotEmpty(t, orgSlug)
	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		Name: "Hosted MCP", Slug: "hosted-" + uuid.NewString()[:8],
		McpSlug: conv.ToPGText(orgSlug + "-hosted"), McpEnabled: true,
	})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: project.ID, Name: conv.ToPGText(toolset.Name), Slug: toolset.McpSlug,
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	fixtures := testrepo.New(conn)
	require.NoError(t, fixtures.InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: principal.OrganizationID,
		DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	require.NoError(t, fixtures.SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{
		OrganizationID: principal.OrganizationID, Status: "online",
	}))
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{
		authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID),
		authz.NewGrant(authz.ScopeMCPRead, toolset.ID.String()),
		authz.NewGrant(authz.ScopeMCPWrite, project.ID.String()),
	})
	settings := NewMCPConnectionSettingsService(conn)
	authorizer := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := NewMCPConnectionMutationService(conn, settings, unusedConnectionEndpointWriter{}, audit.NewLogger(), authorizer, allowConnectionNetworkAccess{}, plugins.PublicationRequests{}, nil)
	before, err := settings.Get(ctx, principal, GetMCPConnectionSettingsInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: toolset.ID.String(),
	})
	require.NoError(t, err)
	input := SetMCPNetworkAccessInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: toolset.ID.String(),
		Mode: "dual", ExpectedVersion: before.Version, IdempotencyKey: uuid.NewString(), Confirmed: true,
	}
	service.now = time.Now
	output, err := service.SetNetworkAccess(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, "dual", output.Settings.NetworkMode)
	require.Equal(t, "fresh_read_after_commit", output.SnapshotScope)
	stored, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, "dual", stored.NetworkAccessMode.String)
	require.Len(t, output.Settings.Endpoints, 1)

	replay, err := service.SetNetworkAccess(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, output.Settings.Version, replay.Settings.Version)
	_, err = service.SetNetworkAccess(ctx, principal, SetMCPNetworkAccessInput{
		ProjectID: project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: toolset.ID.String(),
		Mode: "private_only", ExpectedVersion: before.Version, IdempotencyKey: uuid.NewString(), Confirmed: true,
	})
	require.ErrorIs(t, err, ErrMCPConnectionMutationConflict)
}

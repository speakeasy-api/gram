package mcpendpoints_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	domainsgen "github.com/speakeasy-api/gram/server/gen/domains"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	toolsetsgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostedmcptest"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type settledRun struct{ client.WorkflowRun }

func (settledRun) Get(context.Context, any) error { return nil }

type deleteOnlyTemporal struct{ customdomains.TemporalClient }

func (deleteOnlyTemporal) TerminateCustomDomainRegistration(context.Context, string, string, uuid.UUID, string) error {
	return nil
}

func (deleteOnlyTemporal) ExecuteCustomDomainReconcile(context.Context, uuid.UUID) (client.WorkflowRun, error) {
	return settledRun{}, nil
}

// deleteDomain deletes the organization's custom domain through the product path, which cascades its endpoints.
func (f *hostedAddressFixture) deleteDomain(t *testing.T, ctx context.Context) {
	t.Helper()

	logger := testenv.NewLogger(t)
	service := customdomains.NewService(logger, testenv.NewTracerProvider(t), f.ti.conn, f.ti.sessionManager, deleteOnlyTemporal{},
		authz.NewEngine(logger, f.ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), audit.NewLogger(),
		func(context.Context, pgx.Tx, string, uuid.UUID) (bool, error) { return false, nil }, "cname.example.net.", nil)
	require.NoError(t, service.DeleteDomain(ctx, &domainsgen.DeleteDomainPayload{}))
}

func TestHostedServerRecoversAddressAfterDomainDelete(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	domain := f.activeDomain(t, ctx, "mcp.recover.example.com")
	_, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload("on-domain", func(p *gen.UpdateMcpEndpointPayload) {
		p.CustomDomainID = new(domain.ID.String())
	}))
	require.NoError(t, err)
	f.requireMirrored(t, ctx)

	f.deleteDomain(t, ctx)
	require.Empty(t, f.endpoints(t, ctx))
	hostedmcptest.RequireMirroredWithoutAddress(t, ctx, f.ti.conn, f.projectID, f.toolset.ID)
	toolset, err := toolsetsrepo.New(f.ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Equal(t, "on-domain", toolset.McpSlug.String, "the mirror never invents an address")
	require.Equal(t, uuid.NullUUID{UUID: domain.ID, Valid: true}, toolset.CustomDomainID)

	recovered := f.authCtx.OrganizationSlug + "-recovered"
	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(recovered)})
	require.NoError(t, err)
	endpoint := f.requireProjected(t, ctx)
	require.Equal(t, recovered, endpoint.Slug)
	require.False(t, endpoint.CustomDomainID.Valid)
	require.True(t, f.resolves(t, ctx, recovered))
	f.requireMirrored(t, ctx)

	_, err = f.ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(recovered + "-2")})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "single address")
}

func TestHostedServerWithoutAddressCannotBeEnabled(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	domain := f.activeDomain(t, ctx, "mcp.no-enable.example.com")
	_, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload("parked", func(p *gen.UpdateMcpEndpointPayload) {
		p.CustomDomainID = new(domain.ID.String())
	}))
	require.NoError(t, err)
	f.deleteDomain(t, ctx)

	tx := testenv.BeginTx(t, ctx, f.ti.conn)
	defer func() { _ = tx.Rollback(context.Background()) }()
	server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	_, err = mcpserversrepo.New(tx).SyncHostedMCPServer(ctx, mcpserversrepo.SyncHostedMCPServerParams{
		Name: server.Name, Slug: server.Slug, Visibility: visibility.Disabled, UserSessionIssuerID: server.UserSessionIssuerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID, NetworkAccessMode: server.NetworkAccessMode, ID: server.ID, ProjectID: server.ProjectID,
	})
	require.NoError(t, err)
	toolset, err := toolsetsrepo.New(tx).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.True(t, toolset.McpSlug.Valid, "the toolset still names its address on the deleted domain")

	_, err = hostedmcp.Enable(ctx, tx, audit.NewLogger(), hostedmcp.SystemActor("test"), toolset)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "no address")
}

// A platform address with no endpoint can only come from legacy rows; hosting writes leave the toolset's address
// as it is and add no endpoint. An older pod's sync would recreate the platform endpoint for such a row, as #8009
// itself does for that state, which restores the address; no write here creates the state.
func TestHostedWritesLeavePlatformAliasWithoutEndpoint(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	_, err := mcpendpointsrepo.New(f.ti.conn).DeleteMCPEndpoint(ctx, mcpendpointsrepo.DeleteMCPEndpointParams{ID: f.endpoint.ID, ProjectID: f.projectID})
	require.NoError(t, err)

	_, err = f.toolsets.UpdateToolset(ctx, &toolsetsgen.UpdateToolsetPayload{Slug: types.Slug(f.toolset.Slug), Name: new("Renamed without address")})
	require.NoError(t, err)
	require.Empty(t, f.endpoints(t, ctx), "a hosting write adds no endpoint")
	toolset, err := toolsetsrepo.New(f.ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: f.toolset.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Equal(t, f.toolset.McpSlug, toolset.McpSlug, "the mirror never invents or drops an address")
	require.Equal(t, f.toolset.CustomDomainID, toolset.CustomDomainID)
	require.Equal(t, "Renamed without address", toolset.Name)
}

func TestHostedServerAddressRecoveryChecksServerScopedWrite(t *testing.T) {
	t.Parallel()
	ctx, f := newHostedAddressFixture(t, "pro")
	domain := f.activeDomain(t, ctx, "mcp.scoped.example.com")
	_, err := f.ti.service.UpdateMcpEndpoint(ctx, f.payload("on-domain", func(p *gen.UpdateMcpEndpointPayload) {
		p.CustomDomainID = new(domain.ID.String())
	}))
	require.NoError(t, err)
	f.deleteDomain(t, ctx)
	require.Empty(t, f.endpoints(t, ctx))

	payload := &gen.CreateMcpEndpointPayload{McpServerID: new(f.toolset.ID.String()), Slug: types.McpEndpointSlug(f.authCtx.OrganizationSlug + "-recovered")}
	projectOnly := withExactAuthzGrants(t, ctx, f.ti.conn, authz.NewGrant(authz.ScopeMCPWrite, f.projectID.String()))
	_, err = f.ti.service.CreateMcpEndpoint(projectOnly, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, f.endpoints(t, ctx))

	scoped := withExactAuthzGrants(t, ctx, f.ti.conn, authz.NewGrant(authz.ScopeMCPWrite, f.toolset.ID.String()))
	_, err = f.ti.service.CreateMcpEndpoint(scoped, payload)
	require.NoError(t, err)
	require.Equal(t, string(payload.Slug), f.requireProjected(t, ctx).Slug)
	f.requireMirrored(t, ctx)
}

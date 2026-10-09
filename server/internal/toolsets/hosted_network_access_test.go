package toolsets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

func TestHostedNetworkAccessMaterializesAndDeletesCanonicalEndpoint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	created := createMinimalPublicToolset(t, ctx, ti, "Hosted Network Access")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := uuid.MustParse(created.ID)
	projectID := *authCtx.ProjectID
	servers := mcpserversrepo.New(ti.conn)
	endpoints := mcpendpointsrepo.New(ti.conn)

	server, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	require.NoError(t, err, "creation materializes the canonical wrapper")
	require.False(t, server.NetworkAccessMode.Valid)

	mode := types.NetworkAccessMode("public_only")
	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, NetworkAccessMode: &mode})
	require.NoError(t, err)
	require.Equal(t, &mode, updated.NetworkAccessMode)
	server, err = servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, serverID, server.ToolsetID.UUID)
	require.Equal(t, "public", server.Visibility)
	require.False(t, server.NetworkAccessMode.Valid)
	addresses, err := endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: serverID})
	require.NoError(t, err)
	require.Len(t, addresses, 1)
	require.Equal(t, string(*created.McpSlug), addresses[0].Slug)

	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction exercises the generic lifecycle guard
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = mcpservers.UpdateMCPServerNetworkAccessModeInTransaction(ctx, tx, audit.NewLogger(), mcpservers.LifecycleUpdateInput{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, ActorUserID: authCtx.UserID, ServerID: serverID,
	}, networkaccess.ModeDual, networkaccess.NewAdmissionFinalizer(func(context.Context, pgx.Tx) error { return nil }))
	require.ErrorContains(t, err, "hosted MCP network access is managed through the toolset")
	require.NoError(t, tx.Rollback(ctx))

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, NetworkAccessMode: &mode})
	require.NoError(t, err)
	addresses, err = endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: serverID})
	require.NoError(t, err)
	require.Len(t, addresses, 1)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: created.Slug}))
	_, err = servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	addresses, err = endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: serverID})
	require.NoError(t, err)
	require.Empty(t, addresses)
}

type allowHostedNetworkAccess struct{}

func (allowHostedNetworkAccess) PrepareNetworkAccess(context.Context, networkaccess.EligibilityInput) (networkaccess.AdmissionFinalizer, error) {
	return networkaccess.NewAdmissionFinalizer(func(context.Context, pgx.Tx) error { return nil }), nil
}

func TestHostedNetworkAccessModeTransitions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	ti.service.WithNetworkAccessEligibility(allowHostedNetworkAccess{})
	created := createMinimalPublicToolset(t, ctx, ti, "Hosted Private Ingress")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := uuid.MustParse(created.ID)
	projectID := *authCtx.ProjectID
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: authCtx.ActiveOrganizationID,
		DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	require.NoError(t, fixtures.SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{
		Status: "online", OrganizationID: authCtx.ActiveOrganizationID,
	}))

	for _, value := range []string{"dual", "private_only", "public_only"} {
		mode := types.NetworkAccessMode(value)
		updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, NetworkAccessMode: &mode})
		require.NoError(t, err)
		require.Equal(t, &mode, updated.NetworkAccessMode)
		server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
		require.NoError(t, err)
		storedMode, err := networkaccess.Effective(server.NetworkAccessMode)
		require.NoError(t, err)
		require.Equal(t, value, string(storedMode))
		addresses, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: serverID})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, string(*created.McpSlug), addresses[0].Slug)
	}
}

func TestHostedNetworkAccessCanDisableDuringIngressOutage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	ti.service.WithNetworkAccessEligibility(allowHostedNetworkAccess{})
	created := createMinimalPublicToolset(t, ctx, ti, "Hosted Ingress Outage")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: authCtx.ActiveOrganizationID,
		DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	require.NoError(t, fixtures.SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{
		Status: "online", OrganizationID: authCtx.ActiveOrganizationID,
	}))
	mode := types.NetworkAccessMode("private_only")
	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, NetworkAccessMode: &mode})
	require.NoError(t, err)
	require.NoError(t, fixtures.SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{
		Status: "offline", OrganizationID: authCtx.ActiveOrganizationID,
	}))
	description := "Edited during a private ingress outage"
	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, Description: &description})
	require.NoError(t, err)
	require.Equal(t, &description, updated.Description)
	disabled := false
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, McpEnabled: &disabled})
	require.NoError(t, err)
}

func TestHostedNetworkAccessPreservesDomainRootOnSlugRename(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	created := createMinimalPublicToolset(t, ctx, ti, "Hosted Domain Root")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domain, err := repo.New(ti.conn).CreateCustomDomain(ctx, repo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID, Domain: "hosted-root-" + uuid.NewString() + ".example.test", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	// A legacy hosted toolset may retain a custom domain that has since been removed.
	_, err = ti.conn.Exec(ctx, `UPDATE toolsets SET custom_domain_id = $1 WHERE id = $2`, domain.ID, uuid.MustParse(created.ID)) //nolint:glint // notestingrawsql: legacy address fixture
	require.NoError(t, err)
	mode := types.NetworkAccessMode("public_only")
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, NetworkAccessMode: &mode})
	require.NoError(t, err)
	serverID := uuid.MustParse(created.ID)
	endpoints := mcpendpointsrepo.New(ti.conn)
	addresses, err := endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: *authCtx.ProjectID, McpServerID: serverID})
	require.NoError(t, err)
	require.Len(t, addresses, 1)
	require.NoError(t, repo.New(ti.conn).SetRootMcpEndpoint(ctx, repo.SetRootMcpEndpointParams{CustomDomainID: domain.ID, McpEndpointID: addresses[0].ID}))
	newSlug := types.Slug("renamed-hosted-" + uuid.NewString()[:8])
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, McpSlug: &newSlug})
	require.NoError(t, err)
	addresses, err = endpoints.ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: *authCtx.ProjectID, McpServerID: serverID})
	require.NoError(t, err)
	require.Len(t, addresses, 1)
	require.True(t, addresses[0].IsDomainRoot.Bool)
	require.Equal(t, string(newSlug), addresses[0].Slug)
}

func TestSetHostedNetworkAccessInTransaction(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	created := createMinimalPublicToolset(t, ctx, ti, "Hosted Transaction Policy")
	mode := types.NetworkAccessMode("public_only")
	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, NetworkAccessMode: &mode})
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	serverID := uuid.MustParse(created.ID)
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: authCtx.ActiveOrganizationID,
		DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	require.NoError(t, fixtures.SetNetworkIngressObservationFixture(ctx, testrepo.SetNetworkIngressObservationFixtureParams{
		Status: "online", OrganizationID: authCtx.ActiveOrganizationID,
	}))

	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction tests atomicity
	require.NoError(t, err)
	require.NoError(t, toolsets.SetHostedNetworkAccessInTransaction(ctx, tx, audit.NewLogger(), authCtx, serverID, networkaccess.ModeDual))
	require.NoError(t, tx.Rollback(ctx))
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.False(t, server.NetworkAccessMode.Valid, "rolled-back policy must not persist")

	tx, err = ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: caller-owned transaction tests commit
	require.NoError(t, err)
	require.NoError(t, toolsets.SetHostedNetworkAccessInTransaction(ctx, tx, audit.NewLogger(), authCtx, serverID, networkaccess.ModeDual))
	require.NoError(t, tx.Commit(ctx))
	server, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, "dual", server.NetworkAccessMode.String)
	stored, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{Slug: string(created.Slug)})
	require.NoError(t, err)
	require.Equal(t, types.NetworkAccessMode("dual"), *stored.NetworkAccessMode)
}

func TestHostedNetworkAccessRejectsPrivateModeWithoutIngress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	ti.service.WithNetworkAccessEligibility(allowHostedNetworkAccess{})
	created := createMinimalPublicToolset(t, ctx, ti, "Hosted Ingress Required")
	mode := types.NetworkAccessMode("private_only")
	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: created.Slug, NetworkAccessMode: &mode})
	require.ErrorContains(t, err, "online private network ingress is required")
	stored, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{Slug: string(created.Slug)})
	require.NoError(t, err)
	require.Equal(t, types.NetworkAccessMode("public_only"), *stored.NetworkAccessMode)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.False(t, server.NetworkAccessMode.Valid, "the rejected mode is not stored")
}

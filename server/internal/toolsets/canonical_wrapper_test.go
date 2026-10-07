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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type canonicalState struct {
	server    mcpserversrepo.McpServer
	endpoints []mcpendpointsrepo.McpEndpoint
}

func loadCanonical(t *testing.T, ctx context.Context, ti *testInstance, toolset *types.Toolset) canonicalState {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	id := uuid.MustParse(toolset.ID)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: id, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err, "toolset %s has a canonical wrapper", toolset.Slug)
	require.Equal(t, uuid.NullUUID{UUID: id, Valid: true}, server.ToolsetID)
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: *authCtx.ProjectID, McpServerID: id})
	require.NoError(t, err)
	return canonicalState{server: server, endpoints: endpoints}
}

// requireCanonicalMatches checks the canonical wrapper mirrors the toolset and
// holds exactly one endpoint at its address.
func requireCanonicalMatches(t *testing.T, ctx context.Context, ti *testInstance, toolset *types.Toolset) canonicalState {
	t.Helper()
	state := loadCanonical(t, ctx, ti, toolset)
	wantVisibility := "disabled"
	if toolset.McpEnabled != nil && *toolset.McpEnabled {
		wantVisibility = "private"
		if toolset.McpIsPublic != nil && *toolset.McpIsPublic {
			wantVisibility = "public"
		}
	}
	require.Equal(t, wantVisibility, state.server.Visibility)
	require.Equal(t, toolset.Name, state.server.Name.String)
	require.NotNil(t, toolset.McpSlug)
	require.Equal(t, string(*toolset.McpSlug), state.server.Slug.String)
	require.Equal(t, conv.PtrValOrEmpty(toolset.UserSessionIssuerID, ""), nullUUIDString(state.server.UserSessionIssuerID))
	require.Equal(t, conv.PtrValOrEmpty(toolset.ToolVariationsGroupID, ""), nullUUIDString(state.server.ToolVariationsGroupID))
	require.Len(t, state.endpoints, 1)
	require.Equal(t, string(*toolset.McpSlug), state.endpoints[0].Slug)
	require.Equal(t, conv.PtrValOrEmpty(toolset.CustomDomainID, ""), nullUUIDString(state.endpoints[0].CustomDomainID))
	return state
}

func nullUUIDString(id uuid.NullUUID) string {
	if !id.Valid {
		return ""
	}
	return id.UUID.String()
}

func TestCanonicalWrapperFollowsEveryToolsetWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createMinimalPrivateToolset(t, ctx, ti, "Canonical Writes")
	endpointID := requireCanonicalMatches(t, ctx, ti, toolset).endpoints[0].ID

	for _, state := range []struct{ enabled, public bool }{{true, true}, {true, false}, {false, true}, {false, false}} {
		updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, McpEnabled: new(state.enabled), McpIsPublic: new(state.public)})
		require.NoError(t, err)
		require.Equal(t, endpointID, requireCanonicalMatches(t, ctx, ti, updated).endpoints[0].ID)
	}

	renamedSlug := types.Slug(authCtx.OrganizationSlug + "-canonical-renamed")
	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, McpSlug: &renamedSlug, Name: new("Canonical Renamed")})
	require.NoError(t, err)
	require.Equal(t, endpointID, requireCanonicalMatches(t, ctx, ti, updated).endpoints[0].ID, "an address change re-keys the same endpoint")

	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID).String()
	updated, err = ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{Slug: toolset.Slug, ToolVariationsGroupID: &groupID})
	require.NoError(t, err)
	requireCanonicalMatches(t, ctx, ti, updated)
	updated, err = ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{Slug: toolset.Slug, ToolVariationsGroupID: nil})
	require.NoError(t, err)
	requireCanonicalMatches(t, ctx, ti, updated)

	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:                         "canonical-workforce",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	issuerID := issuer.ID.String()
	updated, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &issuerID})
	require.NoError(t, err)
	requireCanonicalMatches(t, ctx, ti, updated)
	updated, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: nil})
	require.NoError(t, err)
	requireCanonicalMatches(t, ctx, ti, updated)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug}))
	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(toolset.ID), ProjectID: *authCtx.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: *authCtx.ProjectID, McpServerID: uuid.MustParse(toolset.ID)})
	require.NoError(t, err)
	require.Empty(t, endpoints)
}

func TestCanonicalWrapperForClonedToolsetStartsDisabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	original := createMinimalPublicToolset(t, ctx, ti, "Canonical Clone Source")
	clone, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{Slug: original.Slug})
	require.NoError(t, err)
	require.NotEqual(t, *original.McpSlug, *clone.McpSlug)
	state := requireCanonicalMatches(t, ctx, ti, clone)
	require.Equal(t, "disabled", state.server.Visibility)
}

func TestCanonicalWrapperLeavesFreshIDServersAlone(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Gateway Member")
	toolsetID := uuid.MustParse(toolset.ID)

	servers := mcpserversrepo.New(ti.conn)
	member, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText("Gateway member"), Slug: conv.ToPGText("gateway-member-" + uuid.NewString()[:8]),
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	memberEndpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: member.ID, Valid: true}, Slug: authCtx.OrganizationSlug + "-member-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, Name: "Canonical gateway", Visibility: "private",
	})
	require.NoError(t, err)
	for i, serverID := range []uuid.UUID{member.ID, toolsetID} {
		_, err := metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
			ProjectID: projectID, MetaMcpServerID: gateway.ID, McpServerID: serverID, SortOrder: int32(i),
		})
		require.NoError(t, err)
	}

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Gateway Renamed"), McpEnabled: new(false)})
	require.NoError(t, err)
	afterUpdate, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: member.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, member, afterUpdate, "toolset writes never touch a fresh-id server over the same toolset")

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug}))
	afterDelete, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: member.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, member, afterDelete)
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: member.ID})
	require.NoError(t, err)
	require.Equal(t, []mcpendpointsrepo.McpEndpoint{memberEndpoint}, endpoints)
	members, err := metamcprepo.New(ti.conn).ListMetaMCPMembers(ctx, metamcprepo.ListMetaMCPMembersParams{MetaMcpServerID: gateway.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Len(t, members, 1, "deleting the toolset drops only the canonical wrapper's membership")
	require.Equal(t, member.ID, members[0].McpServerID)
}

func TestCanonicalWrapperFollowsCustomDomainLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domains := cdrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID, Domain: "canonical.example.com",
		IngressName: conv.ToPGText("ingress-canonical"), CertSecretName: conv.ToPGText("cert-canonical"),
		ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	_, err = domains.SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	_, err = domains.ActivateVerifiedCustomDomain(ctx, cdrepo.ActivateVerifiedCustomDomainParams{
		IngressName: conv.ToPGText("ingress-canonical"), CertSecretName: conv.ToPGText("cert-canonical"), ProvisionerKind: "ingress", ID: domain.ID,
	})
	require.NoError(t, err)

	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Domain")
	platformEndpoint := requireCanonicalMatches(t, ctx, ti, toolset).endpoints[0]
	domainID := domain.ID.String()
	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, CustomDomainID: &domainID})
	require.NoError(t, err)
	moved := requireCanonicalMatches(t, ctx, ti, updated).endpoints[0]
	require.Equal(t, platformEndpoint.ID, moved.ID)

	require.NoError(t, domains.SetRootMcpEndpoint(ctx, cdrepo.SetRootMcpEndpointParams{McpEndpointID: moved.ID, CustomDomainID: domain.ID}))
	updated, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Domain Renamed")})
	require.NoError(t, err)
	require.True(t, requireCanonicalMatches(t, ctx, ti, updated).endpoints[0].IsDomainRoot.Bool, "a non-address update keeps the root marker")

	updated, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, McpEnabled: new(false)})
	require.NoError(t, err)
	require.False(t, requireCanonicalMatches(t, ctx, ti, updated).endpoints[0].IsDomainRoot.Valid, "disabling clears the root marker")

	// Domain deletion retires its endpoints; the toolset keeps pointing at the dead domain.
	_, err = mcpendpointsrepo.New(ti.conn).SoftDeleteMCPEndpointsByCustomDomainID(ctx, domain.ID)
	require.NoError(t, err)
	require.NoError(t, domains.DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))
	updated, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Dead Domain"), McpEnabled: new(true)})
	require.NoError(t, err)
	state := loadCanonical(t, ctx, ti, updated)
	require.Equal(t, "Canonical Dead Domain", state.server.Name.String)
	require.Equal(t, "public", state.server.Visibility)
	require.Empty(t, state.endpoints, "no endpoint is recreated on a deleted domain")
}

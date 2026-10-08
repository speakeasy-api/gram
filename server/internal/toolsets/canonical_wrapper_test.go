package toolsets_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

func createActiveCustomDomain(t *testing.T, ctx context.Context, ti *testInstance) cdrepo.CustomDomain {
	t.Helper()
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
	return domain
}

// createRootedToolsetOnDomain returns a public toolset whose canonical endpoint is the domain root.
func createRootedToolsetOnDomain(t *testing.T, ctx context.Context, ti *testInstance, domain cdrepo.CustomDomain, name string) *types.Toolset {
	t.Helper()
	toolset := createMinimalPublicToolset(t, ctx, ti, name)
	domainID := domain.ID.String()
	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, CustomDomainID: &domainID})
	require.NoError(t, err)
	endpoint := requireCanonicalMatches(t, ctx, ti, updated).endpoints[0]
	require.NoError(t, cdrepo.New(ti.conn).SetRootMcpEndpoint(ctx, cdrepo.SetRootMcpEndpointParams{McpEndpointID: endpoint.ID, CustomDomainID: domain.ID}))
	return updated
}

func TestCanonicalWrapperFollowsCustomDomainLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domains := cdrepo.New(ti.conn)
	domain := createActiveCustomDomain(t, ctx, ti)

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

func TestCanonicalWrapperDisableAndMoveAuditsRootCleanupOnce(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createRootedToolsetOnDomain(t, ctx, ti, createActiveCustomDomain(t, ctx, ti), "Canonical Disable Move")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	movedSlug := types.Slug(authCtx.OrganizationSlug + "-canonical-disable-move")
	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, McpEnabled: new(false), McpSlug: &movedSlug})
	require.NoError(t, err)
	require.False(t, requireCanonicalMatches(t, ctx, ti, updated).endpoints[0].IsDomainRoot.Valid)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionCustomDomainsUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "one root cleanup, audited once")
}

func TestCanonicalWrapperSyncClearsRootOnAlreadyDisabledServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	domain := createActiveCustomDomain(t, ctx, ti)
	toolset := createRootedToolsetOnDomain(t, ctx, ti, domain, "Canonical Disabled Root")
	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, McpEnabled: new(false)})
	require.NoError(t, err)

	// A root left on a server that was already disabled.
	endpoint := requireCanonicalMatches(t, ctx, ti, updated).endpoints[0]
	require.NoError(t, cdrepo.New(ti.conn).SetRootMcpEndpoint(ctx, cdrepo.SetRootMcpEndpointParams{McpEndpointID: endpoint.ID, CustomDomainID: domain.ID}))

	updated, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Disabled Root Renamed")})
	require.NoError(t, err)
	require.False(t, requireCanonicalMatches(t, ctx, ti, updated).endpoints[0].IsDomainRoot.Valid)
}

func TestCanonicalWrapperDeletedWhenItsDomainIsAlreadyGone(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createRootedToolsetOnDomain(t, ctx, ti, createActiveCustomDomain(t, ctx, ti), "Canonical Domain Race")

	// The domain row is gone while its endpoint is still live, as mid-way through a concurrent domain deletion.
	require.NoError(t, cdrepo.New(ti.conn).DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug}))
	_, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(toolset.ID), ProjectID: *authCtx.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the canonical wrapper is tombstoned with its toolset")
}

func TestCanonicalWrapperUnsetIssuerClearsRemoteIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Remote Issuer")

	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:                         "canonical-remote",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	remote := remotesessionsrepo.New(ti.conn)
	remoteIssuer, err := remote.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              "canonical-remote-issuer",
		Issuer:                            "https://canonical-remote.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://canonical-remote.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://canonical-remote.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)
	client, err := remote.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID:              "canonical-remote-client",
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, remote.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   issuer.ID,
	}))

	issuerID := issuer.ID.String()
	updated, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &issuerID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: remoteIssuer.ID, Valid: true}, requireCanonicalMatches(t, ctx, ti, updated).server.RemoteSessionIssuerID)

	updated, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: nil})
	require.NoError(t, err)
	require.False(t, requireCanonicalMatches(t, ctx, ti, updated).server.RemoteSessionIssuerID.Valid, "no issuer, no derived remote issuer")
}

func TestCanonicalWrapperSyncAcceptsAgentPrincipal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Agent")

	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	agentAuth := *authCtx
	agentAuth.UserID, agentAuth.Email = "", nil
	agentCtx := contextvalues.WithPrincipalAPIKeyAuthorization(ctx, &agentAuth, agent, contextvalues.PrincipalCredential{AuthorizerUserID: authCtx.UserID, DelegatedGrants: nil, DelegatedGrantsVersion: 0})

	tx, err := ti.conn.Begin(agentCtx) //nolint:glint // notestingrawsql: hostedmcp.Sync runs in a caller-owned transaction.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.Background()) })
	locked, err := toolsetsrepo.New(tx).GetToolsetForUpdate(agentCtx, toolsetsrepo.GetToolsetForUpdateParams{Slug: string(toolset.Slug), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	locked.Name = "Canonical Agent Renamed"
	_, err = hostedmcp.Sync(agentCtx, tx, audit.NewLogger(), hostedmcp.Actor{UserID: agentAuth.UserID, Email: nil}, locked, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(agentCtx))

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, string(urn.PrincipalTypeAgent), record.ActorType)
	require.Equal(t, agent.ID, record.ActorID)
}

// makeLegacy drops the toolset's canonical wrapper, as for a toolset that predates wrappers.
func makeLegacy(t *testing.T, ctx context.Context, ti *testInstance, toolset *types.Toolset) {
	t.Helper()
	id := uuid.MustParse(toolset.ID)
	_, err := ti.conn.Exec(ctx, `DELETE FROM mcp_endpoints WHERE mcp_server_id = $1`, id) //nolint:glint // notestingrawsql: legacy wrapper fixture
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, `DELETE FROM mcp_servers WHERE id = $1`, id) //nolint:glint // notestingrawsql: legacy wrapper fixture
	require.NoError(t, err)
}

func requireNoCanonical(t *testing.T, ctx context.Context, ti *testInstance, toolset *types.Toolset) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(toolset.ID), ProjectID: *authCtx.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestCanonicalWrapperCreatedForLegacyToolsetWithFreeAddress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Legacy Free")
	makeLegacy(t, ctx, ti, toolset)

	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Legacy Free Renamed")})
	require.NoError(t, err)
	requireCanonicalMatches(t, ctx, ti, updated)
}

func TestCanonicalWrapperSkippedWhenForeignEndpointHoldsLegacyAddress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Legacy Foreign")
	makeLegacy(t, ctx, ti, toolset)
	other := createMinimalPublicToolset(t, ctx, ti, "Canonical Legacy Holder")
	holder, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: *authCtx.ProjectID, McpServerID: uuid.NullUUID{UUID: uuid.MustParse(other.ID), Valid: true}, Slug: string(*toolset.McpSlug),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Legacy Foreign Renamed")})
	require.NoError(t, err, "a taken address leaves the toolset legacy instead of failing its save")
	requireNoCanonical(t, ctx, ti, toolset)
	after, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{ID: holder.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, holder, after)
}

func TestCanonicalWrapperSkippedWhenOwnFreshIDServerHoldsLegacyAddress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Legacy Own")
	makeLegacy(t, ctx, ti, toolset)

	servers := mcpserversrepo.New(ti.conn)
	member, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText("Fresh member"), Slug: conv.ToPGText("fresh-member-" + uuid.NewString()[:8]),
		ToolsetID: uuid.NullUUID{UUID: uuid.MustParse(toolset.ID), Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)
	memberEndpoint, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: member.ID, Valid: true}, Slug: string(*toolset.McpSlug),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Legacy Own Renamed")})
	require.NoError(t, err)
	requireNoCanonical(t, ctx, ti, toolset)
	afterServer, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: member.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, member, afterServer)
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: member.ID})
	require.NoError(t, err)
	require.Equal(t, []mcpendpointsrepo.McpEndpoint{memberEndpoint}, endpoints)

	// Requesting a network mode for a wrapper that cannot be created is a conflict, not a silent no-op.
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: hostedmcp.Sync runs in a caller-owned transaction.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.Background()) })
	locked, err := toolsetsrepo.New(tx).GetToolsetForUpdate(ctx, toolsetsrepo.GetToolsetForUpdateParams{Slug: string(toolset.Slug), ProjectID: projectID})
	require.NoError(t, err)
	mode := networkaccess.ModePublicOnly
	_, err = hostedmcp.Sync(ctx, tx, audit.NewLogger(), hostedmcp.Actor{UserID: authCtx.UserID, Email: nil}, locked, &mode)
	require.ErrorIs(t, err, hostedmcp.ErrAddressInUse)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
}

func TestCanonicalWrapperMoveOntoTakenAddressStillConflicts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Move Taken")
	before := requireCanonicalMatches(t, ctx, ti, toolset)

	member, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText("Fresh member"), Slug: conv.ToPGText("fresh-member-" + uuid.NewString()[:8]),
		ToolsetID: uuid.NullUUID{UUID: uuid.MustParse(toolset.ID), Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)
	taken := authCtx.OrganizationSlug + "-taken-" + uuid.NewString()[:8]
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: member.ID, Valid: true}, Slug: taken,
	})
	require.NoError(t, err)

	takenSlug := types.Slug(taken)
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, McpSlug: &takenSlug})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
	require.Equal(t, before, loadCanonical(t, ctx, ti, toolset))
}

func TestCanonicalWrapperSkippedWhenOwnFreshIDServerHoldsServerSlug(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Legacy Server Slug")
	makeLegacy(t, ctx, ti, toolset)

	servers := mcpserversrepo.New(ti.conn)
	member, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText("Fresh member"), Slug: conv.ToPGText(string(*toolset.McpSlug)),
		ToolsetID: uuid.NullUUID{UUID: uuid.MustParse(toolset.ID), Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Legacy Server Slug Renamed")})
	require.NoError(t, err)
	requireNoCanonical(t, ctx, ti, toolset)
	after, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: member.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, member, after)
}

func TestCanonicalWrapperSkippedWhenOwnFreshIDServerHoldsSlugAndDomainRoot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	domain := createActiveCustomDomain(t, ctx, ti)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Legacy Disabled Domain")
	domainID := domain.ID.String()
	toolset, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, CustomDomainID: &domainID, McpEnabled: new(false)})
	require.NoError(t, err)
	makeLegacy(t, ctx, ti, toolset)

	servers := mcpserversrepo.New(ti.conn)
	member, err := servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText("Fresh member"), Slug: conv.ToPGText(string(*toolset.McpSlug)),
		ToolsetID: uuid.NullUUID{UUID: uuid.MustParse(toolset.ID), Valid: true}, Visibility: "public",
	})
	require.NoError(t, err)
	created, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: member.ID, Valid: true},
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true}, Slug: string(*toolset.McpSlug),
	})
	require.NoError(t, err)
	require.NoError(t, cdrepo.New(ti.conn).SetRootMcpEndpoint(ctx, cdrepo.SetRootMcpEndpointParams{McpEndpointID: created.ID, CustomDomainID: domain.ID}))
	memberEndpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: member.ID})
	require.NoError(t, err)
	require.Len(t, memberEndpoints, 1)
	require.True(t, memberEndpoints[0].IsDomainRoot.Bool)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Legacy Disabled Domain Renamed")})
	require.NoError(t, err)
	requireNoCanonical(t, ctx, ti, toolset)
	afterServer, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: member.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, member, afterServer)
	afterEndpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: projectID, McpServerID: member.ID})
	require.NoError(t, err)
	require.Equal(t, memberEndpoints, afterEndpoints)
}

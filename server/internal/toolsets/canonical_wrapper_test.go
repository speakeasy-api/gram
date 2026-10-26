package toolsets_test

import (
	"context"
	"strings"
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
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
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

// requireHostingColumnsUnset asserts no toolset write has given the toolset a legacy alias.
func requireHostingColumnsUnset(t *testing.T, ctx context.Context, ti *testInstance, toolsetID string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	alias, err := legacyalias.Get(ctx, ti.conn, *authCtx.ProjectID, uuid.MustParse(toolsetID))
	require.NoError(t, err)
	require.Equal(t, legacyalias.None(), alias)
}

// requireCanonicalMatches checks the toolset view reports its canonical wrapper's state and single
// endpoint, and that the toolset hosting columns stay unset.
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
	require.Equal(t, conv.PtrValOrEmpty(toolset.UserSessionIssuerID, ""), nullUUIDString(state.server.UserSessionIssuerID))
	require.Equal(t, conv.PtrValOrEmpty(toolset.ToolVariationsGroupID, ""), nullUUIDString(state.server.ToolVariationsGroupID))
	require.Len(t, state.endpoints, 1)
	require.NotNil(t, toolset.McpSlug)
	require.Equal(t, string(*toolset.McpSlug), state.endpoints[0].Slug)
	require.Equal(t, conv.PtrValOrEmpty(toolset.CustomDomainID, ""), nullUUIDString(state.endpoints[0].CustomDomainID))
	requireHostingColumnsUnset(t, ctx, ti, toolset.ID)
	return state
}

func nullUUIDString(id uuid.NullUUID) string {
	if !id.Valid {
		return ""
	}
	return id.UUID.String()
}

func getToolset(t *testing.T, ctx context.Context, ti *testInstance, slug types.Slug) *types.Toolset {
	t.Helper()
	toolset, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{Slug: string(slug)})
	require.NoError(t, err)
	return toolset
}

func TestCreateToolsetCreatesCanonicalServerAndPlatformEndpoint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first := createMinimalPrivateToolset(t, ctx, ti, "Canonical First")
	firstState := requireCanonicalMatches(t, ctx, ti, first)
	require.Equal(t, "private", firstState.server.Visibility, "the organization's first server is enabled")
	require.False(t, firstState.endpoints[0].CustomDomainID.Valid)
	require.True(t, strings.HasPrefix(firstState.endpoints[0].Slug, authCtx.OrganizationSlug+"-"))
	require.Equal(t, firstState.endpoints[0].Slug, firstState.server.Slug.String)

	second := createMinimalPrivateToolset(t, ctx, ti, "Canonical Second")
	secondState := requireCanonicalMatches(t, ctx, ti, second)
	require.Equal(t, "disabled", secondState.server.Visibility)
	require.NotEqual(t, firstState.endpoints[0].Slug, secondState.endpoints[0].Slug)

	serverAudit, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerCreate, second.ID)
	require.NoError(t, err)
	require.Equal(t, authCtx.UserID, serverAudit.ActorID)
	_, err = audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpEndpointCreate, secondState.endpoints[0].ID.String())
	require.NoError(t, err)
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
	_, err = audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerCreate, clone.ID)
	require.NoError(t, err)
}

func TestToolsetWritesNeverTouchHostingColumns(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createMinimalPublicToolset(t, ctx, ti, "Canonical Writes")
	endpointID := requireCanonicalMatches(t, ctx, ti, toolset).endpoints[0].ID

	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Renamed"), Description: new("Described")})
	require.NoError(t, err)
	require.Equal(t, endpointID, requireCanonicalMatches(t, ctx, ti, updated).endpoints[0].ID, "a rename leaves the address alone")
	require.Equal(t, "public", loadCanonical(t, ctx, ti, updated).server.Visibility)

	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID).String()
	updated, err = ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{Slug: toolset.Slug, ToolVariationsGroupID: &groupID})
	require.NoError(t, err)
	requireCanonicalMatches(t, ctx, ti, updated)
	updated, err = ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{Slug: toolset.Slug, ToolVariationsGroupID: nil})
	require.NoError(t, err)
	requireCanonicalMatches(t, ctx, ti, updated)

	clone, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{Slug: toolset.Slug})
	require.NoError(t, err)
	requireHostingColumnsUnset(t, ctx, ti, clone.ID)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug}))
	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: uuid.MustParse(toolset.ID), ProjectID: *authCtx.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: *authCtx.ProjectID, McpServerID: uuid.MustParse(toolset.ID)})
	require.NoError(t, err)
	require.Empty(t, endpoints)
}

func TestHostedServerUpdateWritesWrapperNotToolsetColumns(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Canonical Hosted Update")
	before := loadCanonical(t, ctx, ti, toolset)

	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
		Slug:                         "canonical-hosted-update",
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	server, err := ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "public", func(u *mcpservers.HostedServerUpdate) {
		u.Name = new("Canonical Hosted Renamed")
		u.UserSessionIssuerID = uuid.NullUUID{UUID: issuer.ID, Valid: true}
	}))
	require.NoError(t, err)
	require.Equal(t, "public", server.Visibility)
	require.Equal(t, "Canonical Hosted Renamed", server.Name.String)
	require.Equal(t, uuid.NullUUID{UUID: issuer.ID, Valid: true}, server.UserSessionIssuerID)
	require.Equal(t, before.server.Slug, server.Slug, "the server slug does not follow the name")

	view := getToolset(t, ctx, ti, toolset.Slug)
	require.Equal(t, "Canonical Hosted Renamed", view.Name, "the name is shared with the toolset")
	require.True(t, *view.McpIsPublic)
	require.Equal(t, issuer.ID.String(), *view.UserSessionIssuerID)
	requireCanonicalMatches(t, ctx, ti, view)

	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerUpdate, toolset.ID)
	require.NoError(t, err)
	require.Contains(t, string(record.BeforeSnapshot), `"private"`)
	require.Contains(t, string(record.AfterSnapshot), `"public"`)

	require.NoError(t, ti.service.DisableHostedServer(ctx, uuid.MustParse(toolset.ID)))
	disabled := requireCanonicalMatches(t, ctx, ti, getToolset(t, ctx, ti, toolset.Slug))
	require.Equal(t, "disabled", disabled.server.Visibility)
	require.Len(t, disabled.endpoints, 1, "disabling keeps the endpoints")
}

func TestHostedServerUpdateKeepsValidationRules(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Canonical Rules")
	before := loadCanonical(t, ctx, ti, toolset)

	_, err := ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "everyone", nil))
	requireShareableCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "private", func(u *mcpservers.HostedServerUpdate) {
		u.Name = new(strings.Repeat("n", 61))
	}))
	requireShareableCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "private", func(u *mcpservers.HostedServerUpdate) {
		u.UserSessionIssuerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	}))
	requireShareableCode(t, err, oops.CodeNotFound)
	_, err = ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "private", func(u *mcpservers.HostedServerUpdate) {
		u.ToolVariationsGroupID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	}))
	requireShareableCode(t, err, oops.CodeNotFound)
	private := networkaccess.ModePrivateOnly
	_, err = ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "private", func(u *mcpservers.HostedServerUpdate) {
		u.NetworkAccessMode = &private
	}))
	require.Error(t, err, "private access needs an eligible organization and a live ingress")

	require.Equal(t, before.server, loadCanonical(t, ctx, ti, toolset).server, "rejected writes leave the wrapper untouched")
	requireHostingColumnsUnset(t, ctx, ti, toolset.ID)
}

func TestUpdateToolsetRejectsHostingFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Canonical Rejects")
	before := loadCanonical(t, ctx, ti, toolset)
	mode := types.NetworkAccessMode("dual")
	slug := types.Slug("rejected-slug")
	domain := uuid.NewString()

	for name, payload := range map[string]*gen.UpdateToolsetPayload{
		"mcp_enabled":         {Slug: toolset.Slug, McpEnabled: new(false)},
		"mcp_is_public":       {Slug: toolset.Slug, McpIsPublic: new(true)},
		"mcp_slug":            {Slug: toolset.Slug, McpSlug: &slug},
		"network_access_mode": {Slug: toolset.Slug, NetworkAccessMode: &mode},
		"custom_domain_id":    {Slug: toolset.Slug, CustomDomainID: &domain},
	} {
		payload.Name = new("Renamed " + name)
		_, err := ti.service.UpdateToolset(ctx, payload)
		require.ErrorIs(t, err, toolsets.ErrHostingFieldsMoved, name)
		requireShareableCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, name)
	}

	after := getToolset(t, ctx, ti, toolset.Slug)
	require.Equal(t, toolset.Name, after.Name, "a rejected update writes nothing")
	require.Equal(t, before, loadCanonical(t, ctx, ti, toolset))
	requireHostingColumnsUnset(t, ctx, ti, toolset.ID)
}

// hostedUpdate is an mcpServers.update on toolset's canonical wrapper that keeps its tool variations group.
func hostedUpdate(toolset *types.Toolset, visibility string, mutate func(*mcpservers.HostedServerUpdate)) mcpservers.HostedServerUpdate {
	update := mcpservers.HostedServerUpdate{
		ToolsetID:             uuid.MustParse(toolset.ID),
		Name:                  nil,
		Visibility:            visibility,
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		NetworkAccessMode:     nil,
	}
	if toolset.ToolVariationsGroupID != nil {
		update.ToolVariationsGroupID = uuid.NullUUID{UUID: uuid.MustParse(*toolset.ToolVariationsGroupID), Valid: true}
	}
	if mutate != nil {
		mutate(&update)
	}
	return update
}

func requireShareableCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
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

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{Slug: toolset.Slug, Name: new("Canonical Gateway Renamed")})
	require.NoError(t, err)
	require.NoError(t, ti.service.DisableHostedServer(ctx, toolsetID))
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

func TestHostedServerIssuerResyncsRemoteIssuer(t *testing.T) {
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

	server, err := ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "public", func(u *mcpservers.HostedServerUpdate) {
		u.UserSessionIssuerID = uuid.NullUUID{UUID: issuer.ID, Valid: true}
	}))
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: remoteIssuer.ID, Valid: true}, server.RemoteSessionIssuerID)
	requireCanonicalMatches(t, ctx, ti, getToolset(t, ctx, ti, toolset.Slug))
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
	err = hostedmcp.Sync(agentCtx, tx, audit.NewLogger(), hostedmcp.Actor{UserID: agentAuth.UserID, Email: nil}, locked)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(agentCtx))

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, string(urn.PrincipalTypeAgent), record.ActorType)
	require.Equal(t, agent.ID, record.ActorID)
}

func switchCanonicalBackend(t *testing.T, ctx context.Context, ti *testInstance, server mcpserversrepo.McpServer, remoteID, toolsetID uuid.NullUUID) mcpserversrepo.McpServer {
	t.Helper()
	switched, err := mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		Name: server.Name, Slug: server.Slug, EnvironmentID: server.EnvironmentID, UserSessionIssuerID: server.UserSessionIssuerID,
		RemoteMcpServerID: remoteID, TunneledMcpServerID: uuid.NullUUID{}, ToolsetID: toolsetID,
		UnproxiedMcpServerID: uuid.NullUUID{}, ToolVariationsGroupID: uuid.NullUUID{}, Visibility: server.Visibility,
		NetworkAccessModeSet: false, NetworkAccessMode: pgtype.Text{}, ID: server.ID, ProjectID: server.ProjectID,
	})
	require.NoError(t, err)
	return switched
}

func TestDeleteToolsetLeavesServerMovedToRemoteBackend(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Moved To Remote")
	before := requireCanonicalMatches(t, ctx, ti, toolset)

	remote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID: uuid.New(), ProjectID: *authCtx.ProjectID, Name: conv.ToPGText("Moved To Remote"), Slug: conv.ToPGText("moved-to-remote-" + uuid.NewString()[:8]),
		TransportType: "streamable-http", Url: "https://mcp.example.com/mcp",
	})
	require.NoError(t, err)
	switched := switchCanonicalBackend(t, ctx, ti, before.server, uuid.NullUUID{UUID: remote.ID, Valid: true}, uuid.NullUUID{})

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug, SessionToken: nil, ProjectSlugInput: nil}))

	// The toolset is gone; the server that kept its id, and its address, are not.
	_, err = toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: uuid.MustParse(toolset.ID), ProjectID: *authCtx.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	after, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: switched.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, switched, after)
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: *authCtx.ProjectID, McpServerID: switched.ID})
	require.NoError(t, err)
	require.Equal(t, before.endpoints, endpoints)
	kept, err := remotemcprepo.New(ti.conn).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: remote.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, remote, kept)
}

func TestDeleteToolsetConflictsWhenItsIDServerBelongsToAnotherToolset(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Claimed Identity")
	other := createMinimalPrivateToolset(t, ctx, ti, "Claimed Identity Owner")
	before := requireCanonicalMatches(t, ctx, ti, toolset)
	switchCanonicalBackend(t, ctx, ti, before.server, uuid.NullUUID{}, uuid.NullUUID{UUID: uuid.MustParse(other.ID), Valid: true})

	err := ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug, SessionToken: nil, ProjectSlugInput: nil})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
}

func createLiveCustomDomain(t *testing.T, ctx context.Context, ti *testInstance, name string) cdrepo.CustomDomain {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domains := cdrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID, Domain: name,
		IngressName: conv.ToPGText("ingress-" + name), CertSecretName: conv.ToPGText("cert-" + name),
		ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	_, err = domains.SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	_, err = domains.ActivateVerifiedCustomDomain(ctx, cdrepo.ActivateVerifiedCustomDomainParams{
		IngressName: conv.ToPGText("ingress-" + name), CertSecretName: conv.ToPGText("cert-" + name), ProvisionerKind: "ingress", ID: domain.ID,
	})
	require.NoError(t, err)
	return domain
}

// listedEntry returns toolset's entry from the batched list view.
func listedEntry(t *testing.T, ctx context.Context, ti *testInstance, toolsetID string) *types.ToolsetEntry {
	t.Helper()
	listed, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{})
	require.NoError(t, err)
	for _, entry := range listed.Toolsets {
		if entry.ID == toolsetID {
			return entry
		}
	}
	require.Failf(t, "toolset not listed", "%s", toolsetID)
	return nil
}

func TestToolsetViewsDeriveAddressFromCanonicalServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	toolset := createMinimalPublicToolset(t, ctx, ti, "Derived Address")
	toolsetID := uuid.MustParse(toolset.ID)
	platform := loadCanonical(t, ctx, ti, toolset).endpoints[0]
	domain := createLiveCustomDomain(t, ctx, ti, "derived-"+uuid.NewString()[:8]+".example.com")
	endpoints := mcpendpointsrepo.New(ti.conn)
	onDomain, err := endpoints.CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true}, Slug: "derived-on-domain",
	})
	require.NoError(t, err)

	// The platform endpoint wins over a custom-domain one.
	view := getToolset(t, ctx, ti, toolset.Slug)
	require.Equal(t, platform.Slug, string(*view.McpSlug))
	require.Nil(t, view.CustomDomainID)
	entry := listedEntry(t, ctx, ti, toolset.ID)
	require.Equal(t, platform.Slug, string(*entry.McpSlug))
	require.True(t, *entry.McpIsPublic)

	// Without a platform endpoint the live custom-domain endpoint is the address.
	_, err = endpoints.DeleteMCPEndpoint(ctx, mcpendpointsrepo.DeleteMCPEndpointParams{ID: platform.ID, ProjectID: projectID})
	require.NoError(t, err)
	view = getToolset(t, ctx, ti, toolset.Slug)
	require.Equal(t, onDomain.Slug, string(*view.McpSlug))
	require.Equal(t, domain.ID.String(), *view.CustomDomainID)
	entry = listedEntry(t, ctx, ti, toolset.ID)
	require.Equal(t, onDomain.Slug, string(*entry.McpSlug))
	require.Equal(t, domain.ID.String(), *entry.CustomDomainID)

	// A domain that no longer serves leaves the toolset's frozen address, unset for a new toolset.
	require.NoError(t, cdrepo.New(ti.conn).DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))
	view = getToolset(t, ctx, ti, toolset.Slug)
	require.Nil(t, view.McpSlug)
	require.True(t, *view.McpIsPublic, "visibility still comes from the server")
}

func TestToolsetViewsFallBackToFrozenAlias(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	legacy, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, Name: "Frozen Alias", Slug: "frozen-alias",
	})
	require.NoError(t, err)
	alias := authCtx.OrganizationSlug + "-frozen-alias"
	testenv.SetLegacyAlias(t, ctx, ti.conn, *authCtx.ProjectID, legacy.ID, alias, uuid.NullUUID{})
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: legacy.ID, ProjectID: *authCtx.ProjectID, Name: conv.ToPGText(legacy.Name), Slug: conv.ToPGText(alias),
		ToolsetID: conv.ToNullUUID(legacy.ID), Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	testenv.SetHostedMCP(t, ctx, ti.conn, *authCtx.ProjectID, legacy.ID, testenv.HostedMCP{Slug: alias})
	endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: *authCtx.ProjectID, McpServerID: legacy.ID})
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	_, err = mcpendpointsrepo.New(ti.conn).DeleteMCPEndpoint(ctx, mcpendpointsrepo.DeleteMCPEndpointParams{ID: endpoints[0].ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

	view := getToolset(t, ctx, ti, types.Slug(legacy.Slug))
	require.NotNil(t, view.McpSlug, "a server with no servable endpoint reports the alias that still serves it")
	require.Equal(t, alias, string(*view.McpSlug))
	require.Nil(t, view.CustomDomainID)
	require.True(t, *view.McpEnabled)
	listed := listedEntry(t, ctx, ti, legacy.ID.String()).McpSlug
	require.NotNil(t, listed)
	require.Equal(t, alias, string(*listed))
}

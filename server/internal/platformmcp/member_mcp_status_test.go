package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestMemberMCPStatusToolsAreExternalMemberReads(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	for _, name := range []string{"get_my_mcp_access", "get_my_mcp_connection_status"} {
		descriptor := descriptorByName(t, registrar, name)
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.NotNil(t, descriptor.Annotations)
		require.True(t, descriptor.Annotations.ReadOnlyHint)
	}
}

func TestMemberMCPAccessUsesLiveRBACAndTenantQualifiedTarget(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_member_self_access")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	rows, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidates(ctx, principal.OrganizationID)
	require.NoError(t, err)
	var mcpID uuid.UUID
	for _, row := range rows {
		if row.ProjectID == project.ID {
			mcpID = row.ID
			break
		}
	}
	require.NotEqual(t, uuid.Nil, mcpID)

	dashboardURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	service := NewPluginsService(conn, allowBudget(), "member-self-status-key").WithAuthorization(authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)).WithInstallLinks(dashboardURL, dashboardURL)
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, OrganizationSlug: "example-org"}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	input := GetMyMCPStatusInput{ProjectID: project.ID.String(), MCPID: mcpID.String()}

	allowed := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeMCPRead, mcpID.String())})
	output, err := service.GetMyMCPAccess(allowed, principal, input)
	require.NoError(t, err)
	require.True(t, output.CanRead)
	require.True(t, output.CanConnect, "mcp:read implies mcp:connect")
	require.Empty(t, output.RequiredScope)
	require.Empty(t, output.RequestAccessURL)

	blocked := authz.NewGrant(authz.ScopeMCPRead, mcpID.String())
	blockedConnect := authz.NewGrant(authz.ScopeMCPBlockedConnect, mcpID.String())
	output, err = service.GetMyMCPAccess(authz.GrantsToContext(ctx, []authz.Grant{blocked, blockedConnect}), principal, input)
	require.NoError(t, err)
	require.True(t, output.CanRead)
	require.False(t, output.CanConnect)
	require.Equal(t, "mcp:connect", output.RequiredScope)
	require.Contains(t, output.RequestAccessURL, "scope=mcp%3Aconnect")

	_, err = service.GetMyMCPAccess(allowed, principal, GetMyMCPStatusInput{ProjectID: uuid.NewString(), MCPID: mcpID.String()})
	require.ErrorIs(t, err, ErrMemberMCPStatusTargetNotFound)
	_, err = service.GetMyMCPAccess(authz.GrantsToContext(ctx, nil), principal, input)
	require.ErrorIs(t, err, ErrMemberMCPStatusTargetNotFound)
}

type testMemberMCPConnectionReader struct {
	clients  []remotesessions.Client
	statuses map[uuid.UUID]remotesessions.RemoteSessionState
}

func (r testMemberMCPConnectionReader) ListClients(context.Context, uuid.UUID, string, uuid.UUID) ([]remotesessions.Client, error) {
	return r.clients, nil
}

func (r testMemberMCPConnectionReader) RemoteSessionStatuses(context.Context, urn.SessionSubject, uuid.UUID, string, uuid.UUID) (map[uuid.UUID]remotesessions.RemoteSessionState, error) {
	return r.statuses, nil
}

func TestMemberMCPConnectionStatusProjectsOnlyCallerState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_member_connection_status")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	rows, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidates(ctx, principal.OrganizationID)
	require.NoError(t, err)
	var mcpID uuid.UUID
	for _, row := range rows {
		if row.ProjectID == project.ID {
			mcpID = row.ID
			break
		}
	}
	require.NotEqual(t, uuid.Nil, mcpID)
	remoteIssuer, err := remotesessionsrepo.New(conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: conv.ToNullUUID(project.ID), OrganizationID: conv.ToPGText(principal.OrganizationID),
		Slug: "member-status-" + uuid.NewString()[:8], Issuer: "https://provider.example.test",
		ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
		TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
	})
	require.NoError(t, err)
	remoteIssuerID := remoteIssuer.ID
	stamped, err := testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: conv.ToNullUUID(remoteIssuerID), ID: mcpID, ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped)
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, OrganizationSlug: "example-org"}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, mcpID.String())})
	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)
	clientID := uuid.New()
	input := GetMyMCPStatusInput{ProjectID: project.ID.String(), MCPID: mcpID.String()}

	for _, test := range []struct {
		name       string
		reader     testMemberMCPConnectionReader
		state      string
		reason     string
		nextAction string
	}{
		{name: "not connected", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{{ID: clientID, RemoteSessionIssuerID: remoteIssuerID}}}, state: MCPConnectionStateNotConnected, nextAction: "connect"},
		{name: "active", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{{ID: clientID, RemoteSessionIssuerID: remoteIssuerID}}, statuses: map[uuid.UUID]remotesessions.RemoteSessionState{clientID: {Status: remotesessions.RemoteSessionActive}}}, state: MCPConnectionStateActive, nextAction: "use_mcp"},
		{name: "rejected", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{{ID: clientID, RemoteSessionIssuerID: remoteIssuerID}}, statuses: map[uuid.UUID]remotesessions.RemoteSessionState{clientID: {Status: remotesessions.RemoteSessionActive, ValidationStatus: remotesessions.ValidationOutcomeInactive, ValidationReason: "sensitive provider reason"}}}, state: MCPConnectionStateReauthorizationRequired, reason: "authorization_rejected", nextAction: "reconnect"},
		{name: "setup required", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{}}, state: MCPConnectionStateSetupRequired, reason: "upstream_authorization_not_configured", nextAction: "ask_administrator"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := NewPluginsService(conn, allowBudget(), "member-connection-status-key").WithAuthorization(authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)).withMemberMCPConnectionReader(test.reader).WithInstallLinks(serverURL, serverURL)
			output, err := service.GetMyMCPConnectionStatus(ctx, principal, input)
			require.NoError(t, err)
			require.Equal(t, test.state, output.State)
			require.Equal(t, test.reason, output.Reason)
			require.Equal(t, test.nextAction, output.NextAction)
			require.NotContains(t, output.Reason, "sensitive")
			require.True(t, strings.HasPrefix(output.ConnectionURL, "https://gram.example.test/mcp/"))
		})
	}

	service := NewPluginsService(conn, allowBudget(), "member-connection-denied-key").WithAuthorization(authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)).withMemberMCPConnectionReader(testMemberMCPConnectionReader{}).WithInstallLinks(serverURL, serverURL)
	denied := authz.GrantsToContext(ctx, []authz.Grant{
		authz.NewGrant(authz.ScopeMCPRead, mcpID.String()),
		authz.NewGrant(authz.ScopeMCPBlockedConnect, mcpID.String()),
	})
	output, err := service.GetMyMCPConnectionStatus(denied, principal, input)
	require.NoError(t, err)
	require.Equal(t, MCPConnectionStateNotApplicable, output.State)
	require.Equal(t, "permission_required", output.Reason)
	require.Equal(t, "mcp:connect", output.RequiredScope)
	require.Contains(t, output.RequestAccessURL, "scope=mcp%3Aconnect")
	require.Empty(t, output.ConnectionURL)
	require.Equal(t, "request_access", output.NextAction)
}

func TestMemberMCPStatusDependencyAndAccessLinkFailClosed(t *testing.T) {
	t.Parallel()

	service := NewPluginsService(nil, OperationBudget{}, "")
	require.Same(t, service, service.WithRemoteSessions(nil))
	require.Nil(t, service.remoteSessions)

	dashboardURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	service.WithInstallLinks(dashboardURL, nil)
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "other-org", OrganizationSlug: "other-org"}, urn.NewPrincipal(urn.PrincipalTypeUser, "user"))
	require.Empty(t, service.requestMCPAccessURL(ctx, "expected-org", uuid.NewString(), "Example"))
	require.Equal(t, "ask_administrator", memberMCPAccessNextAction(""))
	require.Equal(t, "request_access", memberMCPAccessNextAction("https://app.example.test/request-access"))
}

func TestMemberMCPConnectionClientsSelectTargetIssuer(t *testing.T) {
	t.Parallel()

	targetIssuer := uuid.New()
	targetClient := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: targetIssuer}
	otherClient := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: uuid.New()}

	require.Equal(t, []remotesessions.Client{targetClient}, memberMCPConnectionClients([]remotesessions.Client{otherClient, targetClient}, targetIssuer))
	require.Empty(t, memberMCPConnectionClients([]remotesessions.Client{otherClient, targetClient}, uuid.Nil))
	require.Empty(t, memberMCPConnectionClients([]remotesessions.Client{otherClient}, targetIssuer))
}

func TestMemberMCPStatusOutputsContainOnlyBoundedCallerFields(t *testing.T) {
	t.Parallel()

	access, err := json.Marshal(GetMyMCPAccessOutput{
		ProjectID: "project", MCPID: "mcp", MCPName: "Example", CanRead: true, CanConnect: false,
		RequiredScope: "mcp:connect", RequestAccessURL: "https://app.example.test/request-access", NextAction: "request_access",
	})
	require.NoError(t, err)
	connection, err := json.Marshal(GetMyMCPConnectionStatusOutput{
		ProjectID: "project", MCPID: "mcp", MCPName: "Example", State: MCPConnectionStateActive,
		ConnectionURL: "https://gram.example.test/mcp/example", NextAction: "use_mcp",
	})
	require.NoError(t, err)
	combined := string(access) + string(connection)
	for _, forbidden := range []string{"access_token", "refresh_token", "client_secret", "oauth_code", "connected_as", "account", "session_id", "role", "grant", "principal"} {
		require.NotContains(t, combined, forbidden)
	}
}

func TestMemberMCPReauthorizationReasonIsBounded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 17, 9, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	for _, test := range []struct {
		name   string
		state  remotesessions.RemoteSessionState
		reason string
	}{
		{name: "absolute authorization", state: remotesessions.RemoteSessionState{AuthorizationExpiresAt: &expired}, reason: "authorization_expired"},
		{name: "refresh lifetime", state: remotesessions.RemoteSessionState{AuthorizationExpiresAt: &future, RefreshExpiresAt: &expired}, reason: "refresh_expired"},
		{name: "upstream validation", state: remotesessions.RemoteSessionState{ValidationStatus: remotesessions.ValidationOutcomeRejectedByMember, ValidationReason: "sensitive upstream detail"}, reason: "authorization_rejected"},
		{name: "access lifetime", state: remotesessions.RemoteSessionState{}, reason: "access_expired"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.reason, memberMCPReauthorizationReason(test.state, now))
		})
	}
}

func TestMemberMCPStatusNotFoundIsSafe(t *testing.T) {
	t.Parallel()

	result, ok := memberMCPStatusToolResult(ErrMemberMCPStatusTargetNotFound)
	require.True(t, ok)
	require.True(t, result.IsError)
	payload, err := json.Marshal(result.Content)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "permission")
	require.NotContains(t, string(payload), "organization")
	require.NotContains(t, string(payload), "grant")
}

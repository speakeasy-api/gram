package platformmcp

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestMemberMCPStatusToolsAreExternalMemberReads(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
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

// testMemberMCPConnectionReader serves clients and statuses through the
// sign-in link, and authClients and authStatuses to the auth client resolver.
type testMemberMCPConnectionReader struct {
	clients      []remotesessions.Client
	statuses     map[uuid.UUID]remotesessions.RemoteSessionState
	authClients  []remotesessions.Client
	authStatuses map[uuid.UUID]remotesessions.RemoteSessionState
}

func (r testMemberMCPConnectionReader) ListClients(context.Context, uuid.UUID, string, uuid.UUID) ([]remotesessions.Client, error) {
	return r.clients, nil
}

func (r testMemberMCPConnectionReader) RemoteSessionStatuses(context.Context, urn.SessionSubject, uuid.UUID, string, uuid.UUID) (map[uuid.UUID]remotesessions.RemoteSessionState, error) {
	return r.statuses, nil
}

func (r testMemberMCPConnectionReader) ListAuthClientRegistrations(_ context.Context, _ uuid.UUID, _ string, ids []uuid.UUID) ([]remotesessions.Client, error) {
	return slices.DeleteFunc(slices.Clone(r.authClients), func(client remotesessions.Client) bool { return !slices.Contains(ids, client.ID) }), nil
}

func (r testMemberMCPConnectionReader) AuthClientRemoteSessionStatuses(context.Context, urn.SessionSubject, uuid.UUID, string, []uuid.UUID) (map[uuid.UUID]remotesessions.RemoteSessionState, error) {
	return r.authStatuses, nil
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
	clientID := associateMemberMCPClient(t, ctx, conn, principal.OrganizationID, project.ID, mcpID, remoteIssuerID)
	input := GetMyMCPStatusInput{ProjectID: project.ID.String(), MCPID: mcpID.String()}
	client := remotesessions.Client{ID: clientID, RemoteSessionIssuerID: remoteIssuerID}
	active := map[uuid.UUID]remotesessions.RemoteSessionState{clientID: {Status: remotesessions.RemoteSessionActive}}
	for _, test := range []struct {
		name       string
		reader     testMemberMCPConnectionReader
		state      string
		reason     string
		nextAction string
	}{
		{name: "typed registrations are not read without enforcement", reader: testMemberMCPConnectionReader{authClients: []remotesessions.Client{client}, authStatuses: active}, state: MCPConnectionStateSetupRequired, reason: "upstream_authorization_not_configured", nextAction: "ask_administrator"},
		{name: "not connected", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{{ID: clientID, RemoteSessionIssuerID: remoteIssuerID}}}, state: MCPConnectionStateNotConnected, nextAction: "connect"},
		{name: "active", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{{ID: clientID, RemoteSessionIssuerID: remoteIssuerID}}, statuses: map[uuid.UUID]remotesessions.RemoteSessionState{clientID: {Status: remotesessions.RemoteSessionActive}}}, state: MCPConnectionStateActive, nextAction: "use_mcp"},
		{name: "rejected", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{{ID: clientID, RemoteSessionIssuerID: remoteIssuerID}}, statuses: map[uuid.UUID]remotesessions.RemoteSessionState{clientID: {Status: remotesessions.RemoteSessionActive, ValidationStatus: remotesessions.ValidationOutcomeInactive, ValidationReason: "sensitive provider reason"}}}, state: MCPConnectionStateReauthorizationRequired, reason: "authorization_rejected", nextAction: "reconnect"},
		{name: "setup required", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{}}, state: MCPConnectionStateSetupRequired, reason: "upstream_authorization_not_configured", nextAction: "ask_administrator"},
		{name: "server holds its own credential", reader: testMemberMCPConnectionReader{clients: []remotesessions.Client{{ID: clientID, RemoteSessionIssuerID: remoteIssuerID, CredentialOwner: remotesessions.CredentialOwnerSelf}}}, state: MCPConnectionStateNotApplicable, reason: "upstream_credential_managed", nextAction: "use_mcp"},
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

// With upstream auth enforcement on, the status comes from the auth clients
// the server offers. The enforcement record alone decides that, with no
// resolver flag set, and neither the server's associations nor the
// authorization server derived for it change the answer.
func TestMemberMCPConnectionStatusFollowsTypedAuthClients(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		// personal is how many personal OAuth auth clients the server offers.
		personal int
		// shared offers a static credential every caller shares.
		shared bool
		// passThrough offers a client that forwards the caller's own credential.
		passThrough bool
		// associated associates a registration no auth client offers.
		associated bool
		// unreachable withholds the offered registrations from the reader.
		unreachable bool
		// connected is the index of the personal client the member connected, or -1.
		connected  int
		state      string
		reason     string
		nextAction string
	}{
		{name: "offered registration with no association is connected", personal: 1, connected: 0, state: MCPConnectionStateActive, nextAction: "use_mcp"},
		{name: "offered registration with no association is not connected", personal: 1, connected: -1, state: MCPConnectionStateNotConnected, nextAction: "connect"},
		{name: "no auth client and nothing pending needs no authorization", connected: -1, state: MCPConnectionStateNotApplicable, reason: "authorization_not_required", nextAction: "use_mcp"},
		{name: "association no auth client records is unfinished setup", associated: true, connected: -1, state: MCPConnectionStateSetupRequired, reason: "upstream_authorization_not_configured", nextAction: "ask_administrator"},
		{name: "shared credential alone needs no connection", shared: true, connected: -1, state: MCPConnectionStateNotApplicable, reason: "upstream_credential_managed", nextAction: "use_mcp"},
		{name: "pass-through alone is no shared credential", passThrough: true, connected: -1, state: MCPConnectionStateNotApplicable, reason: "authorization_not_required", nextAction: "use_mcp"},
		{name: "pass-through beside a personal client leaves one to connect", personal: 1, passThrough: true, connected: -1, state: MCPConnectionStateNotConnected, nextAction: "connect"},
		{name: "member chooses between shared and personal", personal: 1, shared: true, connected: -1, state: MCPConnectionStateNotConnected, reason: "choose_on_connect", nextAction: "connect"},
		{name: "member chooses among personal clients", personal: 2, connected: -1, state: MCPConnectionStateNotConnected, reason: "choose_on_connect", nextAction: "connect"},
		{name: "any connected personal client serves", personal: 2, connected: 1, state: MCPConnectionStateActive, nextAction: "use_mcp"},
		{name: "offered registration out of reach is unfinished setup", personal: 1, unreachable: true, connected: -1, state: MCPConnectionStateSetupRequired, reason: "upstream_authorization_not_configured", nextAction: "ask_administrator"},
		{name: "shared credential serves when the registration is out of reach", personal: 1, shared: true, unreachable: true, connected: -1, state: MCPConnectionStateNotApplicable, reason: "upstream_credential_managed", nextAction: "use_mcp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_member_status_typed")
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
				Slug: "member-typed-" + uuid.NewString()[:8], Issuer: "https://provider.example.test",
				ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
				TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
			})
			require.NoError(t, err)
			// The derived authorization server is stamped in every case and must decide nothing.
			stamped, err := testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
				RemoteSessionIssuerID: conv.ToNullUUID(remoteIssuer.ID), ID: mcpID, ProjectID: project.ID,
			})
			require.NoError(t, err)
			require.Equal(t, int64(1), stamped)
			_, err = productfeaturesrepo.New(conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: principal.OrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
			require.NoError(t, err)

			fixtures := authclientbackfill.New(conn)
			var reader testMemberMCPConnectionReader
			reader.authStatuses = map[uuid.UUID]remotesessions.RemoteSessionState{}
			for i := range test.personal {
				registration, err := remotesessionsrepo.New(conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
					ProjectID: conv.ToNullUUID(project.ID), OrganizationID: conv.ToPGTextEmpty(principal.OrganizationID), RemoteSessionIssuerID: remoteIssuer.ID,
					ClientID: "member-typed-client-" + uuid.NewString()[:8], ClientIDIssuedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
				})
				require.NoError(t, err)
				authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: principal.OrganizationID, ProjectID: conv.ToNullUUID(project.ID), ClientType: authz.AuthClientTypeRemoteSession, Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration.ID)})
				require.NoError(t, err)
				require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: mcpID, AuthClientID: authClientID, Source: "admin"}))
				if !test.unreachable {
					reader.authClients = append(reader.authClients, remotesessions.Client{ID: registration.ID, RemoteSessionIssuerID: remoteIssuer.ID})
				}
				if i == test.connected {
					reader.authStatuses[registration.ID] = remotesessions.RemoteSessionState{Status: remotesessions.RemoteSessionActive}
				}
			}
			if test.shared {
				authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: principal.OrganizationID, ProjectID: conv.ToNullUUID(project.ID), ClientType: authz.AuthClientTypeStaticCredential, Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
				require.NoError(t, err)
				require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: mcpID, AuthClientID: authClientID, Source: "admin"}))
			}
			if test.passThrough {
				authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: principal.OrganizationID, ProjectID: conv.ToNullUUID(project.ID), ClientType: authz.AuthClientTypePassThrough, Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
				require.NoError(t, err)
				require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: project.ID, OrganizationID: principal.OrganizationID, McpServerID: mcpID, AuthClientID: authClientID, Source: "admin"}))
			}
			if test.associated {
				unoffered := associateMemberMCPClient(t, ctx, conn, principal.OrganizationID, project.ID, mcpID, remoteIssuer.ID)
				// A reader that served the association would report it connected.
				reader.clients = []remotesessions.Client{{ID: unoffered, RemoteSessionIssuerID: remoteIssuer.ID}}
				reader.statuses = map[uuid.UUID]remotesessions.RemoteSessionState{unoffered: {Status: remotesessions.RemoteSessionActive}}
				reader.authClients = append(reader.authClients, remotesessions.Client{ID: unoffered, RemoteSessionIssuerID: remoteIssuer.ID})
				reader.authStatuses[unoffered] = remotesessions.RemoteSessionState{Status: remotesessions.RemoteSessionActive}
			}

			ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, OrganizationSlug: "example-org"}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
			ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, mcpID.String())})
			serverURL, err := url.Parse("https://gram.example.test")
			require.NoError(t, err)
			service := NewPluginsService(conn, allowBudget(), "member-typed-status-key").WithAuthorization(authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)).withMemberMCPConnectionReader(reader).WithInstallLinks(serverURL, serverURL)
			output, err := service.GetMyMCPConnectionStatus(ctx, principal, GetMyMCPStatusInput{ProjectID: project.ID.String(), MCPID: mcpID.String()})
			require.NoError(t, err)
			require.Equal(t, test.state, output.State)
			require.Equal(t, test.reason, output.Reason)
			require.Equal(t, test.nextAction, output.NextAction)
		})
	}
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

	byIssuer := func(issuer uuid.UUID) memberMCPStatusTarget {
		return memberMCPStatusTarget{remoteSessionIssuerID: issuer}
	}
	require.Equal(t, []remotesessions.Client{targetClient}, matchTargetClients([]remotesessions.Client{otherClient, targetClient}, byIssuer(targetIssuer)))
	require.Empty(t, matchTargetClients([]remotesessions.Client{otherClient, targetClient}, byIssuer(uuid.Nil)))
	require.Empty(t, matchTargetClients([]remotesessions.Client{otherClient}, byIssuer(targetIssuer)))

	// A proxied server's own clients count whatever authorization server was
	// derived for it.
	sameIssuerOther := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: targetIssuer}
	associated := memberMCPStatusTarget{remoteSessionIssuerID: uuid.Nil, associatedClients: map[uuid.UUID]bool{otherClient.ID: true}}
	require.Equal(t, []remotesessions.Client{otherClient}, matchTargetClients([]remotesessions.Client{otherClient, targetClient, sameIssuerOther}, associated))
	require.True(t, associated.signInRequired())
	require.False(t, memberMCPStatusTarget{remoteSessionIssuerID: targetIssuer, associatedClients: map[uuid.UUID]bool{}}.signInRequired(), "no associated client means no sign-in, even with a derived issuer")
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
		{name: "saved sign-in unreadable", state: remotesessions.RemoteSessionState{SignInUnreadable: true}, reason: "sign_in_unreadable"},
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

// associateMemberMCPClient creates a client of remoteIssuerID in the project
// and associates it with the MCP server, as configuring the server's User
// Identity does.
func associateMemberMCPClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID, mcpID, remoteIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	client, err := remotesessionsrepo.New(conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID: remoteIssuerID,
		ClientID:              "member-client-" + uuid.NewString()[:8],
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
		ProjectID:             projectID,
		OrganizationID:        organizationID,
		McpServerID:           mcpID,
		RemoteSessionClientID: client.ID,
	}))
	return client.ID
}

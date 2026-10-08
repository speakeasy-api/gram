package platformmcp

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// recordingGovernor answers Serves with issuer and serves and
// HasUsableCredential with usable, recording every Serves request.
type recordingGovernor struct {
	issuer   uuid.UUID
	serves   bool
	usable   bool
	mu       sync.Mutex
	requests []identitychaining.Request
}

func (g *recordingGovernor) Serves(_ context.Context, req identitychaining.Request) (uuid.UUID, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, req)
	return g.issuer, g.serves
}

func (g *recordingGovernor) HasUsableCredential(context.Context, identitychaining.Request) bool {
	return g.usable
}

func (g *recordingGovernor) recorded() []identitychaining.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]identitychaining.Request(nil), g.requests...)
}

type chainedCatalogFixture struct {
	principal           Principal
	projectID           uuid.UUID
	remoteID            uuid.UUID
	remoteURL           string
	userSessionIssuerID uuid.UUID
	remoteIssuerID      uuid.UUID
	registrationID      uuid.UUID
	sessions            *remotesessions.ChallengeManager
	policy              *guardian.Policy
}

func seedChainedCatalogFixture(t *testing.T, ctx context.Context, conn *pgxpool.Pool) chainedCatalogFixture {
	t.Helper()
	return seedChainedCatalogFixtureClient(t, ctx, conn, true)
}

// seedChainedCatalogFixtureClient attaches the interactive client only when attach is set.
func seedChainedCatalogFixtureClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, attach bool) chainedCatalogFixture {
	t.Helper()

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	userIssuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          project.ID,
		Slug:               "chained-issuer-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	remoteURL := "https://chained.example.test/mcp"
	remote, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{ID: uuid.New(), ProjectID: project.ID, TransportType: "streamable-http", Url: remoteURL})
	require.NoError(t, err)
	remoteIssuer, err := remotesessionsrepo.New(conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: conv.ToNullUUID(project.ID), OrganizationID: conv.ToPGText(principal.OrganizationID),
		Slug: "chained-remote-" + uuid.NewString()[:8], Issuer: "https://chained.example.test",
		ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
		TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
	})
	require.NoError(t, err)
	client, err := remotesessionsrepo.New(conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID: conv.ToNullUUID(project.ID), OrganizationID: conv.ToPGText(principal.OrganizationID), RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID: "chained-client", ClientIDIssuedAt: conv.ToPGTimestamptz(time.Now().UTC()), TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{},
	})
	require.NoError(t, err)
	if attach {
		require.NoError(t, remotesessionsrepo.New(conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: userIssuer.ID}))
	}

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	baseURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)
	sessions := remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, testenv.NewEncryptionClient(t), policy, nil, cache.NewRedisCacheAdapter(redisClient), baseURL)

	return chainedCatalogFixture{
		principal: principal, projectID: project.ID, remoteID: remote.ID, remoteURL: remoteURL,
		userSessionIssuerID: userIssuer.ID, remoteIssuerID: remoteIssuer.ID, registrationID: uuid.New(), sessions: sessions, policy: policy,
	}
}

func (f chainedCatalogFixture) probe(t *testing.T, conn *pgxpool.Pool, governor IdentityChainingGovernor) ProviderReadinessProbeResult {
	t.Helper()
	prober := NewRemoteMCPReadinessProber(testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t), f.policy, f.sessions)
	if governor != nil {
		prober = prober.WithIdentityChaining(governor)
	}
	result, err := prober.ProbeCatalogReadiness(t.Context(), f.principal, f.projectID, f.registrationID, f.remoteID, f.userSessionIssuerID, uuid.New(), uuid.New())
	require.NoError(t, err)
	return result
}

func TestRemoteMCPReadinessWithoutIdentityChainingNeedsAuthorization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_readiness_no_chaining")
	require.NoError(t, err)
	fixture := seedChainedCatalogFixture(t, ctx, conn)

	result := fixture.probe(t, conn, nil)
	require.Equal(t, ReadinessNeedsGramAuthorization, result.State)
	require.Equal(t, "upstream_authorization_required", result.EvidenceCode)

	declined := &recordingGovernor{serves: false}
	result = fixture.probe(t, conn, declined)
	require.Equal(t, ReadinessNeedsGramAuthorization, result.State)
	require.Equal(t, "upstream_authorization_required", result.EvidenceCode)
	require.Len(t, declined.recorded(), 1)
}

func TestRemoteMCPReadinessMarksIdentityChainingConfiguredWithoutReady(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_readiness_chaining")
	require.NoError(t, err)
	fixture := seedChainedCatalogFixture(t, ctx, conn)

	governor := &recordingGovernor{issuer: fixture.remoteIssuerID, serves: true}
	result := fixture.probe(t, conn, governor)
	require.Equal(t, ReadinessNeedsGramAuthorization, result.State, "configuration alone is not provider evidence")
	require.Equal(t, ReadinessEvidenceIdentityChainingConfigured, result.EvidenceCode)
	require.Equal(t, "no_session", result.AuthorizationIdentity.Absence)
	require.Equal(t, fixture.remoteIssuerID, result.AuthorizationIdentity.RemoteSessionIssuerID)
	fingerprint, err := ProviderAuthorizationFingerprint(result.AuthorizationIdentity)
	require.NoError(t, err)
	require.NotEmpty(t, fingerprint)

	require.Equal(t, []identitychaining.Request{{
		OrganizationID:        fixture.principal.OrganizationID,
		ProjectID:             fixture.projectID,
		UserSessionIssuerID:   fixture.userSessionIssuerID,
		UserID:                fixture.principal.UserID,
		UpstreamResource:      fixture.remoteURL,
		RemoteSessionIssuerID: uuid.NullUUID{},
	}}, governor.recorded())
}

func TestRemoteMCPReadinessReadyOnUsableChainedCredential(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_readiness_chained_ready")
	require.NoError(t, err)
	fixture := seedChainedCatalogFixture(t, ctx, conn)

	result := fixture.probe(t, conn, &recordingGovernor{issuer: fixture.remoteIssuerID, serves: true, usable: true})
	require.Equal(t, ReadinessReady, result.State)
	require.Equal(t, ReadinessEvidenceIdentityChainingActive, result.EvidenceCode)
	require.Equal(t, ProviderAuthorizationIdentityChaining, result.AuthorizationIdentity.Absence)
	require.Equal(t, fixture.remoteIssuerID, result.AuthorizationIdentity.RemoteSessionIssuerID)
	ready, err := ProviderAuthorizationFingerprint(result.AuthorizationIdentity)
	require.NoError(t, err)

	configured := fixture.probe(t, conn, &recordingGovernor{issuer: fixture.remoteIssuerID, serves: true})
	pending, err := ProviderAuthorizationFingerprint(configured.AuthorizationIdentity)
	require.NoError(t, err)
	require.NotEqual(t, pending, ready, "a usable chained credential is a distinct authorization identity")

	chainedIssuer := uuid.New()
	rebound := fixture.probe(t, conn, &recordingGovernor{issuer: chainedIssuer, serves: true, usable: true})
	require.Equal(t, chainedIssuer, rebound.AuthorizationIdentity.RemoteSessionIssuerID, "readiness tracks the issuer identity chaining selected, not the interactive client's")
	require.Equal(t, SetupCategory(""), setupCategoryFromReadiness(Readiness{State: result.State, EvidenceCode: result.EvidenceCode}))
}

func TestRemoteMCPReadinessChainingWithoutInteractiveClient(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_catalog_readiness_chained_no_client")
	require.NoError(t, err)
	fixture := seedChainedCatalogFixtureClient(t, ctx, conn, false)

	result := fixture.probe(t, conn, &recordingGovernor{issuer: fixture.remoteIssuerID, serves: true, usable: true})
	require.Equal(t, ReadinessReady, result.State)
	require.Equal(t, ReadinessEvidenceIdentityChainingActive, result.EvidenceCode)
	require.Equal(t, fixture.remoteIssuerID, result.AuthorizationIdentity.RemoteSessionIssuerID)

	result = fixture.probe(t, conn, &recordingGovernor{issuer: fixture.remoteIssuerID, serves: true})
	require.Equal(t, ReadinessNeedsGramAuthorization, result.State)
	require.Equal(t, ReadinessEvidenceIdentityChainingConfigured, result.EvidenceCode)
	require.Equal(t, "no_session", result.AuthorizationIdentity.Absence)
}

func TestMemberMCPConnectionStatusReportsIdentityChaining(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_member_status_chaining")
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
		Slug: "member-chained-" + uuid.NewString()[:8], Issuer: "https://provider.example.test",
		ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
		TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
	})
	require.NoError(t, err)
	stamped, err := testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: conv.ToNullUUID(remoteIssuer.ID), ID: mcpID, ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped)
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, OrganizationSlug: "example-org"}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, mcpID.String())})
	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)
	clientID := uuid.New()
	clients := []remotesessions.Client{{ID: clientID, RemoteSessionIssuerID: remoteIssuer.ID}}
	input := GetMyMCPStatusInput{ProjectID: project.ID.String(), MCPID: mcpID.String()}
	rejected := map[uuid.UUID]remotesessions.RemoteSessionState{clientID: {Status: remotesessions.RemoteSessionActive, ValidationStatus: remotesessions.ValidationOutcomeInactive}}
	expired := map[uuid.UUID]remotesessions.RemoteSessionState{clientID: {Status: remotesessions.RemoteSessionExpired}}
	active := map[uuid.UUID]remotesessions.RemoteSessionState{clientID: {Status: remotesessions.RemoteSessionActive}}

	for _, test := range []struct {
		name       string
		serves     bool
		usable     bool
		noClient   bool
		noIssuer   bool
		multi      bool
		statuses   map[uuid.UUID]remotesessions.RemoteSessionState
		state      string
		reason     string
		nextAction string
		consulted  bool
	}{
		{name: "no issuer usable credential", noIssuer: true, serves: true, usable: true, state: MCPConnectionStateActive, reason: MCPConnectionReasonIdentityChaining, nextAction: "use_mcp", consulted: true},
		{name: "no issuer configured without credential", noIssuer: true, serves: true, state: MCPConnectionStateNotConnected, reason: ReadinessEvidenceIdentityChainingConfigured, nextAction: "use_mcp", consulted: true},
		{name: "no issuer not chained", noIssuer: true, state: MCPConnectionStateNotApplicable, reason: "authorization_not_required", nextAction: "use_mcp", consulted: true},
		{name: "no session usable credential", serves: true, usable: true, state: MCPConnectionStateActive, reason: MCPConnectionReasonIdentityChaining, nextAction: "use_mcp", consulted: true},
		{name: "expired session usable credential", serves: true, usable: true, statuses: expired, state: MCPConnectionStateActive, reason: MCPConnectionReasonIdentityChaining, nextAction: "use_mcp", consulted: true},
		{name: "no session configured without credential", serves: true, state: MCPConnectionStateNotConnected, reason: ReadinessEvidenceIdentityChainingConfigured, nextAction: "use_mcp", consulted: true},
		{name: "expired session configured without credential", serves: true, statuses: expired, state: MCPConnectionStateReauthorizationRequired, reason: ReadinessEvidenceIdentityChainingConfigured, nextAction: "use_mcp", consulted: true},
		{name: "no session not chained", state: MCPConnectionStateNotConnected, nextAction: "connect", consulted: true},
		{name: "expired session not chained", statuses: expired, state: MCPConnectionStateReauthorizationRequired, reason: "access_expired", nextAction: "reconnect", consulted: true},
		{name: "rejected session wins over usable credential", serves: true, usable: true, statuses: rejected, state: MCPConnectionStateReauthorizationRequired, reason: "authorization_rejected", nextAction: "reconnect", consulted: false},
		{name: "rejected session not chained", statuses: rejected, state: MCPConnectionStateReauthorizationRequired, reason: "authorization_rejected", nextAction: "reconnect", consulted: false},
		{name: "interactive session wins", serves: true, usable: true, statuses: active, state: MCPConnectionStateActive, nextAction: "use_mcp", consulted: false},
		{name: "no client usable credential", noClient: true, serves: true, usable: true, state: MCPConnectionStateActive, reason: MCPConnectionReasonIdentityChaining, nextAction: "use_mcp", consulted: true},
		{name: "no client configured without credential", noClient: true, serves: true, state: MCPConnectionStateNotConnected, reason: ReadinessEvidenceIdentityChainingConfigured, nextAction: "use_mcp", consulted: true},
		{name: "no client not chained", noClient: true, state: MCPConnectionStateSetupRequired, reason: "upstream_authorization_not_configured", nextAction: "ask_administrator", consulted: true},
		{name: "multiple clients usable credential", multi: true, serves: true, usable: true, state: MCPConnectionStateActive, reason: MCPConnectionReasonIdentityChaining, nextAction: "use_mcp", consulted: true},
		{name: "multiple clients configured without credential", multi: true, serves: true, state: MCPConnectionStateNotConnected, reason: ReadinessEvidenceIdentityChainingConfigured, nextAction: "use_mcp", consulted: true},
		{name: "multiple clients not chained", multi: true, state: MCPConnectionStateSetupRequired, reason: "multiple_authorization_clients", nextAction: "ask_administrator", consulted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			issuer := conv.ToNullUUID(remoteIssuer.ID)
			if test.noIssuer {
				issuer = uuid.NullUUID{}
			}
			_, err := testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
				RemoteSessionIssuerID: issuer, ID: mcpID, ProjectID: project.ID,
			})
			require.NoError(t, err)
			readerClients := clients
			if test.noClient {
				readerClients = nil
			}
			if test.multi {
				readerClients = append(append([]remotesessions.Client(nil), clients...), remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: remoteIssuer.ID})
			}
			governor := &recordingGovernor{issuer: remoteIssuer.ID, serves: test.serves, usable: test.usable}
			service := NewPluginsService(conn, allowBudget(), "member-chained-status-key").
				WithAuthorization(authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)).
				withMemberMCPConnectionReader(testMemberMCPConnectionReader{clients: readerClients, statuses: test.statuses}).
				WithIdentityChaining(governor).
				WithInstallLinks(serverURL, serverURL)
			output, err := service.GetMyMCPConnectionStatus(ctx, principal, input)
			require.NoError(t, err)
			require.Equal(t, test.state, output.State)
			require.Equal(t, test.reason, output.Reason)
			require.Equal(t, test.nextAction, output.NextAction)
			if !test.consulted {
				require.Empty(t, governor.recorded())
				return
			}
			require.Len(t, governor.recorded(), 1)
			req := governor.recorded()[0]
			require.Equal(t, principal.OrganizationID, req.OrganizationID)
			require.Equal(t, project.ID, req.ProjectID)
			require.Equal(t, principal.UserID, req.UserID)
			require.NotEqual(t, uuid.Nil, req.UserSessionIssuerID)
			require.Equal(t, "https://cohort.example.test/mcp", req.UpstreamResource)
			require.False(t, req.RemoteSessionIssuerID.Valid)
		})
	}
}

package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// createCapturingServer creates an MCP server through the transaction the
// product uses, which is what captures an organization issuer's client onto a
// new server.
func createCapturingServer(t *testing.T, ctx context.Context, ti *testInstance, slug string, userIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	remote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     *auth.ProjectID,
		Name:          conv.ToPGText(slug),
		Slug:          conv.ToPGText(slug),
		TransportType: "streamable-http",
		Url:           closedTargetURL(slug),
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID:      auth.ActiveOrganizationID,
		ProjectID:           *auth.ProjectID,
		ActorUserID:         auth.UserID,
		ActorEmail:          auth.Email,
		Name:                slug,
		Visibility:          mcpservers.VisibilityPrivate,
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuerID, Valid: true},
		RemoteMCPServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	return server.ID
}

func serverAssociatedClients(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := upstreamclientsrepo.New(ti.conn).ListServerAssociatedClients(ctx, upstreamclientsrepo.ListServerAssociatedClientsParams{
		McpServerIds:   []uuid.UUID{serverID},
		OrganizationID: activeOrganizationID(t, ctx),
	})
	require.NoError(t, err)
	clients := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		clients = append(clients, row.RemoteSessionClientID)
	}
	return clients
}

func commitManualClient(t *testing.T, ctx context.Context, ti *testInstance, serverID, providerID uuid.UUID, clientID string) uuid.UUID {
	t.Helper()
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(serverID, providerID, clientID))
	require.NoError(t, err)
	id, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)
	return id
}

// Adding a server for a second provider leaves the first server's client,
// association and typed link as they were, including on an organization issuer,
// where the new server was given the first server's client when it was created.
func TestCommitAddsAnotherProviderAlongsideAServersClient(t *testing.T) {
	t.Parallel()

	issuers := []struct {
		name   string
		create func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID
	}{
		{name: "organization issuer", create: func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
			t.Helper()
			return seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "alongside-org-issuer")
		}},
		{name: "project issuer", create: func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
			t.Helper()
			return createUserSessionIssuer(t, ctx, ti.conn, "alongside-project-issuer")
		}},
	}
	for _, tc := range issuers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			issuer := tc.create(t, ctx, ti)
			alpha := createServerIdentityProvider(t, ctx, ti, "alongside-alpha", "", false, []string{"none"})
			beta := createServerIdentityProvider(t, ctx, ti, "alongside-beta", "", false, []string{"none"})

			first := createCapturingServer(t, ctx, ti, "alongside-first", issuer)
			alphaClient := commitManualClient(t, ctx, ti, first, alpha, "alongside-alpha-client")
			second := createCapturingServer(t, ctx, ti, "alongside-second", issuer)
			betaClient := commitManualClient(t, ctx, ti, second, beta, "alongside-beta-client")

			requireClientBound(t, ctx, ti, alphaClient, issuer, true)
			requireClientBound(t, ctx, ti, betaClient, issuer, true)
			require.Equal(t, []uuid.UUID{alphaClient}, serverAssociatedClients(t, ctx, ti, first))
			require.Equal(t, []uuid.UUID{betaClient}, serverAssociatedClients(t, ctx, ti, second), "only this server is associated with its new client")
		})
	}
}

// A retried commit, and a second server for the same provider, reuse the bound
// client: no upstream registration and nothing removed.
func TestCommitReusesTheProvidersBoundClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(registrationServer.Close)

	issuer := createUserSessionIssuer(t, ctx, ti.conn, "reuse-issuer")
	provider := createServerIdentityProvider(t, ctx, ti, "reuse-provider", registrationServer.URL, false, []string{"client_secret_basic"})
	first := createCapturingServer(t, ctx, ti, "reuse-first", issuer)
	client := commitManualClient(t, ctx, ti, first, provider, "reuse-client")
	second := createCapturingServer(t, ctx, ti, "reuse-second", issuer)

	for range 2 {
		result, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(second, provider))
		require.NoError(t, err)
		require.Equal(t, client.String(), result.Client.ID)
		require.Equal(t, "linked", conv.PtrValOr(result.Status, ""))
		require.Equal(t, string(remotesessions.RegistrationLinked), conv.PtrValOr(result.RegistrationMethod, ""))
	}

	require.Zero(t, requests.Load(), "reusing a client registers nothing upstream")
	requireClientBound(t, ctx, ti, client, issuer, true)
	require.Equal(t, []uuid.UUID{client}, serverAssociatedClients(t, ctx, ti, first))
	require.Equal(t, []uuid.UUID{client}, serverAssociatedClients(t, ctx, ti, second))
}

// Asking for a client automatically never replaces one, even the server's own:
// only credentials or a client the administrator names can do that.
func TestCommitAutomaticRegistrationKeepsTheServersOwnClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(registrationServer.Close)

	target, issuer := createServerIdentityTarget(t, ctx, ti, "keep-own-target")
	provider := createServerIdentityProvider(t, ctx, ti, "keep-own-provider", registrationServer.URL, false, []string{"client_secret_basic"})
	client := commitManualClient(t, ctx, ti, target, provider, "keep-own-client")

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(target, provider))
	require.NoError(t, err)
	require.Equal(t, client.String(), result.Client.ID)
	require.Zero(t, requests.Load())
	requireClientBound(t, ctx, ti, client, issuer, true)
}

// Different credentials for a provider whose client another server uses are
// refused before anything is written, and the message points at the existing
// client.
func TestCommitRefusesDifferentCredentialsForAClientAnotherServerUses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "credentials-issuer")
	provider := createServerIdentityProvider(t, ctx, ti, "credentials-provider", "", false, []string{"none"})
	first := createCapturingServer(t, ctx, ti, "credentials-first", issuer)
	client := commitManualClient(t, ctx, ti, first, provider, "credentials-client")
	second := createCapturingServer(t, ctx, ti, "credentials-second", issuer)

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(second, provider, "credentials-other-client"))
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "Use the existing client")

	requireClientBound(t, ctx, ti, client, issuer, true)
	require.Equal(t, []uuid.UUID{client}, serverAssociatedClients(t, ctx, ti, first))
	require.Empty(t, serverAssociatedClients(t, ctx, ti, second))
}

// Moving a server to another provider releases a client other servers use: it
// stays bound and only this server's association goes, audited, and resyncing
// does not hand the client back.
func TestCommitMovingToAnotherProviderReleasesASharedClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "move-issuer")
	alpha := createServerIdentityProvider(t, ctx, ti, "move-alpha", "", false, []string{"none"})
	beta := createServerIdentityProvider(t, ctx, ti, "move-beta", "", false, []string{"none"})
	moving := createCapturingServer(t, ctx, ti, "move-moving", issuer)
	alphaClient := commitManualClient(t, ctx, ti, moving, alpha, "move-alpha-client")
	staying := createCapturingServer(t, ctx, ti, "move-staying", issuer)
	_, err := ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(staying, alpha, alphaClient))
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachMcpServer)
	require.NoError(t, err)

	betaClient := commitManualClient(t, ctx, ti, moving, beta, "move-beta-client")

	requireClientBound(t, ctx, ti, alphaClient, issuer, true)
	require.Equal(t, []uuid.UUID{alphaClient}, serverAssociatedClients(t, ctx, ti, staying))
	require.Equal(t, []uuid.UUID{betaClient}, serverAssociatedClients(t, ctx, ti, moving))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachMcpServer)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, activeOrganizationID(t, ctx), projectIDFromContext(t, ctx), []uuid.UUID{issuer}))
	require.Equal(t, []uuid.UUID{betaClient}, serverAssociatedClients(t, ctx, ti, moving))
}

// A client only this server uses is replaced, as a change of provider always
// has been.
func TestCommitMovingToAnotherProviderUnbindsAClientOnlyThisServerUses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "sole-issuer")
	alpha := createServerIdentityProvider(t, ctx, ti, "sole-alpha", "", false, []string{"none"})
	beta := createServerIdentityProvider(t, ctx, ti, "sole-beta", "", false, []string{"none"})
	moving := createCapturingServer(t, ctx, ti, "sole-moving", issuer)
	alphaClient := commitManualClient(t, ctx, ti, moving, alpha, "sole-alpha-client")
	createCapturingServer(t, ctx, ti, "sole-unconfigured", issuer)

	betaClient := commitManualClient(t, ctx, ti, moving, beta, "sole-beta-client")

	requireClientBound(t, ctx, ti, alphaClient, issuer, false)
	requireClientBound(t, ctx, ti, betaClient, issuer, true)
	require.Equal(t, []uuid.UUID{betaClient}, serverAssociatedClients(t, ctx, ti, moving))
}

// A gateway signs in through the issuer's bindings with no association to show
// for it, so a client stays bound for it.
func TestCommitKeepsAClientAGatewayMayUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	target, issuer := createGatewayTarget(t, ctx, ti, "gateway-keep-target")
	createImpactGateway(t, ctx, ti, "gateway-keep", issuer)
	alpha := createServerIdentityProvider(t, ctx, ti, "gateway-keep-alpha", "", false, []string{"none"})
	beta := createServerIdentityProvider(t, ctx, ti, "gateway-keep-beta", "", false, []string{"none"})
	alphaClient := commitManualClient(t, ctx, ti, target, alpha, "gateway-keep-alpha-client")

	betaClient := commitManualClient(t, ctx, ti, target, beta, "gateway-keep-beta-client")

	requireClientBound(t, ctx, ti, alphaClient, issuer, true)
	require.Equal(t, []uuid.UUID{betaClient}, serverAssociatedClients(t, ctx, ti, target))
}

// The live-agent refusal only applies to a client the commit removes, so agents
// attached through another server's client do not block adding a server.
func TestCommitAddingAServerIsNotBlockedByAnotherServersAgents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "agents-issuer")
	alpha := createServerIdentityProvider(t, ctx, ti, "agents-alpha", "", false, []string{"none"})
	beta := createServerIdentityProvider(t, ctx, ti, "agents-beta", "", false, []string{"none"})
	first := createCapturingServer(t, ctx, ti, "agents-first", issuer)
	alphaClient := commitManualClient(t, ctx, ti, first, alpha, "agents-alpha-client")
	seedLiveAgentBinding(t, ctx, ti, alphaClient, issuer)
	second := createCapturingServer(t, ctx, ti, "agents-second", issuer)

	betaClient := commitManualClient(t, ctx, ti, second, beta, "agents-beta-client")

	requireClientBound(t, ctx, ti, alphaClient, issuer, true)
	requireClientBound(t, ctx, ti, betaClient, issuer, true)
	requireLiveAgentBindings(t, ctx, ti, alphaClient, issuer)
}

// A server's callers need only the server's own clients: a sibling's client on
// the issuer cannot refuse them, while a server with no association of its own
// still needs every bound client, so it never calls upstream without one.
func TestResolveServerAccessTokensRequiresOnlyTheServersOwnClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, organizationID := *auth.ProjectID, auth.ActiveOrganizationID
	enc := testenv.NewEncryptionClient(t)
	manager := newResolveManager(t, ti.conn, enc)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "own-clients-issuer")
	ownClient, ownProvider := seedActiveClient(t, ctx, ti.conn, projectID, issuer, organizationID, "own-clients-own")
	siblingClient, siblingProvider := seedActiveClient(t, ctx, ti.conn, projectID, issuer, organizationID, "own-clients-sibling")
	associated := createServerIdentityTargetOnIssuer(t, ctx, ti, "own-clients-associated", issuer)
	unassociated := createServerIdentityTargetOnIssuer(t, ctx, ti, "own-clients-unassociated", issuer)
	rows, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
		OrganizationID:        organizationID,
		RemoteSessionClientID: ownClient,
		McpServerID:           associated,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	// The caller connected the server's own client and never the sibling's.
	subject := urn.NewUserSubject(auth.UserID)
	cipher, err := enc.Encrypt([]byte("own-token"))
	require.NoError(t, err)
	_, err = repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn: subject, UserSessionIssuerID: issuer,
		RemoteSessionClientID: ownClient, AccessTokenEncrypted: cipher,
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		Resource:        conv.ToPGText("https://upstream.example.com/mcp"), Scopes: []string{},
	})
	require.NoError(t, err)

	tokens, err := manager.ResolveServerAccessTokens(ctx, remotesessions.RefreshAttribution{}, projectID, organizationID, issuer, subject, associated, false)
	require.NoError(t, err)
	require.Equal(t, "own-token", tokens[ownProvider].Token)
	require.NotContains(t, tokens, siblingProvider, "a sibling's client is never resolved for this server")
	require.NoError(t, manager.CheckAccessTokens(ctx, remotesessions.RefreshAttribution{}, projectID, organizationID, issuer, subject, uuid.NullUUID{UUID: associated, Valid: true}))

	_, err = manager.ResolveServerAccessTokens(ctx, remotesessions.RefreshAttribution{}, projectID, organizationID, issuer, subject, unassociated, false)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "a server whose clients are unknown stays fail-closed")
	require.ErrorIs(t, manager.CheckAccessTokens(ctx, remotesessions.RefreshAttribution{}, projectID, organizationID, issuer, subject, uuid.NullUUID{UUID: uuid.Nil, Valid: false}), remotesessions.ErrNoValidToken)
	require.NotEqual(t, ownClient, siblingClient)
}

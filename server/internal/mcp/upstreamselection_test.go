package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// selectionFixture is one organization serving MCP servers.
type selectionFixture struct {
	ti        *testInstance
	projectID uuid.UUID
	orgID     string
}

func newSelectionFixture(t *testing.T) (context.Context, selectionFixture) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	ti.service.manual = true
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return ctx, selectionFixture{ti: ti, projectID: *authCtx.ProjectID, orgID: authCtx.ActiveOrganizationID}
}

func (f selectionFixture) associate(t *testing.T, ctx context.Context, serverID, clientID uuid.UUID) {
	t.Helper()
	n, err := upstreamclientsrepo.New(f.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
		OrganizationID:        f.orgID,
		RemoteSessionClientID: clientID,
		McpServerID:           serverID,
		ProjectID:             f.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

// issue mints a bearer for slug on issuerID and records its choices as
// issuance would, at this moment's configuration.
func (f selectionFixture) issue(t *testing.T, ctx context.Context, slug string, issuerID uuid.UUID, subject urn.SessionSubject) string {
	t.Helper()
	return f.issuePicked(t, ctx, slug, issuerID, subject, nil)
}

// issuePicked is issue after a consent page that picked a client for each
// server in picks.
func (f selectionFixture) issuePicked(t *testing.T, ctx context.Context, slug string, issuerID uuid.UUID, subject urn.SessionSubject, picks map[uuid.UUID]uuid.UUID) string {
	t.Helper()
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   f.ti.serverURL.String() + "/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, f.ti, issuerID, subject, jti)
	policy, err := usersessions_repo.New(f.ti.conn).GetUserSessionPolicyByJTI(ctx, usersessions_repo.GetUserSessionPolicyByJTIParams{
		UserSessionIssuerID: issuerID,
		Jti:                 jti,
	})
	require.NoError(t, err)
	servers, clients := []uuid.UUID{}, []uuid.UUID{}
	for serverID, clientID := range picks {
		servers = append(servers, serverID)
		clients = append(clients, clientID)
	}
	_, err = usersessions_repo.New(f.ti.conn).RecordUpstreamClientSelections(ctx, usersessions_repo.RecordUpstreamClientSelectionsParams{
		UserSessionIssuerID: issuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        policy.CredentialID,
		Source:              upstreamclients.SelectionSourceIssuance,
		PickedMcpServerIds:  servers,
		PickedClientIds:     clients,
		PickedAuthClientIds: nil,
	})
	require.NoError(t, err)
	return token
}

// callTool drives tools/call through the public surface. A request the gate
// refuses returns the recorder along with the refusal.
func callTool(t *testing.T, ti *testInstance, slug, bearer, tool string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": map[string]any{}},
	})
	require.NoError(t, err)
	return servePublicHTTP(t, context.Background(), ti, slug, body, bearer, map[string]string{"Accept": "application/json, text/event-stream"})
}

// requireReconnect asserts the gate refused the request with a reconnect
// challenge naming the endpoint's protected resource.
func requireReconnect(t *testing.T, resp *httptest.ResponseRecorder, err error, slug string) {
	t.Helper()
	require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))
	require.Contains(t, resp.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp/"+slug)
}

// Two remote servers share one user session issuer but use different upstream
// clients. Each forwards only its own client's credential, a server issued
// after the credential fails closed into reconnect, and a server needing no
// upstream authentication calls anonymously.
func TestUpstreamClientSelection_DirectServersSharingAnIssuer(t *testing.T) {
	t.Parallel()
	ctx, f := newSelectionFixture(t)

	sharedIssuerID := createUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	linearUpstream := newRecordingUpstream(t, "ping")
	notionUpstream := newRecordingUpstream(t, "ping")
	linearSlug := "sel-linear-" + uuid.NewString()[:8]
	notionSlug := "sel-notion-" + uuid.NewString()[:8]
	linear, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, linearUpstream.url, linearSlug, "public", sharedIssuerID)
	notion, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, notionUpstream.url, notionSlug, "public", sharedIssuerID)

	linearClient := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "sel-linear", "", []uuid.UUID{sharedIssuerID})
	notionClient := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "sel-notion", "", []uuid.UUID{sharedIssuerID})
	f.associate(t, ctx, linear.ID, linearClient)
	f.associate(t, ctx, notion.ID, notionClient)

	subject := createTestUser(t, ctx, f.ti, "sel-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, sharedIssuerID, linearClient, subject, "token-linear", linearUpstream.url)
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, sharedIssuerID, notionClient, subject, "token-notion", notionUpstream.url)

	linearBearer := f.issue(t, ctx, linearSlug, sharedIssuerID, subject)
	notionBearer := f.issue(t, ctx, notionSlug, sharedIssuerID, subject)

	resp, err := callTool(t, f.ti, linearSlug, linearBearer, "ping")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	linearUpstream.requireToolCallAuth(t, "Bearer token-linear")
	linearUpstream.requireNeverCarried(t, "Bearer token-notion", "one server's upstream never sees a sibling's credential")

	resp, err = callTool(t, f.ti, notionSlug, notionBearer, "ping")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	notionUpstream.requireToolCallAuth(t, "Bearer token-notion")
	notionUpstream.requireNeverCarried(t, "Bearer token-linear")

	// Two servers deliberately share one client: each forwards the same
	// connection, and detaching one leaves the other and the client in place.
	mirrorUpstream := newRecordingUpstream(t, "ping")
	mirrorSlug := "sel-mirror-" + uuid.NewString()[:8]
	mirror, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, mirrorUpstream.url, mirrorSlug, "public", sharedIssuerID)
	f.associate(t, ctx, mirror.ID, linearClient)
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, sharedIssuerID, linearClient, subject, "token-linear", linearUpstream.url)
	mirrorBearer := f.issue(t, ctx, mirrorSlug, sharedIssuerID, subject)
	_, err = upstreamclientsrepo.New(f.ti.conn).DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
		McpServerID:           mirror.ID,
		RemoteSessionClientID: linearClient,
		ProjectID:             f.projectID,
	})
	require.NoError(t, err)
	resp, err = callTool(t, f.ti, mirrorSlug, mirrorBearer, "ping")
	requireReconnect(t, resp, err, mirrorSlug)
	resp, err = callTool(t, f.ti, linearSlug, linearBearer, "ping")
	require.NoError(t, err, "the other server keeps the shared client")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// A gated server whose issuer binds no client and that has none
	// associated records an explicit no-auth choice and calls anonymously.
	bareIssuerID := createUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	anonymousUpstream := newRecordingUpstream(t, "ping")
	anonymousSlug := "sel-anon-" + uuid.NewString()[:8]
	createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, anonymousUpstream.url, anonymousSlug, "public", bareIssuerID)
	anonymousBearer := f.issue(t, ctx, anonymousSlug, bareIssuerID, subject)
	resp, err = callTool(t, f.ti, anonymousSlug, anonymousBearer, "ping")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	anonymousUpstream.requireToolCallAuth(t, "")

	// A credential issued before a server was configured has no choice for
	// it, and is asked to reconnect rather than given a guessed client.
	githubUpstream := newRecordingUpstream(t, "ping")
	githubSlug := "sel-github-" + uuid.NewString()[:8]
	github, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, githubUpstream.url, githubSlug, "public", sharedIssuerID)
	f.associate(t, ctx, github.ID, linearClient)
	staleBearer := linearBearer
	resp, err = callTool(t, f.ti, githubSlug, staleBearer, "ping")
	requireReconnect(t, resp, err, githubSlug)
	require.Zero(t, githubUpstream.requestCount(), "a missing choice never reaches the upstream")

	// Detaching the chosen client makes the choice unusable; serving does not
	// fall back to another client the issuer binds.
	_, err = upstreamclientsrepo.New(f.ti.conn).DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
		McpServerID:           notion.ID,
		RemoteSessionClientID: notionClient,
		ProjectID:             f.projectID,
	})
	require.NoError(t, err)
	f.associate(t, ctx, notion.ID, linearClient)
	before := notionUpstream.requestCount()
	resp, err = callTool(t, f.ti, notionSlug, notionBearer, "ping")
	requireReconnect(t, resp, err, notionSlug)
	require.Equal(t, before, notionUpstream.requestCount())

	// Unbinding the chosen client from the issuer, though it stays associated
	// with the server, also asks for a reconnection rather than serving.
	_, err = remotesessions_repo.New(f.ti.conn).DetachProjectRemoteSessionClientFromUserSessionIssuer(ctx, remotesessions_repo.DetachProjectRemoteSessionClientFromUserSessionIssuerParams{
		RemoteSessionClientID: linearClient,
		UserSessionIssuerID:   sharedIssuerID,
		ProjectID:             f.projectID,
		OrganizationID:        f.orgID,
	})
	require.NoError(t, err)
	before = linearUpstream.requestCount()
	resp, err = callTool(t, f.ti, linearSlug, linearBearer, "ping")
	requireReconnect(t, resp, err, linearSlug)
	require.Equal(t, before, linearUpstream.requestCount())
}

// An anonymous session is served through its recorded choices like any other
// session: its chosen client forwards its own connection, and a server
// configured after it was issued asks it to reconnect rather than serving
// through the issuer's clients.
func TestUpstreamClientSelection_AnonymousSessionFollowsItsChoices(t *testing.T) {
	t.Parallel()
	ctx, f := newSelectionFixture(t)

	issuerID := createUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	upstream := newRecordingUpstream(t, "ping")
	slug := "sel-anon-chosen-" + uuid.NewString()[:8]
	server, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, upstream.url, slug, "public", issuerID)
	client := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "sel-anon", "", []uuid.UUID{issuerID})
	f.associate(t, ctx, server.ID, client)

	subject := urn.NewAnonymousSubject("sel-anon-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, issuerID, client, subject, "token-anon", upstream.url)
	bearer := f.issue(t, ctx, slug, issuerID, subject)

	resp, err := callTool(t, f.ti, slug, bearer, "ping")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	upstream.requireToolCallAuth(t, "Bearer token-anon")

	laterUpstream := newRecordingUpstream(t, "ping")
	laterSlug := "sel-anon-later-" + uuid.NewString()[:8]
	later, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, laterUpstream.url, laterSlug, "public", issuerID)
	f.associate(t, ctx, later.ID, client)
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, issuerID, client, subject, "token-anon", laterUpstream.url)
	resp, err = callTool(t, f.ti, laterSlug, bearer, "ping")
	requireReconnect(t, resp, err, laterSlug)
	require.Zero(t, laterUpstream.requestCount(), "a missing choice never reaches the upstream")
}

// A gateway member's credential comes from the client the credential chose
// for that member, and a member added after issuance is unavailable until
// the gateway is signed in to again, while the others keep serving.
func TestUpstreamClientSelection_GatewayMembers(t *testing.T) {
	t.Parallel()
	ctx, f := newSelectionFixture(t)

	sharedIssuerID := createUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	metaSlug := "sel-meta-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, f.ti.conn, f.projectID, f.orgID, metaSlug, sharedIssuerID)

	upstreamA := newRecordingUpstream(t, "ping")
	upstreamB := newRecordingUpstream(t, "ping")
	memberA := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Member A", "member-a", 0, upstreamA.url)
	memberB := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Member B", "member-b", 1, upstreamB.url)

	clientA := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "sel-meta-a", "", []uuid.UUID{sharedIssuerID})
	clientB := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "sel-meta-b", "", []uuid.UUID{sharedIssuerID})
	f.associate(t, ctx, memberA, clientA)
	f.associate(t, ctx, memberB, clientB)

	subject := createTestUser(t, ctx, f.ti, "sel-meta-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, sharedIssuerID, clientA, subject, "token-member-a", upstreamA.url)
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, sharedIssuerID, clientB, subject, "token-member-b", upstreamB.url)
	bearer := f.issue(t, ctx, metaSlug, sharedIssuerID, subject)

	text, isError := metaToolResultText(t, executeMetaTool(t, f.ti, metaSlug, bearer, "member-a--ping"))
	require.False(t, isError, text)
	upstreamA.requireToolCallAuth(t, "Bearer token-member-a")

	text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, metaSlug, bearer, "member-b--ping"))
	require.False(t, isError, text)
	upstreamB.requireToolCallAuth(t, "Bearer token-member-b")

	upstreamC := newRecordingUpstream(t, "ping")
	memberC := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Member C", "member-c", 2, upstreamC.url)
	f.associate(t, ctx, memberC, clientA)
	text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, metaSlug, bearer, "member-c--ping"))
	require.True(t, isError, "a member without a choice must not be served: %s", text)
	require.Contains(t, text, "signed in to again")
	require.Zero(t, upstreamC.requestCount())

	text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, metaSlug, bearer, "member-a--ping"))
	require.False(t, isError, "other members keep serving: %s", text)
}

// On an issuer shared by several servers, a server's consent page offers
// only the clients configured for it.
func TestUpstreamClientSelection_ConsentOffersOnlyTheServersOwnClients(t *testing.T) {
	t.Parallel()
	const upstream = "https://own-client.example.com/mcp"
	var serverID uuid.UUID
	ctx, fx := standaloneConsent(t, "sel-consent", upstream, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		server, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, projectID, upstream, slug, "public", issuerID)
		serverID = server.ID
		return server.ID
	})
	fx.ti.service.manual = true
	own := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "sel-own", "", []uuid.UUID{fx.shared})
	createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "sel-sibling", "", []uuid.UUID{fx.shared})

	n, err := upstreamclientsrepo.New(fx.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
		OrganizationID:        fx.orgID,
		RemoteSessionClientID: own,
		McpServerID:           serverID,
		ProjectID:             fx.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	// With one client left to offer, consent goes straight to it.
	code, html, location := render(t, fx)
	require.Equal(t, http.StatusSeeOther, code, html)
	require.NotNil(t, location)
	require.Equal(t, "sel-own-as.example.com", location.Host, "only the server's own client is offered")
}

// A server offering two clients serves each person through the one picked on
// their consent page, recorded when their credential is issued. A credential
// without a pick is asked to reconnect.
func TestUpstreamClientSelection_ServerOfferingSeveralClientsServesThePickedOne(t *testing.T) {
	t.Parallel()
	ctx, f := newSelectionFixture(t)

	issuerID := createUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	upstream := newRecordingUpstream(t, "ping")
	slug := "sel-choice-" + uuid.NewString()[:8]
	server, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, upstream.url, slug, "public", issuerID)
	first := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "sel-first", "", []uuid.UUID{issuerID})
	second := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "sel-second", "", []uuid.UUID{issuerID})
	f.associate(t, ctx, server.ID, first)
	f.associate(t, ctx, server.ID, second)

	picked := createTestUser(t, ctx, f.ti, "sel-picked-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, issuerID, first, picked, "token-first", upstream.url)
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, issuerID, second, picked, "token-second", upstream.url)
	resp, err := callTool(t, f.ti, slug, f.issuePicked(t, ctx, slug, issuerID, picked, map[uuid.UUID]uuid.UUID{server.ID: second}), "ping")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	upstream.requireToolCallAuth(t, "Bearer token-second")
	upstream.requireNeverCarried(t, "Bearer token-first")
	upstream.awaitMetadataProbe(t)

	before := upstream.requestCount()
	resp, err = callTool(t, f.ti, slug, f.issue(t, ctx, slug, issuerID, picked), "ping")
	requireReconnect(t, resp, err, slug)
	require.Equal(t, before, upstream.requestCount())
}

// A typed auth client picked on the consent page is recorded as the
// credential's choice even when the sign-in does not bind its registration
// for the server, and a picked headers-only client records no registration.
func TestUpstreamClientSelection_RecordsTypedConsentPicks(t *testing.T) {
	t.Parallel()
	ctx, f := newSelectionFixture(t)

	issuerID := createUserSessionIssuer(t, ctx, f.ti.conn, f.projectID)
	oauthServer, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, newRecordingUpstream(t, "ping").url, "typed-oauth-"+uuid.NewString()[:8], "public", issuerID)
	headerServer, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, newRecordingUpstream(t, "ping").url, "typed-header-"+uuid.NewString()[:8], "public", issuerID)
	// Registered for the project, but bound to no sign-in.
	registration := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "typed-unbound", "", nil)

	fixtures := authclientbackfill.New(f.ti.conn)
	seed := func(serverID uuid.UUID, clientType string, registration uuid.NullUUID) uuid.UUID {
		id, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: f.orgID, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, ClientType: clientType, Source: "admin", RemoteSessionClientID: registration})
		require.NoError(t, err)
		require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: serverID, AuthClientID: id, Source: "admin"}))
		return id
	}
	personal := seed(oauthServer.ID, "remote_session", uuid.NullUUID{UUID: registration, Valid: true})
	staticToken := seed(headerServer.ID, "static_credential", uuid.NullUUID{})

	subject := createTestUser(t, ctx, f.ti, "typed-user-"+uuid.NewString())
	_, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: f.ti.serverURL.String() + "/mcp/typed", Lifetime: time.Hour})
	require.NoError(t, err)
	persistTestUserSession(t, f.ti, issuerID, subject, jti)
	policy, err := usersessions_repo.New(f.ti.conn).GetUserSessionPolicyByJTI(ctx, usersessions_repo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: issuerID, Jti: jti})
	require.NoError(t, err)

	_, err = usersessions_repo.New(f.ti.conn).RecordUpstreamClientSelections(ctx, usersessions_repo.RecordUpstreamClientSelectionsParams{
		UserSessionIssuerID: issuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        policy.CredentialID,
		Source:              upstreamclients.SelectionSourceIssuance,
		PickedMcpServerIds:  []uuid.UUID{oauthServer.ID, headerServer.ID},
		PickedClientIds:     []uuid.UUID{registration, uuid.Nil},
		PickedAuthClientIds: []uuid.UUID{personal, staticToken},
	})
	require.NoError(t, err)

	rows, err := upstreamclientsrepo.New(f.ti.conn).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{
		Proposed: false, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: policy.CredentialID,
		McpServerIds: []uuid.UUID{oauthServer.ID, headerServer.ID}, OrganizationID: f.orgID,
	})
	require.NoError(t, err)
	byServer := map[uuid.UUID]upstreamclientsrepo.GetAuthClientSelectionRow{}
	for _, row := range rows {
		byServer[row.McpServerID] = row
	}
	require.Equal(t, uuid.NullUUID{UUID: personal, Valid: true}, byServer[oauthServer.ID].AuthClientID)
	require.Equal(t, uuid.NullUUID{UUID: registration, Valid: true}, byServer[oauthServer.ID].RegistrationID)
	require.Equal(t, uuid.NullUUID{UUID: staticToken, Valid: true}, byServer[headerServer.ID].AuthClientID)
	require.False(t, byServer[headerServer.ID].RegistrationID.Valid)
}

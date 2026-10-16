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
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// selectionFixture is one organization serving from credential choices.
type selectionFixture struct {
	ti        *testInstance
	projectID uuid.UUID
	orgID     string
}

func newSelectionFixture(t *testing.T) (context.Context, selectionFixture) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.features.SetFlag(feature.FlagUpstreamClientSelectionServing, authCtx.ActiveOrganizationID, true)
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
	_, err = usersessions_repo.New(f.ti.conn).RecordUpstreamClientSelections(ctx, usersessions_repo.RecordUpstreamClientSelectionsParams{
		UserSessionIssuerID: issuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        policy.CredentialID,
		Source:              upstreamclients.SelectionSourceIssuance,
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
// only the clients configured for it once serving reads associations.
func TestUpstreamClientSelection_ConsentOffersOnlyTheServersOwnClients(t *testing.T) {
	t.Parallel()
	const upstream = "https://own-client.example.com/mcp"
	var serverID uuid.UUID
	ctx, fx := standaloneConsent(t, "sel-consent", upstream, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		server, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, projectID, upstream, slug, "public", issuerID)
		serverID = server.ID
		return server.ID
	})
	own := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "sel-own", "", []uuid.UUID{fx.shared})
	createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "sel-sibling", "", []uuid.UUID{fx.shared})

	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, "sel-own-rsi")
	require.Contains(t, html, "sel-sibling-rsi", "with the flag off, every client the issuer binds is offered")

	fx.ti.features.SetFlag(feature.FlagUpstreamClientSelectionServing, fx.orgID, true)
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

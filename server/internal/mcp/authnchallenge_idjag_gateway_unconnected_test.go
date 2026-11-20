package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// idJAGGatewayFixture is an enforcing organization whose project gateway
// trusts an ID-JAG issuer, with members that each sign in through one
// personal upstream connection.
type idJAGGatewayFixture struct {
	ti        *testInstance
	signer    idJAGTestAssertion
	orgID     string
	projectID uuid.UUID
	issuerID  uuid.UUID
	clientID  string
	subject   urn.SessionSubject
}

const idJAGGatewayUpstreamIssuer = "https://identity.example.test"

func newIDJAGGatewayFixture(t *testing.T) (context.Context, idJAGGatewayFixture) {
	t.Helper()
	ctx, ti, signer, jwksURL := newIDJAGTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	orgID := authCtx.ActiveOrganizationID

	issuer := createTrustedIDJAGIssuer(t, ctx, ti, orgID, idJAGGatewayUpstreamIssuer, jwksURL)
	client := seedCIMDClient(t, ctx, ti, issuer.ID, codexCIMDClientID)
	seedIDJAGDirectoryUser(t, ctx, ti, orgID)

	ti.service.manual = true
	ti.service.SetAuthClientPermitted(true)
	_, err := featuresrepo.New(ti.conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{OrganizationID: orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	mcp.ResetAuthEnforcementForTest(ti.service.Service)

	return ctx, idJAGGatewayFixture{
		ti: ti, signer: signer, orgID: orgID, projectID: *authCtx.ProjectID,
		issuerID: issuer.ID, clientID: client.ClientID, subject: urn.NewUserSubject(mockidp.MockUserID),
	}
}

// personalClient gives serverID one personal upstream connection as its only
// typed auth client and returns the connection's registration.
func (f idJAGGatewayFixture) personalClient(t *testing.T, ctx context.Context, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	registration := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "personal-"+uuid.NewString()[:8], "", nil)
	require.NoError(t, testrepo.New(f.ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: serverID, RemoteSessionClientID: registration}))
	clients := authclientsrepo.New(f.ti.conn)
	client, err := clients.CreateAuthClient(ctx, authclientsrepo.CreateAuthClientParams{OrganizationID: f.orgID, ProjectID: conv.ToNullUUID(f.projectID), Name: "Personal sign-in", ClientType: authz.AuthClientTypeRemoteSession, RemoteSessionClientID: conv.ToNullUUID(registration), ClientAttachmentScope: conv.ToPGText("project:" + f.projectID.String())})
	require.NoError(t, err)
	_, err = clients.AttachAuthClientToServer(ctx, authclientsrepo.AttachAuthClientToServerParams{OrganizationID: f.orgID, McpServerID: serverID, AuthClientID: client.ID})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, f.ti.conn, f.orgID, mockidp.MockUserID, serverID.String())
	return registration
}

// connect stores the subject's upstream token for registration, as finishing
// its connect page would.
func (f idJAGGatewayFixture) connect(t *testing.T, ctx context.Context, registration uuid.UUID, token, resource string) {
	t.Helper()
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, f.issuerID, registration, f.subject, token, resource)
}

// exchange runs one ID-JAG exchange for slug.
func (f idJAGGatewayFixture) exchange(t *testing.T, ctx context.Context, slug string) (int, string) {
	t.Helper()
	endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(ctx, f.ti.logger, slug, "mcp")
	require.NoError(t, err)
	resource, err := endpoint.RootURL(f.ti.serverURL.String())
	require.NoError(t, err)
	assertion := f.signer.sign(t, idJAGGatewayUpstreamIssuer, resource, resource, f.clientID, mockidp.MockUserEmail, uuid.NewString())
	w := postIDJAGTokenHandled(t, ctx, f.ti, slug, f.clientID, assertion)
	return w.Code, w.Body.String()
}

func (f idJAGGatewayFixture) accessToken(t *testing.T, ctx context.Context, slug string) string {
	t.Helper()
	status, body := f.exchange(t, ctx, slug)
	require.Equal(t, http.StatusOK, status, body)
	var token struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	require.NotEmpty(t, token.AccessToken)
	return token.AccessToken
}

// A gateway member nobody has connected yet leaves only itself out of an
// ID-JAG session: the session is issued, the connected member serves, and the
// unconnected one answers with its connect page.
func TestIDJAGGatewaySessionSkipsUnconnectedMember(t *testing.T) {
	t.Parallel()
	ctx, f := newIDJAGGatewayFixture(t)

	slug := "id-jag-unconnected-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, f.ti.conn, f.projectID, f.orgID, slug, f.issuerID)
	connectedUpstream := newRecordingUpstream(t, "ping")
	unconnectedUpstream := newRecordingUpstream(t, "ping")
	connectedID := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Connected", "connected", 0, connectedUpstream.url)
	unconnectedID := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Unconnected", "unconnected", 1, unconnectedUpstream.url)
	f.connect(t, ctx, f.personalClient(t, ctx, connectedID), "token-connected", connectedUpstream.url)
	unconnected := f.personalClient(t, ctx, unconnectedID)

	bearer := f.accessToken(t, ctx, slug)

	text, isError := metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "connected--ping"))
	require.False(t, isError, text)
	connectedUpstream.requireToolCallAuth(t, "Bearer token-connected", "the connected member receives its own bearer")

	text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "unconnected--ping"))
	require.True(t, isError)
	require.Contains(t, text, "/mcp/"+slug+"/connect/first-party?server=unconnected")

	// The session recorded no choice for the member, so it keeps asking for
	// one after the person connects; another exchange picks the connection up.
	f.connect(t, ctx, unconnected, "token-unconnected", unconnectedUpstream.url)
	text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "unconnected--ping"))
	require.True(t, isError, "the session issued before connecting records no choice for the member: %s", text)
	require.Contains(t, text, "needs to sign in to this gateway again")

	bearer = f.accessToken(t, ctx, slug)
	text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "unconnected--ping"))
	require.False(t, isError, text)
	unconnectedUpstream.requireToolCallAuth(t, "Bearer token-unconnected", "the newly connected member receives its own bearer")
}

// A single server's session is useless without its credential, so the
// exchange still refuses it until the person connects.
func TestIDJAGSingleServerSessionNeedsItsConnection(t *testing.T) {
	t.Parallel()
	ctx, f := newIDJAGGatewayFixture(t)

	slug := "id-jag-direct-" + uuid.NewString()[:8]
	upstream := newRecordingUpstream(t, "ping")
	server, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, upstream.url, slug, "public", f.issuerID)
	serverID := server.ID
	f.personalClient(t, ctx, serverID)

	status, body := f.exchange(t, ctx, slug)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, `"error":"setup_required"`)
}

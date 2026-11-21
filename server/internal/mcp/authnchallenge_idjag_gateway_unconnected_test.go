package mcp_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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
// typed auth client and returns the connection's registration and auth
// client. bound has the
// gateway's issuer bind the registration, which lets issuance decide it for a
// server nobody picked.
func (f idJAGGatewayFixture) personalClient(t *testing.T, ctx context.Context, serverID uuid.UUID, bound bool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	var issuers []uuid.UUID
	if bound {
		issuers = []uuid.UUID{f.issuerID}
	}
	registration := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "personal-"+uuid.NewString()[:8], "", issuers)
	require.NoError(t, testrepo.New(f.ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: serverID, RemoteSessionClientID: registration}))
	clients := authclientsrepo.New(f.ti.conn)
	client, err := clients.CreateAuthClient(ctx, authclientsrepo.CreateAuthClientParams{OrganizationID: f.orgID, ProjectID: conv.ToNullUUID(f.projectID), Name: "Personal sign-in", ClientType: authz.AuthClientTypeRemoteSession, RemoteSessionClientID: conv.ToNullUUID(registration), ClientAttachmentScope: conv.ToPGText("project:" + f.projectID.String())})
	require.NoError(t, err)
	_, err = clients.AttachAuthClientToServer(ctx, authclientsrepo.AttachAuthClientToServerParams{OrganizationID: f.orgID, McpServerID: serverID, AuthClientID: client.ID})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, f.ti.conn, f.orgID, mockidp.MockUserID, serverID.String())
	return registration, client.ID
}

// connect stores the subject's upstream token for registration, as finishing
// its connect page would.
func (f idJAGGatewayFixture) connect(t *testing.T, ctx context.Context, registration uuid.UUID, token, resource string) {
	t.Helper()
	insertQualifiedRemoteSessionToken(t, ctx, f.ti, f.issuerID, registration, f.subject, token, resource)
}

// resource is slug's protected resource, which its tokens are bound to.
func (f idJAGGatewayFixture) resource(t *testing.T, ctx context.Context, slug string) string {
	t.Helper()
	endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(ctx, f.ti.logger, slug, "mcp")
	require.NoError(t, err)
	resource, err := endpoint.RootURL(f.ti.serverURL.String())
	require.NoError(t, err)
	return resource
}

// exchange runs one ID-JAG exchange for slug, adding form to the request.
func (f idJAGGatewayFixture) exchange(t *testing.T, ctx context.Context, slug string, form url.Values) (int, string) {
	t.Helper()
	resource := f.resource(t, ctx, slug)
	values := url.Values{
		"grant_type": {oauthwire.GrantTypeJWTBearer},
		"client_id":  {f.clientID},
		"assertion":  {f.signer.sign(t, idJAGGatewayUpstreamIssuer, resource, resource, f.clientID, mockidp.MockUserEmail, uuid.NewString())},
	}
	maps.Copy(values, form)
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", slug)
	w := httptest.NewRecorder()
	oops.OAuthErrHandle(f.ti.logger, f.ti.service.HandleToken).ServeHTTP(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx)))
	return w.Code, w.Body.String()
}

func (f idJAGGatewayFixture) accessToken(t *testing.T, ctx context.Context, slug string) string {
	t.Helper()
	status, body := f.exchange(t, ctx, slug, nil)
	require.Equal(t, http.StatusOK, status, body)
	var token struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	require.NotEmpty(t, token.AccessToken)
	return token.AccessToken
}

// recordsChoice reports whether bearer's credential recorded a choice for serverID.
func (f idJAGGatewayFixture) recordsChoice(t *testing.T, ctx context.Context, slug, bearer string, serverID uuid.UUID) bool {
	t.Helper()
	validated, err := usersessions.NewSigner("test-jwt-secret").ValidateExactAudienceBearer(ctx, bearer, f.resource(t, ctx, slug), f.ti.chatSessionsManager)
	require.NoError(t, err)
	policy, err := usersessionsrepo.New(f.ti.conn).GetUserSessionPolicyByJTI(ctx, usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: f.issuerID, Jti: validated.JTI()})
	require.NoError(t, err)
	rows, err := upstreamclientsrepo.New(f.ti.conn).ListCredentialChoices(ctx, upstreamclientsrepo.ListCredentialChoicesParams{
		CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: policy.CredentialID, OrganizationID: f.orgID, McpServerIds: []uuid.UUID{serverID},
	})
	require.NoError(t, err)
	return len(rows) > 0
}

// A gateway member nobody has connected yet leaves only itself out of an
// ID-JAG session: the session is issued, the connected member serves, and the
// unconnected one answers with its connect page. The session records no
// choice for that member, whether or not the issuer binds its registration,
// so only a later exchange picks up the connection.
func TestIDJAGGatewaySessionSkipsUnconnectedMember(t *testing.T) {
	t.Parallel()
	for name, bound := range map[string]bool{"unbound registration": false, "issuer-bound registration": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newIDJAGGatewayFixture(t)

			slug := "id-jag-unconnected-" + uuid.NewString()[:8]
			meta := createMetaMcpEndpoint(t, ctx, f.ti.conn, f.projectID, f.orgID, slug, f.issuerID)
			connectedUpstream := newRecordingUpstream(t, "ping")
			unconnectedUpstream := newRecordingUpstream(t, "ping")
			connectedID := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Connected", "connected", 0, connectedUpstream.url)
			unconnectedID := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Unconnected", "unconnected", 1, unconnectedUpstream.url)
			connected, _ := f.personalClient(t, ctx, connectedID, bound)
			f.connect(t, ctx, connected, "token-connected", connectedUpstream.url)
			unconnected, _ := f.personalClient(t, ctx, unconnectedID, bound)

			bearer := f.accessToken(t, ctx, slug)
			require.True(t, f.recordsChoice(t, ctx, slug, bearer, connectedID), "the connected member's choice is recorded")
			require.False(t, f.recordsChoice(t, ctx, slug, bearer, unconnectedID), "the deferred member records no choice")

			text, isError := metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "connected--ping"))
			require.False(t, isError, text)
			connectedUpstream.requireToolCallAuth(t, "Bearer token-connected", "the connected member receives its own bearer")

			text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "unconnected--ping"))
			require.True(t, isError)
			require.Contains(t, text, "/mcp/"+slug+"/connect/first-party?server=unconnected")

			// Connecting does not give the old session a choice for the member;
			// another exchange picks the connection up.
			f.connect(t, ctx, unconnected, "token-unconnected", unconnectedUpstream.url)
			text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "unconnected--ping"))
			require.True(t, isError, "the session issued before connecting records no choice for the member: %s", text)
			require.Contains(t, text, "needs to sign in to this gateway again")
			require.False(t, f.recordsChoice(t, ctx, slug, bearer, unconnectedID), "calling the member records no choice for it either")
			unconnectedUpstream.requireNeverCarried(t, "Bearer token-unconnected", "the old session never reaches the member")

			bearer = f.accessToken(t, ctx, slug)
			text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "unconnected--ping"))
			require.False(t, isError, text)
			unconnectedUpstream.requireToolCallAuth(t, "Bearer token-unconnected", "the newly connected member receives its own bearer")
		})
	}
}

// A member whose sign-in is not set up is deferred only when the request
// names no credential for it: a named credential cannot be checked against a
// server offering none, so the exchange keeps refusing it. Calling the
// deferred member sends the person to an administrator, since connecting
// cannot fix it.
func TestIDJAGGatewayNamedChoiceForIncompleteMemberIsRefused(t *testing.T) {
	t.Parallel()
	ctx, f := newIDJAGGatewayFixture(t)

	slug := "id-jag-incomplete-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, f.ti.conn, f.projectID, f.orgID, slug, f.issuerID)
	connectedUpstream := newRecordingUpstream(t, "ping")
	incompleteUpstream := newRecordingUpstream(t, "ping")
	connectedID := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Connected", "connected", 0, connectedUpstream.url)
	incompleteID := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Incomplete", "incomplete", 1, incompleteUpstream.url)
	connected, _ := f.personalClient(t, ctx, connectedID, false)
	f.connect(t, ctx, connected, "token-connected", connectedUpstream.url)
	// An upstream OAuth client that no auth client offers yet.
	registration := createConsentRemoteClient(t, ctx, f.ti.conn, f.projectID, f.orgID, "incomplete-"+uuid.NewString()[:8], "", nil)
	require.NoError(t, testrepo.New(f.ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: incompleteID, RemoteSessionClientID: registration}))
	seedUserMCPConnectGrant(t, ctx, f.ti.conn, f.orgID, mockidp.MockUserID, incompleteID.String())

	bearer := f.accessToken(t, ctx, slug)
	text, isError := metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "connected--ping"))
	require.False(t, isError, "the session serves its connected member: %s", text)
	text, isError = metaToolResultText(t, executeMetaTool(t, f.ti, slug, bearer, "incomplete--ping"))
	require.True(t, isError)
	require.Contains(t, text, "Ask your administrator")
	require.NotContains(t, text, "/connect/first-party", "connecting cannot fix a sign-in nobody has set up")
	incompleteUpstream.requireNeverCarried(t, "Bearer token-connected", "the incomplete member never receives a sibling's bearer")

	status, body := f.exchange(t, ctx, slug, url.Values{"upstream_auth_client": {incompleteID.String() + ":" + uuid.NewString()}})
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, `"error":"setup_required"`)
}

// Naming an unconnected credential defers its member even when the member
// also offers a ready credential the issuer binds: the session records
// neither, rather than substituting the ready one for the one the caller named.
func TestIDJAGGatewayNamedUnconnectedChoiceRecordsNoSubstitute(t *testing.T) {
	t.Parallel()
	ctx, f := newIDJAGGatewayFixture(t)

	slug := "id-jag-named-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, f.ti.conn, f.projectID, f.orgID, slug, f.issuerID)
	upstream := newRecordingUpstream(t, "ping")
	memberID := seedMetaMemberWithUpstream(t, ctx, f.ti.conn, f.projectID, meta.ID, "Two clients", "two-clients", 0, upstream.url)
	ready, _ := f.personalClient(t, ctx, memberID, true)
	f.connect(t, ctx, ready, "token-ready", upstream.url)
	_, named := f.personalClient(t, ctx, memberID, true)

	status, body := f.exchange(t, ctx, slug, url.Values{"upstream_auth_client": {memberID.String() + ":" + named.String()}})
	require.Equal(t, http.StatusOK, status, body)
	var token struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	require.False(t, f.recordsChoice(t, ctx, slug, token.AccessToken, memberID), "the session records neither the named nor the ready credential")

	text, isError := metaToolResultText(t, executeMetaTool(t, f.ti, slug, token.AccessToken, "two-clients--ping"))
	require.True(t, isError, text)
	upstream.requireNeverCarried(t, "Bearer token-ready", "the ready credential is never substituted for the named one")
}

// A single server's session is useless without its credential, so the
// exchange still refuses it until the person connects.
func TestIDJAGSingleServerSessionNeedsItsConnection(t *testing.T) {
	t.Parallel()
	ctx, f := newIDJAGGatewayFixture(t)

	slug := "id-jag-direct-" + uuid.NewString()[:8]
	upstream := newRecordingUpstream(t, "ping")
	server, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, f.projectID, upstream.url, slug, "public", f.issuerID)
	_, _ = f.personalClient(t, ctx, server.ID, false)

	status, body := f.exchange(t, ctx, slug, nil)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Contains(t, body, `"error":"setup_required"`)
}

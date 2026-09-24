package mcp_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type idJAGTestAssertion struct {
	signer jose.Signer
	jwks   []byte
}

func newIDJAGTestAssertion(t *testing.T) idJAGTestAssertion {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: privateKey},
		(&jose.SignerOptions{}).WithType(jose.ContentType(idjag.Type)).WithHeader(jose.HeaderKey("kid"), "id-jag-test-key"),
	)
	require.NoError(t, err)
	jwks, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: privateKey.Public(), KeyID: "id-jag-test-key", Algorithm: string(jose.ES256), Use: "sig",
	}}})
	require.NoError(t, err)
	return idJAGTestAssertion{signer: signer, jwks: jwks}
}

func (a idJAGTestAssertion) sign(t *testing.T, issuer, resource, clientID, email, assertionID string) string {
	t.Helper()
	now := time.Now()
	claims := jwt.Claims{
		Issuer: issuer, Subject: "external-user", Audience: jwt.Audience{resource},
		Expiry: jwt.NewNumericDate(now.Add(2 * time.Minute)), IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)), ID: assertionID,
	}
	extra := struct {
		Resource string `json:"resource"`
		ClientID string `json:"client_id"`
		Email    string `json:"email"`
	}{Resource: resource, ClientID: clientID, Email: email}
	raw, err := jwt.Signed(a.signer).Claims(claims).Claims(extra).Serialize()
	require.NoError(t, err)
	return raw
}

func postIDJAGToken(t *testing.T, ctx context.Context, ti *testInstance, slug, clientID, assertion string, resources ...string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"grant_type": {oauthwire.GrantTypeJWTBearer},
		"client_id":  {clientID},
		"assertion":  {assertion},
	}
	form["resource"] = resources
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(w, req))
	return w
}

func newIDJAGTestService(t *testing.T, trustedCertificates ...*x509.Certificate) (context.Context, *testInstance, idJAGTestAssertion, string) {
	t.Helper()
	assertionSigner := newIDJAGTestAssertion(t)
	jwksServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(assertionSigner.jwks); err != nil {
			t.Errorf("write JWKS response: %v", err)
		}
	}))
	t.Cleanup(jwksServer.Close)
	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(jwksServer.Certificate())
	for _, certificate := range trustedCertificates {
		rootCAs.AddCert(certificate)
	}
	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardian.WithTLSRootCAs(rootCAs))
	return ctx, ti, assertionSigner, jwksServer.URL
}

func setOrganizationIssuerAdmissionMode(t *testing.T, ctx context.Context, ti *testInstance, issuer usersessionsrepo.UserSessionIssuer, mode admission.Mode) {
	t.Helper()

	_, err := usersessionsrepo.New(ti.conn).UpdateOrganizationUserSessionIssuer(ctx, usersessionsrepo.UpdateOrganizationUserSessionIssuerParams{
		Slug:                          pgtype.Text{},
		AuthnChallengeMode:            pgtype.Text{},
		SessionDuration:               pgtype.Interval{},
		ClientIDMetadataAdmissionMode: conv.ToPGText(string(mode)),
		TrustedRemoteSessionIssuerID:  pgtype.Text{},
		ID:                            issuer.ID,
		OrganizationID:                issuer.OrganizationID.String,
	})
	require.NoError(t, err)
}

func createTrustedIDJAGIssuer(t *testing.T, ctx context.Context, ti *testInstance, organizationID, upstreamIssuer, jwksURL string) usersessionsrepo.UserSessionIssuer {
	t.Helper()
	remoteIssuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              "id-jag-" + uuid.NewString(),
		Issuer:                            upstreamIssuer,
		JwksUri:                           conv.ToPGText(jwksURL),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{},
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: []string{},
	})
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(organizationID),
		Slug:                         "id-jag-" + uuid.NewString(),
		AuthnChallengeMode:           "chain",
		SessionDuration:              pgtype.Interval{Microseconds: int64((8 * time.Hour) / time.Microsecond), Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: remoteIssuer.ID, Valid: true},
	})
	require.NoError(t, err)
	return issuer
}

func createIDJAGClient(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) usersessionsrepo.UserSessionClient {
	t.Helper()
	client, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuerID,
		ClientID:                "id-jag-client-" + uuid.NewString(),
		ClientName:              "ID-JAG test client",
		RedirectUris:            []string{"http://127.0.0.1/callback"},
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	require.NoError(t, err)
	return client
}

func seedIDJAGDirectoryUser(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := directoryrepo.New(ti.conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                conv.ToPGText(mockidp.MockUserID),
		WorkosDirectoryUserID: "id-jag-directory-" + uuid.NewString(),
		Email:                 conv.ToPGText(mockidp.MockUserEmail),
		Attributes:            []byte(`{}`),
		WorkosCreatedAt:       conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGText("id-jag-event-" + uuid.NewString()),
		RestoreDeleted:        false,
	})
	require.NoError(t, err)
}

func TestTokenIDJAGExchangeMintsResourceBoundAccessOnlySession(t *testing.T) {
	t.Parallel()

	ctx, ti, assertionSigner, jwksURL := newIDJAGTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	upstreamIssuer := "https://identity.example.test"
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, upstreamIssuer, jwksURL)
	var err error
	toolset, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	client := createIDJAGClient(t, ctx, ti, issuer.ID)
	seedIDJAGDirectoryUser(t, ctx, ti, authCtx.ActiveOrganizationID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), toolset.ID)

	resource, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)
	assertionID := uuid.NewString()
	assertion := assertionSigner.sign(t, upstreamIssuer, resource, client.ClientID, mockidp.MockUserEmail, assertionID)
	w := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, assertion)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var token struct {
		AccessToken            string `json:"access_token"`
		TokenType              string `json:"token_type"`
		ExpiresIn              int64  `json:"expires_in"`
		RefreshToken           string `json:"refresh_token"`
		AuthorizationExpiresIn int64  `json:"authorization_expires_in"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &token))
	require.Equal(t, "Bearer", token.TokenType)
	require.NotEmpty(t, token.AccessToken)
	require.Empty(t, token.RefreshToken)
	require.InDelta(t, int64(time.Hour/time.Second), token.ExpiresIn, 2)
	require.InDelta(t, int64(time.Hour/time.Second), token.AuthorizationExpiresIn, 2)

	validated, err := usersessions.NewSigner("test-jwt-secret").ValidateExactAudienceBearer(ctx, token.AccessToken, resource, ti.chatSessionsManager)
	require.NoError(t, err)
	require.Equal(t, urn.NewUserSubject(mockidp.MockUserID), validated.Subject())
	require.Equal(t, client.ClientID, validated.ClientID())
	_, err = usersessions.NewSigner("test-jwt-secret").ValidateExactAudienceBearer(ctx, token.AccessToken, resource+"-sibling", ti.chatSessionsManager)
	require.Error(t, err)

	session, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{
		UserSessionIssuerID: issuer.ID,
		Jti:                 validated.JTI(),
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(session.RefreshTokenHash.String, "id-jag:"))

	replay := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, assertion)
	require.Equal(t, http.StatusBadRequest, replay.Code, replay.Body.String())
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"assertion is invalid"}`, replay.Body.String())
}

func TestTokenIDJAGExchangeRejectsMismatchedResourceWithoutConsumingAssertion(t *testing.T) {
	t.Parallel()

	ctx, ti, assertionSigner, jwksURL := newIDJAGTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	const upstreamIssuer = "https://identity.example.test"
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, upstreamIssuer, jwksURL)
	toolset, err := toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	client := createIDJAGClient(t, ctx, ti, issuer.ID)
	seedIDJAGDirectoryUser(t, ctx, ti, authCtx.ActiveOrganizationID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), toolset.ID)

	resource, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)
	assertion := assertionSigner.sign(t, upstreamIssuer, resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	rejected := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, assertion, resource, "https://other.example.test/mcp")
	require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
	require.JSONEq(t, `{"error":"invalid_target","error_description":"resource does not identify this MCP server (expected \"`+resource+`\")"}`, rejected.Body.String())

	accepted := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, assertion, resource)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
}

func TestTokenIDJAGExchangeAppliesCurrentCIMDAdmissionBeforeConsumingAssertion(t *testing.T) {
	t.Parallel()

	ds := startCIMDDocServer(t)
	ctx, ti, assertionSigner, jwksURL := newIDJAGTestService(t, ds.srv.Certificate())
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	const upstreamIssuer = "https://identity.example.test"
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, upstreamIssuer, jwksURL)
	toolset, err := toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	seedIDJAGDirectoryUser(t, ctx, ti, authCtx.ActiveOrganizationID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), toolset.ID)

	resource, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)
	assertion := assertionSigner.sign(t, upstreamIssuer, resource, ds.clientID, mockidp.MockUserEmail, uuid.NewString())
	setOrganizationIssuerAdmissionMode(t, ctx, ti, issuer, admission.ModePresets)
	rejected := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, ds.clientID, assertion, resource)
	require.Equal(t, http.StatusUnauthorized, rejected.Code, rejected.Body.String())
	require.JSONEq(t, `{"error":"invalid_client","error_description":"this client ID metadata document URL is not permitted by the server's client policy; the server operator must allow it"}`, rejected.Body.String())
	require.Zero(t, ds.requests.Load(), "admission must reject before fetching client metadata")

	setOrganizationIssuerAdmissionMode(t, ctx, ti, issuer, admission.ModeOpen)
	accepted := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, ds.clientID, assertion, resource)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	require.EqualValues(t, 1, ds.requests.Load())
}

func TestTokenIDJAGExchangeAuthorizesMetaMCPMembers(t *testing.T) {
	t.Parallel()

	ctx, ti, assertionSigner, jwksURL := newIDJAGTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	const upstreamIssuer = "https://identity.example.test"
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, upstreamIssuer, jwksURL)
	slug := "id-jag-meta-" + uuid.NewString()
	projectIssuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, projectIssuerID)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "private ID-JAG member", 1, mcpservers.VisibilityPrivate, "member_tool")
	client := createIDJAGClient(t, ctx, ti, issuer.ID)
	seedIDJAGDirectoryUser(t, ctx, ti, authCtx.ActiveOrganizationID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), member.toolsetID)

	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	// Exercise the shared post-resolution token path with organization-tier
	// trust while keeping this test focused on member authorization policy.
	endpoint.UserSessionIssuerID = issuer.ID
	resource, err := endpoint.RootURL(ti.serverURL.String())
	require.NoError(t, err)
	assertion := assertionSigner.sign(t, upstreamIssuer, resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	form := url.Values{
		"grant_type": {oauthwire.GrantTypeJWTBearer},
		"client_id":  {client.ClientID},
		"assertion":  {assertion},
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeToken(w, req, endpoint))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestTokenIDJAGExchangeRejectsTrustedIssuerFromAnotherOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti, assertionSigner, jwksURL := newIDJAGTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, toolset.McpSlug.String, "mcp")
	require.NoError(t, err)

	otherOrganizationID := "org_" + uuid.NewString()
	require.NoError(t, orgsrepo.New(ti.conn).CreateOrganizationMetadata(ctx, orgsrepo.CreateOrganizationMetadataParams{
		ID: otherOrganizationID, Name: "Other organization", Slug: "other-" + uuid.NewString(),
	}))
	const upstreamIssuer = "https://other-identity.example.test"
	otherIssuer := createTrustedIDJAGIssuer(t, ctx, ti, otherOrganizationID, upstreamIssuer, jwksURL)
	client := createIDJAGClient(t, ctx, ti, otherIssuer.ID)
	seedIDJAGDirectoryUser(t, ctx, ti, otherOrganizationID)

	// Preserve the addressed endpoint's organization while simulating a stale
	// or corrupt cross-tenant issuer reference. The ID-JAG store must scope the
	// trust lookup to the endpoint organization and reject the assertion.
	endpoint.UserSessionIssuerID = otherIssuer.ID
	resource, err := endpoint.RootURL(ti.serverURL.String())
	require.NoError(t, err)
	assertion := assertionSigner.sign(t, upstreamIssuer, resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	form := url.Values{
		"grant_type": {oauthwire.GrantTypeJWTBearer},
		"client_id":  {client.ClientID},
		"assertion":  {assertion},
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeToken(w, req, endpoint))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"assertion is invalid"}`, w.Body.String())
}

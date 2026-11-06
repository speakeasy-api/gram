package mcp_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// serviceChoiceGateway is a gateway whose one member, "source", signs in through two services the person connected.
type serviceChoiceGateway struct {
	ti       *testInstance
	issuerID uuid.UUID
	client   usersessionsrepo.UserSessionClient
	gateway  uuid.UUID
	slug     string
	serverID uuid.UUID
	orgID    string
	services []uuid.UUID
}

func seedServiceChoiceGateway(t *testing.T, connected int) (context.Context, serviceChoiceGateway) {
	t.Helper()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "service-choice-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)
	fx := serviceChoiceGateway{ti: ti, issuerID: issuer.ID, client: client, gateway: gateway.ID, slug: slug, serverID: serverID, orgID: auth.ActiveOrganizationID, services: nil}
	for i := range connected {
		registration := createConsentRemoteClient(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "service-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
		_, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, McpServerID: serverID, RemoteSessionClientID: registration})
		require.NoError(t, err)
		insertQualifiedRemoteSessionToken(t, ctx, ti, issuer.ID, registration, urn.NewUserSubject(mockidp.MockUserID), "service-token-"+string(rune('a'+i)), upstream.url)
		fx.services = append(fx.services, registration)
	}
	ti.service.manual = true
	return ctx, fx
}

// consentPageWithPicks renders the consent page with the picks its service picker submits.
func consentPageWithPicks(t *testing.T, ctx context.Context, fx serviceChoiceGateway, stateID string, picks url.Values) string {
	t.Helper()
	query := url.Values{"state": {stateID}}
	maps.Copy(query, picks)
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+fx.slug+"/connect?"+query.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", fx.slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.HandleConsent(w, req))
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

// selectedService is the service the page's picker selects for the source member.
func selectedService(t *testing.T, page string) uuid.UUID {
	t.Helper()
	match := regexp.MustCompile(`<option value="([0-9a-f-]{36})" selected`).FindStringSubmatch(page)
	require.Len(t, match, 2, "the picker selects one service")
	return uuid.MustParse(match[1])
}

// approveServiceChoice approves the page the person last loaded and returns the service the new connection records for the member.
func approveServiceChoice(t *testing.T, ctx context.Context, fx serviceChoiceGateway, stateID, csrf, page string) uuid.NullUUID {
	t.Helper()
	endpoint, err := fx.ti.service.LoadResolvedMcpEndpointBySlug(ctx, fx.ti.logger, fx.slug, "mcp")
	require.NoError(t, err)
	inventory := gatewayInventoryFromPage(t, page)
	form := url.Values{"state": {stateID}, "csrf_token": {csrf}, "action": {"approve"}, "tool_inventory_id": {inventory.Attempt}, "tool_filtering": {"off"}, "tool_catalog_mode": {"cached"}, "discovery_mode": {"direct"}}
	request := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.slug+"/connect", strings.NewReader(form.Encode())).WithContext(t.Context())
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsent(response, request, endpoint))
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	redirect, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {redirect.Query().Get("code")}, "client_id": {fx.client.ClientID}, "redirect_uri": {fx.client.RedirectUris[0]}, "code_verifier": {gatewayConsentVerifier}}
	issued := postForm(t, fx.ti, fx.slug, "token", tokenForm)
	require.Equal(t, http.StatusOK, issued.Code, issued.Body.String())
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &tokens))
	claims, err := usersessions.NewSigner("test-jwt-secret").Validate(tokens.AccessToken, urn.NewUserSessionIssuer(fx.issuerID).String())
	require.NoError(t, err)
	session, err := usersessionsrepo.New(fx.ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: fx.issuerID, Jti: claims.ID})
	require.NoError(t, err)
	credentialID := session.ID
	if session.CredentialID.Valid {
		credentialID = session.CredentialID.UUID
	}
	choice, err := usersessionsrepo.New(fx.ti.conn).GetUpstreamClientSelection(ctx, usersessionsrepo.GetUpstreamClientSelectionParams{UserSessionIssuerID: fx.issuerID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credentialID, McpServerID: fx.serverID, OrganizationID: fx.orgID})
	require.NoError(t, err)
	return choice.RemoteSessionClientID
}

// With sign-in enforcement off, a member with two connected services offers a
// choice between them instead of reading as not set up, and the chosen one
// becomes the connection's choice and the next consent's default.
func TestGatewayConsentChoosesBetweenConnectedServices(t *testing.T) {
	t.Parallel()
	ctx, fx := seedServiceChoiceGateway(t, 2)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, fx.ti, fx.issuerID, fx.client, fx.gateway, fx.slug)

	page := consentPageWithPicks(t, ctx, fx, stateID, nil)
	require.Contains(t, page, `name="auth_client.source"`)
	for _, service := range fx.services {
		require.Contains(t, page, `value="`+service.String()+`"`)
	}
	outcomes := gatewayOutcomesFromPage(page)
	require.Len(t, outcomes, 1)
	require.Equal(t, "connected", outcomes[0].Status)

	chosen := fx.services[0]
	if selectedService(t, page) == chosen {
		chosen = fx.services[1]
	}
	page = consentPageWithPicks(t, ctx, fx, stateID, url.Values{"auth_client.source": {chosen.String()}})
	require.Equal(t, chosen, selectedService(t, page))
	require.Equal(t, uuid.NullUUID{UUID: chosen, Valid: true}, approveServiceChoice(t, ctx, fx, stateID, csrf, page))

	next, _ := seedGatewayConsentChallenge(t, ctx, fx.ti, fx.issuerID, fx.client, fx.gateway, fx.slug)
	require.Equal(t, chosen, selectedService(t, consentPageWithPicks(t, ctx, fx, next, nil)), "the last approved service stays the default")
}

// A member with one connected service has nothing to choose.
func TestGatewayConsentOneConnectedServiceShowsNoPicker(t *testing.T) {
	t.Parallel()
	ctx, fx := seedServiceChoiceGateway(t, 1)
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, fx.ti, fx.issuerID, fx.client, fx.gateway, fx.slug)
	page := consentPageWithPicks(t, ctx, fx, stateID, nil)
	require.NotContains(t, page, `name="auth_client.source"`)
	require.Equal(t, uuid.NullUUID{UUID: fx.services[0], Valid: true}, approveServiceChoice(t, ctx, fx, stateID, csrf, page))
}

// A pick naming a service the person has not connected for this server is ignored, and the default is approved instead.
func TestGatewayConsentIgnoresAPickOutsideTheConnectedServices(t *testing.T) {
	t.Parallel()
	ctx, fx := seedServiceChoiceGateway(t, 2)
	auth, _ := contextvalues.GetAuthContext(ctx)
	unconnected := createConsentRemoteClient(t, ctx, fx.ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "unconnected-"+uuid.NewString()[:8], "", []uuid.UUID{fx.issuerID})
	stateID, csrf := seedGatewayConsentChallenge(t, ctx, fx.ti, fx.issuerID, fx.client, fx.gateway, fx.slug)
	fallback := selectedService(t, consentPageWithPicks(t, ctx, fx, stateID, nil))
	for _, forged := range []uuid.UUID{uuid.New(), unconnected} {
		page := consentPageWithPicks(t, ctx, fx, stateID, url.Values{"auth_client.source": {forged.String()}})
		require.NotContains(t, page, `value="`+forged.String()+`"`)
		require.Equal(t, fallback, selectedService(t, page))
	}
	page := consentPageWithPicks(t, ctx, fx, stateID, url.Values{"auth_client.source": {unconnected.String()}})
	require.Equal(t, uuid.NullUUID{UUID: fallback, Valid: true}, approveServiceChoice(t, ctx, fx, stateID, csrf, page))
}

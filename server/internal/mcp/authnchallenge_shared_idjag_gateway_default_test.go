package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// setOrganizationGateway turns the fixture organization's gateway on or off.
func setOrganizationGateway(t *testing.T, ctx context.Context, ti *testInstance, enabled bool) {
	t.Helper()
	authCtx := requireProjectAuthContext(t, ctx)
	features := featuresrepo.New(ti.conn)
	setting := featuresrepo.EnableFeatureParams{OrganizationID: authCtx.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)}
	_, err := features.EnableFeature(ctx, setting)
	require.NoError(t, err)
	if enabled {
		_, err = features.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(setting))
		require.NoError(t, err)
	}
}

func organizationGatewayResource(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	return ti.serverURL.String() + "/mcp/gateway/" + requireProjectAuthContext(t, ctx).OrganizationSlug
}

func claimlessIDJAGForm(t *testing.T, signer idJAGTestAssertion, f sharedIssuerFixture, audience, jti string) url.Values {
	t.Helper()
	return url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamClientID:  {f.client.ClientID},
		oauthwire.ParamAssertion: {signer.signWithoutResource(t, sharedTestUpstreamIssuer, audience, f.client.ClientID, mockidp.MockUserEmail, jti)},
	}
}

// Okta and Codex name no resource at all: the organization gateway on this issuer is the target.
func TestSharedAuthorizationServer_IDJAGWithoutResourceOrClaimDefaultsToOrganizationGateway(t *testing.T) {
	t.Parallel()
	ctx, ti, signer, f := sharedIDJAGFixture(t)
	setOrganizationGateway(t, ctx, ti, true)
	gateway := organizationGatewayResource(t, ctx, ti)

	w := sharedToken(t, ctx, ti, f.issuer.ID.String(), claimlessIDJAGForm(t, signer, f, f.issuerURL, uuid.NewString()))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	resp := decodeSharedTokenResponse(t, w)
	require.Empty(t, resp.RefreshToken)
	require.Equal(t, []any{gateway}, unverifiedClaims(t, resp.AccessToken)["aud"])

	gatewayEndpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, requireProjectAuthContext(t, ctx).OrganizationSlug, "mcp/gateway")
	require.NoError(t, err)
	_, _, _, err = ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), resp.AccessToken, ti.serverURL.String(), gatewayEndpoint)
	require.NoError(t, err)
	for _, slug := range []string{f.slugA, f.slugB} {
		endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
		require.NoError(t, err)
		_, _, _, err = ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), resp.AccessToken, ti.serverURL.String(), endpoint)
		require.Error(t, err)
	}
	listed, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, requireProjectAuthContext(t, ctx).OrganizationSlug, resp.AccessToken, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"structuredContent":{"servers":`)
}

// Without a usable gateway the request is refused before the assertion is spent.
func TestSharedAuthorizationServer_IDJAGWithoutResourceOrClaimNeedsUsableGateway(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{"no gateway", "gateway turned off", "several organization issuers"} {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			ctx, ti, signer, f := sharedIDJAGFixture(t)
			switch tc {
			case "gateway turned off":
				setOrganizationGateway(t, ctx, ti, true)
				setOrganizationGateway(t, ctx, ti, false)
			case "several organization issuers":
				setOrganizationGateway(t, ctx, ti, true)
				createOrganizationIssuer(t, ctx, ti, requireProjectAuthContext(t, ctx).ActiveOrganizationID)
			}
			form := claimlessIDJAGForm(t, signer, f, f.issuerURL, uuid.NewString())
			w := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Equal(t, "invalid_target", decodeSharedTokenResponse(t, w).Error)

			form[oauthwire.ParamResource] = []string{f.resource(ti, f.slugA)}
			w = sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		})
	}
}

// The request's resource and the assertion's claim both take precedence over the gateway.
func TestSharedAuthorizationServer_IDJAGGatewayDefaultYieldsToNamedResource(t *testing.T) {
	t.Parallel()
	ctx, ti, signer, f := sharedIDJAGFixture(t)
	setOrganizationGateway(t, ctx, ti, true)

	parameter := f.resource(ti, f.slugA)
	form := claimlessIDJAGForm(t, signer, f, f.issuerURL, uuid.NewString())
	form[oauthwire.ParamResource] = []string{parameter}
	w := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []any{parameter}, unverifiedClaims(t, decodeSharedTokenResponse(t, w).AccessToken)["aud"])

	claim := f.resource(ti, f.slugB)
	w = sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamClientID:  {f.client.ClientID},
		oauthwire.ParamAssertion: {signer.sign(t, sharedTestUpstreamIssuer, f.issuerURL, claim, f.client.ClientID, mockidp.MockUserEmail, uuid.NewString())},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []any{claim}, unverifiedClaims(t, decodeSharedTokenResponse(t, w).AccessToken)["aud"])

	w = sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamClientID:  {f.client.ClientID},
		oauthwire.ParamAssertion: {signer.sign(t, sharedTestUpstreamIssuer, f.issuerURL, "not-a-resource", f.client.ClientID, mockidp.MockUserEmail, uuid.NewString())},
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "invalid_grant", decodeSharedTokenResponse(t, w).Error)
}

// The gateway default still runs the full assertion checks.
func TestSharedAuthorizationServer_IDJAGGatewayDefaultRejectsWrongAudience(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []string{"gateway audience", "token audience", "foreign issuer audience"} {
		t.Run(mismatch, func(t *testing.T) {
			t.Parallel()
			ctx, ti, signer, f := sharedIDJAGFixture(t)
			setOrganizationGateway(t, ctx, ti, true)
			audience := organizationGatewayResource(t, ctx, ti)
			switch mismatch {
			case "token audience":
				audience = f.issuerURL + "/token"
			case "foreign issuer audience":
				audience = ti.serverURL.String() + "/oauth/usi/" + uuid.NewString()
			}
			w := sharedToken(t, ctx, ti, f.issuer.ID.String(), claimlessIDJAGForm(t, signer, f, audience, uuid.NewString()))
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Equal(t, "invalid_grant", decodeSharedTokenResponse(t, w).Error)
		})
	}
}

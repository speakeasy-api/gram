package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const sharedTestUpstreamIssuer = "https://identity.example.test"

// sharedIDJAGFixture gives one trusted organization issuer two authorized
// MCP resources, so exchanges can prove both issuer reuse and resource isolation.
func sharedIDJAGFixture(t *testing.T) (context.Context, *testInstance, idJAGTestAssertion, sharedIssuerFixture) {
	t.Helper()
	ctx, ti, signer, jwksURL := newIDJAGTestService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, sharedTestUpstreamIssuer, jwksURL)
	setIssuerMode(t, ctx, ti, issuer.ID, "shared", "")
	f := sharedIssuerFixture{
		issuer: issuer, client: createIDJAGClient(t, ctx, ti, issuer.ID),
		slugA:     seedPublicServerOnIssuer(t, ctx, ti, issuer.ID),
		slugB:     seedPublicServerOnIssuer(t, ctx, ti, issuer.ID),
		issuerURL: ti.serverURL.String() + "/oauth/usi/" + issuer.ID.String(),
	}
	seedIDJAGDirectoryUser(t, ctx, ti, authCtx.ActiveOrganizationID)
	for _, slug := range []string{f.slugA, f.slugB} {
		endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
		require.NoError(t, err)
		seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), endpoint.McpServerID.UUID)
	}
	return ctx, ti, signer, f
}

func TestSharedAuthorizationServer_IDJAGBindsEachResource(t *testing.T) {
	t.Parallel()
	ctx, ti, signer, f := sharedIDJAGFixture(t)
	for _, slug := range []string{f.slugA, f.slugB} {
		resource := f.resource(ti, slug)
		form := url.Values{
			oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
			oauthwire.ParamClientID:  {f.client.ClientID},
			oauthwire.ParamAssertion: {signer.sign(t, sharedTestUpstreamIssuer, f.issuerURL, resource, f.client.ClientID, mockidp.MockUserEmail, uuid.NewString())},
			oauthwire.ParamResource:  {resource},
		}
		w := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		resp := decodeSharedTokenResponse(t, w)
		require.Empty(t, resp.RefreshToken)
		claims := unverifiedClaims(t, resp.AccessToken)
		require.Equal(t, f.issuerURL, claims["iss"])
		require.Equal(t, []any{resource}, claims["aud"])
		for _, target := range []string{f.slugA, f.slugB} {
			endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, target, "mcp")
			require.NoError(t, err)
			_, _, _, err = ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), resp.AccessToken, ti.serverURL.String(), endpoint)
			if target == slug {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		}
		jti, ok := claims["jti"].(string)
		require.True(t, ok)
		policy, err := usersessionsrepo.New(ti.conn).GetUserSessionPolicyByJTI(ctx, usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: f.issuer.ID, Jti: jti})
		require.NoError(t, err)
		require.False(t, policy.Refreshable)
		replay := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
		require.Equal(t, "invalid_grant", decodeSharedTokenResponse(t, replay).Error)
	}
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedAuthorizationServerMetadata(w, sharedRequest(ctx, http.MethodGet, f.issuerURL+"/.well-known/oauth-authorization-server", f.issuer.ID.String(), nil)))
	var metadata struct {
		Grants   []string `json:"grant_types_supported"`
		Profiles []string `json:"authorization_grant_profiles_supported"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Contains(t, metadata.Grants, oauthwire.GrantTypeJWTBearer)
	require.Contains(t, metadata.Profiles, oauthwire.GrantProfileIDJAG)
}

func TestSharedAuthorizationServer_IDJAGRejectsWrongAudienceOrSignedResource(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []string{"resource audience", "token audience", "foreign issuer audience", "signed resource"} {
		t.Run(mismatch, func(t *testing.T) {
			t.Parallel()
			ctx, ti, signer, f := sharedIDJAGFixture(t)
			resource := f.resource(ti, f.slugA)
			audience, signedResource := f.issuerURL, resource
			switch mismatch {
			case "resource audience":
				audience = resource
			case "token audience":
				audience += "/token"
			case "foreign issuer audience":
				audience = ti.serverURL.String() + "/oauth/usi/" + uuid.NewString()
			case "signed resource":
				signedResource = f.resource(ti, f.slugB)
			}
			w := sharedToken(t, ctx, ti, f.issuer.ID.String(), url.Values{
				oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamClientID: {f.client.ClientID},
				oauthwire.ParamAssertion: {signer.sign(t, sharedTestUpstreamIssuer, audience, signedResource, f.client.ClientID, mockidp.MockUserEmail, uuid.NewString())},
				oauthwire.ParamResource:  {resource},
			})
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Equal(t, "invalid_grant", decodeSharedTokenResponse(t, w).Error)
		})
	}
}

func TestSharedAuthorizationServer_AssertionResourceRefusalDoesNotConsumeIDJAG(t *testing.T) {
	t.Parallel()
	ctx, ti, signer, f := sharedIDJAGFixture(t)
	foreign := seedSharedIssuer(t, ctx, ti)
	resource := f.resource(ti, f.slugA)
	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamClientID: {f.client.ClientID},
		oauthwire.ParamAssertion: {signer.sign(t, sharedTestUpstreamIssuer, f.issuerURL, resource, f.client.ClientID, mockidp.MockUserEmail, uuid.NewString())},
	}
	for _, tc := range []struct {
		resources []string
		code      string
	}{
		{nil, "invalid_request"}, {[]string{resource, resource}, "invalid_target"},
		{[]string{resource + "/"}, "invalid_target"}, {[]string{"not-a-resource"}, "invalid_target"},
		{[]string{foreign.resource(ti, foreign.slugA)}, "invalid_target"},
		{[]string{ti.serverURL.String() + "/mcp/unknown"}, "invalid_target"},
	} {
		form[oauthwire.ParamResource] = tc.resources
		w := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Equal(t, tc.code, decodeSharedTokenResponse(t, w).Error)
	}
	form[oauthwire.ParamResource] = []string{resource}
	w := sharedToken(t, ctx, ti, f.issuer.ID.String(), form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestSharedAuthorizationServer_WorkloadAcceptsIssuerAndTokenAudience(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"", "/token"} {
		t.Run("audience"+suffix, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			ctx := t.Context()
			issuerURL := f.sharedIssuer(t)
			form := url.Values{
				oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
				oauthwire.ParamAssertion: {f.assertion(t, issuerURL+suffix)}, oauthwire.ParamResource: {f.resource},
			}
			w := sharedToken(t, ctx, f.ti, f.fx.target.UserSessionIssuerID.String(), form)
			f.requireWorkloadSession(t, w, issuerURL)
			claims := unverifiedClaims(t, decodeSharedTokenResponse(t, w).AccessToken)
			require.Equal(t, []any{f.resource}, claims["aud"])
			jti, ok := claims["jti"].(string)
			require.True(t, ok)
			policy, err := usersessionsrepo.New(f.ti.conn).GetUserSessionPolicyByJTI(ctx, usersessionsrepo.GetUserSessionPolicyByJTIParams{UserSessionIssuerID: f.fx.target.UserSessionIssuerID, Jti: jti})
			require.NoError(t, err)
			require.False(t, policy.Refreshable)
			requireWorkloadGrantRefused(t, sharedToken(t, ctx, f.ti, f.fx.target.UserSessionIssuerID.String(), form))
			// The legacy per-endpoint route remains usable for this same issuer.
			f.requireWorkloadSession(t, f.exchange(t, f.assertion(t, f.advertisedIssuer+"/token"), f.resource), f.advertisedIssuer)
		})
	}
}

func TestSharedAuthorizationServer_WorkloadRejectsForeignResourceBeforeReplayReservation(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	ctx := t.Context()
	issuerURL := f.sharedIssuer(t)
	fixtureCtx := contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ProjectID: &f.fx.target.ProjectID, ActiveOrganizationID: f.fx.orgID})
	foreign := seedSharedIssuer(t, fixtureCtx, f.ti)
	foreignResource := foreign.resource(f.ti, foreign.slugA)
	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {f.assertion(t, issuerURL)}, oauthwire.ParamResource: {foreignResource},
	}
	w := sharedToken(t, ctx, f.ti, f.fx.target.UserSessionIssuerID.String(), form)
	require.Equal(t, "invalid_target", decodeSharedTokenResponse(t, w).Error)
	require.Empty(t, f.workloadSessions(t))
	form[oauthwire.ParamResource] = []string{f.resource}
	f.requireWorkloadSession(t, sharedToken(t, ctx, f.ti, f.fx.target.UserSessionIssuerID.String(), form), issuerURL)
}

// sharedIssuer enables shared routing for the workload fixture without
// changing its trusted workload issuer or agent admission policy.
func (f workloadGrantFixture) sharedIssuer(t *testing.T) string {
	t.Helper()
	rows, err := testrepo.New(f.ti.conn).SetUserSessionIssuerAuthorizationServerModeFixture(t.Context(), testrepo.SetUserSessionIssuerAuthorizationServerModeFixtureParams{
		AuthorizationServerMode: "shared", IssuerID: f.fx.target.UserSessionIssuerID, OrganizationID: f.fx.orgID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	return f.ti.serverURL.String() + "/oauth/usi/" + f.fx.target.UserSessionIssuerID.String()
}

func TestSharedAuthorizationServer_WorkloadRejectsInvalidAudience(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []string{"resource", "another issuer", "multiple"} {
		t.Run(mismatch, func(t *testing.T) {
			t.Parallel()
			f := newWorkloadGrantFixture(t)
			issuerURL := f.sharedIssuer(t)
			claims := oauthtest.WorkloadClaims(f.issuer, f.subject, issuerURL)
			switch mismatch {
			case "resource":
				claims.Audience = jwt.Audience{f.resource}
			case "another issuer":
				claims.Audience = jwt.Audience{f.ti.serverURL.String() + "/oauth/usi/" + uuid.NewString()}
			case "multiple":
				claims.Audience = jwt.Audience{issuerURL, issuerURL + "/token"}
			}
			form := url.Values{
				oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamResource: {f.resource},
				oauthwire.ParamAssertion: {oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims)},
			}
			requireWorkloadGrantRefused(t, sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), form))
			require.Empty(t, f.workloadSessions(t))
		})
	}
}

func TestSharedAuthorizationServer_WorkloadRetainsAdmissionAndClientDispatch(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)
	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer}, oauthwire.ParamResource: {f.resource},
		oauthwire.ParamAssertion: {f.assertion(t, issuerURL)},
	}
	f.ti.features.SetFlag(feature.FlagAgentManagement, f.fx.orgID, false)
	requireWorkloadGrantRefused(t, sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), form))
	f.ti.features.SetFlag(feature.FlagAgentManagement, f.fx.orgID, true)
	// Presenting client credentials must not fall back to workload admission.
	form[oauthwire.ParamClientID] = []string{"unknown-client"}
	w := sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), form)
	require.Equal(t, "invalid_client", decodeSharedTokenResponse(t, w).Error)
	delete(form, oauthwire.ParamClientID)
	f.requireWorkloadSession(t, sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), form), issuerURL)
}

func TestSharedAuthorizationServer_MetadataRequiresPublicWorkloadResource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	f := seedSharedIssuer(t, ctx, ti)
	var serverIDs []uuid.UUID
	for _, slug := range []string{f.slugA, f.slugB} {
		endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
		require.NoError(t, err)
		serverIDs = append(serverIDs, endpoint.McpServerID.UUID)
	}
	projectID := *requireProjectAuthContext(t, ctx).ProjectID
	for _, serverID := range serverIDs {
		rows, err := testrepo.New(ti.conn).SetMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMCPServerNetworkAccessModeFixtureParams{
			ID: serverID, ProjectID: projectID, NetworkAccessMode: conv.ToPGText("private_only"),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)
	}
	for _, tc := range []struct {
		mode       string
		advertised bool
	}{
		{"private_only", false}, {"dual", true},
	} {
		rows, err := testrepo.New(ti.conn).SetMCPServerNetworkAccessModeFixture(ctx, testrepo.SetMCPServerNetworkAccessModeFixtureParams{
			ID: serverIDs[0], ProjectID: projectID, NetworkAccessMode: conv.ToPGText(tc.mode),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleSharedAuthorizationServerMetadata(w, sharedRequest(ctx, http.MethodGet, f.issuerURL+"/.well-known/oauth-authorization-server", f.issuer.ID.String(), nil)))
		var metadata authorizationServerGrantMetadata
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
		if tc.advertised {
			require.Contains(t, metadata.GrantTypes, oauthwire.GrantTypeJWTBearer)
		} else {
			require.NotContains(t, metadata.GrantTypes, oauthwire.GrantTypeJWTBearer)
		}
		require.Empty(t, metadata.GrantProfiles)
	}
}

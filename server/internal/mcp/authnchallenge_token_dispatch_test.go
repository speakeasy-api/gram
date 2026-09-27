package mcp_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

const missingClientIDResponse = `{"error":"invalid_client","error_description":"client_id is required"}`

// The client-bound grants demand a client before looking at anything
// else in the request.
func TestHandleToken_ClientBoundGrantsWithoutClientAreInvalidClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	forms := map[string]url.Values{
		oauthwire.GrantTypeAuthorizationCode: {
			"grant_type":    {oauthwire.GrantTypeAuthorizationCode},
			"code":          {"irrelevant"},
			"code_verifier": {"irrelevant"},
			"redirect_uri":  {"http://127.0.0.1:51423/callback"},
		},
		oauthwire.GrantTypeRefreshToken: {
			"grant_type":    {oauthwire.GrantTypeRefreshToken},
			"refresh_token": {"irrelevant"},
		},
	}
	for grantType, form := range forms {
		w := postForm(t, ti, toolset.McpSlug.String, "token", form)
		require.Equal(t, http.StatusUnauthorized, w.Code, grantType)
		require.JSONEq(t, missingClientIDResponse, w.Body.String(), grantType)
	}
}

// A JWT bearer request carrying no client authentication reaches the
// clientless workload grant rather than the ID-JAG exchange. This endpoint's
// organization is outside the agent authorization rollout, so the grant
// refuses it.
func TestHandleToken_JWTBearerWithoutClientIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	w := postForm(t, ti, toolset.McpSlug.String, "token", url.Values{
		"grant_type": {oauthwire.GrantTypeJWTBearer},
		"assertion":  {"header.payload.signature"},
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_grant")
}

// A JWT bearer request that presents a client_id is an ID-JAG exchange and
// authenticates that client before the assertion is considered.
func TestHandleToken_JWTBearerWithClientAuthenticatesClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	w := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, "client_"+uuid.NewString(), "header.payload.signature")
	requireInvalidClient(t, w)
}

// grant_type is dispatched before client authentication, so an unsupported
// grant is reported as such whatever client authentication accompanies it.
func TestHandleToken_UnsupportedGrantPrecedesClientAuthentication(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, publicClient := seedPrivateToolsetWithIssuer(t, ctx, ti)

	forms := map[string]url.Values{
		"no client":           {"grant_type": {"urn:example:grant-type:unknown"}},
		"no grant_type":       {},
		"client_credentials":  {"grant_type": {"client_credentials"}},
		"unregistered client": {"grant_type": {"urn:example:grant-type:unknown"}, "client_id": {"client_" + uuid.NewString()}},
		"wrong secret":        {"grant_type": {"password"}, "client_id": {publicClient.ClientID}, "client_secret": {"wrong"}},
	}
	for name, form := range forms {
		w := postForm(t, ti, toolset.McpSlug.String, "token", form)
		require.Equal(t, http.StatusBadRequest, w.Code, name)
		require.JSONEq(t, `{"error":"unsupported_grant_type","error_description":"unsupported grant_type"}`, w.Body.String(), name)
	}
}

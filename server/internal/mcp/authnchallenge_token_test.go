package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestHandleTokenCode_AgentAuthorizationUsesIsolatedGrantNamespace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Token subject agent")
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	code := "agent-v1." + uuid.NewString()
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
		Code:                        code,
		UserSessionIssuerID:         fx.target.UserSessionIssuerID,
		UserSessionClientID:         fx.client.ID,
		ClientID:                    fx.client.ClientID,
		RedirectURI:                 fx.client.RedirectUris[0],
		CodeChallenge:               base64.RawURLEncoding.EncodeToString(sum[:]),
		CodeChallengeMethod:         "S256",
		Subject:                     urn.NewUserSubject(fx.userID),
		AgentAuthorization:          &mcp.AgentAuthorizationResult{AgentID: agent.ID, AuthorizerUserID: fx.userID, Target: fx.target},
		DesiredSessionDurationHours: 0,
		CreatedAt:                   time.Now(),
	}))

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", fx.client.RedirectUris[0])
	form.Set("client_id", fx.client.ClientID)
	form.Set("code_verifier", verifier)
	w := postForm(t, ti, fx.toolset.McpSlug.String, "token", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var response struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	claims, err := sessiontokens.NewSigner("test-jwt-secret").Validate(response.AccessToken, urn.NewToolset(fx.toolset.ID).String())
	require.NoError(t, err)
	require.Equal(t, urn.NewAgentSubject(agent.ID).String(), claims.Subject)
	require.Equal(t, fx.client.ClientID, claims.ClientID)

	session, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
		Jti:                 claims.ID,
	})
	require.NoError(t, err)
	require.Equal(t, urn.NewAgentSubject(agent.ID).String(), session.SubjectUrn.String())
	require.True(t, session.AuthorizerUserID.Valid)
	require.Equal(t, fx.userID, session.AuthorizerUserID.String)
	require.True(t, session.DelegatedGrantsVersion.Valid)
	require.Equal(t, int32(authz.CurrentDelegatedPolicyVersion), session.DelegatedGrantsVersion.Int32)
	policy, err := authz.DecodeDelegatedPolicy(authz.DelegatedPolicyVersion(session.DelegatedGrantsVersion.Int32), session.DelegatedGrants)
	require.NoError(t, err)
	require.Len(t, policy.Requested, 1)
	require.Equal(t, authz.ScopeMCPConnect, policy.Requested[0].Scope)
	require.Equal(t, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
		authz.SelectorKeyResourceID:   fx.target.MCPResourceID.String(),
		authz.SelectorKeyProjectID:    fx.target.ProjectID.String(),
	}, policy.Requested[0].Selector)
	require.True(t, authz.GrantsSatisfy(policy.RuntimeGrants(), authz.MCPCheck(authz.ScopeMCPConnect, fx.target.MCPResourceID.String(), fx.target.ProjectID.String())))

	endpoint := &mcp.ResolvedMcpEndpoint{
		AudienceURN:         urn.NewToolset(fx.toolset.ID).String(),
		OrganizationID:      fx.orgID,
		ProjectID:           fx.target.ProjectID,
		RouteBase:           "mcp",
		Slug:                fx.toolset.McpSlug.String,
		ToolsetID:           uuid.NullUUID{UUID: fx.toolset.ID, Valid: true},
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
	}
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), response.AccessToken, ti.serverURL.String(), endpoint)
	require.NoError(t, err)
	refreshed := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, response.RefreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	var refreshedResponse struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(refreshed.body), &refreshedResponse))
	refreshedClaims, err := sessiontokens.NewSigner("test-jwt-secret").Validate(refreshedResponse.AccessToken, urn.NewToolset(fx.toolset.ID).String())
	require.NoError(t, err)
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), refreshedResponse.AccessToken, ti.serverURL.String(), endpoint)
	require.NoError(t, err)
	refreshedSession, err := usersessionsrepo.New(ti.conn).GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
		Jti:                 refreshedClaims.ID,
	})
	require.NoError(t, err)
	require.Equal(t, session.SubjectUrn.String(), refreshedSession.SubjectUrn.String())
	require.Equal(t, session.AuthorizerUserID, refreshedSession.AuthorizerUserID)
	require.JSONEq(t, string(session.DelegatedGrants), string(refreshedSession.DelegatedGrants))
	require.Equal(t, session.DelegatedGrantsVersion, refreshedSession.DelegatedGrantsVersion)
}

// TestHandleTokenCode_ResourceIndicator covers the RFC 8707 check on the
// authorization_code grant: a resource naming another server is invalid_target,
// the advertised identifier is accepted, and an absent parameter stays legal
// for clients predating MCP 2026-07-28.
func TestHandleTokenCode_ResourceIndicator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedResource, _ := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)

	verifier := "verifier-" + uuid.NewString()
	sum := sha256.Sum256([]byte(verifier))
	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	redirectURI := "http://127.0.0.1:51423/callback"

	// Codes are single-use, so each redemption gets a freshly stored grant.
	redeem := func(resource string) *httptest.ResponseRecorder {
		code := "code-" + uuid.NewString()
		require.NoError(t, grantCache.Store(ctx, mcp.UserSessionGrant{
			Code:                        code,
			FlowID:                      "",
			UserSessionIssuerID:         toolset.UserSessionIssuerID.UUID,
			UserSessionClientID:         client.ID,
			ClientID:                    client.ClientID,
			RedirectURI:                 redirectURI,
			CodeChallenge:               base64.RawURLEncoding.EncodeToString(sum[:]),
			CodeChallengeMethod:         "S256",
			Subject:                     urn.NewUserSubject("code-user-" + uuid.NewString()),
			DesiredSessionDurationHours: 0,
			ToolSelection:               nil,
			CreatedAt:                   time.Now(),
		}))

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", code)
		form.Set("redirect_uri", redirectURI)
		form.Set("client_id", client.ClientID)
		form.Set("code_verifier", verifier)
		if resource != "" {
			form.Set("resource", resource)
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", mcpSlug)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		require.NoError(t, ti.service.HandleToken(w, req))
		return w
	}

	w := redeem("https://someone-else.example.com/mcp/" + mcpSlug)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_target")

	// Same server, wrong route base: /mcp and /x/mcp mint different audiences,
	// so they are never interchangeable resource identifiers.
	w = redeem(strings.Replace(advertisedResource, "/mcp/", "/x/mcp/", 1))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_target")

	w = redeem(advertisedResource + "/")
	require.Equal(t, http.StatusBadRequest, w.Code, "a trailing slash is not the advertised identifier")

	w = redeem(advertisedResource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")

	w = redeem("")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}

// TestHandleTokenRefresh_ResourceIndicator asserts the same check on the
// refresh grant, which MCP 2026-07-28 clients also send `resource` on.
func TestHandleTokenRefresh_ResourceIndicator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	mcpSlug := toolset.McpSlug.String

	advertisedResource, _ := fetchAdvertisedIssuer(t, ctx, ti, mcpSlug)

	w := postRefreshGrant(t, ti, mcpSlug, client.ClientID,
		seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, nil),
		"https://someone-else.example.com/mcp/"+mcpSlug)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_target")

	w = postRefreshGrant(t, ti, mcpSlug, client.ClientID,
		seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, nil),
		advertisedResource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")

	w = postRefreshGrant(t, ti, mcpSlug, client.ClientID,
		seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, nil), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "access_token")
}

package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// seedGatewayConsentChallenge stores a consent challenge for a gateway
// endpoint and returns the state id and CSRF token.
func seedGatewayConsentChallenge(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, client usersessions_repo.UserSessionClient, gatewayID uuid.UUID, endpointSlug string) (string, string) {
	t.Helper()
	subject := urn.NewUserSubject(mockidp.MockUserID)
	stateID := uuid.NewString()
	csrfToken := "csrf-" + uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: issuerID,
		Endpoint: mcp.EndpointRef{
			RouteBase:       "mcp",
			McpSlug:         endpointSlug,
			MetaMcpServerID: uuid.NullUUID{UUID: gatewayID, Valid: true},
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
		CSRFToken:           csrfToken,
		Subject:             &subject,
		CreatedAt:           time.Now(),
	}))
	return stateID, csrfToken
}

func TestGatewayConsentPicksToolsPerSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-consent-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
	issues := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Issue tracker", 1, mcpservers.VisibilityPublic, "create_issue", "list_issues")
	// Hosted members share the project's latest deployment, so the second
	// source is a remote server.
	docsSlug := "docs-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Docs", docsSlug, 2, newRecordingUpstream(t, "search_docs").url)
	stateID, csrfToken := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)

	page := consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.Contains(t, page, "consent-tools-root")

	attempt := uuid.NewString()
	w := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrfToken, attempt, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	var listed struct {
		Result struct {
			Tools []struct {
				Name string         `json:"name"`
				Meta map[string]any `json:"_meta"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed), w.Body.String())
	sources := map[string]any{}
	for _, tool := range listed.Result.Tools {
		sources[tool.Name] = tool.Meta["gram.dev/source"]
	}
	require.Equal(t, map[string]any{
		issues.slug + "--create_issue": "Issue tracker",
		issues.slug + "--list_issues":  "Issue tracker",
		docsSlug + "--search_docs":     "Docs",
	}, sources)

	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", csrfToken)
	form.Set("action", "approve")
	form.Set("tool_inventory_id", attempt)
	form.Set("tool_filtering", "on")
	form.Add("tools", issues.slug+"--create_issue")
	form.Add("tools", docsSlug+"--search_docs")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approve := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(approve, req.WithContext(ctx), endpoint))
	require.Equal(t, http.StatusSeeOther, approve.Code, approve.Body.String())

	location, err := url.Parse(approve.Header().Get("Location"))
	require.NoError(t, err)
	code := location.Query().Get("code")
	require.True(t, strings.HasPrefix(code, "gateway-v1."), "a gateway selection rides the versioned grant")

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grantCache.Get(ctx, "gatewayUserSessionGrant:"+issuer.ID.String()+":"+code)
	require.NoError(t, err)
	require.Nil(t, grant.ToolSelection)
	require.NotNil(t, grant.GatewayPolicy)
	require.Nil(t, grant.GatewayPolicy.Gateway.DiscoveryMode, "the gateway default still applies")
	// The token exchange persists the policy as JSON; parse it back the same way.
	raw, err := json.Marshal(grant.GatewayPolicy)
	require.NoError(t, err)
	policy, err := toolfilter.ParseSessionPolicy(raw)
	require.NoError(t, err)
	selection := policy.Selection
	require.NotNil(t, selection)
	require.Equal(t, "meta_mcp_server:"+gateway.ID.String(), selection.Resource)
	require.True(t, selection.AllowsName(issues.slug+"--create_issue"))
	require.True(t, selection.AllowsName(docsSlug+"--search_docs"))
	require.False(t, selection.AllowsName(issues.slug+"--list_issues"))
}

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

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// setOrganizationGatewayMode stores the organization gateway's default
// discovery mode for the test context's organization.
func setOrganizationGatewayMode(t *testing.T, ctx context.Context, ti *testInstance, mode metamcp.DiscoveryMode) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := orgrepo.New(ti.conn).SetOrganizationGatewayDiscoveryMode(ctx, orgrepo.SetOrganizationGatewayDiscoveryModeParams{
		GatewayDiscoveryMode: conv.ToPGText(string(mode)),
		ID:                   authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
}

// seedOrganizationGatewayHostedMember adds a public hosted member with one
// tool, alpha, that every organization gateway caller reaches.
func seedOrganizationGatewayHostedMember(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	decoy := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	return seedHostedMetaMember(t, ctx, ti, decoy.ID, "Hosted tools", 3, mcpservers.VisibilityPublic, "alpha").slug
}

func TestServeOrganizationGateway_ServesStoredDefaultDiscoveryMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	hosted := seedOrganizationGatewayHostedMember(t, ctx, ti)
	setOrganizationGatewayMode(t, ctx, ti, metamcp.DiscoveryModeDirect)
	listTools := makeMetaRPCBody(t, "tools/list", map[string]any{})

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listTools)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"`+hosted+`--alpha"`, "a connection without a choice follows the organization's default")
	require.NotContains(t, w.Body.String(), `"name":"list_servers"`)

	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeProgressive)
	w, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, listTools)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"list_servers"`, "a connection's own choice wins over the default")
	require.NotContains(t, w.Body.String(), hosted+"--alpha")
}

func TestServeOrganizationGateway_CodeModeDefaultFallsBackToProgressive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	setOrganizationGatewayMode(t, ctx, ti, metamcp.DiscoveryModeCode)
	listTools := makeMetaRPCBody(t, "tools/list", map[string]any{})
	bearer := mintOrganizationGatewayBearer(t, ti, fx)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, listTools)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"list_servers"`, "a Code Mode default serves Progressive while the runtime is unavailable")
	require.NotContains(t, w.Body.String(), `"name":"execute"`)

	enableCodeMode(t, ctx, ti)
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{})
	w, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, listTools)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"execute"`, "signed-in callers get the Code Mode default once it is available")
	require.NotContains(t, w.Body.String(), `"name":"list_servers"`)
}

// organizationGatewayConsent is a pending consent for an organization
// gateway connection and its rendered page.
type organizationGatewayConsent struct {
	endpoint  *mcp.ResolvedMcpEndpoint
	stateID   string
	csrfToken string
	page      string
}

func renderOrganizationGatewayConsent(t *testing.T, ctx context.Context, ti *testInstance, fx organizationGatewayFixture) organizationGatewayConsent {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	client, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID:     fx.issuerID,
		ClientID:                "org-default-" + uuid.NewString()[:8],
		ClientName:              "org default",
		RedirectUris:            []string{"http://localhost:3000/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, testenv.NewLogger(t), fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	pkce := sha256.Sum256([]byte(gatewayConsentVerifier))
	consent := organizationGatewayConsent{endpoint: endpoint, stateID: uuid.NewString(), csrfToken: "csrf-" + uuid.NewString(), page: ""}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  consent.stateID,
		UserSessionIssuerID: fx.issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:         endpoint.Slug,
			RouteBase:       endpoint.RouteBase,
			MetaMcpServerID: endpoint.MetaMcpServerID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       base64.RawURLEncoding.EncodeToString(pkce[:]),
		CodeChallengeMethod: "S256",
		CSRFToken:           consent.csrfToken,
		Subject:             &fx.subject,
		CreatedAt:           time.Now(),
	}))

	// The browser request carries identity, while admission loads persisted grants.
	caller := contextvalues.SetAuthContext(t.Context(), authCtx)
	page := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(page, httptest.NewRequest(http.MethodGet, "/mcp/gateway/"+fx.organizationSlug+"/connect?state="+consent.stateID, nil).WithContext(caller), endpoint))
	require.Equal(t, http.StatusOK, page.Code)
	consent.page = page.Body.String()
	return consent
}

func TestServeConsent_OrganizationGatewayLabelsStoredDefault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		mode  metamcp.DiscoveryMode
		label string
	}{
		{name: "unset", mode: "", label: "Gateway default (Progressive)"},
		{name: "direct", mode: metamcp.DiscoveryModeDirect, label: "Gateway default (Direct)"},
		// The runtime is unavailable in tests, so a Code Mode default is
		// labelled with the mode it serves.
		{name: "code mode unavailable", mode: metamcp.DiscoveryModeCode, label: "Gateway default (Progressive)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			fx := seedOrganizationGateway(t, ctx, ti)
			if tc.mode != "" {
				setOrganizationGatewayMode(t, ctx, ti, tc.mode)
			}

			consent := renderOrganizationGatewayConsent(t, ctx, ti, fx)

			require.Contains(t, consent.page, tc.label)
		})
	}
}

// TestServeConsent_OrganizationGatewayDefaultRecordsStoredMode pins that
// approving with "Gateway default" records the organization's current
// default, so a later default change reaches the connection only when it
// reconnects.
func TestServeConsent_OrganizationGatewayDefaultRecordsStoredMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	seedOrganizationGatewayHostedMember(t, ctx, ti)
	setOrganizationGatewayMode(t, ctx, ti, metamcp.DiscoveryModeDirect)
	consent := renderOrganizationGatewayConsent(t, ctx, ti, fx)
	inventory := gatewayInventoryFromPage(t, consent.page)

	form := url.Values{}
	form.Set("state", consent.stateID)
	form.Set("csrf_token", consent.csrfToken)
	form.Set("action", "approve")
	form.Set("tool_inventory_id", inventory.Attempt)
	form.Set("discovery_mode", "")
	form.Set("tool_catalog_mode", string(toolfilter.CatalogModeCached))
	form.Set("tool_filtering", "off")
	req := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+fx.organizationSlug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approve := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeConsent(approve, req.WithContext(ctx), consent.endpoint))
	require.Equal(t, http.StatusSeeOther, approve.Code, approve.Body.String())
	location, err := url.Parse(approve.Header().Get("Location"))
	require.NoError(t, err)

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grantCache.Get(ctx, "gatewayCatalogGrant:"+fx.issuerID.String()+":"+location.Query().Get("code"))
	require.NoError(t, err)
	require.NotNil(t, grant.GatewayPolicy)
	raw, err := json.Marshal(grant.GatewayPolicy)
	require.NoError(t, err)
	policy, err := toolfilter.ParseSessionPolicy(raw)
	require.NoError(t, err)
	require.NotNil(t, policy.Gateway)
	require.NotNil(t, policy.Gateway.DiscoveryMode)
	require.Equal(t, metamcp.DiscoveryModeDirect, *policy.Gateway.DiscoveryMode)
}

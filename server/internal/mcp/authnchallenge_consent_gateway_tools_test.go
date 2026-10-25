package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// gatewayConsentVerifier drives real authorization-code exchange in gateway tests.
const gatewayConsentVerifier = "gateway-consent-verifier-for-catalog-tests"

// seedGatewayConsentChallenge stores a consent challenge for a gateway
// endpoint and returns the state id and CSRF token.
func seedGatewayConsentChallenge(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, client usersessions_repo.UserSessionClient, gatewayID uuid.UUID, endpointSlug string) (string, string) {
	t.Helper()
	subject := urn.NewUserSubject(mockidp.MockUserID)
	challenge := sha256.Sum256([]byte(gatewayConsentVerifier))
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
		ClientID:               client.ClientID,
		RedirectURI:            client.RedirectUris[0],
		State:                  "client-state",
		CodeChallenge:          base64.RawURLEncoding.EncodeToString(challenge[:]),
		CodeChallengeMethod:    "S256",
		CSRFToken:              csrfToken,
		Subject:                &subject,
		AuthorizerUserID:       subject.ID,
		AuthorizerImpersonated: new(false),
		CreatedAt:              time.Now(),
	}))
	return stateID, csrfToken
}

type gatewayConsentOutcome struct {
	Slug   string
	Status string
	Label  string
}

type gatewayConsentInventory struct {
	Attempt string `json:"attempt"`
	Tools   []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	} `json:"tools"`
	MemberOutcomes []gatewayConsentOutcome `json:"-"`
}

func gatewayInventoryFromPage(t *testing.T, page string) gatewayConsentInventory {
	t.Helper()
	match := regexp.MustCompile(`data-gateway-inventory="([^"]+)"`).FindStringSubmatch(page)
	require.Len(t, match, 2, page)
	var inventory gatewayConsentInventory
	require.NoError(t, json.Unmarshal([]byte(html.UnescapeString(match[1])), &inventory))
	require.NotEmpty(t, inventory.Attempt)
	inventory.MemberOutcomes = gatewayOutcomesFromPage(page)
	require.NotContains(t, page, "Checking connection…")
	require.Contains(t, page, "enable JavaScript and reload")
	return inventory
}

func gatewayOutcomesFromPage(page string) []gatewayConsentOutcome {
	var outcomes []gatewayConsentOutcome
	rows := regexp.MustCompile(`(?s)data-gateway-server="([^"]+)".*?data-gateway-server-status="([^"]+)"\s*>([^<]+)</span\s*>`).FindAllStringSubmatch(page, -1)
	for _, row := range rows {
		outcomes = append(outcomes, gatewayConsentOutcome{Slug: row[1], Status: row[2], Label: strings.TrimSpace(html.UnescapeString(row[3]))})
	}
	return outcomes
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

	inventory := gatewayInventoryFromPage(t, page)
	attempt := inventory.Attempt
	sources := map[string]any{}
	for _, tool := range inventory.Tools {
		sources[tool.Name] = tool.Source
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
	require.True(t, strings.HasPrefix(code, "gateway-v2."), "old servers must not redeem catalogue grants")

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	_, err = grantCache.Get(ctx, "userSessionGrant:"+issuer.ID.String()+":"+code)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss, "a gateway-v1 reader cannot find this grant")
	grant, err := grantCache.Get(ctx, "gatewayCatalogGrant:"+issuer.ID.String()+":"+code)
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
	require.NotNil(t, policy.Catalog)
}

func TestGatewayCatalogCodeRejectsPolicyDowngrades(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		prefix  string
		policy  bool
		catalog bool
	}{
		{name: "catalogue in legacy code", prefix: "gateway-v1.", policy: true, catalog: true},
		{name: "catalogue code without catalogue", prefix: "gateway-v2.", policy: true, catalog: false},
		{name: "catalogue code without policy", prefix: "gateway-v2.", policy: false, catalog: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			slug := "catalogue-code-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
			var policy *toolfilter.SessionPolicy
			if tc.policy {
				policy = &toolfilter.SessionPolicy{Resource: "meta_mcp_server:" + gateway.ID.String()}
				if tc.catalog {
					policy.Catalog = &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeCached, AllTools: true}
				}
			}
			challenge := sha256.Sum256([]byte(gatewayConsentVerifier))
			code := tc.prefix + uuid.NewString()
			grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
			require.NoError(t, grants.Store(ctx, mcp.UserSessionGrant{
				Code: code, UserSessionIssuerID: issuer.ID, UserSessionClientID: client.ID,
				ClientID: client.ClientID, RedirectURI: client.RedirectUris[0],
				CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256",
				Subject: urn.NewUserSubject(mockidp.MockUserID), GatewayPolicy: policy, CreatedAt: time.Now(),
			}))
			response := postForm(t, ti, slug, "token", codeGrantForm(client, code, gatewayConsentVerifier))
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "invalid_grant")
		})
	}
}

// TestGatewayToolSelectionSurvivesRefresh pins renewal: refreshing a gateway
// session carries the approved selection forward unchanged, even after the
// gateway's inventory changes, rather than re-deriving or dropping it.
func TestGatewayToolSelectionSurvivesRefresh(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-refresh-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Issue tracker", 1, mcpservers.VisibilityPublic, "create_issue", "list_issues")

	resource := "meta_mcp_server:" + gateway.ID.String()
	selection, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: member.slug + "--create_issue", Mode: nil, Tools: nil},
	})
	require.NoError(t, err)
	original, err := json.Marshal(&toolfilter.SessionPolicy{Resource: resource, Selection: selection, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: nil}, Catalog: &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeCached, AllTools: false}})
	require.NoError(t, err)
	refreshToken := seedUserSessionWithSelection(t, ctx, ti, issuer.ID, client.ID, original)

	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Added later", "added-"+uuid.NewString()[:8], 2, newRecordingUpstream(t, "new_tool").url)

	for range 2 {
		w := postRefreshGrant(t, ti, slug, client.ClientID, refreshToken, "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var issued struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &issued))
		claims, err := usersessions.NewSigner("test-jwt-secret").Validate(issued.AccessToken, urn.NewUserSessionIssuer(issuer.ID).String())
		require.NoError(t, err)
		row, err := usersessions_repo.New(ti.conn).GetUserSessionByJTI(ctx, usersessions_repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
		require.NoError(t, err)
		require.JSONEq(t, string(original), string(row.ToolSelection))
		require.False(t, row.ResourceIdentity.Valid, "a session that recorded no resource identity keeps none through refresh")
		refreshToken = issued.RefreshToken
	}
}

// TestGatewayConsentListsConnectedSourcesWhenOneProviderIsMissing pins
// partial resolution at consent: a provider the subject has not connected
// makes only its own member unavailable, while connected members still offer
// their tools.
func TestGatewayConsentListsConnectedSourcesWhenOneProviderIsMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-partial-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, issuer.ID)

	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(deny.Close)
	connected := newRecordingUpstream(t, "search_docs")
	trackerSlug := "tracker-" + uuid.NewString()[:8]
	trackerID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Tracker", trackerSlug, 1, deny.URL)
	docsSlug := "docs-" + uuid.NewString()[:8]
	docsID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Docs", docsSlug, 2, connected.url)

	// Each member signs in through its own provider; only Docs's is connected.
	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "gw-partial-a-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "gw-partial-b-"+uuid.NewString()[:8], "", []uuid.UUID{issuer.ID})
	associations := upstreamclientsrepo.New(ti.conn)
	for member, client := range map[uuid.UUID]uuid.UUID{trackerID: clientA, docsID: clientB} {
		_, err := associations.AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: orgID, ProjectID: projectID, McpServerID: member, RemoteSessionClientID: client})
		require.NoError(t, err)
	}
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuer.ID, clientB, urn.NewUserSubject(mockidp.MockUserID), "token-docs", connected.url)

	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	inventory := gatewayInventoryFromPage(t, consentGetPageWithContext(t, ctx, ti, slug, stateID))
	names := []string{}
	for _, tool := range inventory.Tools {
		names = append(names, tool.Name)
	}
	require.Equal(t, []string{docsSlug + "--search_docs"}, names)
	outcomes := map[string]string{}
	for _, outcome := range inventory.MemberOutcomes {
		outcomes[outcome.Slug] = outcome.Status
	}
	require.Equal(t, "connected", outcomes[docsSlug])
	require.Equal(t, "not_connected", outcomes[trackerSlug])
	require.Len(t, outcomes, 2)
	listed := false
	for _, request := range connected.journal() {
		if request.rpcMethod == "tools/list" {
			listed = true
			require.Equal(t, "Bearer token-docs", request.authorization, "Docs is discovered with its connected provider's credential")
		}
	}
	require.True(t, listed, "Docs was discovered")
}

func TestGatewaySessionRecordsGatewayIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-identity-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
	sharedSlug := "gateway-identity-shared-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, sharedSlug, issuer.ID)

	challenge := sha256.Sum256([]byte(gatewayConsentVerifier))
	code := uuid.NewString()
	grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	require.NoError(t, grants.Store(ctx, mcp.UserSessionGrant{
		Code: code, UserSessionIssuerID: issuer.ID, UserSessionClientID: client.ID,
		ClientID: client.ClientID, RedirectURI: client.RedirectUris[0],
		CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256",
		Subject: urn.NewUserSubject(mockidp.MockUserID), CreatedAt: time.Now(),
	}))

	recorded := func(w *httptest.ResponseRecorder) (pgtype.Text, string) {
		t.Helper()
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var issued struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &issued))
		claims, err := usersessions.NewSigner("test-jwt-secret").Validate(issued.AccessToken, urn.NewUserSessionIssuer(issuer.ID).String())
		require.NoError(t, err)
		row, err := usersessions_repo.New(ti.conn).GetUserSessionByJTI(ctx, usersessions_repo.GetUserSessionByJTIParams{UserSessionIssuerID: issuer.ID, Jti: claims.ID})
		require.NoError(t, err)
		return row.ResourceIdentity, issued.RefreshToken
	}

	want := pgtype.Text{String: "meta_mcp_server:" + gateway.ID.String(), Valid: true}
	identity, refreshToken := recorded(postForm(t, ti, slug, "token", codeGrantForm(client, code, gatewayConsentVerifier)))
	require.Equal(t, want, identity)

	identity, refreshToken = recorded(postRefreshGrant(t, ti, slug, client.ClientID, refreshToken, ""))
	require.Equal(t, want, identity, "refresh keeps the recorded gateway")

	identity, _ = recorded(postRefreshGrant(t, ti, sharedSlug, client.ClientID, refreshToken, ""))
	require.Equal(t, want, identity, "refresh through another gateway on the same issuer keeps the original")
}

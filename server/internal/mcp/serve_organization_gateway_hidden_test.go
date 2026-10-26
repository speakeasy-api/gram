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
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type hiddenGatewayFixture struct {
	organizationGatewayFixture
	organizationID string
	openServerID   uuid.UUID
	deniedServerID uuid.UUID
}

func seedHiddenGateway(t *testing.T, ctx context.Context, ti *testInstance) hiddenGatewayFixture {
	t.Helper()
	fx := seedOrganizationGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return hiddenGatewayFixture{
		organizationGatewayFixture: fx,
		organizationID:             authCtx.ActiveOrganizationID,
		openServerID:               mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fx.open),
		deniedServerID:             mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fx.denied),
	}
}

func callOrganizationGatewayTool(t *testing.T, ti *testInstance, fx organizationGatewayFixture, name string, arguments map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": name, "arguments": arguments})
	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return w
}

func organizationGatewayListServers(t *testing.T, ti *testInstance, fx organizationGatewayFixture) metamcp.ListServersResult {
	t.Helper()
	rpc := decodeRPCResponse(t, callOrganizationGatewayTool(t, ti, fx, metamcp.ToolListServers, map[string]any{}))
	var envelope struct {
		StructuredContent metamcp.ListServersResult `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &envelope))
	return envelope.StructuredContent
}

func listedServerSlugs(result metamcp.ListServersResult) []string {
	slugs := make([]string, 0, len(result.Servers))
	for _, server := range result.Servers {
		slugs = append(slugs, server.Slug)
	}
	return slugs
}

func TestServeOrganizationGateway_HiddenMemberLeavesListServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, fx.organizationID, fx.subject.ID, fx.openServerID))

	result := organizationGatewayListServers(t, ti, fx.organizationGatewayFixture)

	require.Equal(t, []string{fx.granted}, listedServerSlugs(result))
	require.Equal(t, []metamcp.HiddenServer{{Slug: fx.open, Name: "public member"}}, result.Hidden)
	require.True(t, strings.HasSuffix(result.ConnectURL, "/mcp/gateway/"+fx.organizationSlug+"/connect/first-party"), "connectUrl %q", result.ConnectURL)
	require.Equal(t, metamcp.HiddenNote, result.Note)

	require.NoError(t, hiddenmcpservers.Unhide(ctx, ti.conn, fx.organizationID, fx.subject.ID, fx.openServerID))
	result = organizationGatewayListServers(t, ti, fx.organizationGatewayFixture)
	require.ElementsMatch(t, []string{fx.granted, fx.open}, listedServerSlugs(result))
	require.Empty(t, result.Hidden)
	require.Empty(t, result.ConnectURL)
	require.Empty(t, result.Note)
}

func TestServeOrganizationGateway_HiddenMemberReadsUnknown(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, fx.organizationID, fx.subject.ID, fx.openServerID))

	w := callOrganizationGatewayTool(t, ti, fx.organizationGatewayFixture, metamcp.ToolDescribeServer, map[string]any{"server": fx.open})
	require.Contains(t, w.Body.String(), "is hidden by the user")

	w = callOrganizationGatewayTool(t, ti, fx.organizationGatewayFixture, metamcp.ToolExecuteTool, map[string]any{"name": fx.open + "--any_tool", "arguments": map[string]any{}})
	require.Contains(t, w.Body.String(), "is hidden by the user")
}

func TestServeOrganizationGateway_HiddenMemberStaysForOtherUsers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, fx.organizationID, fx.subject.ID, fx.openServerID))

	other := fx.organizationGatewayFixture
	other.subject = urn.NewUserSubject(seedConsentMember(t, ctx, ti, fx.organizationID))
	result := organizationGatewayListServers(t, ti, other)

	require.Equal(t, []string{fx.open}, listedServerSlugs(result))
	require.Empty(t, result.Hidden)
}

func TestServeOrganizationGateway_ConnectionStatusReportsHiddenMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, fx.organizationID, fx.subject.ID, fx.openServerID))

	rpc := decodeRPCResponse(t, callOrganizationGatewayTool(t, ti, fx.organizationGatewayFixture, metamcp.ToolConnectionStatus, map[string]any{}))
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, text)
	var envelope struct {
		StructuredContent metamcp.ConnectionStatusResult `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &envelope))

	statuses := make(map[string]string, len(envelope.StructuredContent.Servers))
	for _, server := range envelope.StructuredContent.Servers {
		statuses[server.Slug] = server.Status
	}
	require.Equal(t, metamcp.SignInHidden, statuses[fx.open])
	require.NotEqual(t, metamcp.SignInHidden, statuses[fx.granted])
	require.Contains(t, text, "hidden by the user")
	require.Contains(t, text, "show it again at "+envelope.StructuredContent.ConnectURL)
}

// storeHiddenGatewayChallenge stores a consent challenge for the gateway's
// subject and returns the gateway endpoint and the state id.
func storeHiddenGatewayChallenge(t *testing.T, ti *testInstance, fx hiddenGatewayFixture) (*mcp.ResolvedMcpEndpoint, string) {
	t.Helper()
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(t.Context(), mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:         endpoint.Slug,
			RouteBase:       endpoint.RouteBase,
			MetaMcpServerID: endpoint.MetaMcpServerID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:    "org-gateway-mcp-client",
		RedirectURI: "http://example.com/cb",
		CSRFToken:   "csrf-token",
		Subject:     &fx.subject,
		CreatedAt:   time.Now(),
	}))
	return endpoint, stateID
}

func postConsentVisibility(t *testing.T, ti *testInstance, endpoint *mcp.ResolvedMcpEndpoint, organizationSlug, stateID, action string, serverID uuid.UUID, accept string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	form := url.Values{}
	form.Set("state", stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", action)
	form.Set("server_id", serverID.String())
	req := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+organizationSlug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	err := ti.service.ServeConsentAction(w, req, endpoint)
	return w, err
}

func hiddenIDs(t *testing.T, ti *testInstance, fx hiddenGatewayFixture) map[uuid.UUID]bool {
	t.Helper()
	hidden, err := hiddenmcpservers.HiddenServerIDs(t.Context(), ti.conn, fx.organizationID, fx.subject.ID)
	require.NoError(t, err)
	return hidden
}

func TestServeConsentAction_HideServerJSON(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	endpoint, stateID := storeHiddenGatewayChallenge(t, ti, fx)

	w, err := postConsentVisibility(t, ti, endpoint, fx.organizationSlug, stateID, "hide_server", fx.openServerID, "application/json")
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, map[uuid.UUID]bool{fx.openServerID: true}, hiddenIDs(t, ti, fx))

	// A hidden server can still be shown again from the page.
	w, err = postConsentVisibility(t, ti, endpoint, fx.organizationSlug, stateID, "show_server", fx.openServerID, "application/json")
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Empty(t, hiddenIDs(t, ti, fx))
}

func TestServeConsentAction_HideServerFormRedirects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	endpoint, stateID := storeHiddenGatewayChallenge(t, ti, fx)

	w, err := postConsentVisibility(t, ti, endpoint, fx.organizationSlug, stateID, "hide_server", fx.openServerID, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	location, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, stateID, location.Query().Get("state"))
	require.True(t, strings.HasSuffix(location.Path, "/mcp/gateway/"+fx.organizationSlug+"/connect"), "location %q", location.Path)
	require.Equal(t, map[uuid.UUID]bool{fx.openServerID: true}, hiddenIDs(t, ti, fx))
}

func TestServeConsentAction_HideServerOutsideGatewayIsRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	endpoint, stateID := storeHiddenGatewayChallenge(t, ti, fx)

	for name, serverID := range map[string]uuid.UUID{"unadmitted member": fx.deniedServerID, "unknown server": uuid.New()} {
		_, err := postConsentVisibility(t, ti, endpoint, fx.organizationSlug, stateID, "hide_server", serverID, "application/json")
		require.Error(t, err, name)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	require.Empty(t, hiddenIDs(t, ti, fx))
}

func TestServeOrganizationGateway_ReadsStaleConnectionStatusURI(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	stale := metamcp.ConnectionStatusResourcePrefix + "000000000000"
	require.NotEqual(t, metamcp.ConnectionStatusResourceURI, stale)

	body := makeMetaRPCBody(t, "resources/read", map[string]any{"uri": stale})
	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	rpc := decodeRPCResponse(t, w)
	require.NotContains(t, rpc, "error", string(rpc["error"]))
	var read struct {
		Contents []struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &read))
	require.Len(t, read.Contents, 1)
	require.Equal(t, stale, read.Contents[0].URI)
	require.Equal(t, metamcp.ConnectionStatusHTML, read.Contents[0].Text)
}

func TestGatewayConsent_HidingEveryReadyServerKeepsApproval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "hidden-ready-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newCatalogUpstream(t, 1)
	serverID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Source", "source", 1, upstream.url)

	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	page := consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.NotContains(t, page, "No servers could be verified", "the member must verify before it is hidden")

	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, auth.ActiveOrganizationID, mockidp.MockUserID, serverID))
	stateID, _ = seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	page = consentGetPageWithContext(t, ctx, ti, slug, stateID)
	require.NotContains(t, page, "No servers could be verified")
	hiddenStart := strings.Index(page, "data-gateway-hidden-list")
	require.Positive(t, hiddenStart)
	require.Contains(t, page[hiddenStart:], `name="server_id" value="`+serverID.String()+`"`)
}

func TestResolveGateMembers_AssistantRunIgnoresOwnersHiddenServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, "meta-"+uuid.NewString(), uuid.Nil)
	hiddenSlug, keptSlug := "hidden-"+uuid.NewString()[:8], "kept-"+uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, projectID, meta.ID, "hidden member", hiddenSlug, 0, mcpservers.VisibilityPublic)
	seedMetaMember(t, ctx, ti.conn, projectID, meta.ID, "kept member", keptSlug, 1, mcpservers.VisibilityPublic)
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, orgID, authCtx.UserID, mcpServerBySlug(t, ctx, ti, projectID, hiddenSlug)))

	served, hidden := mcp.ResolveStoredGatewayMemberSlugsForTest(t, ctx, ti.service.Service, orgID, projectID, meta.ID, authCtx.UserID)
	require.Equal(t, []string{keptSlug}, served)
	require.Equal(t, []string{hiddenSlug}, hidden)

	assistantCtx := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	served, hidden = mcp.ResolveStoredGatewayMemberSlugsForTest(t, assistantCtx, ti.service.Service, orgID, projectID, meta.ID, authCtx.UserID)
	require.ElementsMatch(t, []string{hiddenSlug, keptSlug}, served)
	require.Empty(t, hidden)
}

func TestServePublic_StoredGatewayHiddenMemberLeavesListServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-hidden-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	hiddenSlug, keptSlug := "hidden-"+uuid.NewString()[:8], "kept-"+uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, projectID, meta.ID, "hidden member", hiddenSlug, 0, mcpservers.VisibilityPublic)
	seedMetaMember(t, ctx, ti.conn, projectID, meta.ID, "kept member", keptSlug, 1, mcpservers.VisibilityPublic)
	subject := createTestUser(t, ctx, ti, "hidden-user-"+uuid.NewString())
	other := createTestUser(t, ctx, ti, "other-user-"+uuid.NewString())
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, orgID, subject.ID, mcpServerBySlug(t, ctx, ti, projectID, hiddenSlug)))

	listServers := func(bearer string) metamcp.ListServersResult {
		w, err := servePublicHTTP(t, context.Background(), ti, metaSlug, listServersBody(t), bearer, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var envelope struct {
			StructuredContent metamcp.ListServersResult `json:"structuredContent"`
		}
		require.NoError(t, json.Unmarshal(decodeRPCResponse(t, w)["result"], &envelope))
		return envelope.StructuredContent
	}

	result := listServers(mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject))
	require.Equal(t, []string{keptSlug}, listedServerSlugs(result))
	require.Equal(t, []metamcp.HiddenServer{{Slug: hiddenSlug, Name: "hidden member"}}, result.Hidden)
	require.True(t, strings.HasSuffix(result.ConnectURL, "/mcp/"+metaSlug+"/connect/first-party"), "connectUrl %q", result.ConnectURL)

	result = listServers(mintMetaIssuerBearer(t, ti, metaSlug, issuerID, other))
	require.ElementsMatch(t, []string{hiddenSlug, keptSlug}, listedServerSlugs(result))
	require.Empty(t, result.Hidden)
}

func TestServeOrganizationGateway_DirectModeOmitsHiddenMemberTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	decoy := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	hosted := seedHostedMetaMember(t, ctx, ti, decoy.ID, "Hosted tools", 3, mcpservers.VisibilityPublic, "alpha")
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx.organizationGatewayFixture, metamcp.DiscoveryModeDirect)
	listTools := makeMetaRPCBody(t, "tools/list", map[string]any{})

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, listTools)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"name":"`+hosted.slug+`--alpha"`)

	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, fx.organizationID, fx.subject.ID, hosted.serverID))
	w, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, listTools)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), hosted.slug+"--alpha")
}

func TestServeOrganizationGateway_HideAndUnhideWithinOneSession(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedHiddenGateway(t, ctx, ti)
	bearer := mintOrganizationGatewayBearer(t, ti, fx.organizationGatewayFixture)
	listServers := func() metamcp.ListServersResult {
		w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, listServersBody(t))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var envelope struct {
			StructuredContent metamcp.ListServersResult `json:"structuredContent"`
		}
		require.NoError(t, json.Unmarshal(decodeRPCResponse(t, w)["result"], &envelope))
		return envelope.StructuredContent
	}

	require.ElementsMatch(t, []string{fx.granted, fx.open}, listedServerSlugs(listServers()))
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, fx.organizationID, fx.subject.ID, fx.openServerID))
	require.Equal(t, []string{fx.granted}, listedServerSlugs(listServers()))
	require.NoError(t, hiddenmcpservers.Unhide(ctx, ti.conn, fx.organizationID, fx.subject.ID, fx.openServerID))
	require.ElementsMatch(t, []string{fx.granted, fx.open}, listedServerSlugs(listServers()))
}

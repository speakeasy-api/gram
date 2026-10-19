package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type sharedConsentFlow struct {
	state  mcp.AuthnChallengeState
	cookie *http.Cookie
}

// Runs the shared authorize handler and supplies the identity the IdP callback
// normally stamps. The browser proof itself must come from authorize.
func sharedIdentifiedConsent(t *testing.T, ctx context.Context, ti *testInstance, f sharedIssuerFixture, slug string) sharedConsentFlow {
	t.Helper()
	authorized := sharedAuthorize(t, ctx, ti, f, pkceChallenge("consent-reuse-verifier"), f.resource(ti, slug))
	require.Equal(t, http.StatusFound, authorized.Code)
	target, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+target.Query().Get("state"))
	require.NoError(t, err)
	subject := urn.NewUserSubject(requireProjectAuthContext(t, ctx).UserID)
	state.Subject = &subject
	state.AuthorizerUserID = subject.ID
	state.AuthorizerImpersonated = new(false)
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	cookies := authorized.Result().Cookies()
	require.Len(t, cookies, 1)
	return sharedConsentFlow{state: state, cookie: cookies[0]}
}

func (f sharedConsentFlow) inventory(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	attempt := uuid.NewString()
	req := sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+f.state.UserSessionIssuerID.String()+"/connect/mcp", f.state.UserSessionIssuerID.String(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Gram-Consent-State", f.state.ID)
	req.Header.Set("Gram-Consent-Csrf", f.state.CSRFToken)
	req.Header.Set("Gram-Consent-Inventory-Attempt", attempt)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedConsentMCP(w, req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"tools"`)
	return attempt
}

func (f sharedConsentFlow) page(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	req := sharedRequest(ctx, http.MethodGet, "/oauth/usi/"+f.state.UserSessionIssuerID.String()+"/connect?state="+f.state.ID, f.state.UserSessionIssuerID.String(), nil)
	if f.cookie != nil {
		req.AddCookie(f.cookie)
	}
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedConsent(w, req))
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

func (f sharedConsentFlow) submit(t *testing.T, ctx context.Context, ti *testInstance, action, attempt, filtering string) string {
	t.Helper()
	form := url.Values{"state": {f.state.ID}, "csrf_token": {f.state.CSRFToken}, "action": {action}, "tool_inventory_id": {attempt}, "tool_filtering": {filtering}, "session_duration_hours": {"1"}}
	req := sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+f.state.UserSessionIssuerID.String()+"/connect", f.state.UserSessionIssuerID.String(), strings.NewReader(form.Encode()))
	if f.cookie != nil {
		req.AddCookie(f.cookie)
	}
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleSharedConsent(w, req))
	require.Equal(t, http.StatusSeeOther, w.Code)
	return w.Header().Get("Location")
}

func TestSharedConsentReusePreservesResourcePolicy(t *testing.T) {
	t.Parallel()
	for _, filtering := range []string{"off", "on"} {
		t.Run(filtering, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			issuer := seedSharedIssuer(t, ctx, ti)
			first := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
			require.NotContains(t, first.page(t, ctx, ti), `id="consent-reuse"`)
			explicit, err := url.Parse(first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), filtering))
			require.NoError(t, err)
			require.NotEmpty(t, explicit.Query().Get("code"))
			second := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
			require.Contains(t, second.page(t, ctx, ti), `id="consent-reuse"`)
			automatic, err := url.Parse(second.submit(t, ctx, ti, "reuse", second.inventory(t, ctx, ti), "off"))
			require.NoError(t, err)
			code := automatic.Query().Get("code")
			require.NotEmpty(t, code)
			grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
			grant, err := grants.Get(ctx, "userSessionGrant:"+issuer.issuer.ID.String()+":"+code)
			require.NoError(t, err)
			require.Equal(t, 1, grant.DesiredSessionDurationHours)
			require.Equal(t, second.state.Endpoint.McpServerID, grant.Endpoint.McpServerID)
			if filtering == "on" {
				original, err := grants.Get(ctx, "userSessionGrant:"+issuer.issuer.ID.String()+":"+explicit.Query().Get("code"))
				require.NoError(t, err)
				require.NotNil(t, grant.ToolSelection)
				require.Empty(t, grant.ToolSelection.Allow, "a saved zero-tools policy must never widen to all tools")
				require.NotEqual(t, original.ToolSelection.GrantID, grant.ToolSelection.GrantID)
			} else {
				require.Nil(t, grant.ToolSelection)
			}
			sibling := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugB)
			require.NotContains(t, sibling.page(t, ctx, ti), `id="consent-reuse"`, "an unseen sibling needs its own approval")
			sibling.submit(t, ctx, ti, "approve", sibling.inventory(t, ctx, ti), filtering)
			for _, slug := range []string{issuer.slugA, issuer.slugB} {
				flow := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
				require.Contains(t, flow.page(t, ctx, ti), `id="consent-reuse"`)
				target, err := url.Parse(flow.submit(t, ctx, ti, "reuse", flow.inventory(t, ctx, ti), "off"))
				require.NoError(t, err)
				require.NotEmpty(t, target.Query().Get("code"))
			}
		})
	}
}

func TestSharedConsentReuseRequiresInitiatingBrowser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	second := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	second.cookie = nil
	require.NotContains(t, second.page(t, ctx, ti), `id="consent-reuse"`)
	redirect, err := url.Parse(second.submit(t, ctx, ti, "reuse", second.inventory(t, ctx, ti), "off"))
	require.NoError(t, err)
	require.Empty(t, redirect.Query().Get("code"))
	require.Equal(t, "required", redirect.Query().Get("consent"))
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+second.state.ID)
	require.NoError(t, err, "fallback keeps the explicit consent challenge usable")
}

func TestSharedConsentReuseWithdrawalDuringInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	second := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	require.Contains(t, second.page(t, ctx, ti), `id="consent-reuse"`)
	attempt := second.inventory(t, ctx, ti)
	auth := requireProjectAuthContext(t, ctx)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := usersessions_repo.New(tx).LockUserSessionConsentClient(ctx, usersessions_repo.LockUserSessionConsentClientParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionClientID: issuer.client.ID,
	})
	require.NoError(t, err)
	form := url.Values{"state": {second.state.ID}, "csrf_token": {second.state.CSRFToken}, "action": {"reuse"}, "tool_inventory_id": {attempt}}
	req := sharedRequest(ctx, http.MethodPost, issuer.issuerURL+"/connect", issuer.issuer.ID.String(), strings.NewReader(form.Encode()))
	req.AddCookie(second.cookie)
	w := httptest.NewRecorder()
	completed := make(chan error, 1)
	go func() { completed <- ti.service.HandleSharedConsent(w, req) }()
	testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), 1)
	revoked, err := usersessions_repo.New(tx).RevokeUserSessionConsentGroup(ctx, usersessions_repo.RevokeUserSessionConsentGroupParams{
		ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionClientID: issuer.client.ID, SubjectUrn: *second.state.Subject,
	})
	require.NoError(t, err)
	require.NotEmpty(t, revoked)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-completed)
	require.Equal(t, http.StatusSeeOther, w.Code)
	redirect, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Empty(t, redirect.Query().Get("code"))
	require.Equal(t, "required", redirect.Query().Get("consent"))
	require.NotContains(t, second.page(t, ctx, ti), `id="consent-reuse"`)
}

func TestSharedConsentExplicitApprovalWithoutSnapshotSupersedesEvidence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	second := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	second.submit(t, ctx, ti, "approve", "", "off")
	third := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	require.NotContains(t, third.page(t, ctx, ti), `id="consent-reuse"`)
}

func TestSharedConsentReuseChangedInventoryRequiresApproval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	toolset, _ := seedSelectionToolset(t, ctx, ti)
	slug := "changing-inventory-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuer.issuer.ID)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	_, err := toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, Version: 2, ToolUrns: []urn.Tool{}, ResourceUrns: []urn.Resource{},
	})
	require.NoError(t, err)
	second := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	redirect, err := url.Parse(second.submit(t, ctx, ti, "reuse", second.inventory(t, ctx, ti), "off"))
	require.NoError(t, err)
	require.Empty(t, redirect.Query().Get("code"))
	require.Equal(t, "required", redirect.Query().Get("consent"))
}

func TestSharedConsentReuseMissingInventoryRequiresApproval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	second := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	redirect, err := url.Parse(second.submit(t, ctx, ti, "reuse", uuid.NewString(), "off"))
	require.NoError(t, err)
	require.Empty(t, redirect.Query().Get("code"))
	require.Equal(t, "required", redirect.Query().Get("consent"))
}

func TestSharedConsentReuseInventoryChangesAfterDiscovery(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	toolset, _ := seedSelectionToolset(t, ctx, ti)
	slug := "changing-inventory-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuer.issuer.ID)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	second := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	attempt := second.inventory(t, ctx, ti)
	_, err := toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, Version: 2, ToolUrns: []urn.Tool{}, ResourceUrns: []urn.Resource{},
	})
	require.NoError(t, err)
	redirect, err := url.Parse(second.submit(t, ctx, ti, "reuse", attempt, "off"))
	require.NoError(t, err)
	require.Empty(t, redirect.Query().Get("code"))
	require.Equal(t, "required", redirect.Query().Get("consent"))
}

func TestSharedConsentRestrictiveApprovalSupersedesAllTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	second := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	second.submit(t, ctx, ti, "approve", second.inventory(t, ctx, ti), "on")
	third := sharedIdentifiedConsent(t, ctx, ti, issuer, issuer.slugA)
	target, err := url.Parse(third.submit(t, ctx, ti, "reuse", third.inventory(t, ctx, ti), "off"))
	require.NoError(t, err)
	require.NotEmpty(t, target.Query().Get("code"))
	grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grants.Get(ctx, "userSessionGrant:"+issuer.issuer.ID.String()+":"+target.Query().Get("code"))
	require.NoError(t, err)
	require.NotNil(t, grant.ToolSelection)
	require.Empty(t, grant.ToolSelection.Allow)
}

func TestSharedConsentReuseUnchangedNonemptyInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	toolset, _ := seedSelectionToolset(t, ctx, ti)
	slug := "unchanged-inventory-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuer.issuer.ID)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	second := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	redirect, err := url.Parse(second.submit(t, ctx, ti, "reuse", second.inventory(t, ctx, ti), "off"))
	require.NoError(t, err)
	require.NotEmpty(t, redirect.Query().Get("code"))
}

func TestSharedConsentPerEndpointApprovalSupersedesEvidence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	issuer := seedSharedIssuer(t, ctx, ti)
	toolset, _ := seedSelectionToolset(t, ctx, ti)
	slug := "cross-route-consent-" + uuid.NewString()
	createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{}, issuer.issuer.ID)
	first := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	first.submit(t, ctx, ti, "approve", first.inventory(t, ctx, ti), "off")
	pending := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	require.Contains(t, pending.page(t, ctx, ti), `id="consent-reuse"`)
	pendingAttempt := pending.inventory(t, ctx, ti)

	// The same client accepts a newer explicit decision at the per-endpoint AS.
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", slug)
	endpointCtx := context.WithValue(ctx, chi.RouteCtxKey, route)
	query := url.Values{
		"response_type": {"code"}, "client_id": {issuer.client.ClientID},
		"redirect_uri":   {sharedTestRedirectURI},
		"code_challenge": {pkceChallenge(pkceVerifier(t))}, "code_challenge_method": {"S256"},
	}
	authorized := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(authorized, httptest.NewRequestWithContext(endpointCtx, http.MethodGet, "/mcp/"+slug+"/authorize?"+query.Encode(), nil)))
	require.Equal(t, http.StatusFound, authorized.Code)
	consentURL, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentURL.Query().Get("state"))
	require.NoError(t, err)
	require.False(t, state.Endpoint.SharedAuthorizationServer)
	require.Equal(t, first.state.Endpoint.McpServerID, state.Endpoint.McpServerID)
	state.Subject = first.state.Subject
	state.AuthorizerUserID = first.state.AuthorizerUserID
	state.AuthorizerImpersonated = new(false)
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	require.NotContains(t, consentGetPageWithContext(t, ctx, ti, slug, state.ID), `id="consent-reuse"`)

	attempt := uuid.NewString()
	inventoryRequest := httptest.NewRequestWithContext(endpointCtx, http.MethodPost, "/mcp/"+slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	inventoryRequest.Header.Set("Content-Type", "application/json")
	inventoryRequest.Header.Set("Accept", "application/json, text/event-stream")
	inventoryRequest.Header.Set("Gram-Consent-State", state.ID)
	inventoryRequest.Header.Set("Gram-Consent-Csrf", state.CSRFToken)
	inventoryRequest.Header.Set("Gram-Consent-Inventory-Attempt", attempt)
	inventoryResponse := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsentMCP(inventoryResponse, inventoryRequest))
	require.Equal(t, http.StatusOK, inventoryResponse.Code)

	form := url.Values{"state": {state.ID}, "csrf_token": {state.CSRFToken}, "action": {"approve"}, "tool_filtering": {"on"}, "tool_inventory_id": {attempt}}
	request := httptest.NewRequestWithContext(endpointCtx, http.MethodPost, "/mcp/"+slug+"/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(response, request))
	require.Equal(t, http.StatusSeeOther, response.Code)
	approved, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, approved.Query().Get("code"))
	grants := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grants.Get(ctx, "userSessionGrant:"+issuer.issuer.ID.String()+":"+approved.Query().Get("code"))
	require.NoError(t, err)
	require.NotNil(t, grant.ToolSelection)
	require.Empty(t, grant.ToolSelection.Allow)
	require.Zero(t, grant.DesiredSessionDurationHours, "per-endpoint consent keeps its default session duration behavior")

	// Even a shared flow that already discovered the old inventory cannot reuse it.
	target, err := url.Parse(pending.submit(t, ctx, ti, "reuse", pendingAttempt, "off"))
	require.NoError(t, err)
	require.Empty(t, target.Query().Get("code"))
	require.Equal(t, "required", target.Query().Get("consent"))
	next := sharedIdentifiedConsent(t, ctx, ti, issuer, slug)
	require.NotContains(t, next.page(t, ctx, ti), `id="consent-reuse"`)
}

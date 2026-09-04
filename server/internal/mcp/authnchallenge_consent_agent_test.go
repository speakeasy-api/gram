package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type agentConsentFixture struct {
	toolset toolsetsrepo.Toolset
	client  usersessionsrepo.UserSessionClient
	stateID string
	csrf    string
	target  mcp.AgentAuthorizationTarget
	userID  string
	orgID   string
}

func newAgentConsentFixture(t *testing.T, ctx context.Context, ti *testInstance) agentConsentFixture {
	t.Helper()

	toolset, _, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ti.features.SetFlag(feature.FlagAgentManagementM1, authCtx.ActiveOrganizationID, true)
	ti.features.SetFlag(feature.FlagAgentMCPAuthorizationM2, authCtx.ActiveOrganizationID, true)

	stateID := uuid.NewString()
	csrf := "csrf-" + uuid.NewString()
	subject := urn.NewUserSubject(authCtx.UserID)
	target := mcp.AgentAuthorizationTarget{
		Scope:               authz.ScopeMCPConnect,
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		MCPResourceID:       toolset.ID,
	}
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                       stateID,
		UserSessionIssuerID:      target.UserSessionIssuerID,
		AuthorizerUserID:         authCtx.UserID,
		AuthorizerImpersonated:   new(bool),
		AgentAuthorizationTarget: &target,
		Endpoint: mcp.EndpointRef{
			McpSlug:   toolset.McpSlug.String,
			RouteBase: "mcp",
		},
		ClientID:            client.ClientID,
		RedirectURI:         client.RedirectUris[0],
		State:               "client-state",
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		CSRFToken:           csrf,
		Subject:             &subject,
		CreatedAt:           time.Now(),
	}))

	return agentConsentFixture{toolset: toolset, client: client, stateID: stateID, csrf: csrf, target: target, userID: authCtx.UserID, orgID: authCtx.ActiveOrganizationID}
}

func createConsentAgent(t *testing.T, ctx context.Context, ti *testInstance, fx agentConsentFixture, name string) agentsrepo.Agent {
	t.Helper()
	agent, err := agentsrepo.New(ti.conn).CreateAgentWithID(ctx, agentsrepo.CreateAgentWithIDParams{
		ID:             uuid.New(),
		OrganizationID: fx.orgID,
		OwnerUserID:    fx.userID,
		Name:           name,
	})
	require.NoError(t, err)
	return agent
}

func seedConsentMember(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) string {
	t.Helper()
	userID := "consent-human-" + uuid.NewString()
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       userID + "@example.com",
		DisplayName: "Consent human",
	})
	require.NoError(t, err)
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	return userID
}

func seedPrincipalGrant(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, principal urn.Principal, scope authz.Scope, resourceID string) uuid.UUID {
	t.Helper()
	selectors, err := authz.NewSelector(scope, resourceID).MarshalJSON()
	require.NoError(t, err)
	grant, err := accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	return grant.ID
}

func seedPrincipalMCPConnectGrant(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, principal urn.Principal, resourceID uuid.UUID) uuid.UUID {
	t.Helper()
	return seedPrincipalGrant(t, ctx, ti, organizationID, principal, authz.ScopeMCPConnect, resourceID.String())
}

func serveAgentConsentGet(t *testing.T, ctx context.Context, ti *testInstance, fx agentConsentFixture) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+fx.toolset.McpSlug.String+"/connect?state="+fx.stateID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", fx.toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleConsent(w, req))
	return w
}

func serveAgentConsentPost(t *testing.T, ctx context.Context, ti *testInstance, fx agentConsentFixture, agentID uuid.UUID) (*httptest.ResponseRecorder, error) {
	t.Helper()
	form := url.Values{
		"state":          {fx.stateID},
		"csrf_token":     {fx.csrf},
		"action":         {"approve_agent"},
		"agent_id":       {agentID.String()},
		"tool_filtering": {"on"},
		"tools":          {"must-be-ignored"},
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", fx.toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	if err := ti.service.HandleConsent(w, req); err != nil {
		return w, fmt.Errorf("serve agent consent post: %w", err)
	}
	return w, nil
}

func TestConsentAgentSelectionRequiresDistinctAction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	form := url.Values{
		"state":      {fx.stateID},
		"csrf_token": {fx.csrf},
		"action":     {"approve"},
		"agent_id":   {uuid.NewString()},
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", fx.toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	err := ti.service.HandleConsent(httptest.NewRecorder(), req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "agent approval action and selection do not match")
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err, "mismatched rolling-deploy action must not consume the challenge")
}

func TestConsentAgentPickerShowsOnlyEligibleAgents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())

	eligible := createConsentAgent(t, ctx, ti, fx, "Eligible agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, eligible.ID.String()), fx.target.MCPResourceID)

	missingPolicy := createConsentAgent(t, ctx, ti, fx, "Missing policy agent")
	suspended := createConsentAgent(t, ctx, ti, fx, "Suspended agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, suspended.ID.String()), fx.target.MCPResourceID)
	_, err := agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: fx.orgID, ID: suspended.ID})
	require.NoError(t, err)

	w := serveAgentConsentGet(t, ctx, ti, fx)
	require.Equal(t, http.StatusOK, w.Code)
	html := w.Body.String()
	require.Contains(t, html, "Authorize as")
	require.Contains(t, html, eligible.Name)
	require.Contains(t, html, eligible.ID.String())
	require.NotContains(t, html, missingPolicy.Name)
	require.NotContains(t, html, suspended.Name)
	require.Contains(t, html, "mcp:connect")
	require.Contains(t, html, "agent-management")
}

func TestConsentAgentPickerAllowsExactAgentAuthorizer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Delegated agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	authorizerID := seedConsentMember(t, ctx, ti, fx.orgID)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	subject := urn.NewUserSubject(authorizerID)
	state.AuthorizerUserID = authorizerID
	state.Subject = &subject
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
	grantID := seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, authorizerID), authz.ScopeAgentAuthorize, agent.ID.String())

	w := serveAgentConsentGet(t, ctx, ti, fx)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), agent.Name)

	rows, err := accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: grantID, OrganizationID: fx.orgID})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	w = serveAgentConsentGet(t, ctx, ti, fx)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), agent.Name)
}

func TestConsentAgentApprovalCarriesFixedResultWithoutReusableConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Approved agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	w, err := serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	redirect, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	code := redirect.Query().Get("code")
	require.True(t, strings.HasPrefix(code, "agent-v1."))

	grantCache := cache.NewTypedObjectCache[mcp.UserSessionGrant](ti.logger, ti.cacheAdapter, cache.SuffixNone)
	grant, err := grantCache.Get(ctx, "agentUserSessionGrant:"+fx.target.UserSessionIssuerID.String()+":"+code)
	require.NoError(t, err)
	require.Equal(t, urn.NewUserSubject(fx.userID), grant.Subject)
	require.Nil(t, grant.ToolSelection)
	require.NotNil(t, grant.AgentAuthorization)
	require.Equal(t, agent.ID, grant.AgentAuthorization.AgentID)
	require.Equal(t, fx.userID, grant.AgentAuthorization.AuthorizerUserID)
	require.Equal(t, fx.target, grant.AgentAuthorization.Target)

	consents, err := usersessionsrepo.New(ti.conn).ListUserSessionConsentsByProjectID(ctx, usersessionsrepo.ListUserSessionConsentsByProjectIDParams{
		ProjectID:           fx.target.ProjectID,
		OrganizationID:      fx.orgID,
		SubjectUrn:          pgtype.Text{String: grant.Subject.String(), Valid: true},
		UserSessionClientID: uuid.NullUUID{UUID: fx.client.ID, Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: fx.target.UserSessionIssuerID, Valid: true},
		LimitValue:          10,
	})
	require.NoError(t, err)
	require.Empty(t, consents)
}

func TestConsentAgentApprovalRechecksLiveAgentPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Stale policy agent")
	grantID := seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	w := serveAgentConsentGet(t, ctx, ti, fx)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), agent.Name)
	rows, err := accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: grantID, OrganizationID: fx.orgID})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	w, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	require.Empty(t, w.Header().Get("Location"))
}

func TestConsentAgentSelectionRejectsImpersonatedAuthorizer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Support-hidden agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	impersonated := true
	state.AuthorizerImpersonated = &impersonated
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

	w := serveAgentConsentGet(t, ctx, ti, fx)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "Authorize as")
	require.NotContains(t, w.Body.String(), agent.Name)

	w, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	require.Empty(t, w.Header().Get("Location"))
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err, "impersonated rejection must not consume the challenge")
}

func TestConsentAgentSelectionRejectsLegacyAuthorizerWithoutProvenance(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Legacy-hidden agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.AuthorizerImpersonated = nil
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))

	w := serveAgentConsentGet(t, ctx, ti, fx)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "Authorize as")
	require.NotContains(t, w.Body.String(), agent.Name)

	w, err = serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	require.Empty(t, w.Header().Get("Location"))
	_, err = ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err, "legacy-state rejection must not consume the challenge")
}

func TestConsentAgentSelectionFailsClosedWhenRolloutDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	agent := createConsentAgent(t, ctx, ti, fx, "Hidden agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	ti.features.SetFlag(feature.FlagAgentMCPAuthorizationM2, fx.orgID, false)

	w := serveAgentConsentGet(t, ctx, ti, fx)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "Authorize as")
	require.NotContains(t, w.Body.String(), agent.Name)

	w, err := serveAgentConsentPost(t, ctx, ti, fx, agent.ID)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	require.Empty(t, w.Header().Get("Location"))
}

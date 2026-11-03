package mcp_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestAgentClientCredentialsPublicRegistrationRenewalAndRevocation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx, agent, _, session := seedAgentRefreshSession(t, ctx, ti)
	raw, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{OrganizationID: fx.orgID, CreatedByUserID: fx.userID, Name: "agent registration", KeyPrefix: prefix, KeyHash: hash, SubjectUrn: pgtype.Text{String: session.SubjectUrn.String(), Valid: true}, DelegatedGrants: session.DelegatedGrants, DelegatedGrantsVersion: session.DelegatedGrantsVersion, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	require.NoError(t, err)
	signer := newAssertionSigner(t)
	badBody, err := json.Marshal(map[string]any{"agent_urn": urn.NewAgentSubject(agent.ID).String(), "grant_types": []string{"client_credentials"}, "token_endpoint_auth_method": "private_key_jwt", "jwks": json.RawMessage(signer.jwks), "redirect_uris": []string{"https://example.com/callback"}})
	require.NoError(t, err)
	badRequest := newRegisterRequest(t, fx.toolset.McpSlug.String, string(badBody))
	badRequest.Header.Set("Authorization", "Bearer "+raw)
	badResponse := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleRegister(badResponse, badRequest))
	require.Equal(t, http.StatusBadRequest, badResponse.Code)
	require.Contains(t, badResponse.Body.String(), "invalid_client_metadata")
	body, _ := json.Marshal(map[string]any{"client_name": "ordinary agent", "agent_urn": urn.NewAgentSubject(agent.ID).String(), "grant_types": []string{"client_credentials"}, "token_endpoint_auth_method": "private_key_jwt", "jwks": json.RawMessage(signer.jwks)})
	req := newRegisterRequest(t, fx.toolset.McpSlug.String, string(body))
	// Anonymous DCR cannot assign a trusted agent profile.
	require.Error(t, ti.service.HandleRegister(httptest.NewRecorder(), req))
	req = newRegisterRequest(t, fx.toolset.McpSlug.String, string(body))
	req.Header.Set("Authorization", "Bearer "+raw)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleRegister(w, req))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var registration struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &registration))
	client, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: fx.target.UserSessionIssuerID, ClientID: registration.ClientID})
	require.NoError(t, err)
	require.Equal(t, urn.NewAgentSubject(agent.ID).String(), client.SubjectUrn.String)
	require.False(t, client.AssistantRuntimeID.Valid)
	require.False(t, client.AssistantTriggerWorkloadBindingID.Valid)
	issuer, _ := fetchAdvertisedIssuer(t, ctx, ti, fx.toolset.McpSlug.String)
	resource := ti.serverURL.String() + "/mcp/" + fx.toolset.McpSlug.String
	issue := func() *httptest.ResponseRecorder {
		return postForm(t, ti, fx.toolset.McpSlug.String, "token", withAssertion(url.Values{"grant_type": {"client_credentials"}, "client_id": {client.ClientID}, "scope": {"mcp:connect"}, "resource": {resource}}, signer.assertion(t, client.ClientID, issuer)))
	}
	first := issue()
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &token))
	require.NotEmpty(t, token.AccessToken)
	require.LessOrEqual(t, token.ExpiresIn, 900)
	require.NotContains(t, first.Body.String(), "refresh_token")
	second := issue()
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	endpoint := mcp.ResolvedMcpEndpoint{AudienceURN: urn.NewToolset(fx.toolset.ID).String(), OrganizationID: fx.orgID, ProjectID: fx.target.ProjectID, RouteBase: mcproute.MCP, Slug: fx.toolset.McpSlug.String, ToolsetID: uuid.NullUUID{UUID: fx.toolset.ID, Valid: true}, UserSessionIssuerID: fx.target.UserSessionIssuerID}
	_, _, _, err = ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), token.AccessToken, ti.serverURL.String(), &endpoint)
	require.NoError(t, err)
	_, err = agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)
	_, _, _, err = ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), token.AccessToken, ti.serverURL.String(), &endpoint)
	require.Error(t, err, "live suspension must reject a previously issued access token")
	require.NotEqual(t, http.StatusOK, issue().Code)
	_, err = agentsrepo.New(ti.conn).ResumeAgent(ctx, agentsrepo.ResumeAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).RevokeUserSessionClient(ctx, usersessionsrepo.RevokeUserSessionClientParams{ID: client.ID, ProjectID: fx.target.ProjectID, OrganizationID: fx.orgID})
	require.NoError(t, err)
	_, _, _, err = ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), token.AccessToken, ti.serverURL.String(), &endpoint)
	require.Error(t, err, "client revocation must reject a previously issued access token immediately")
	require.NotEqual(t, http.StatusOK, issue().Code)
}

func TestRuntimeBuiltinToolsAcceptOrdinaryAgentKey(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	fx, agent, _, session := seedAgentRefreshSession(t, ctx, ti)
	seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), authz.ScopeProjectRead, fx.target.ProjectID.String())
	seedPrincipalGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), authz.ScopeProjectRead, fx.target.ProjectID.String())
	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{Scope: authz.ScopeProjectRead, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindProject, authz.SelectorKeyResourceID: fx.target.ProjectID.String()}}})
	require.NoError(t, err)
	session.DelegatedGrants, err = runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	raw, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{OrganizationID: fx.orgID, CreatedByUserID: fx.userID, Name: "ordinary scoped agent", KeyPrefix: prefix, KeyHash: hash, SubjectUrn: pgtype.Text{String: session.SubjectUrn.String(), Valid: true}, DelegatedGrants: session.DelegatedGrants, DelegatedGrantsVersion: session.DelegatedGrantsVersion, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)
	// No assistant, runtime binding, trigger, or managed-project association.
	for slug, expected := range map[string]string{platformtools.AssistantsPlatformToolsetSlug: "skills_load", platformtools.ManagedAssistantPlatformToolsetSlug: "platform_get_tool_usage_summary"} {
		w, err := servePlatformHTTP(t, ti, slug, toolsListBody(), raw, *authCtx.ProjectSlug)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"`+expected+`"`)
		require.NotContains(t, w.Body.String(), `"error"`)
	}
	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, fx.orgID, feature.VariantAssistantToolsPlatformMCP)
	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), raw, *authCtx.ProjectSlug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"get_platform_context"`)
	require.Contains(t, w.Body.String(), `"list_projects"`)
	require.NotContains(t, w.Body.String(), `"error"`)
}

func TestAgentClientOrganizationSharedIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx, agent, _, _ := seedAgentRefreshSession(t, ctx, ti)
	issuerID := createOrganizationIssuer(t, ctx, ti, fx.orgID)
	setIssuerMode(t, ctx, ti, issuerID, "shared", "")
	slug := seedPublicServerOnIssuer(t, ctx, ti, issuerID)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	target := fx.target
	target.UserSessionIssuerID = issuerID
	target.MCPResourceID = endpoint.ToolsetID.UUID
	if endpoint.McpServerID.Valid {
		target.MCPResourceID = endpoint.McpServerID.UUID
	}
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), target.MCPResourceID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID), target.MCPResourceID)
	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: target.MCPResourceID.String(), authz.SelectorKeyProjectID: target.ProjectID.String()}}})
	require.NoError(t, err)
	ceiling, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	authCtx := requireProjectAuthContext(t, ctx)
	agentCtx := contextvalues.WithPrincipalCredentialAuthorization(ctx, authCtx, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), contextvalues.PrincipalCredential{AuthorizerUserID: fx.userID, DelegatedGrants: ceiling, DelegatedGrantsVersion: int32(runtimepolicy.CurrentDelegatedPolicyVersion)})
	signer := newAssertionSigner(t)
	request := usersessions.RegistrationRequest{ClientName: "Organization issuer agent", GrantTypes: []string{"client_credentials"}, TokenEndpointAuthMethod: "private_key_jwt", JWKS: signer.jwks}
	register := func(target mcp.AgentAuthorizationTarget) (usersessionsrepo.UserSessionClient, error) {
		tx := testenv.BeginTx(t, ctx, ti.conn)
		defer func() { _ = tx.Rollback(ctx) }()
		client, err := mcp.RegisterAgentOAuthClient(agentCtx, tx, target, request)
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			return client, fmt.Errorf("register test agent client: %w", err)
		}
		return client, nil
	}
	for _, field := range []string{"organization", "project", "issuer"} {
		bad := target
		switch field {
		case "organization":
			bad.OrganizationID = "org_other"
		case "project":
			bad.ProjectID = uuid.New()
		case "issuer":
			bad.UserSessionIssuerID = uuid.New()
		}
		_, err := register(bad)
		require.Error(t, err, field)
	}
	client, err := register(target)
	require.NoError(t, err)
	require.False(t, client.ProjectID.Valid)
	require.Equal(t, fx.orgID, client.OrganizationID.String)
	require.Equal(t, fx.userID, client.AuthorizerUserID.String)
	require.Equal(t, urn.NewAgentSubject(agent.ID).String(), client.SubjectUrn.String)
	require.JSONEq(t, string(ceiling), string(client.DelegatedGrants))
	issuerURL := ti.serverURL.String() + "/oauth/usi/" + issuerID.String()
	resource := ti.serverURL.String() + "/mcp/" + slug
	exchange := func(resource string) *httptest.ResponseRecorder {
		return sharedToken(t, ctx, ti, issuerID.String(), withAssertion(url.Values{"grant_type": {"client_credentials"}, "client_id": {client.ClientID}, "scope": {"mcp:connect"}, "resource": {resource}}, signer.assertion(t, client.ClientID, issuerURL)))
	}
	w := exchange(resource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := decodeSharedTokenResponse(t, w)
	_, _, _, err = ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), token.AccessToken, ti.serverURL.String(), endpoint)
	require.NoError(t, err)
	otherSlug := seedPublicServerOnIssuer(t, ctx, ti, issuerID)
	require.NotEqual(t, http.StatusOK, exchange(ti.serverURL.String()+"/mcp/"+otherSlug).Code, "shared issuer does not widen the resource ceiling")
	for _, field := range []string{"organization", "project"} {
		bad := *endpoint
		if field == "organization" {
			bad.OrganizationID = "org_other"
		} else {
			bad.ProjectID = uuid.New()
		}
		_, _, _, err := ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), token.AccessToken, ti.serverURL.String(), &bad)
		require.Error(t, err, field)
	}
}

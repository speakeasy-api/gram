package mcp_test

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
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
	ctx, ti := newTestMCPService(t)
	fx, _, _, session := seedAgentRefreshSession(t, ctx, ti)
	raw, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = tokenrepo.New(ti.conn).CreateKitAPIKey(ctx, tokenrepo.CreateKitAPIKeyParams{ProjectID: uuid.NullUUID{UUID: fx.target.ProjectID, Valid: true}, OrganizationID: fx.orgID, CreatedByUserID: fx.userID, Name: "ordinary scoped agent", KeyPrefix: prefix, KeyHash: hash, SubjectUrn: pgtype.Text{String: session.SubjectUrn.String(), Valid: true}, DelegatedGrants: session.DelegatedGrants, DelegatedGrantsVersion: session.DelegatedGrantsVersion, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	require.NoError(t, err)
	// No assistant, runtime binding, trigger, or managed-project association.
	for _, slug := range []string{platformtools.AssistantsPlatformToolsetSlug, platformtools.ManagedAssistantPlatformToolsetSlug} {
		w, err := servePlatformHTTP(t, ti, slug, toolsListBody(), raw)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
}

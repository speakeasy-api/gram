package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	gramMCP "github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestPrincipalCredentialAuthenticatesAsItsAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithoutTemporal(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)

	agents := agentrepo.New(ti.conn)
	agent, err := agents.CreateAgent(t.Context(), agentrepo.CreateAgentParams{OrganizationID: ac.ActiveOrganizationID, OwnerUserID: ac.UserID, ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, Name: "Principal credential agent " + uuid.NewString()[:8]})
	require.NoError(t, err)
	connect := authz.NewGrant(authz.ScopeMCPConnect, toolset.ID.String())
	selector, err := json.Marshal(connect.Selector)
	require.NoError(t, err)
	_, err = agents.CreateAgentPolicyGrant(t.Context(), agentrepo.CreateAgentPolicyGrantParams{OrganizationID: ac.ActiveOrganizationID, AgentID: agent.ID, Scope: string(authz.ScopeMCPConnect), Selectors: selector})
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), Scope: string(authz.ScopeMCPConnect), Selectors: selector})
	require.NoError(t, err)
	projectRead, err := authz.NewSelector(authz.ScopeProjectRead, ac.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), Scope: string(authz.ScopeProjectRead), Selectors: projectRead})
	require.NoError(t, err)
	token, _, err := ti.principalCredentials.Mint(principalcredential.Credential{
		OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID,
		Principal: urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), AuthorizerUserID: ac.UserID,
		Grants: []authz.Grant{connect},
	})
	require.NoError(t, err)

	endpoint := &gramMCP.ResolvedMcpEndpoint{OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, UserSessionIssuerID: issuer.ID, AudienceURN: urn.NewUserSessionIssuer(issuer.ID).String(), Slug: toolset.Slug, RouteBase: "mcp"}
	admitted, _, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "issuer-gated endpoints admit principal credentials like agent keys")
	actor, ok := contextvalues.AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), actor)

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/"+toolset.Slug, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	private, err := ti.service.RequirePrivateIdentityAuth(t.Context(), httptest.NewRecorder(), request, false, toolset.ID, "")
	require.NoError(t, err, "private toolsets without an issuer authenticate principal credentials too")
	actor, ok = contextvalues.AuthenticatedActor(private)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeAgent, actor.Type)
	private, err = ti.authzEngine.PrepareContext(private)
	require.NoError(t, err)
	require.NoError(t, ti.authzEngine.Require(private, authz.MCPCheck(authz.ScopeMCPConnect, toolset.ID.String(), ac.ProjectID.String())))

	foreign := *endpoint
	foreign.ToolsetID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	rejected := httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), rejected, token, ti.serverURL.String(), &foreign)
	require.Error(t, err, "the credential's grants bound the resources it reaches")
	require.NotEmpty(t, rejected.Header().Get("WWW-Authenticate"))

	_, err = agents.SuspendAgent(t.Context(), agentrepo.SuspendAgentParams{OrganizationID: ac.ActiveOrganizationID, ID: agent.ID})
	require.NoError(t, err)
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
	require.Error(t, err, "admission reads the agent's live lifecycle")
}

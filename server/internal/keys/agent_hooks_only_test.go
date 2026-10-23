package keys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentgen "github.com/speakeasy-api/gram/server/gen/agents"
	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/agentmanagement"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The dashboard creates an agent identity with no MCP server as one that only
// sends hook events and syncs device-agent plugins. This walks that flow end
// to end: the policy it writes, the unscoped discovery it reads, and the key
// it issues from that discovery.
func TestKeysService_HooksOnlyAgentKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestKeysService(t)
	authCtx := testAuthContext(t, ctx)
	require.NotNil(t, authCtx.ProjectID)
	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()

	caller := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	upsertGrant(t, ctx, ti, caller, authz.ScopeAgentWrite, "*")
	upsertGrant(t, ctx, ti, caller, authz.ScopeOrgHooksIngest, orgID)
	upsertGrant(t, ctx, ti, caller, authz.ScopeOrgDeviceAgentSync, orgID)

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential})
	management := agentmanagement.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, engine, audit.NewLogger(), nil, nil)

	agent, err := management.Create(ctx, &agentgen.CreatePayload{
		Name:      "Hooks only agent",
		ProjectID: &projectID,
		PolicyGrants: []*agentgen.AgentPolicyGrantForm{
			{Scope: string(authz.ScopeOrgHooksIngest), Effect: "allow", Selector: &agentgen.AgentPolicySelector{ResourceKind: authz.ResourceKindOrg, ResourceID: orgID, ProjectID: &projectID}},
			{Scope: string(authz.ScopeOrgDeviceAgentSync), Effect: "allow", Selector: &agentgen.AgentPolicySelector{ResourceKind: authz.ResourceKindOrg, ResourceID: orgID}},
		},
	})
	require.NoError(t, err)

	// No server is named, because neither grant names one.
	delegable, err := management.ListDelegableGrants(ctx, &agentgen.ListDelegableGrantsPayload{AgentID: agent.ID})
	require.NoError(t, err)
	scopes := make([]string, 0, len(delegable))
	requested := make([]*gen.AgentPolicyGrantForm, 0, len(delegable))
	for _, grant := range delegable {
		scopes = append(scopes, grant.Scope)
		requested = append(requested, &gen.AgentPolicyGrantForm{
			Scope:  grant.Scope,
			Effect: grant.Effect,
			Selector: &gen.AgentPolicySelector{
				ResourceKind: grant.Selector.ResourceKind,
				ResourceID:   grant.Selector.ResourceID,
				ProjectID:    grant.Selector.ProjectID,
			},
		})
	}
	require.ElementsMatch(t, []string{string(authz.ScopeOrgHooksIngest), string(authz.ScopeOrgDeviceAgentSync)}, scopes)

	created, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
		AgentID: &agent.ID, Name: "hooks only agent key", DelegatedGrantsVersion: new(2), RequestedGrants: requested,
	})
	require.NoError(t, err)
	require.NotNil(t, created.Key)

	authorized, err := ti.service.APIKeyAuth(t.Context(), *created.Key, agentAPIKeyScheme())
	require.NoError(t, err)
	require.NoError(t, engine.Require(authorized, authz.HooksIngestCheck(orgID, projectID)))
	require.NoError(t, engine.Require(authorized, authz.Check{Scope: authz.ScopeOrgDeviceAgentSync, ResourceKind: "", ResourceID: orgID, Dimensions: nil}))

	// Hook ingestion stays bound to the agent's project, and the key reaches
	// no MCP server.
	requireOopsCode(t, engine.Require(authorized, authz.HooksIngestCheck(orgID, uuid.NewString())), oops.CodeForbidden)
	requireOopsCode(t, engine.Require(authorized, authz.Check{Scope: authz.ScopeMCPConnect, ResourceKind: "", ResourceID: uuid.NewString(), Dimensions: nil}), oops.CodeForbidden)
}

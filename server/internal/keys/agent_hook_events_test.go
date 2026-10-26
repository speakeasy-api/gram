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
	"github.com/speakeasy-api/gram/server/internal/conv"
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

	// The stored policy is exactly these two grants, selectors included.
	stored, err := management.ListPolicyGrants(ctx, &agentgen.ListPolicyGrantsPayload{AgentID: agent.ID})
	require.NoError(t, err)
	type storedGrant struct {
		scope, resourceKind, resourceID, projectID string
	}
	got := make([]storedGrant, 0, len(stored))
	for _, grant := range stored {
		require.Equal(t, "allow", grant.Effect)
		got = append(got, storedGrant{scope: grant.Scope, resourceKind: grant.Selector.ResourceKind, resourceID: grant.Selector.ResourceID, projectID: conv.PtrValOr(grant.Selector.ProjectID, "")})
	}
	require.ElementsMatch(t, []storedGrant{
		{scope: string(authz.ScopeOrgHooksIngest), resourceKind: authz.ResourceKindOrg, resourceID: orgID, projectID: projectID},
		{scope: string(authz.ScopeOrgDeviceAgentSync), resourceKind: authz.ResourceKindOrg, resourceID: orgID, projectID: ""},
	}, got)

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

// Every new agent identity sends hook events by default. The dashboard
// creates one that reaches an MCP server with hook ingestion in its project
// beside that server, and its keys ask for both: the first from the new-agent
// wizard, later ones from the Issue a key wizard, each reading hook ingestion
// from unscoped discovery.
func TestKeysService_AgentWithServerSendsHookEventsByDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestKeysService(t)
	authCtx := testAuthContext(t, ctx)
	require.NotNil(t, authCtx.ProjectID)
	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	serverID := uuid.NewString()

	caller := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	upsertGrant(t, ctx, ti, caller, authz.ScopeAgentWrite, "*")
	upsertGrant(t, ctx, ti, caller, authz.ScopeMCPConnect, "*")
	upsertGrant(t, ctx, ti, caller, authz.ScopeOrgHooksIngest, orgID)

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential})
	management := agentmanagement.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, engine, audit.NewLogger(), nil, nil)

	agent, err := management.Create(ctx, &agentgen.CreatePayload{
		Name:      "Server agent",
		ProjectID: &projectID,
		PolicyGrants: []*agentgen.AgentPolicyGrantForm{
			{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &agentgen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: serverID, ProjectID: &projectID}},
			{Scope: string(authz.ScopeOrgHooksIngest), Effect: "allow", Selector: &agentgen.AgentPolicySelector{ResourceKind: authz.ResourceKindOrg, ResourceID: orgID, ProjectID: &projectID}},
		},
	})
	require.NoError(t, err)

	// The stored policy is the server and hook ingestion in the agent's
	// project, nothing more.
	stored, err := management.ListPolicyGrants(ctx, &agentgen.ListPolicyGrantsPayload{AgentID: agent.ID})
	require.NoError(t, err)
	type storedGrant struct {
		scope, resourceKind, resourceID, projectID string
	}
	got := make([]storedGrant, 0, len(stored))
	for _, grant := range stored {
		require.Equal(t, "allow", grant.Effect)
		got = append(got, storedGrant{scope: grant.Scope, resourceKind: grant.Selector.ResourceKind, resourceID: grant.Selector.ResourceID, projectID: conv.PtrValOr(grant.Selector.ProjectID, "")})
	}
	require.ElementsMatch(t, []storedGrant{
		{scope: string(authz.ScopeMCPConnect), resourceKind: authz.ResourceKindMCP, resourceID: serverID, projectID: projectID},
		{scope: string(authz.ScopeOrgHooksIngest), resourceKind: authz.ResourceKindOrg, resourceID: orgID, projectID: projectID},
	}, got)

	// Hook ingestion names no server, so unscoped discovery is what returns
	// it, still bounded to the agent's project.
	delegable, err := management.ListDelegableGrants(ctx, &agentgen.ListDelegableGrantsPayload{AgentID: agent.ID})
	require.NoError(t, err)
	var hookGrant *agentgen.AgentPolicyGrantForm
	for _, grant := range delegable {
		if grant.Scope == string(authz.ScopeOrgHooksIngest) {
			hookGrant = grant
		}
	}
	require.NotNil(t, hookGrant)
	require.Equal(t, projectID, conv.PtrValOr(hookGrant.Selector.ProjectID, ""))

	for _, issue := range []struct {
		name    string
		version int
	}{
		{name: "first key", version: 2},
		{name: "later key", version: 3},
	} {
		created, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			AgentID: &agent.ID, Name: issue.name, DelegatedGrantsVersion: new(issue.version),
			RequestedGrants: []*gen.AgentPolicyGrantForm{
				{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: serverID, ProjectID: &projectID}},
				{Scope: hookGrant.Scope, Effect: hookGrant.Effect, Selector: &gen.AgentPolicySelector{ResourceKind: hookGrant.Selector.ResourceKind, ResourceID: hookGrant.Selector.ResourceID, ProjectID: hookGrant.Selector.ProjectID}},
			},
		})
		require.NoError(t, err, issue.name)
		require.NotNil(t, created.Key, issue.name)

		authorized, err := ti.service.APIKeyAuth(t.Context(), *created.Key, agentAPIKeyScheme())
		require.NoError(t, err, issue.name)
		require.NoError(t, engine.Require(authorized, authz.HooksIngestCheck(orgID, projectID)), issue.name)
		require.NoError(t, engine.Require(authorized, authz.MCPCheck(authz.ScopeMCPConnect, serverID, projectID)), issue.name)
		requireOopsCode(t, engine.Require(authorized, authz.HooksIngestCheck(orgID, uuid.NewString())), oops.CodeForbidden)
	}
}

// Hook ingestion in a new agent's policy comes out of its creator's own
// authority, like every initial policy grant. The dashboard leaves it out for
// a creator who lacks it, and that agent still reaches its servers.
func TestKeysService_DefaultHookEventsNeedTheCreatorsGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestKeysService(t)
	authCtx := testAuthContext(t, ctx)
	require.NotNil(t, authCtx.ProjectID)
	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	serverID := uuid.NewString()

	caller := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	upsertGrant(t, ctx, ti, caller, authz.ScopeAgentWrite, "*")
	upsertGrant(t, ctx, ti, caller, authz.ScopeMCPConnect, "*")

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential})
	management := agentmanagement.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, engine, audit.NewLogger(), nil, nil)

	connect := &agentgen.AgentPolicyGrantForm{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &agentgen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: serverID, ProjectID: &projectID}}
	hooks := &agentgen.AgentPolicyGrantForm{Scope: string(authz.ScopeOrgHooksIngest), Effect: "allow", Selector: &agentgen.AgentPolicySelector{ResourceKind: authz.ResourceKindOrg, ResourceID: orgID, ProjectID: &projectID}}

	_, err := management.Create(ctx, &agentgen.CreatePayload{Name: "Hook events agent", ProjectID: &projectID, PolicyGrants: []*agentgen.AgentPolicyGrantForm{connect, hooks}})
	requireOopsCode(t, err, oops.CodeForbidden)

	agent, err := management.Create(ctx, &agentgen.CreatePayload{Name: "Server only agent", ProjectID: &projectID, PolicyGrants: []*agentgen.AgentPolicyGrantForm{connect}})
	require.NoError(t, err)
	delegable, err := management.ListDelegableGrants(ctx, &agentgen.ListDelegableGrantsPayload{AgentID: agent.ID})
	require.NoError(t, err)
	require.NotEmpty(t, delegable)
	for _, grant := range delegable {
		require.NotEqual(t, string(authz.ScopeOrgHooksIngest), grant.Scope)
	}
}

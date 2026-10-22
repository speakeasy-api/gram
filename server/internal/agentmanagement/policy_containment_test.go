package agentmanagement

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestPolicyWritesRequireCallerAndOwnerContainment(t *testing.T) {
	t.Parallel()
	read := authz.NewGrant(authz.ScopeMCPRead, "server-a")
	write := authz.NewGrant(authz.ScopeMCPWrite, "server-a")
	wildcard := authz.NewGrant(authz.ScopeMCPRead, "*")
	toolBlock := authz.NewGrantWithSelector(authz.ScopeMCPBlockedConnect, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
		authz.SelectorKeyResourceID:   "server-a", authz.SelectorKeyTool: "hidden-tool",
	})
	toolRead := authz.NewGrantWithSelector(authz.ScopeMCPRead, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
		authz.SelectorKeyResourceID:   "server-a", authz.SelectorKeyTool: "allowed-tool",
	})
	for _, tc := range []struct {
		name          string
		caller, owner []authz.Grant
		requested     authz.Grant
		denial        string
	}{
		{name: "implied read and connect", caller: []authz.Grant{write}, owner: []authz.Grant{write}, requested: read},
		{name: "wildcard parents", caller: []authz.Grant{wildcard}, owner: []authz.Grant{wildcard}, requested: read},
		{name: "management is not resource authority", owner: []authz.Grant{write}, requested: read, denial: "your permissions"},
		{name: "admin cannot bypass owner", caller: []authz.Grant{write}, requested: read, denial: "owner's permissions"},
		{name: "wildcard request exceeds caller", caller: []authz.Grant{read}, owner: []authz.Grant{wildcard}, requested: wildcard, denial: "your permissions"},
		{name: "write exceeds read", caller: []authz.Grant{read}, owner: []authz.Grant{write}, requested: write, denial: "your permissions"},
		{name: "caller implied connect exclusion", caller: []authz.Grant{write, toolBlock}, owner: []authz.Grant{write}, requested: read, denial: "your permissions"},
		{name: "owner implied connect exclusion", caller: []authz.Grant{write}, owner: []authz.Grant{write, toolBlock}, requested: read, denial: "owner's permissions"},
		{name: "nonoverlapping tool", caller: []authz.Grant{write, toolBlock}, owner: []authz.Grant{write, toolBlock}, requested: toolRead},
		{name: "tool narrowing cannot be omitted", caller: []authz.Grant{toolRead}, owner: []authz.Grant{write}, requested: read, denial: "your permissions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, operation := range []string{"initial", "create", "update"} {
				t.Run(operation, func(t *testing.T) {
					t.Parallel()
					f := newDelegableFixture(t)
					f.grant(t, "caller", authz.ScopeAgentWrite, authz.NewSelector(authz.ScopeAgentWrite, "*"))
					for user, grants := range map[string][]authz.Grant{"caller": tc.caller, "owner": tc.owner} {
						for _, grant := range grants {
							f.grant(t, user, grant.Scope, grant.Selector)
						}
					}
					ctx := validatedHumanContext(t, "org-delegable", "caller")
					before, err := repo.New(f.db).ListAgentPolicyGrants(ctx, repo.ListAgentPolicyGrantsParams{OrganizationID: "org-delegable", AgentID: f.agentID})
					require.NoError(t, err)
					var grantID string
					if operation == "update" {
						grantID = f.grant(t, "agent", authz.ScopeProjectRead, authz.NewSelector(authz.ScopeProjectRead, "existing-project")).String()
						before, err = repo.New(f.db).ListAgentPolicyGrants(ctx, repo.ListAgentPolicyGrantsParams{OrganizationID: "org-delegable", AgentID: f.agentID})
						require.NoError(t, err)
					}
					form := &gen.AgentPolicyGrantForm{Scope: string(tc.requested.Scope), Effect: "allow", Selector: policySelectorView(tc.requested.Selector)}
					switch operation {
					case "initial":
						_, err = f.service.Create(ctx, &gen.CreatePayload{Name: "New policy agent", OwnerUserID: new("owner"), PolicyGrants: []*gen.AgentPolicyGrantForm{form}})
					case "create":
						_, err = f.service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{AgentID: f.agentID.String(), Scope: form.Scope, Effect: form.Effect, Selector: form.Selector})
					case "update":
						_, err = f.service.UpdatePolicyGrant(ctx, &gen.UpdatePolicyGrantPayload{AgentID: f.agentID.String(), GrantID: grantID, Scope: form.Scope, Effect: form.Effect, Selector: form.Selector})
					}
					if tc.denial == "" {
						require.NoError(t, err)
						require.NotEmpty(t, agentWebhookOutboxActions(t, f.db, "org-delegable"))
						return
					}
					requireOopsCode(t, err, oops.CodeForbidden)
					require.Contains(t, err.Error(), tc.denial)
					var public *oops.ShareableError
					require.ErrorAs(t, err, &public)
					wire, encodeErr := json.Marshal(public.AsGoa(ctx))
					require.NoError(t, encodeErr)
					for _, hidden := range []string{"server-a", "hidden-tool", "existing-project", "mcp:", "user:"} {
						require.NotContains(t, err.Error(), hidden)
						require.NotContains(t, string(wire), hidden)
					}
					// No policy, agent, audit or webhook state survives a denial.
					after, loadErr := repo.New(f.db).ListAgentPolicyGrants(ctx, repo.ListAgentPolicyGrantsParams{OrganizationID: "org-delegable", AgentID: f.agentID})
					require.NoError(t, loadErr)
					require.Equal(t, before, after)
					agents, loadErr := repo.New(f.db).ListManagedAgents(ctx, repo.ListManagedAgentsParams{
						OrganizationID: "org-delegable",
						// Empty rather than nil: the query guards on cardinality(),
						// and a NULL array there drops every row.
						Lifecycles:   []string{},
						OwnerUserIds: []string{},
						PageLimit:    100,
					})
					require.NoError(t, loadErr)
					require.Len(t, agents, 1)
					require.Empty(t, auditActions(t, f.db, "org-delegable", ""))
					require.Empty(t, agentWebhookOutboxActions(t, f.db, "org-delegable"))
				})
			}
		})
	}
}

func TestPolicyWritesReloadPermissionsAfterOwnershipTransfer(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	seedOrganizationUser(t, f.db, "org-delegable", "replacement")
	f.grant(t, "owner", authz.ScopeMCPRead, authz.NewSelector(authz.ScopeMCPRead, "server-a"))
	ctx := validatedHumanContext(t, "org-delegable", "owner")
	payload := &gen.CreatePolicyGrantPayload{AgentID: f.agentID.String(), Scope: string(authz.ScopeMCPRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "server-a"}}
	grant, err := f.service.CreatePolicyGrant(ctx, payload)
	require.NoError(t, err)
	_, err = f.service.Transfer(ctx, &gen.TransferPayload{AgentID: f.agentID.String(), OwnerUserID: "replacement"})
	require.NoError(t, err)
	// The former owner can still administer this agent explicitly, but the
	// current owner's live resource authority is now the policy-write boundary.
	f.grant(t, "owner", authz.ScopeAgentWrite, authz.NewSelector(authz.ScopeAgentWrite, f.agentID.String()))
	update := &gen.UpdatePolicyGrantPayload{AgentID: payload.AgentID, GrantID: grant.ID, Scope: payload.Scope, Effect: payload.Effect, Selector: payload.Selector}
	_, err = f.service.UpdatePolicyGrant(ctx, update)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Contains(t, err.Error(), "owner's permissions")
	f.grant(t, "replacement", authz.ScopeMCPRead, authz.NewSelector(authz.ScopeMCPRead, "server-a"))
	_, err = f.service.UpdatePolicyGrant(ctx, update)
	require.NoError(t, err)
	// Cached request grants do not rescue a permission removed in the database.
	f.grant(t, "owner", authz.ScopeMCPBlockedRead, authz.NewSelector(authz.ScopeMCPBlockedRead, "server-a"))
	_, err = f.service.UpdatePolicyGrant(ctx, update)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Contains(t, err.Error(), "your permissions")
	// Removing a policy remains possible even when neither human can delegate it.
	require.NoError(t, f.service.DeletePolicyGrant(ctx, &gen.DeletePolicyGrantPayload{AgentID: payload.AgentID, GrantID: grant.ID}))
}

func TestPolicyWriteRejectsCrossTenantAgentWithoutPermissionDetails(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	seedOrganization(t, f.db, "org-foreign")
	seedOrganizationUser(t, f.db, "org-foreign", "foreign-owner")
	agent := createAgent(t, f.db, "org-foreign", "foreign-owner", "Private agent")
	f.grant(t, "caller", authz.ScopeAgentWrite, authz.NewSelector(authz.ScopeAgentWrite, "*"))
	seedGrant(t, t.Context(), f.db, "org-foreign", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeMCPRead, "*")
	_, err := f.service.CreatePolicyGrant(validatedHumanContext(t, "org-delegable", "caller"), &gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: string(authz.ScopeMCPRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "*"}})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, oops.CodeForbidden.UserMessage(), err.Error())
	require.Empty(t, auditActions(t, f.db, "org-foreign", ""))
}

func TestPolicyContainmentRespectsDirectGrantPrecedence(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	ctx := validatedHumanContext(t, "org-delegable", "owner")
	seedGrant(t, ctx, f.db, "org-delegable", authz.AllUsersPrincipal(), authz.ScopeMCPBlockedRead, "server-a")
	f.grant(t, "owner", authz.ScopeMCPRead, authz.NewSelector(authz.ScopeMCPRead, "server-a"))
	payload := &gen.CreatePolicyGrantPayload{AgentID: f.agentID.String(), Scope: string(authz.ScopeMCPRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "server-a"}}
	grant, err := f.service.CreatePolicyGrant(ctx, payload)
	require.NoError(t, err, "a direct concrete grant outranks an inherited block")
	f.grant(t, "owner", authz.ScopeMCPBlockedRead, authz.NewSelector(authz.ScopeMCPBlockedRead, "server-a"))
	_, err = f.service.UpdatePolicyGrant(ctx, &gen.UpdatePolicyGrantPayload{AgentID: payload.AgentID, GrantID: grant.ID, Scope: payload.Scope, Effect: payload.Effect, Selector: payload.Selector})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, []string{"agent:policy_grant_create"}, auditActions(t, f.db, "org-delegable", f.agentID.String()))
}

func TestInitialPolicyContainmentFailureRollsBackEarlierGrants(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	f.grant(t, "owner", authz.ScopeMCPRead, authz.NewSelector(authz.ScopeMCPRead, "server-a"))
	ctx := validatedHumanContext(t, "org-delegable", "owner")
	_, err := f.service.Create(ctx, &gen.CreatePayload{Name: "Partially allowed policy", PolicyGrants: []*gen.AgentPolicyGrantForm{
		{Scope: string(authz.ScopeMCPRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "server-a"}},
		{Scope: string(authz.ScopeMCPRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "server-b"}},
	}})
	requireOopsCode(t, err, oops.CodeForbidden)
	agents, err := repo.New(f.db).ListManagedAgents(ctx, repo.ListManagedAgentsParams{
		OrganizationID: "org-delegable",
		// Empty rather than nil: the query guards on cardinality(),
		// and a NULL array there drops every row.
		Lifecycles:   []string{},
		OwnerUserIds: []string{},
		PageLimit:    100,
	})
	require.NoError(t, err)
	require.Len(t, agents, 1)
	grants, err := repo.New(f.db).ListAgentPolicyGrants(ctx, repo.ListAgentPolicyGrantsParams{OrganizationID: "org-delegable", AgentID: f.agentID})
	require.NoError(t, err)
	require.Empty(t, grants)
	require.Empty(t, auditActions(t, f.db, "org-delegable", ""))
	require.Empty(t, agentWebhookOutboxActions(t, f.db, "org-delegable"))
}

func TestPolicyWritesRequireActiveOwnerButDeletionAllowsRecovery(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	f.grant(t, "caller", authz.ScopeAgentWrite, authz.NewSelector(authz.ScopeAgentWrite, "*"))
	f.grant(t, "caller", authz.ScopeMCPRead, authz.NewSelector(authz.ScopeMCPRead, "server-a"))
	f.grant(t, "owner", authz.ScopeMCPRead, authz.NewSelector(authz.ScopeMCPRead, "server-a"))
	ctx := validatedHumanContext(t, "org-delegable", "caller")
	payload := &gen.CreatePolicyGrantPayload{AgentID: f.agentID.String(), Scope: string(authz.ScopeMCPRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "server-a"}}
	grant, err := f.service.CreatePolicyGrant(ctx, payload)
	require.NoError(t, err)
	require.NoError(t, orgrepo.New(f.db).DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: "org-delegable", UserID: conv.ToPGText("owner")}))
	_, err = f.service.UpdatePolicyGrant(ctx, &gen.UpdatePolicyGrantPayload{AgentID: payload.AgentID, GrantID: grant.ID, Scope: payload.Scope, Effect: payload.Effect, Selector: payload.Selector})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, oops.CodeForbidden.UserMessage(), err.Error())
	require.NoError(t, f.service.DeletePolicyGrant(ctx, &gen.DeletePolicyGrantPayload{AgentID: payload.AgentID, GrantID: grant.ID}))
	require.Equal(t, []string{"agent:policy_grant_create", "agent:policy_grant_delete"}, auditActions(t, f.db, "org-delegable", f.agentID.String()))
}

func TestPolicyContainmentDoesNotLoadAnotherTenantsGrants(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	seedOrganization(t, f.db, "org-foreign")
	seedGrant(t, t.Context(), f.db, "org-foreign", urn.NewPrincipal(urn.PrincipalTypeUser, "owner"), authz.ScopeMCPRead, "*")
	_, err := f.service.CreatePolicyGrant(validatedHumanContext(t, "org-delegable", "owner"), &gen.CreatePolicyGrantPayload{AgentID: f.agentID.String(), Scope: string(authz.ScopeMCPRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "mcp", ResourceID: "*"}})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, auditActions(t, f.db, "org-delegable", ""))
}

package agentmanagement

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// allServersGrant is the grant the dashboard's "All servers" choice writes:
// every MCP server in one project, including servers added later.
func allServersGrant(projectID string) *gen.AgentPolicyGrantForm {
	return &gen.AgentPolicyGrantForm{
		Scope:  string(authz.ScopeMCPConnect),
		Effect: "allow",
		Selector: policySelectorView(authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID,
		}),
	}
}

func blockedServer(serverID, projectID string) authz.Selector {
	selector := authz.NewSelector(authz.ScopeMCPBlockedConnect, serverID)
	if projectID != "" {
		selector[authz.SelectorKeyProjectID] = projectID
	}
	return selector
}

// A member holds mcp:connect on every server, but a block on one server in
// the project keeps "every server in the project" from fitting within their
// permissions. Creating the agent with that grant is refused.
func TestCreateWithAllServersRefusedWhenCallerIsBlockedFromOneServer(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	projectID := uuid.NewString()
	f.grant(t, "caller", authz.ScopeMCPConnect, authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource))
	f.grant(t, "caller", authz.ScopeMCPBlockedConnect, blockedServer("server-b", ""))
	ctx := validatedHumanContext(t, "org-delegable", "caller")

	_, err := f.service.Create(ctx, &gen.CreatePayload{
		Name:         "All servers agent",
		PolicyGrants: []*gen.AgentPolicyGrantForm{allServersGrant(projectID)},
	})

	requireOopsCode(t, err, oops.CodeForbidden)
	require.Contains(t, err.Error(), "exceeds your permissions")
}

// The check answers what creating the agent would answer, for the same
// grant, without creating anything.
func TestCheckPolicyGrantsForNewAgentMatchesCreate(t *testing.T) {
	t.Parallel()
	projectID := uuid.NewString()
	otherProjectID := uuid.NewString()
	everyServer := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource)
	for _, tc := range []struct {
		name    string
		grants  []authz.Grant
		allowed bool
	}{
		{
			name:    "every server",
			grants:  []authz.Grant{authz.NewGrantWithSelector(authz.ScopeMCPConnect, everyServer)},
			allowed: true,
		},
		{
			name: "blocked from one server",
			grants: []authz.Grant{
				authz.NewGrantWithSelector(authz.ScopeMCPConnect, everyServer),
				authz.NewGrantWithSelector(authz.ScopeMCPBlockedConnect, blockedServer("server-b", "")),
			},
			allowed: false,
		},
		{
			name: "blocked from a server in another project",
			grants: []authz.Grant{
				authz.NewGrantWithSelector(authz.ScopeMCPConnect, everyServer),
				authz.NewGrantWithSelector(authz.ScopeMCPBlockedConnect, blockedServer("server-b", otherProjectID)),
			},
			allowed: true,
		},
		{
			name:    "named servers only",
			grants:  []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, "server-a")},
			allowed: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			for _, grant := range tc.grants {
				f.grant(t, "caller", grant.Scope, grant.Selector)
			}
			ctx := validatedHumanContext(t, "org-delegable", "caller")

			checked, err := f.service.CheckPolicyGrants(ctx, &gen.CheckPolicyGrantsPayload{
				PolicyGrants: []*gen.AgentPolicyGrantForm{allServersGrant(projectID)},
			})
			require.NoError(t, err)
			require.Len(t, checked.Results, 1)
			require.Equal(t, tc.allowed, checked.Results[0].Allowed)
			if tc.allowed {
				require.Nil(t, checked.Results[0].LimitedBy)
			}
			if !tc.allowed {
				require.Equal(t, new(string(policyLimitCaller)), checked.Results[0].LimitedBy)
			}
			// Checking writes nothing: no audit entry and no webhook.
			require.Empty(t, auditActions(t, f.db, "org-delegable", ""))
			require.Empty(t, agentWebhookOutboxActions(t, f.db, "org-delegable"))

			_, err = f.service.Create(ctx, &gen.CreatePayload{
				Name:         "All servers agent",
				PolicyGrants: []*gen.AgentPolicyGrantForm{allServersGrant(projectID)},
			})
			if tc.allowed {
				require.NoError(t, err)
				return
			}
			requireOopsCode(t, err, oops.CodeForbidden)
		})
	}
}

// For an existing agent the owner's permissions bound the grant too, and the
// check says whose permissions it exceeds, as the write's refusal does.
func TestCheckPolicyGrantsForExistingAgentMatchesCreatePolicyGrant(t *testing.T) {
	t.Parallel()
	projectID := uuid.NewString()
	everyServer := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource)
	for _, tc := range []struct {
		name          string
		caller, owner []authz.Selector
		ownerBlock    bool
		limitedBy     policyLimit
		denial        string
	}{
		{name: "both reach every server", caller: []authz.Selector{everyServer}, owner: []authz.Selector{everyServer}, limitedBy: policyLimitNone},
		{name: "owner blocked from one server", caller: []authz.Selector{everyServer}, owner: []authz.Selector{everyServer}, ownerBlock: true, limitedBy: policyLimitOwner, denial: "owner's permissions"},
		{name: "caller reaches named servers only", caller: []authz.Selector{authz.NewSelector(authz.ScopeMCPConnect, "server-a")}, owner: []authz.Selector{everyServer}, limitedBy: policyLimitCaller, denial: "your permissions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDelegableFixture(t)
			f.grant(t, "caller", authz.ScopeAgentWrite, authz.NewSelector(authz.ScopeAgentWrite, authz.WildcardResource))
			for _, selector := range tc.caller {
				f.grant(t, "caller", authz.ScopeMCPConnect, selector)
			}
			for _, selector := range tc.owner {
				f.grant(t, "owner", authz.ScopeMCPConnect, selector)
			}
			if tc.ownerBlock {
				f.grant(t, "owner", authz.ScopeMCPBlockedConnect, blockedServer("server-b", ""))
			}
			ctx := validatedHumanContext(t, "org-delegable", "caller")
			before, err := repo.New(f.db).ListAgentPolicyGrants(ctx, repo.ListAgentPolicyGrantsParams{OrganizationID: "org-delegable", AgentID: f.agentID})
			require.NoError(t, err)

			checked, err := f.service.CheckPolicyGrants(ctx, &gen.CheckPolicyGrantsPayload{
				AgentID:      new(f.agentID.String()),
				PolicyGrants: []*gen.AgentPolicyGrantForm{allServersGrant(projectID)},
			})
			require.NoError(t, err)
			require.Len(t, checked.Results, 1)
			require.Equal(t, tc.limitedBy == policyLimitNone, checked.Results[0].Allowed)
			after, err := repo.New(f.db).ListAgentPolicyGrants(ctx, repo.ListAgentPolicyGrantsParams{OrganizationID: "org-delegable", AgentID: f.agentID})
			require.NoError(t, err)
			require.Equal(t, before, after)

			grant := allServersGrant(projectID)
			_, err = f.service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{AgentID: f.agentID.String(), Scope: grant.Scope, Effect: grant.Effect, Selector: grant.Selector})
			if tc.limitedBy == policyLimitNone {
				require.Nil(t, checked.Results[0].LimitedBy)
				require.NoError(t, err)
				return
			}
			require.Equal(t, new(string(tc.limitedBy)), checked.Results[0].LimitedBy)
			requireOopsCode(t, err, oops.CodeForbidden)
			require.Contains(t, err.Error(), tc.denial)
		})
	}
}

// Checking grants on an agent needs the same authority as writing them.
func TestCheckPolicyGrantsRequiresAgentSetupAuthority(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	f.grant(t, "caller", authz.ScopeMCPConnect, authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource))
	ctx := validatedHumanContext(t, "org-delegable", "caller")

	_, err := f.service.CheckPolicyGrants(ctx, &gen.CheckPolicyGrantsPayload{
		AgentID:      new(f.agentID.String()),
		PolicyGrants: []*gen.AgentPolicyGrantForm{allServersGrant(uuid.NewString())},
	})

	requireOopsCode(t, err, oops.CodeForbidden)
}

// A grant no write would accept is refused before anything is checked.
func TestCheckPolicyGrantsRejectsInvalidGrant(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	ctx := validatedHumanContext(t, "org-delegable", "caller")
	grant := allServersGrant(uuid.NewString())
	grant.Scope = string(authz.ScopeMCPBlockedConnect)

	_, err := f.service.CheckPolicyGrants(ctx, &gen.CheckPolicyGrantsPayload{
		PolicyGrants: []*gen.AgentPolicyGrantForm{grant},
	})

	requireOopsCode(t, err, oops.CodeBadRequest)
}

// A new agent created for another owner is bounded by that owner too, and
// naming one needs agent:write, as agents.create does.
func TestCheckPolicyGrantsForAnotherOwnerMatchesCreate(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	projectID := uuid.NewString()
	everyServer := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource)
	f.grant(t, "caller", authz.ScopeMCPConnect, everyServer)
	f.grant(t, "owner", authz.ScopeMCPConnect, everyServer)
	f.grant(t, "owner", authz.ScopeMCPBlockedConnect, blockedServer("server-b", ""))
	ctx := validatedHumanContext(t, "org-delegable", "caller")
	payload := &gen.CheckPolicyGrantsPayload{
		OwnerUserID:  new("owner"),
		PolicyGrants: []*gen.AgentPolicyGrantForm{allServersGrant(projectID)},
	}

	_, err := f.service.CheckPolicyGrants(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	f.grant(t, "caller", authz.ScopeAgentWrite, authz.NewSelector(authz.ScopeAgentWrite, authz.WildcardResource))
	checked, err := f.service.CheckPolicyGrants(ctx, payload)
	require.NoError(t, err)
	require.Len(t, checked.Results, 1)
	require.False(t, checked.Results[0].Allowed)
	require.Equal(t, new(string(policyLimitOwner)), checked.Results[0].LimitedBy)

	_, err = f.service.Create(ctx, &gen.CreatePayload{
		Name:         "Owned elsewhere",
		OwnerUserID:  new("owner"),
		PolicyGrants: []*gen.AgentPolicyGrantForm{allServersGrant(projectID)},
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Contains(t, err.Error(), "owner's permissions")
}

// An existing agent's owner is its own, so naming another is refused.
func TestCheckPolicyGrantsRejectsOwnerWithAgent(t *testing.T) {
	t.Parallel()
	f := newDelegableFixture(t)
	ctx := validatedHumanContext(t, "org-delegable", "caller")

	_, err := f.service.CheckPolicyGrants(ctx, &gen.CheckPolicyGrantsPayload{
		AgentID:      new(f.agentID.String()),
		OwnerUserID:  new("owner"),
		PolicyGrants: []*gen.AgentPolicyGrantForm{allServersGrant(uuid.NewString())},
	})

	requireOopsCode(t, err, oops.CodeBadRequest)
}

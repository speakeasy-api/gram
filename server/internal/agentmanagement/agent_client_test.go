package agentmanagement

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Each delegated grant decides the same permissions on another member's agent
// whichever evaluator decides, including owner selectors and the binding of
// "self" to the caller.
func TestDelegatedAgentPermissionsAgreeAcrossEvaluators(t *testing.T) {
	t.Parallel()
	type grant struct {
		scope    authz.Scope
		resource string
		owner    string
	}
	cases := []struct {
		name   string
		grants []grant
		want   AgentPermissions
	}{
		{name: "no grants", grants: nil, want: AgentPermissions{}},
		{name: "read this agent", grants: []grant{{authz.ScopeAgentRead, "agent", ""}}, want: AgentPermissions{Read: true}},
		{name: "write implies read", grants: []grant{{authz.ScopeAgentWrite, "agent", ""}}, want: AgentPermissions{Read: true, Write: true}},
		{name: "write for the owner", grants: []grant{{authz.ScopeAgentWrite, "*", "owner"}}, want: AgentPermissions{Read: true, Write: true}},
		{name: "write for another owner", grants: []grant{{authz.ScopeAgentWrite, "*", "third"}}, want: AgentPermissions{}},
		{name: "self is the caller, not the owner", grants: []grant{{authz.ScopeAgentWrite, "*", authz.AgentOwnerSelf}}, want: AgentPermissions{}},
		{name: "this agent for the wrong owner", grants: []grant{{authz.ScopeAgentWrite, "agent", "third"}}, want: AgentPermissions{}},
		{name: "authorize every agent", grants: []grant{{authz.ScopeAgentAuthorize, "*", ""}}, want: AgentPermissions{Authorize: true}},
		{name: "transfer for the owner", grants: []grant{{authz.ScopeAgentTransfer, "*", "owner"}}, want: AgentPermissions{Transfer: true}},
		{name: "all four", grants: []grant{
			{authz.ScopeAgentWrite, "*", ""}, {authz.ScopeAgentAuthorize, "agent", ""}, {authz.ScopeAgentTransfer, "*", "owner"},
		}, want: AgentPermissions{Read: true, Write: true, Authorize: true, Transfer: true}},
	}
	for _, mode := range agentClientModes {
		for _, tc := range cases {
			t.Run(mode.String()+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				db := newTestDB(t)
				seedOrganization(t, db, "org-agent-client")
				for _, user := range []string{"caller", "owner", "third"} {
					seedOrganizationUser(t, db, "org-agent-client", user)
				}
				agent := createAgent(t, db, "org-agent-client", "owner", "Delegated agent")
				ctx := validatedHumanContext(t, "org-agent-client", "caller")
				for _, g := range tc.grants {
					resource := g.resource
					if resource == "agent" {
						resource = agent.ID.String()
					}
					owners := []string{}
					if g.owner != "" {
						owners = append(owners, g.owner)
					}
					seedGrant(t, ctx, db, "org-agent-client", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), g.scope, resource, owners...)
				}
				engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
				service := newClientTestService(t, db, engine, mode)

				human, err := service.authorizer.RequireHuman(ctx, db)
				require.NoError(t, err)
				permissions, err := service.authorizer.Permissions(ctx, human, agent)
				require.NoError(t, err)
				require.Equal(t, tc.want, permissions)

				requireAgentMatchesPermissions(t, ctx, service, agent.ID, tc.want)
			})
		}
	}
}

// A scope override narrows what a platform administrator may do with an
// agent in every evaluator, while the agent stays visible to their live
// grants.
func TestScopeOverrideNarrowsAgentPermissionsAcrossEvaluators(t *testing.T) {
	t.Parallel()
	for _, mode := range agentClientModes {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			seedOrganization(t, db, "org-agent-override")
			for _, user := range []string{"caller", "owner"} {
				seedOrganizationUser(t, db, "org-agent-override", user)
			}
			agent := createAgent(t, db, "org-agent-override", "owner", "Overridden agent")
			base := validatedHumanContext(t, "org-agent-override", "caller")
			seedGrant(t, base, db, "org-agent-override", urn.NewPrincipal(urn.PrincipalTypeUser, "caller"), authz.ScopeAgentWrite, "*")
			engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
			service := newClientTestService(t, db, engine, mode)
			authCtx := *mustAuthContext(t, base)
			authCtx.IsAdmin = true
			admin := contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(t.Context(), &authCtx), &authCtx, false)

			for override, want := range map[string]AgentPermissions{
				"agent:read":                      {Read: true},
				"agent:write":                     {Read: true, Write: true},
				"rbac-override:none":              {},
				"agent:read=" + agent.ID.String(): {Read: true},
			} {
				ctx := contextvalues.SetRBACScopeOverride(admin, override)
				human, err := service.authorizer.RequireHuman(ctx, db)
				require.NoError(t, err, override)
				permissions, err := service.authorizer.Permissions(ctx, human, agent)
				require.NoError(t, err, override)
				require.Equal(t, want, permissions, override)
				visible, err := service.authorizer.visible(ctx, human, agent)
				require.NoError(t, err, override)
				require.True(t, visible, override)
				requireAgentMatchesPermissions(t, ctx, service, agent.ID, want)
			}
		})
	}
}

// requireAgentMatchesPermissions checks that enforcing each owner predicate
// on the agent agrees with the projected permissions.
func requireAgentMatchesPermissions(t *testing.T, ctx context.Context, service *Service, agentID uuid.UUID, want AgentPermissions) {
	t.Helper()
	for predicate, allowed := range map[OwnerPredicate]bool{
		OwnedAgentRead:      want.Read,
		OwnedAgentSetup:     want.Write,
		OwnedAgentAuthorize: want.Authorize,
		OwnedAgentTransfer:  want.Transfer,
	} {
		_, _, err := service.authorizer.RequireAgent(ctx, service.db, agentID, predicate)
		if allowed {
			require.NoError(t, err, predicate)
		} else {
			requireOopsCode(t, err, oops.CodeForbidden)
		}
	}
}

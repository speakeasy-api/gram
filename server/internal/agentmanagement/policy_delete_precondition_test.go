package agentmanagement

import (
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAgentPolicyDeletePrecondition(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"scope", "selector"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			conn := newTestDB(t)
			seedOrganization(t, conn, "org-precondition")
			seedOrganizationUser(t, conn, "org-precondition", "owner")
			seedOrganizationUser(t, conn, "org-precondition", "other")
			agent := createAgent(t, conn, "org-precondition", "owner", "Policy agent")
			service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
			ctx := validatedHumanContext(t, "org-precondition", "owner")
			seedGrant(t, ctx, conn, "org-precondition", urn.NewPrincipal(urn.PrincipalTypeUser, "owner"), authz.ScopeProjectWrite, "*")
			created, err := service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectWrite), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"}})
			require.NoError(t, err)
			expected := &gen.AgentExpectedPolicy{Scope: created.Scope, Selector: created.Selector}
			update := &gen.UpdatePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: created.ID, Scope: created.Scope, Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"}}
			if change == "scope" {
				update.Scope = string(authz.ScopeProjectRead)
			} else {
				update.Selector.ResourceID = "project-two"
			}
			updated, err := service.UpdatePolicyGrant(ctx, update)
			require.NoError(t, err)
			deletion := &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: created.ID, ExpectedPolicy: expected}
			requireOopsCode(t, service.DeletePolicyGrant(ctx, deletion), oops.CodeConflict)
			listed, err := service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
			require.NoError(t, err)
			require.Equal(t, []*gen.AgentPolicyGrant{updated}, listed)
			require.Len(t, auditLogs(t, conn, "org-precondition", agent.ID.String()), 2, "conflict must not emit a delete audit")
			deletion.ExpectedPolicy = &gen.AgentExpectedPolicy{Scope: updated.Scope, Selector: updated.Selector}
			requireOopsCode(t, service.DeletePolicyGrant(validatedHumanContext(t, "org-precondition", "other"), deletion), oops.CodeForbidden)
			seedOrganization(t, conn, "org-foreign-precondition")
			seedOrganizationUser(t, conn, "org-foreign-precondition", "foreign")
			requireOopsCode(t, service.DeletePolicyGrant(validatedHumanContext(t, "org-foreign-precondition", "foreign"), deletion), oops.CodeForbidden)
			require.NoError(t, service.DeletePolicyGrant(ctx, deletion))
			listed, err = service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
			require.NoError(t, err)
			require.Empty(t, listed)
		})
	}
}

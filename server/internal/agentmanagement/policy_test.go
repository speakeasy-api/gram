package agentmanagement

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAgentPolicyCRUDIsExactAllowOnlyAndAudited(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-policy")
	seedOrganizationUser(t, conn, "org-policy", "owner")
	agent := createAgent(t, conn, "org-policy", "owner", "Policy agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-policy", "owner")
	seedGrant(t, ctx, conn, "org-policy", urn.NewPrincipal(urn.PrincipalTypeUser, "owner"), authz.ScopeProjectWrite, "*")

	created, err := service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectWrite), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "project-one"},
	})
	require.NoError(t, err)
	require.Equal(t, "allow", created.Effect)
	require.Equal(t, string(authz.ScopeProjectWrite), created.Scope)
	require.Equal(t, authz.ResourceKindProject, created.Selector.ResourceKind)

	listed, err := service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, created.ID, listed[0].ID)

	updated, err := service.UpdatePolicyGrant(ctx, &gen.UpdatePolicyGrantPayload{
		AgentID: agent.ID.String(), GrantID: created.ID, Scope: string(authz.ScopeProjectRead), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"},
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, string(authz.ScopeProjectRead), updated.Scope)

	// The list is allow-only and filtered to the agent principal, so finding the
	// row proves both its principal and its effect.
	persisted, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(t.Context(), accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: "org-policy", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String(),
	})
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(persisted, func(row accessrepo.ListPrincipalGrantsByOrgRow) bool {
		return row.ID.String() == created.ID
	}), "created grant must persist as an allow row for the agent principal")

	require.NoError(t, service.DeletePolicyGrant(ctx, &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: created.ID}))
	listed, err = service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Empty(t, listed)

	var actions []string
	for _, row := range auditLogs(t, conn, "org-policy", agent.ID.String()) {
		actions = append(actions, row.Action)
		require.Equal(t, "owner", row.ActorID)
		require.Equal(t, "user", row.ActorType)
		require.Equal(t, agent.ID.String(), row.SubjectID)
		require.Equal(t, "agent", row.SubjectType)
	}
	require.Equal(t, []string{"agent:policy_grant_create", "agent:policy_grant_update", "agent:policy_grant_delete"}, actions)
	require.Equal(t, actions, agentWebhookOutboxActions(t, conn, "org-policy"))
}

func TestAgentPolicyRejectsUnsafeDenyAndMalformedGrantsAtomically(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-validation")
	seedOrganizationUser(t, conn, "org-validation", "owner")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-validation", "owner")

	tests := []struct {
		name    string
		payload gen.CreatePolicyGrantPayload
	}{
		{name: "deny", payload: gen.CreatePolicyGrantPayload{Scope: string(authz.ScopeProjectRead), Effect: "deny", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"}}},
		{name: "unknown", payload: gen.CreatePolicyGrantPayload{Scope: "unknown:scope", Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"}}},
		{name: "root", payload: gen.CreatePolicyGrantPayload{Scope: string(authz.ScopeRoot), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "*", ResourceID: "*"}}},
		{name: "management", payload: gen.CreatePolicyGrantPayload{Scope: string(authz.ScopeAgentWrite), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindAgent, ResourceID: "*"}}},
		{name: "blocklist", payload: gen.CreatePolicyGrantPayload{Scope: string(authz.ScopeProjectBlockedRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"}}},
		{name: "malformed selector", payload: gen.CreatePolicyGrantPayload{Scope: string(authz.ScopeProjectRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: "*"}}},
		{name: "unclassified disposition", payload: gen.CreatePolicyGrantPayload{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: "*", Disposition: new(authz.DispositionUnclassified)}}},
		{name: "wildcard disposition", payload: gen.CreatePolicyGrantPayload{Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: "*", Disposition: new("*")}}},
	}
	for _, tt := range tests {
		agent := createAgent(t, conn, "org-validation", "owner", tt.name+" validation agent")
		tt.payload.AgentID = agent.ID.String()
		principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := service.CreatePolicyGrant(ctx, &tt.payload)
			requireOopsCode(t, err, oops.CodeBadRequest)

			var count int
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM principal_grants WHERE organization_id = $1 AND principal_urn = $2`, "org-validation", principal).Scan(&count)) //nolint:glint // notestingrawsql: verifies rejected writes are atomic
			require.Zero(t, count)
		})
	}
}

func TestAgentPolicyGrantIDsCannotCrossAgentOrTenant(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	for _, orgID := range []string{"org-one", "org-two"} {
		seedOrganization(t, conn, orgID)
		seedOrganizationUser(t, conn, orgID, "owner-"+orgID)
	}
	first := createAgent(t, conn, "org-one", "owner-org-one", "First agent")
	second := createAgent(t, conn, "org-one", "owner-org-one", "Second agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-one", "owner-org-one")
	seedGrant(t, ctx, conn, "org-one", urn.NewPrincipal(urn.PrincipalTypeUser, "owner-org-one"), authz.ScopeProjectRead, "*")
	grant, err := service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{
		AgentID: first.ID.String(), Scope: string(authz.ScopeProjectRead), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"},
	})
	require.NoError(t, err)

	_, err = service.UpdatePolicyGrant(ctx, &gen.UpdatePolicyGrantPayload{
		AgentID: second.ID.String(), GrantID: grant.ID, Scope: string(authz.ScopeProjectWrite), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"},
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	err = service.DeletePolicyGrant(ctx, &gen.DeletePolicyGrantPayload{AgentID: second.ID.String(), GrantID: grant.ID})
	requireOopsCode(t, err, oops.CodeNotFound)

	otherCtx := validatedHumanContext(t, "org-two", "owner-org-two")
	_, err = service.ListPolicyGrants(otherCtx, &gen.ListPolicyGrantsPayload{AgentID: first.ID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)

	listed, err := service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: first.ID.String()})
	require.NoError(t, err)
	require.Len(t, listed, 1)
}

func TestAgentPolicyRequiresExactWriteAuthorityForNonOwner(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-authority")
	seedOrganizationUser(t, conn, "org-authority", "owner")
	seedOrganizationUser(t, conn, "org-authority", "operator")
	agent := createAgent(t, conn, "org-authority", "owner", "Authority agent")
	engine := &fakeAuthorizationEngine{allowed: map[string]bool{}}
	service := newTestService(conn, engine)
	ctx := validatedHumanContext(t, "org-authority", "operator")

	allow(engine, authz.ScopeAgentRead, agent.ID)
	_, err := service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)

	allow(engine, authz.ScopeAgentWrite, agent.ID)
	skillID := uuid.NewString()
	for _, userID := range []string{"owner", "operator"} {
		seedGrant(t, ctx, conn, "org-authority", urn.NewPrincipal(urn.PrincipalTypeUser, userID), authz.ScopeSkillRead, skillID)
	}
	_, err = service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeSkillRead), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindSkill, ResourceID: skillID},
	})
	require.NoError(t, err)
}

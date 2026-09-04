package agentmanagement

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestAgentPolicyCRUDIsExactAllowOnlyAndAudited(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-policy")
	seedOrganizationUser(t, conn, "org-policy", "owner")
	agent := createAgent(t, conn, "org-policy", "owner", "Policy agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-policy", "owner")

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

	var principal, effect string
	err = conn.QueryRow(t.Context(), `SELECT principal_urn, COALESCE(effect, 'allow') FROM principal_grants WHERE id = $1`, created.ID).Scan(&principal, &effect) //nolint:glint // notestingrawsql: verifies the persisted security boundary
	require.NoError(t, err)
	require.Equal(t, "agent:"+agent.ID.String(), principal)
	require.Equal(t, "allow", effect)

	require.NoError(t, service.DeletePolicyGrant(ctx, &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: created.ID}))
	listed, err = service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Empty(t, listed)

	rows, err := conn.Query(t.Context(), `SELECT action, actor_id, actor_type, subject_id, subject_type FROM audit_logs WHERE organization_id = $1 AND subject_id = $2 ORDER BY seq`, "org-policy", agent.ID.String()) //nolint:glint // notestingrawsql: verifies transactional attribution
	require.NoError(t, err)
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action, actorID, actorType, subjectID, subjectType string
		require.NoError(t, rows.Scan(&action, &actorID, &actorType, &subjectID, &subjectType))
		actions = append(actions, action)
		require.Equal(t, "owner", actorID)
		require.Equal(t, "user", actorType)
		require.Equal(t, agent.ID.String(), subjectID)
		require.Equal(t, "agent", subjectType)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"agent:policy_grant_create", "agent:policy_grant_update", "agent:policy_grant_delete"}, actions)
}

func TestAgentPolicyRejectsUnsafeDenyAndMalformedGrantsAtomically(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-validation")
	seedOrganizationUser(t, conn, "org-validation", "owner")
	agent := createAgent(t, conn, "org-validation", "owner", "Validation agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-validation", "owner")

	tests := []struct {
		name    string
		payload gen.CreatePolicyGrantPayload
	}{
		{name: "deny", payload: gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectRead), Effect: "deny", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"}}},
		{name: "unknown", payload: gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: "unknown:scope", Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"}}},
		{name: "root", payload: gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: string(authz.ScopeRoot), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: "*", ResourceID: "*"}}},
		{name: "management", payload: gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: string(authz.ScopeAgentWrite), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindAgent, ResourceID: "*"}}},
		{name: "blocklist", payload: gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectBlockedRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"}}},
		{name: "malformed selector", payload: gen.CreatePolicyGrantPayload{AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: "*"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.CreatePolicyGrant(ctx, &tt.payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}

	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM principal_grants WHERE organization_id = $1`, "org-validation").Scan(&count)) //nolint:glint // notestingrawsql: verifies rejected writes are atomic
	require.Zero(t, count)
}

func TestAgentPolicyDoesNotNormalizeOrDeleteExistingDenyRows(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-deny-row")
	seedOrganizationUser(t, conn, "org-deny-row", "owner")
	agent := createAgent(t, conn, "org-deny-row", "owner", "Deny row agent")
	service := newTestService(conn, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-deny-row", "owner")
	selector := []byte(`{"resource_kind":"project","resource_id":"*"}`)
	denyID := uuid.New()
	//nolint:glint // notestingrawsql: seeds a legacy deny row to test isolation
	_, err := conn.Exec(t.Context(), `INSERT INTO principal_grants (id, organization_id, principal_urn, scope, effect, selectors) VALUES ($1, $2, $3, $4, 'deny', $5)`,
		denyID, "org-deny-row", "agent:"+agent.ID.String(), string(authz.ScopeProjectRead), selector)
	require.NoError(t, err)

	listed, err := service.ListPolicyGrants(ctx, &gen.ListPolicyGrantsPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Empty(t, listed)
	_, err = service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeProjectRead), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: "*"},
	})
	requireOopsCode(t, err, oops.CodeConflict)
	err = service.DeletePolicyGrant(ctx, &gen.DeletePolicyGrantPayload{AgentID: agent.ID.String(), GrantID: denyID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)

	var effect string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT effect FROM principal_grants WHERE id = $1`, denyID).Scan(&effect)) //nolint:glint // notestingrawsql: verifies deny state was not normalized
	require.Equal(t, "deny", effect)
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
	_, err = service.CreatePolicyGrant(ctx, &gen.CreatePolicyGrantPayload{
		AgentID: agent.ID.String(), Scope: string(authz.ScopeSkillRead), Effect: "allow",
		Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindSkill, ResourceID: uuid.NewString()},
	})
	require.NoError(t, err)
}

package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestLoadAgentPolicyUsesOnlyExactSafeAgentGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	for _, orgID := range []string{"org-agent-policy", "org-other-policy"} {
		seedOrganization(t, ctx, conn, orgID)
		userID := "owner-" + orgID
		_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
			ID: userID, Email: userID + "@example.com", DisplayName: userID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
		})
		require.NoError(t, err)
		_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: orgID, UserID: conv.ToPGText(userID),
		})
		require.NoError(t, err)
	}

	q := agentsrepo.New(conn)
	agent, err := q.CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: "org-agent-policy", OwnerUserID: "owner-org-agent-policy", Name: "Runtime agent"})
	require.NoError(t, err)
	otherAgent, err := q.CreateAgent(ctx, agentsrepo.CreateAgentParams{OrganizationID: "org-agent-policy", OwnerUserID: "owner-org-agent-policy", Name: "Other runtime agent"})
	require.NoError(t, err)
	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())

	seedGrant(t, ctx, conn, "org-agent-policy", principal, ScopeProjectWrite, "project-one")
	seedGrant(t, ctx, conn, "org-agent-policy", principal, ScopeAgentWrite, agent.ID.String())
	seedGrant(t, ctx, conn, "org-agent-policy", urn.NewPrincipal(urn.PrincipalTypeAgent, otherAgent.ID.String()), ScopeProjectRead, "other-project")
	seedGrant(t, ctx, conn, "org-agent-policy", urn.NewPrincipal(urn.PrincipalTypeUser, "owner-org-agent-policy"), ScopeProjectRead, "human-project")
	seedGrant(t, ctx, conn, "org-agent-policy", AllUsersPrincipal(), ScopeProjectRead, "all-users-project")
	seedGrant(t, ctx, conn, "org-agent-policy", urn.NewPrincipal(urn.PrincipalTypeRole, "global:00000000-0000-0000-0000-000000000001"), ScopeProjectRead, "role-project")

	//nolint:glint // notestingrawsql: seeds rows that the narrow runtime resolver must reject
	_, err = conn.Exec(ctx, `INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, selectors) VALUES ($1, $2, $3, 'deny', $4), ($1, $2, $5, NULL, $6)`,
		"org-agent-policy", principal.String(), string(ScopeSkillRead), []byte(`{"resource_kind":"skill","resource_id":"*"}`), string(ScopeMCPRead), []byte(`{"resource_kind":"project","resource_id":"*"}`))
	require.NoError(t, err)

	grants, err := LoadAgentPolicy(ctx, conn, "org-agent-policy", principal)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, principal.String(), grants[0].PrincipalUrn)
	require.Equal(t, ScopeProjectWrite, grants[0].Scope)
	require.True(t, GrantsSatisfy(grants, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}), "safe implication must remain effective")
	require.False(t, GrantsSatisfy(grants, Check{Scope: ScopeAgentWrite, ResourceID: agent.ID.String()}), "agent policy must not gain management authority")
	require.False(t, GrantsSatisfy(grants, Check{Scope: ScopeProjectRead, ResourceID: "human-project"}), "owner grants must not be inherited")
	require.False(t, GrantsSatisfy(grants, Check{Scope: ScopeProjectRead, ResourceID: "all-users-project"}), "user:all grants must not be inherited")
	require.False(t, GrantsSatisfy(grants, Check{Scope: ScopeProjectRead, ResourceID: "role-project"}), "role grants must not be inherited")

	_, err = LoadAgentPolicy(ctx, conn, "org-other-policy", principal)
	require.Error(t, err)
	_, err = LoadAgentPolicy(ctx, conn, "org-agent-policy", urn.NewPrincipal(urn.PrincipalTypeAgent, "NOT-CANONICAL"))
	require.Error(t, err)

	rows, err := accessrepo.New(conn).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{OrganizationID: "org-agent-policy", PrincipalUrns: []string{principal.String()}})
	require.NoError(t, err)
	require.Greater(t, len(rows), len(grants), "generic grant behavior remains broader than the agent runtime resolver")
}

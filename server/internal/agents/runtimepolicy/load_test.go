package runtimepolicy

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// A role assigned to an agent widens the agent's own policy, but only with the
// scopes an agent can hold: the rest are dropped by the same runtime filter
// that guards direct agent policy, so a role cannot be used to hand an agent
// something it could never be granted directly.
func TestLoadAgentPolicyIncludesAssignedRoleGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newAgentRoleFixture(t, ctx)

	seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.agentPrincipal, authz.ScopeProjectRead, fixture.projectID)
	seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.rolePrincipal, authz.ScopeMCPConnect, fixture.resourceID)
	seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.rolePrincipal, authz.ScopeOrgAdmin, fixture.organizationID)

	grants, err := LoadAgentPolicy(ctx, fixture.db, fixture.organizationID, fixture.agentPrincipal)
	require.NoError(t, err)

	scopes := scopeSet(grants)
	require.Contains(t, scopes, authz.ScopeProjectRead, "the agent keeps its own policy")
	require.Contains(t, scopes, authz.ScopeMCPConnect, "an agent-runtime-safe role grant widens it")
	require.NotContains(t, scopes, authz.ScopeOrgAdmin, "a role grant an agent cannot hold is dropped, not granted")
}

// Removing the assignment takes the role's grants away again, and a deleted
// role stops granting without any assignment write at all.
func TestLoadAgentPolicyDropsRoleGrantsOnceUnassigned(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newAgentRoleFixture(t, ctx)
	seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.rolePrincipal, authz.ScopeMCPConnect, fixture.resourceID)

	grants, err := LoadAgentPolicy(ctx, fixture.db, fixture.organizationID, fixture.agentPrincipal)
	require.NoError(t, err)
	require.Contains(t, scopeSet(grants), authz.ScopeMCPConnect)

	require.NoError(t, accessrepo.New(fixture.db).SoftDeleteAgentRoleAssignmentsByRole(ctx, accessrepo.SoftDeleteAgentRoleAssignmentsByRoleParams{
		OrganizationID: fixture.organizationID,
		RoleUrn:        fixture.rolePrincipal.String(),
	}))

	grants, err = LoadAgentPolicy(ctx, fixture.db, fixture.organizationID, fixture.agentPrincipal)
	require.NoError(t, err)
	require.NotContains(t, scopeSet(grants), authz.ScopeMCPConnect)
}

// The batched loader has to agree with the single-agent one, or a consent
// surface would show reach the request path does not honour.
func TestLoadKnownAgentPoliciesIncludesAssignedRoleGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newAgentRoleFixture(t, ctx)
	seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.agentPrincipal, authz.ScopeProjectRead, fixture.projectID)
	seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.rolePrincipal, authz.ScopeMCPConnect, fixture.resourceID)
	seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.rolePrincipal, authz.ScopeOrgAdmin, fixture.organizationID)

	policies, err := LoadKnownAgentPolicies(ctx, fixture.db, fixture.organizationID, []uuid.UUID{fixture.agentID})
	require.NoError(t, err)

	scopes := scopeSet(policies[fixture.agentID])
	require.Contains(t, scopes, authz.ScopeProjectRead, "the agent keeps its own policy")
	require.Contains(t, scopes, authz.ScopeMCPConnect, "an agent-runtime-safe role grant widens it")
	require.NotContains(t, scopes, authz.ScopeOrgAdmin, "a role grant an agent cannot hold is dropped, not granted")
}

type agentRoleFixture struct {
	db             *pgxpool.Pool
	organizationID string
	projectID      string
	resourceID     string
	agentID        uuid.UUID
	agentPrincipal urn.Principal
	rolePrincipal  urn.Principal
}

func newAgentRoleFixture(t *testing.T, ctx context.Context) agentRoleFixture {
	t.Helper()

	db := newTestDB(t)
	organizationID := "org-agent-role-" + uuid.NewString()
	ownerUserID := "owner-" + uuid.NewString()
	seedOrganization(t, ctx, db, organizationID)

	_, err := usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: ownerUserID, Email: ownerUserID + "@example.com", DisplayName: ownerUserID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: conv.ToPGText(ownerUserID),
	})
	require.NoError(t, err)

	agent, err := agentsrepo.New(db).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: organizationID, OwnerUserID: ownerUserID, Name: "Agent role fixture",
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	role, err := accessrepo.New(db).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        "fixture-role",
		WorkosName:        "Fixture role",
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	assigned, err := accessrepo.New(db).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{
		OrganizationID: organizationID,
		RoleUrn:        role.RoleUrn,
		AgentID:        agent.ID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), assigned)

	rolePrincipal, err := urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)

	return agentRoleFixture{
		db:             db,
		organizationID: organizationID,
		projectID:      "project-" + uuid.NewString(),
		resourceID:     uuid.NewString(),
		agentID:        agent.ID,
		agentPrincipal: urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		rolePrincipal:  rolePrincipal,
	}
}

func scopeSet(grants []authz.Grant) []authz.Scope {
	scopes := make([]authz.Scope, 0, len(grants))
	for _, grant := range grants {
		scopes = append(scopes, grant.Scope)
	}
	return scopes
}

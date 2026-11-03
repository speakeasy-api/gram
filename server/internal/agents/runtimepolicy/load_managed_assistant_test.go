package runtimepolicy

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type managedAssistantBinding struct {
	projectID   uuid.UUID
	assistantID uuid.UUID
}

// seedManagedAssistantAgent places agentID in a new project and binds it to
// that project's managed assistant, the only shape allowed to keep org:read.
func seedManagedAssistantAgent(t *testing.T, ctx context.Context, db *pgxpool.Pool, organizationID string, agentID uuid.UUID) managedAssistantBinding {
	t.Helper()

	slug := "p-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: slug, Slug: slug, OrganizationID: organizationID,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(db).SetAgentProjectFixture(ctx, testrepo.SetAgentProjectFixtureParams{
		ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, OrganizationID: organizationID, ID: agentID,
	}))

	assistant, err := assistantsrepo.New(db).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       project.ID,
		OrganizationID:  organizationID,
		CreatedByUserID: pgtype.Text{},
		Name:            "Managed assistant",
		Model:           "test-model",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          "active",
	})
	require.NoError(t, err)
	_, err = identityrepo.New(db).CreateAssistantBinding(ctx, identityrepo.CreateAssistantBindingParams{
		OrganizationID: organizationID, ProjectID: project.ID, AssistantID: assistant.ID, AgentID: agentID,
	})
	require.NoError(t, err)
	require.NoError(t, assistantsrepo.New(db).CreateProjectManagedAssistant(ctx, assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID: project.ID, AssistantID: assistant.ID,
	}))
	return managedAssistantBinding{projectID: project.ID, assistantID: assistant.ID}
}

func loadBothPaths(t *testing.T, fixture agentRoleFixture) ([]authz.Scope, []authz.Scope) {
	t.Helper()
	direct, err := LoadAgentPolicy(t.Context(), fixture.db, fixture.organizationID, fixture.agentPrincipal)
	require.NoError(t, err)
	batched, err := LoadKnownAgentPolicies(t.Context(), fixture.db, fixture.organizationID, []uuid.UUID{fixture.agentID})
	require.NoError(t, err)
	return scopeSet(direct), scopeSet(batched[fixture.agentID])
}

// An ordinary agent cannot hold org:read through its own grants or a role,
// while MCP exclusions and other safe grants still load.
func TestAgentPolicyFiltersOrganizationReadForUnmanagedAgents(t *testing.T) {
	t.Parallel()
	for _, viaRole := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "role"}[viaRole], func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			fixture := newAgentRoleFixture(t, ctx)
			holder := fixture.agentPrincipal
			if viaRole {
				holder = fixture.rolePrincipal
			}
			seedGrant(t, ctx, fixture.db, fixture.organizationID, holder, authz.ScopeOrgRead, fixture.organizationID)
			seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.agentPrincipal, authz.ScopeProjectRead, fixture.projectID)
			seedGrant(t, ctx, fixture.db, fixture.organizationID, fixture.agentPrincipal, authz.ScopeMCPBlockedConnect, fixture.resourceID)

			for _, scopes := range func() [][]authz.Scope { d, b := loadBothPaths(t, fixture); return [][]authz.Scope{d, b} }() {
				require.NotContains(t, scopes, authz.ScopeOrgRead)
				require.Contains(t, scopes, authz.ScopeProjectRead)
				require.Contains(t, scopes, authz.ScopeMCPBlockedConnect)
			}
		})
	}
}

// An agent bound to a project's managed assistant keeps org:read on both
// loader paths, and loses it once the binding is tombstoned.
func TestAgentPolicyAdmitsOrganizationReadForManagedAssistantAgents(t *testing.T) {
	t.Parallel()
	for _, viaRole := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "role"}[viaRole], func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			fixture := newAgentRoleFixture(t, ctx)
			binding := seedManagedAssistantAgent(t, ctx, fixture.db, fixture.organizationID, fixture.agentID)
			holder := fixture.agentPrincipal
			if viaRole {
				holder = fixture.rolePrincipal
			}
			seedGrant(t, ctx, fixture.db, fixture.organizationID, holder, authz.ScopeOrgRead, fixture.organizationID)

			direct, batched := loadBothPaths(t, fixture)
			require.Contains(t, direct, authz.ScopeOrgRead)
			require.Contains(t, batched, authz.ScopeOrgRead)

			require.NoError(t, identityrepo.New(fixture.db).TombstoneAssistantBinding(ctx, identityrepo.TombstoneAssistantBindingParams{
				ProjectID: binding.projectID, AssistantID: binding.assistantID,
			}))
			direct, batched = loadBothPaths(t, fixture)
			require.NotContains(t, direct, authz.ScopeOrgRead)
			require.NotContains(t, batched, authz.ScopeOrgRead)
		})
	}
}

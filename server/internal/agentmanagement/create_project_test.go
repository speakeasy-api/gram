package agentmanagement

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func seedProject(t *testing.T, conn *pgxpool.Pool, organizationID, slug string) projectsrepo.Project {
	t.Helper()
	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: slug, Slug: slug, OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return project
}

func newProjectScopedService(t *testing.T, conn *pgxpool.Pool) *Service {
	t.Helper()
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := newTestService(conn, engine)
	service.features = &recordingAgentManagementFeatures{evaluation: feature.EvaluationEnabled}
	return service
}

func storedAgent(t *testing.T, conn *pgxpool.Pool, organizationID, agentID string) repo.Agent {
	t.Helper()
	agent, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{
		OrganizationID: organizationID, ID: uuid.MustParse(agentID),
	})
	require.NoError(t, err)
	return agent
}

// The binding is whatever the caller asked for, as it is for API keys — not
// whatever project the dashboard happened to be pointed at.
func TestCreateAgentRecordsRequestedProject(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-agent-project")
	seedOrganizationUser(t, conn, "org-agent-project", "owner")
	project := seedProject(t, conn, "org-agent-project", "agent-project-alpha")
	service := newProjectScopedService(t, conn)

	projectID := project.ID.String()
	ctx := validatedHumanContext(t, "org-agent-project", "owner")
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Project agent", ProjectID: &projectID})
	require.NoError(t, err)
	require.NotNil(t, created.ProjectID)
	require.Equal(t, projectID, *created.ProjectID)

	stored := storedAgent(t, conn, "org-agent-project", created.ID)
	require.True(t, stored.ProjectID.Valid)
	require.Equal(t, project.ID, stored.ProjectID.UUID)
}

// Omitting the binding means an organization-wide agent. It must not fall back
// to an active project, or "organization-wide" would be unrequestable from a
// dashboard that always has one selected.
func TestCreateAgentWithoutProjectIsOrganizationWide(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-agent-no-project")
	seedOrganizationUser(t, conn, "org-agent-no-project", "owner")
	seedProject(t, conn, "org-agent-no-project", "agent-project-unused")
	service := newProjectScopedService(t, conn)

	ctx := validatedHumanContext(t, "org-agent-no-project", "owner")
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Org wide agent"})
	require.NoError(t, err)
	require.Nil(t, created.ProjectID)

	stored := storedAgent(t, conn, "org-agent-no-project", created.ID)
	require.False(t, stored.ProjectID.Valid)
}

// An empty string is a cleared form field, not a malformed id.
func TestCreateAgentTreatsBlankProjectAsOrganizationWide(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-agent-blank-project")
	seedOrganizationUser(t, conn, "org-agent-blank-project", "owner")
	service := newProjectScopedService(t, conn)

	blank := ""
	ctx := validatedHumanContext(t, "org-agent-blank-project", "owner")
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Blank project agent", ProjectID: &blank})
	require.NoError(t, err)
	require.Nil(t, created.ProjectID)
}

// The composite foreign key pins the project to the agent's own organization,
// so a project belonging to another tenant is rejected by the database rather
// than by a check every call site has to remember.
func TestCreateAgentRejectsProjectFromAnotherOrganization(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-agent-tenant-a")
	seedOrganizationUser(t, conn, "org-agent-tenant-a", "owner")
	seedOrganization(t, conn, "org-agent-tenant-b")
	foreign := seedProject(t, conn, "org-agent-tenant-b", "agent-project-foreign")
	service := newProjectScopedService(t, conn)

	foreignID := foreign.ID.String()
	ctx := validatedHumanContext(t, "org-agent-tenant-a", "owner")
	_, err := service.Create(ctx, &gen.CreatePayload{Name: "Cross tenant agent", ProjectID: &foreignID})
	require.Error(t, err)

	// Another tenant's project is indistinguishable from one that does not
	// exist, and naming it is the client being wrong, not a fault.
	requireOopsCode(t, err, oops.CodeNotFound)

	stored, err := repo.New(conn).ListManagedAgents(t.Context(), "org-agent-tenant-a")
	require.NoError(t, err)
	require.Empty(t, stored, "a rejected create must leave no agent behind")
}

// The service rejects a cross-tenant binding before the insert, so this pins
// the database backstop directly: any writer that skips that check still
// cannot record one.
func TestAgentProjectBindingIsPinnedToItsOrganization(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-agent-pin-a")
	seedOrganizationUser(t, conn, "org-agent-pin-a", "owner")
	seedOrganization(t, conn, "org-agent-pin-b")
	foreign := seedProject(t, conn, "org-agent-pin-b", "agent-project-pin")

	_, err := repo.New(conn).CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org-agent-pin-a",
		OwnerUserID:    "owner",
		ProjectID:      uuid.NullUUID{UUID: foreign.ID, Valid: true},
		Name:           "Direct cross tenant agent",
	})
	require.Error(t, err)

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pgerrcode.ForeignKeyViolation, pgErr.Code)
	require.Equal(t, "agents_organization_id_project_id_fkey", pgErr.ConstraintName)
}

// Projects are soft deleted, so the composite key still matches one that is
// gone. An agent must not be born bound to a deleted project.
func TestCreateAgentRejectsDeletedProject(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-agent-deleted-project")
	seedOrganizationUser(t, conn, "org-agent-deleted-project", "owner")
	project := seedProject(t, conn, "org-agent-deleted-project", "agent-project-gone")
	_, err := projectsrepo.New(conn).DeleteProject(t.Context(), project.ID)
	require.NoError(t, err)
	service := newProjectScopedService(t, conn)

	projectID := project.ID.String()
	ctx := validatedHumanContext(t, "org-agent-deleted-project", "owner")
	_, err = service.Create(ctx, &gen.CreatePayload{Name: "Deleted project agent", ProjectID: &projectID})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)

	stored, err := repo.New(conn).ListManagedAgents(t.Context(), "org-agent-deleted-project")
	require.NoError(t, err)
	require.Empty(t, stored)
}

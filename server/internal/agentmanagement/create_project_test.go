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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
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

func humanContextInProject(t *testing.T, organizationID, userID string, projectID *uuid.UUID) context.Context {
	t.Helper()
	sessionID := "session-" + userID
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: organizationID,
		UserID:               userID,
		SessionID:            &sessionID,
		ProjectID:            projectID,
	})
	return contextvalues.WithValidatedGramSession(ctx, mustAuthContext(t, ctx), false)
}

func newProjectScopedService(t *testing.T, conn *pgxpool.Pool) *Service {
	t.Helper()
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	service := newTestService(conn, engine)
	service.features = &recordingAgentManagementFeatures{evaluation: feature.EvaluationEnabled}
	return service
}

// An agent belongs to the project it was created in, so a gateway derived from
// its grants has a project to enumerate without re-reading the key's selectors.
func TestCreateAgentRecordsCreatingProject(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-agent-project")
	seedOrganizationUser(t, conn, "org-agent-project", "owner")
	project := seedProject(t, conn, "org-agent-project", "agent-project-alpha")
	service := newProjectScopedService(t, conn)

	ctx := humanContextInProject(t, "org-agent-project", "owner", &project.ID)
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Project agent"})
	require.NoError(t, err)

	stored, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{
		OrganizationID: "org-agent-project", ID: uuid.MustParse(created.ID),
	})
	require.NoError(t, err)
	require.True(t, stored.ProjectID.Valid, "agent created inside a project must record it")
	require.Equal(t, project.ID, stored.ProjectID.UUID)
}

// Creating without an active project must still succeed: agents predate
// project scoping, and the column stays nullable through the expand phase.
func TestCreateAgentWithoutActiveProjectStoresNoProject(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	seedOrganization(t, conn, "org-agent-no-project")
	seedOrganizationUser(t, conn, "org-agent-no-project", "owner")
	service := newProjectScopedService(t, conn)

	ctx := humanContextInProject(t, "org-agent-no-project", "owner", nil)
	created, err := service.Create(ctx, &gen.CreatePayload{Name: "Unscoped agent"})
	require.NoError(t, err)

	stored, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{
		OrganizationID: "org-agent-no-project", ID: uuid.MustParse(created.ID),
	})
	require.NoError(t, err)
	require.False(t, stored.ProjectID.Valid)
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

	ctx := humanContextInProject(t, "org-agent-tenant-a", "owner", &foreign.ID)
	_, err := service.Create(ctx, &gen.CreatePayload{Name: "Cross tenant agent"})
	require.Error(t, err)

	// Naming the constraint is the point: a bare require.Error would still pass
	// if the composite key stopped enforcing and something else happened to
	// reject the write.
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pgerrcode.ForeignKeyViolation, pgErr.Code)
	require.Equal(t, "agents_organization_id_project_id_fkey", pgErr.ConstraintName)

	stored, err := repo.New(conn).ListManagedAgents(t.Context(), "org-agent-tenant-a")
	require.NoError(t, err)
	require.Empty(t, stored, "a rejected create must leave no agent behind")
}

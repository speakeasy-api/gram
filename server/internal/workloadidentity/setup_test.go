package workloadidentity_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	wirepo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

// tenant is one organization with one project inside it, which is the smallest
// shape a tenancy assertion needs.
type tenant struct {
	organizationID string
	projectID      uuid.UUID
}

func newTenant(t *testing.T, conn *pgxpool.Pool) tenant {
	t.Helper()

	ctx := t.Context()

	orgID := fmt.Sprintf("org-%s", uuid.NewString()[:8])
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Test Org", Slug: orgID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	return tenant{organizationID: orgID, projectID: newProject(t, conn, orgID)}
}

// newProject adds another project to an organization that already exists, which
// is the only way to build a sibling: workload_issuers pins project_id to the
// row's own organization through a composite key, so a project borrowed from a
// different organization cannot be written at all.
func newProject(t *testing.T, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Test Project", Slug: fmt.Sprintf("test-%s", uuid.NewString()[:8]), OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return project.ID
}

// seedIssuer writes one workload_issuers row through a fixture: writes are the
// management API's job, while this package is read-only by design. createdAt is explicit so the oldest-first tie-break can be asserted rather
// than raced.
func seedIssuer(t *testing.T, conn *pgxpool.Pool, organizationID string, projectID uuid.NullUUID, name, issuer string, createdAt time.Time) uuid.UUID {
	t.Helper()

	id, err := wirepo.New(conn).SeedWorkloadIssuerFixture(t.Context(), wirepo.SeedWorkloadIssuerFixtureParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           name,
		Issuer:         issuer,
		JwksUri:        issuer + "/.well-known/jwks.json",
		CreatedAt:      pgtype.Timestamptz{Time: createdAt, Valid: true, InfinityModifier: pgtype.Finite},
	})
	require.NoError(t, err)

	return id
}

func softDelete(t *testing.T, conn *pgxpool.Pool, organizationID string, id uuid.UUID) {
	t.Helper()

	deleted, err := testrepo.New(conn).SoftDeleteWorkloadIssuerFixture(t.Context(), testrepo.SoftDeleteWorkloadIssuerFixtureParams{
		ID: id, OrganizationID: organizationID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
}

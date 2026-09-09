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

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Test Project", Slug: fmt.Sprintf("test-%s", uuid.NewString()[:8]), OrganizationID: orgID,
	})
	require.NoError(t, err)

	return tenant{organizationID: orgID, projectID: project.ID}
}

// seedIssuer writes one workload_issuers row directly.
//
// Raw SQL because there is no create query yet: writes are the management API's
// job and land in a later milestone, while this package is read-only by design.
// createdAt is explicit so the oldest-first tie-break can be asserted rather
// than raced.
func seedIssuer(t *testing.T, conn *pgxpool.Pool, organizationID string, projectID uuid.NullUUID, name, issuer string, createdAt time.Time) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		t.Context(), `
		INSERT INTO workload_issuers
		  (organization_id, project_id, name, issuer, jwks_uri, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, organizationID, projectID, name, issuer, issuer+"/.well-known/jwks.json", createdAt).Scan(&id)
	require.NoError(t, err)

	return id
}

func softDelete(t *testing.T, conn *pgxpool.Pool, id uuid.UUID) {
	t.Helper()

	_, err := conn.Exec( //nolint:glint // notestingrawsql: see seedIssuer
		t.Context(), `UPDATE workload_issuers SET deleted_at = clock_timestamp() WHERE id = $1`, id)
	require.NoError(t, err)
}

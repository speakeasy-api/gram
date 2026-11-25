package cimdcandidates_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatal(err)
	}
	infra = env
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}

const testOrgID = "org-cimd-candidates"

type testIssuer struct {
	id        uuid.UUID
	projectID uuid.UUID
}

// newTestDB clones a database holding one organization.
func newTestDB(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	require.NoError(t, orgsrepo.New(conn).CreateOrganizationMetadata(t.Context(), orgsrepo.CreateOrganizationMetadataParams{
		ID:   testOrgID,
		Name: "Candidates Org",
		Slug: "candidates-org",
	}))
	return conn
}

// seedIssuer creates a project and its issuer in mode.
func seedIssuer(t *testing.T, conn *pgxpool.Pool, slug string, mode admission.Mode) testIssuer {
	t.Helper()
	ctx := t.Context()

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: testOrgID,
	})
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:               project.ID,
		OrganizationID:          conv.ToPGText(testOrgID),
		Slug:                    slug,
		AuthnChallengeMode:      "interactive",
		SessionDuration:         pgtype.Interval{Microseconds: 3_600_000_000, Days: 0, Months: 0, Valid: true},
		AuthorizationServerMode: pgtype.Text{},
		UseAuthenticationHost:   pgtype.Bool{},
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).SetUserSessionIssuerCIMDAdmissionMode(ctx, testrepo.SetUserSessionIssuerCIMDAdmissionModeParams{
		ClientIDMetadataAdmissionMode: conv.ToPGText(string(mode)),
		ID:                            issuer.ID,
		ProjectID:                     project.ID,
	}))
	return testIssuer{id: issuer.ID, projectID: project.ID}
}

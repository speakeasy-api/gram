package functionsaccessorganization_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/secretbackfills/functionsaccessorganization"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestRunFillsRunnerKeysFromTheirProjectOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	org := seedOrganization(t, ctx, pool)
	otherOrg := seedOrganization(t, ctx, pool)
	project := seedProject(t, ctx, pool, org)
	otherProject := seedProject(t, ctx, pool, otherOrg)

	first := seedRunnerKey(t, ctx, pool, org, project)
	second := seedRunnerKey(t, ctx, pool, org, project)
	other := seedRunnerKey(t, ctx, pool, otherOrg, otherProject)
	deleted := seedRunnerKey(t, ctx, pool, org, project)
	require.NoError(t, testrepo.New(pool).SoftDeleteFunctionsAccessFixture(ctx, deleted))
	for _, id := range []uuid.UUID{first, second, other, deleted} {
		require.NoError(t, testrepo.New(pool).ClearFunctionsAccessOrganizationFixture(ctx, id))
	}
	deletedBefore := runnerKeyOrganization(t, ctx, pool, deleted)

	report, err := functionsaccessorganization.Run(ctx, pool, functionsaccessorganization.Options{Apply: false, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, functionsaccessorganization.Report{Filled: 0, Counts: functionsaccessorganization.Counts{Pending: 4}}, report)
	require.False(t, runnerKeyOrganization(t, ctx, pool, first).OrganizationID.Valid, "a dry run writes nothing")

	report, err = functionsaccessorganization.Run(ctx, pool, functionsaccessorganization.Options{Apply: true, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, functionsaccessorganization.Report{Filled: 4, Counts: functionsaccessorganization.Counts{}}, report)
	require.Equal(t, conv.ToPGText(org), runnerKeyOrganization(t, ctx, pool, first).OrganizationID)
	require.Equal(t, conv.ToPGText(org), runnerKeyOrganization(t, ctx, pool, second).OrganizationID)
	require.Equal(t, conv.ToPGText(otherOrg), runnerKeyOrganization(t, ctx, pool, other).OrganizationID)
	deletedAfter := runnerKeyOrganization(t, ctx, pool, deleted)
	require.Equal(t, conv.ToPGText(org), deletedAfter.OrganizationID, "soft-deleted runner keys are filled too")
	require.Equal(t, deletedBefore.UpdatedAt, deletedAfter.UpdatedAt, "the backfill leaves updated_at alone")

	report, err = functionsaccessorganization.Run(ctx, pool, functionsaccessorganization.Options{Apply: true, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, functionsaccessorganization.Report{Filled: 0, Counts: functionsaccessorganization.Counts{}}, report)
}

func TestRunLeavesRunnerKeysWithoutAnOrganizationNull(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	// projects.organization_id has no foreign key, so a project can name an
	// organization with no metadata row. The deploy path stores NULL for it
	// instead of failing. Assets do reference organization_metadata, so the
	// function's asset is stored under a real organization.
	org := seedOrganization(t, ctx, pool)
	orphanOrg := "org_" + uuid.NewString()
	orphan := seedRunnerKey(t, ctx, pool, org, seedProject(t, ctx, pool, orphanOrg))
	require.False(t, runnerKeyOrganization(t, ctx, pool, orphan).OrganizationID.Valid)

	// A fillable row after the orphan in id order: the batch that skips the
	// orphan must not stop the run.
	fillable := seedRunnerKey(t, ctx, pool, org, seedProject(t, ctx, pool, org))
	require.NoError(t, testrepo.New(pool).ClearFunctionsAccessOrganizationFixture(ctx, fillable))

	report, err := functionsaccessorganization.Run(ctx, pool, functionsaccessorganization.Options{Apply: false, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, functionsaccessorganization.Report{Filled: 0, Counts: functionsaccessorganization.Counts{Pending: 1, MissingOrganization: 1}}, report)

	report, err = functionsaccessorganization.Run(ctx, pool, functionsaccessorganization.Options{Apply: true, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, functionsaccessorganization.Report{Filled: 1, Counts: functionsaccessorganization.Counts{MissingOrganization: 1}}, report)
	require.False(t, runnerKeyOrganization(t, ctx, pool, orphan).OrganizationID.Valid)
	require.Equal(t, conv.ToPGText(org), runnerKeyOrganization(t, ctx, pool, fillable).OrganizationID)
}

func seedOrganization(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(pool).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       orgID,
		Name:     "Test Org",
		Slug:     orgID,
		WorkosID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	return orgID
}

func seedProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	// Project names and slugs are capped at 40 characters.
	slug := "p-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(pool).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: organizationID})
	require.NoError(t, err)
	return project.ID
}

// seedRunnerKey writes a function runner key for a new deployment of the
// project, with the organization the deploy path stores. The function's asset
// is stored under assetOrganizationID.
func seedRunnerKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, assetOrganizationID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	deployments := deploymentsrepo.New(pool)
	deploymentID, err := deployments.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: assetOrganizationID,
		UserID:         "user-" + uuid.NewString(),
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)

	asset, err := assetsrepo.New(pool).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name: "fn.zip", Url: "file://fn.zip", ProjectID: projectID,
		OrganizationID: assetOrganizationID, Sha256: "fn-" + uuid.NewString(),
		Kind: "functions", ContentType: "application/zip", ContentLength: 1,
	})
	require.NoError(t, err)

	function, err := deployments.UpsertDeploymentFunctionsAsset(ctx, deploymentsrepo.UpsertDeploymentFunctionsAssetParams{
		DeploymentID: deploymentID, AssetID: asset.ID, Name: "fn", Slug: "fn",
		Runtime: "nodejs:22", MemoryMib: pgtype.Int4{Int32: 128, Valid: true},
		Scale: pgtype.Int4{Int32: 1, Valid: true},
	})
	require.NoError(t, err)

	accessID, err := testrepo.New(pool).CreateFunctionsAccessFixture(ctx, testrepo.CreateFunctionsAccessFixtureParams{
		ProjectID: projectID, DeploymentID: deploymentID, FunctionID: function.ID,
		EncryptionKey: conv.NewSecret([]byte("fixture-not-a-key")), BearerFormat: conv.ToPGText("v01"),
	})
	require.NoError(t, err)
	return accessID
}

func runnerKeyOrganization(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) testrepo.GetFunctionsAccessOrganizationFixtureRow {
	t.Helper()

	row, err := testrepo.New(pool).GetFunctionsAccessOrganizationFixture(ctx, id)
	require.NoError(t, err)
	return row
}

func TestRunForOneOrganizationLeavesTheOthers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	org := seedOrganization(t, ctx, pool)
	otherOrg := seedOrganization(t, ctx, pool)
	mine := seedRunnerKey(t, ctx, pool, org, seedProject(t, ctx, pool, org))
	other := seedRunnerKey(t, ctx, pool, otherOrg, seedProject(t, ctx, pool, otherOrg))
	seedRunnerKey(t, ctx, pool, org, seedProject(t, ctx, pool, "org_"+uuid.NewString()))
	for _, id := range []uuid.UUID{mine, other} {
		require.NoError(t, testrepo.New(pool).ClearFunctionsAccessOrganizationFixture(ctx, id))
	}

	organizations, err := functionsaccessorganization.Organizations(ctx, pool)
	require.NoError(t, err)
	require.ElementsMatch(t, []functionsaccessorganization.Organization{
		{ID: org, Name: "Test Org", Slug: org, Pending: 1},
		{ID: otherOrg, Name: "Test Org", Slug: otherOrg, Pending: 1},
	}, organizations, "a runner key whose organization has no metadata row belongs to none")

	report, err := functionsaccessorganization.Run(ctx, pool, functionsaccessorganization.Options{OrganizationID: org, Apply: true, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, functionsaccessorganization.Report{Filled: 1, Counts: functionsaccessorganization.Counts{}}, report, "the counts cover only that organization")
	require.Equal(t, conv.ToPGText(org), runnerKeyOrganization(t, ctx, pool, mine).OrganizationID)
	require.False(t, runnerKeyOrganization(t, ctx, pool, other).OrganizationID.Valid, "another organization's runner key stays without one")

	organizations, err = functionsaccessorganization.Organizations(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, []functionsaccessorganization.Organization{{ID: otherOrg, Name: "Test Org", Slug: otherOrg, Pending: 1}}, organizations)

	report, err = functionsaccessorganization.Run(ctx, pool, functionsaccessorganization.Options{OrganizationID: "", Apply: false, BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, functionsaccessorganization.Counts{Pending: 1, MissingOrganization: 1}, report.Counts, "without an organization the counts cover all of them")
}

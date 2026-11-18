package activities_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/functions"
	funcrepo "github.com/speakeasy-api/gram/server/internal/functions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// seedReadyFlyApp records a deployment with one function whose Fly.io app is
// ready, as a deployment on the flyio provider leaves it.
func seedReadyFlyApp(t *testing.T, db *pgxpool.Pool, organizationID string, projectID uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	deploymentID := createPendingDeployment(t, db, organizationID, projectID)
	asset, err := assetsrepo.New(db).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name: "reap.zip", Url: "file://reap.zip", ProjectID: projectID,
		OrganizationID: organizationID, Sha256: "reap-" + uuid.NewString(),
		Kind: "functions", ContentType: "application/zip", ContentLength: 1,
	})
	require.NoError(t, err)

	deployments := deploymentsrepo.New(db)
	function, err := deployments.UpsertDeploymentFunctionsAsset(ctx, deploymentsrepo.UpsertDeploymentFunctionsAssetParams{
		DeploymentID: deploymentID, AssetID: asset.ID, Name: "reap", Slug: "reap",
		Runtime: string(functions.RuntimeNodeJS22), MemoryMib: pgtype.Int4{Int32: 128, Valid: true},
		Scale: pgtype.Int4{Int32: 1, Valid: true},
	})
	require.NoError(t, err)
	accessID, err := deployments.CreateDeploymentFunctionsAccess(ctx, deploymentsrepo.CreateDeploymentFunctionsAccessParams{
		ProjectID: projectID, DeploymentID: deploymentID, FunctionID: function.ID,
		EncryptionKey: conv.NewSecret([]byte("unused-runner-key")), BearerFormat: conv.ToPGText("v1"),
	})
	require.NoError(t, err)

	apps := funcrepo.New(db)
	appID, err := apps.InitFlyApp(ctx, funcrepo.InitFlyAppParams{
		ProjectID: projectID, DeploymentID: deploymentID, FunctionID: function.ID, AccessID: accessID,
		FlyOrgID: "org", FlyOrgSlug: "org", AppName: "app-" + uuid.NewString()[:8],
		AppUrl: "https://app.fly.dev", RunnerVersion: "v1", PrimaryRegion: "us",
	})
	require.NoError(t, err)
	_, err = apps.FinalizeFlyApp(ctx, funcrepo.FinalizeFlyAppParams{
		Status: "ready", ReapedAt: pgtype.Timestamptz{}, ID: appID,
		ProjectID: projectID, DeploymentID: deploymentID, FunctionID: function.ID,
	})
	require.NoError(t, err)
}

// A deployment switched to the disabled provider cannot tear down Fly.io apps
// recorded earlier. The reaper stops without counting failures and leaves the
// apps recorded for a provider that can reap them.
func TestReapFlyAppsStopsWhenFunctionsDisabled(t *testing.T) {
	t.Parallel()

	db, projectID, organizationID := setupTransitionDeploymentTest(t, "reap_functions_disabled")
	seedReadyFlyApp(t, db, organizationID, projectID)
	seedReadyFlyApp(t, db, organizationID, projectID)
	project := uuid.NullUUID{UUID: projectID, Valid: true}

	result, err := activities.NewReapFlyApps(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, functions.Disabled{}, 1).Do(t.Context(), activities.ReapFlyAppsRequest{
		Scope:     activities.FunctionsReaperScopeProject,
		ProjectID: project,
	})
	require.NoError(t, err)
	require.Equal(t, &activities.ReapFlyAppsResult{Reaped: 0, Errors: 0}, result)

	remaining, err := funcrepo.New(db).GetFlyAppsToReap(t.Context(), funcrepo.GetFlyAppsToReapParams{
		KeepCount: pgtype.Int8{Int64: 1, Valid: true},
		BatchSize: pgtype.Int8{Int64: 10, Valid: true},
		ProjectID: project,
	})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
}

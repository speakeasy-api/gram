package main

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestCatchUp(t *testing.T) {
	t.Parallel()
	env, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := env.CloneTestDatabase(t, "catchup")
	require.NoError(t, err)
	ctx := t.Context()
	q := testrepo.New(db)
	features := featurerepo.New(db)
	for _, id := range []string{"org-never-enabled", "org-disabled-flag", "org-enabled", "org-inactive"} {
		disabled := pgtype.Timestamptz{Valid: false}
		if id == "org-inactive" {
			disabled = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		}
		require.NoError(t, q.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
			ID: id, Name: "Catch-up fixture", Slug: id, GramAccountType: "enterprise", DisabledAt: disabled,
			FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			FreeTrialEndsAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
		}))
	}
	for _, org := range []string{"org-enabled", "org-disabled-flag"} {
		_, err := features.EnableFeature(ctx, featurerepo.EnableFeatureParams{OrganizationID: org, FeatureName: "automatic-role-distribution"})
		require.NoError(t, err)
	}
	_, err = features.DeleteFeature(ctx, featurerepo.DeleteFeatureParams{OrganizationID: "org-disabled-flag", FeatureName: "automatic-role-distribution"})
	require.NoError(t, err)

	n, err := catchUp(ctx, db, false)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	rows, err := q.ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Empty(t, rows, "preview must not queue setup")

	n, err = catchUp(ctx, db, true)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	rows, err = q.ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	var targets []string
	for _, row := range rows {
		require.Equal(t, "gram.role_distribution.v1.RoleDistributionSetupRequestedV1", row.Topic)
		var event roledistributionv1.RoleDistributionSetupRequestedV1
		require.NoError(t, proto.Unmarshal(row.Message, &event))
		targets = append(targets, event.GetBootstrapOrganizationId())
		require.Equal(t, row.OrganizationID, event.GetBootstrapOrganizationId())
		require.Empty(t, event.GetOrganizationId())
		require.Empty(t, event.GetRoleUrn())
		require.Empty(t, event.GetCursor())
	}
	require.ElementsMatch(t, []string{"org-never-enabled", "org-disabled-flag"}, targets)
	enabled, err := features.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: "org-enabled", FeatureName: "automatic-role-distribution"})
	require.NoError(t, err)
	require.True(t, enabled, "stored flag cleanup is a separate operation")

	// Selection is ordered by ID: queue org-disabled-flag, then reject org-never-enabled.
	require.NoError(t, q.RejectCatchUpSecondOrganizationFixture(ctx))
	_, err = catchUp(ctx, db, true)
	require.Error(t, err)
	rows, err = q.ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2, "failed catch-up must not add partial work")
}

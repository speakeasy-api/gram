package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestOrganizationOnboardingOwnership(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	container, clone, err := testenv.NewTestPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	conn, err := clone(t, "onboarding_ownership")
	require.NoError(t, err)
	fixtures := testrepo.New(conn)

	err = fixtures.InsertOrganizationOnboardingFixture(ctx, pgtype.Text{String: "", Valid: false})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23502", pgErr.Code)
	require.Equal(t, "organization_id", pgErr.ColumnName)

	const orgID = "org_onboarding_ownership_test"
	now := time.Now().UTC()
	at := func(value time.Time) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: value, InfinityModifier: pgtype.Finite, Valid: true}
	}
	unset := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 orgID,
		Name:               "Test organization",
		Slug:               "onboarding-ownership-test",
		GramAccountType:    "free",
		WorkosID:           pgtype.Text{String: "", Valid: false},
		Whitelisted:        false,
		FreeTrialStartedAt: at(now),
		FreeTrialEndsAt:    at(now.AddDate(0, 0, 14)),
		DisabledAt:         unset,
		CreatedAt:          unset,
	}))
	orgText := pgtype.Text{String: orgID, Valid: true}
	// The preset may remain unset while onboarding is incomplete.
	require.NoError(t, fixtures.InsertOrganizationOnboardingFixture(ctx, orgText))

	err = fixtures.InsertOrganizationOnboardingFixture(ctx, orgText)
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23505", pgErr.Code)
	require.Equal(t, "organization_onboarding_organization_id_key", pgErr.ConstraintName)

	require.NoError(t, fixtures.DeleteOrganizationMetadataFixture(ctx, orgID))
	remaining, err := fixtures.CountOrganizationOnboardingFixture(ctx)
	require.NoError(t, err)
	require.Zero(t, remaining)
}

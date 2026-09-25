//nolint:glint // Schema regression tests need raw writes to exercise database constraints.
package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

	_, err = conn.Exec(ctx, `INSERT INTO organization_onboarding (organization_id) VALUES (NULL)`)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23502", pgErr.Code)
	require.Equal(t, "organization_id", pgErr.ColumnName)

	const orgID = "org_onboarding_ownership_test"
	_, err = conn.Exec(ctx, `INSERT INTO organization_metadata (id, name, slug) VALUES ($1, 'Test organization', 'onboarding-ownership-test')`, orgID)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO organization_onboarding (organization_id) VALUES ($1)`, orgID)
	require.NoError(t, err) // The preset may remain unset while onboarding is incomplete.

	_, err = conn.Exec(ctx, `INSERT INTO organization_onboarding (organization_id) VALUES ($1)`, orgID)
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23505", pgErr.Code)
	require.Equal(t, "organization_onboarding_organization_id_key", pgErr.ConstraintName)

	_, err = conn.Exec(ctx, `DELETE FROM organization_metadata WHERE id = $1`, orgID)
	require.NoError(t, err)
	var remaining int
	err = conn.QueryRow(ctx, `SELECT count(*) FROM organization_onboarding`).Scan(&remaining)
	require.NoError(t, err)
	require.Zero(t, remaining)
}

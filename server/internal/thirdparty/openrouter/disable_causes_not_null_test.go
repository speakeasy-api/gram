package openrouter

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

func TestDisableCausesColumnIsNotNullWithEmptyDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	provisioner, _, _ := newDisableTestProvisioner(t, "org-"+uuid.NewString()[:8])
	column, err := testrepo.New(provisioner.db).GetOpenRouterAPIKeyDisableCausesColumnFixture(ctx)
	require.NoError(t, err)
	require.Equal(t, "NO", column.IsNullable)
	require.Contains(t, column.ColumnDefault, "{}")
}

func TestCreateOpenRouterAPIKeyWritesEmptyDisableCauses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	_, _, queries := newDisableTestProvisioner(t, orgID)

	row, err := queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		KeyEncrypted:   pgtype.Text{String: "ciphertext", Valid: true},
		KeyHash:        "hash-" + orgID,
		MonthlyCredits: 5,
	})
	require.NoError(t, err)
	require.NotNil(t, row.DisableCauses)
	require.Empty(t, row.DisableCauses)
	require.False(t, row.Disabled)
}

func TestInsertOmittingDisableCausesUsesEmptyDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, _ := newDisableTestProvisioner(t, orgID)

	causes, err := testrepo.New(provisioner.db).InsertOpenRouterAPIKeyOmittingDisableCausesFixture(ctx, testrepo.InsertOpenRouterAPIKeyOmittingDisableCausesFixtureParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
		KeyHash:        "hash-default-" + orgID,
	})
	require.NoError(t, err)
	require.NotNil(t, causes)
	require.Empty(t, causes)
}

func TestNullDisableCausesWritesAreRejected(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)
	fixtures := testrepo.New(provisioner.db)

	err := fixtures.InsertOpenRouterAPIKeyNullDisableCausesFixture(ctx, testrepo.InsertOpenRouterAPIKeyNullDisableCausesFixtureParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		KeyHash:        "hash-null-" + orgID,
	})
	requireNotNullDisableCauses(t, err)

	_, err = queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		KeyEncrypted:   pgtype.Text{String: "ciphertext", Valid: true},
		KeyHash:        "hash-" + orgID,
		MonthlyCredits: 5,
	})
	require.NoError(t, err)

	err = fixtures.SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		Disabled:       true,
		DisableCauses:  nil,
	})
	requireNotNullDisableCauses(t, err)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.NotNil(t, row.DisableCauses)
	require.Empty(t, row.DisableCauses)
	require.False(t, row.Disabled)
}

func requireNotNullDisableCauses(t *testing.T, err error) {
	t.Helper()

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pgerrcode.NotNullViolation, pgErr.Code)
	require.Equal(t, "disable_causes", pgErr.ColumnName)
}

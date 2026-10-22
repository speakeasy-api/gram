package gram

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	seedrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestAICatalogStartupRefusesUnavailableDatabase(t *testing.T) {
	t.Parallel()
	// A closed pool rejects bootstrap immediately without opening a connection.
	db, err := pgxpool.New(t.Context(), "postgresql://localhost:1/catalog_test?sslmode=disable")
	require.NoError(t, err)
	db.Close()
	catalog, stop, err := startAICatalog(t.Context(), db, testenv.NewLogger(t))
	require.ErrorContains(t, err, "bootstrap AI catalog")
	require.Nil(t, catalog)
	require.Nil(t, stop, "no refresh loop starts before a successful initial load")
}

func TestAICatalogStartupRefusesInvalidDeviceDefinition(t *testing.T) {
	t.Parallel()
	infra, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "invalid_ai_catalog")
	require.NoError(t, err)
	require.NoError(t, seedrepo.New(db).InsertProduct(t.Context(), seedrepo.InsertProductParams{
		ID: "invalid-fixture", VendorKey: "fixture", DisplayName: "Fixture",
		BundleIds: []string{}, Binaries: []string{"fixture"}, ConfigDirs: []string{},
		ProcessNames: []string{}, ClientInfoNames: []string{},
	}))
	catalog, stop, err := startAICatalog(t.Context(), db, testenv.NewLogger(t))
	require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
	require.ErrorContains(t, err, "unknown category")
	require.Nil(t, catalog)
	require.Nil(t, stop, "invalid initial data must not start a refresh loop")
}

package gram

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	seedrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestAICatalogStartupRefusesUnavailableDatabase(t *testing.T) {
	t.Parallel()
	// A closed pool fails the read without opening a connection.
	db, err := pgxpool.New(t.Context(), "postgresql://localhost:1/catalog_test?sslmode=disable")
	require.NoError(t, err)
	db.Close()
	catalog, stop, err := startAICatalog(t.Context(), db, testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.ErrorContains(t, err, "load initial AI catalog")
	require.Nil(t, catalog)
	require.Nil(t, stop, "no refresh loop starts before a successful initial load")
}

func TestAICatalogStartupServesTheBuiltInCatalogForAnInvalidDefinition(t *testing.T) {
	t.Parallel()
	infra, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "invalid_ai_catalog")
	require.NoError(t, err)
	_, err = seedrepo.New(db).InsertProduct(t.Context(), seedrepo.InsertProductParams{
		ID: "invalid-fixture", VendorKey: "fixture", DisplayName: "Fixture", Category: pgtype.Text{String: "unknown", Valid: true},
		BundleIds: []string{}, Binaries: []string{"fixture"}, ConfigDirs: []string{},
		ProcessNames: []string{}, ClientInfoNames: []string{},
	})
	require.NoError(t, err)
	catalog, stop, err := startAICatalog(t.Context(), db, testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	t.Cleanup(stop)
	require.Same(t, aivendors.CompiledSnapshot(), catalog.Snapshot())
}

func TestAICatalogStartupSurvivesANullArrayElement(t *testing.T) {
	t.Parallel()
	infra, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "null_ai_catalog_element")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	_, err = db.Exec(t.Context(), "UPDATE ai_products SET client_info_names = ARRAY[NULL]::text[], bundle_ids = array_append(bundle_ids, NULL) WHERE id = 'zed'")
	require.NoError(t, err)
	catalog, stop, err := startAICatalog(t.Context(), db, testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	t.Cleanup(stop)
	require.False(t, catalog.Snapshot().BuiltIn())
}

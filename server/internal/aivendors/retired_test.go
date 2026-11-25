package aivendors_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	seedrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const (
	retiredZedURL       = "https://zed.dev/oauth/client-metadata.json"
	retiredPiPattern    = "https://pi.dev/oauth/*/client.json"
	retiredPiConnection = "https://pi.dev/oauth/fixture-server/client.json"
)

var retiredAt = pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite}

func seededCatalog(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	return db
}

func editDocument(t *testing.T, db *pgxpool.Pool, url string, enabled bool, deleted pgtype.Timestamptz) {
	t.Helper()
	docs, err := seedrepo.New(db).ListDocuments(t.Context())
	require.NoError(t, err)
	edited := false
	for _, d := range docs {
		if d.Url == url && !d.Deleted {
			require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: d.ID, Url: d.Url, DisplayName: d.DisplayName, Enabled: enabled, DeletedAt: deleted}))
			edited = true
		}
	}
	require.True(t, edited, url)
}

func setProductDeleted(t *testing.T, db *pgxpool.Pool, id string, name string, deleted pgtype.Timestamptz) {
	t.Helper()
	require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{ID: id, DisplayName: name, DeletedAt: deleted}))
}

// requireRetiredOnlyForBlocks checks a removed client ID reaches nothing but block enforcement.
func requireRetiredOnlyForBlocks(t *testing.T, snapshot *aivendors.Snapshot, clientID string, url string) {
	t.Helper()
	require.NotEqual(t, admission.OutcomeAdmit, admission.Evaluate(snapshot, admission.ModePresets, clientID).Outcome, "a removed document never admits on a presets issuer")
	_, known := snapshot.CatalogPreset(clientID)
	require.False(t, known, "a removed document does not attribute the client")
	for _, preset := range snapshot.Catalog() {
		if preset.URL == url {
			require.False(t, preset.Enabled, "a removed document is never listed as a preset")
		}
	}
	for _, target := range aitargets.NewOrganizationList(snapshot, nil).Snapshot.Targets() {
		require.NotContains(t, target.GatewayClient.OAuthClientIDs, url, "a removed document never reaches device payloads")
	}
}

func TestLastKnownKeepsTheProductOfARemovedDocument(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		remove   func(t *testing.T, db *pgxpool.Pool)
		clientID string
		url      string
		product  string
		display  string
	}{
		{name: "soft-deleted product", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			setProductDeleted(t, db, "zed", "Zed", retiredAt)
		}, clientID: retiredZedURL, url: retiredZedURL, product: "zed", display: "Zed"},
		{name: "disabled document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, retiredZedURL, false, pgtype.Timestamptz{})
		}, clientID: retiredZedURL, url: retiredZedURL, product: "zed", display: "Zed"},
		{name: "soft-deleted document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, retiredZedURL, true, retiredAt)
		}, clientID: retiredZedURL, url: retiredZedURL, product: "zed", display: "Zed"},
		{name: "soft-deleted pattern document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, retiredPiPattern, true, retiredAt)
		}, clientID: retiredPiConnection, url: retiredPiPattern, product: "pi", display: "Pi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := seededCatalog(t)
			tc.remove(t, db)
			snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
			require.NoError(t, err)
			require.False(t, snapshot.BuiltIn())

			last, ok := snapshot.LastKnown(tc.clientID)
			require.True(t, ok)
			require.Equal(t, aivendors.LastKnownProduct{ProductID: tc.product, DisplayName: tc.display}, last)
			requireRetiredOnlyForBlocks(t, snapshot, tc.clientID, tc.url)
		})
	}
}

func TestLastKnownIgnoresClientsTheLiveCatalogAdmits(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	for _, clientID := range []string{retiredZedURL, retiredPiConnection, "https://unknown.example.com/client.json"} {
		_, ok := snapshot.LastKnown(clientID)
		require.False(t, ok, clientID)
	}
	require.Equal(t, aivendors.CompiledSnapshot().Fingerprint(), snapshot.Fingerprint(), "nothing removed leaves the fingerprint as it was")
}

func TestLastKnownFollowsAURLReaddedUnderAnotherProduct(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{name: "enabled", enabled: true},
		{name: "disabled", enabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := seededCatalog(t)
			editDocument(t, db, retiredZedURL, true, retiredAt)
			require.NoError(t, testrepo.New(db).InsertDocumentFixture(t.Context(), testrepo.InsertDocumentFixtureParams{ID: uuid.New(), AiProductID: "goose", Url: retiredZedURL, DisplayName: "Goose", Enabled: tc.enabled, IsPattern: false}))
			snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
			require.NoError(t, err)
			require.False(t, snapshot.BuiltIn())

			last, ok := snapshot.LastKnown(retiredZedURL)
			if tc.enabled {
				require.False(t, ok, "live attribution decides")
				preset, known := snapshot.CatalogPreset(retiredZedURL)
				require.True(t, known)
				require.Equal(t, "goose", preset.ProductID)
				return
			}
			require.True(t, ok)
			require.Equal(t, "goose", last.ProductID, "the live disabled document wins over the removed one")
		})
	}
}

func TestRetiredDocumentsDoNotCountInValidation(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	// Enabled, this would overlap the ChatGPT pattern and reject the catalog.
	id := uuid.New()
	_, err := seedrepo.New(db).InsertDocument(t.Context(), seedrepo.InsertDocumentParams{
		ID: id, AiProductID: "zed", Url: "https://chatgpt.com/*/fixture/client.json", DisplayName: "Overlapping pattern", Enabled: true, IsPattern: true,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: id, Url: "https://chatgpt.com/*/fixture/client.json", DisplayName: "Overlapping pattern", Enabled: true, DeletedAt: retiredAt}))
	// A malformed removed pattern is dropped, not reported.
	malformed := uuid.New()
	require.NoError(t, testrepo.New(db).InsertDocumentFixture(t.Context(), testrepo.InsertDocumentFixtureParams{ID: malformed, AiProductID: "zed", Url: "https://*.example.com/client.json", DisplayName: "Malformed", Enabled: true, IsPattern: true}))
	require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: malformed, Url: "https://*.example.com/client.json", DisplayName: "Malformed", Enabled: true, DeletedAt: retiredAt}))

	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.False(t, snapshot.BuiltIn(), "removed documents never reject the catalog")
	last, ok := snapshot.LastKnown("https://chatgpt.com/other/fixture/client.json")
	require.True(t, ok)
	require.Equal(t, "zed", last.ProductID)
	preset, known := snapshot.CatalogPreset("https://chatgpt.com/oauth/fixture/client.json")
	require.True(t, known)
	require.Equal(t, "chatgpt", preset.ProductID, "a removed pattern never changes live attribution")
}

func TestRefreshPicksUpARemovalAndARestore(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)

	setProductDeleted(t, db, "zed", "Zed", retiredAt)
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	last, ok := store.Snapshot().LastKnown(retiredZedURL)
	require.True(t, ok)
	require.Equal(t, "zed", last.ProductID)

	setProductDeleted(t, db, "zed", "Zed", pgtype.Timestamptz{})
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	_, ok = store.Snapshot().LastKnown(retiredZedURL)
	require.False(t, ok)
	preset, known := store.Snapshot().CatalogPreset(retiredZedURL)
	require.True(t, known)
	require.Equal(t, "zed", preset.ProductID)
}

func TestRefreshPublishesARemovalOnlyTheRetiredIndexSees(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	logger := testenv.NewLogger(t)
	setProductDeleted(t, db, "zed", "Zed", retiredAt)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	// The product is already gone, so only the removed documents change.
	setProductDeleted(t, db, "zed", "Zed Renamed", retiredAt)
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	require.NotEqual(t, initial.Fingerprint(), store.Snapshot().Fingerprint())
	last, ok := store.Snapshot().LastKnown(retiredZedURL)
	require.True(t, ok)
	require.Equal(t, "Zed Renamed", last.DisplayName)
}

func TestBuiltInFallbackKeepsRemovedDocumentsForBlocks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		remove func(t *testing.T, db *pgxpool.Pool)
	}{
		{name: "soft-deleted product", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			setProductDeleted(t, db, "zed", "Zed", retiredAt)
		}},
		{name: "disabled document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, retiredZedURL, false, pgtype.Timestamptz{})
		}},
		{name: "soft-deleted document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, retiredZedURL, true, retiredAt)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := seededCatalog(t)
			tc.remove(t, db)
			insertInvalidProduct(t, db)
			loaded, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
			require.NoError(t, err)
			require.True(t, loaded.BuiltIn())
			last, ok := loaded.LastKnown(retiredZedURL)
			require.True(t, ok)
			require.Equal(t, "zed", last.ProductID)
			requireRetiredOnlyForBlocks(t, loaded, retiredZedURL, retiredZedURL)
		})
	}
}

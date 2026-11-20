package aivendors_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	seedrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestDatabaseCatalogPreservesShippedAdmissionAndProducts(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), acceptCatalog)
	require.NoError(t, err)
	require.Len(t, snapshot.Products(), len(aivendors.Products()))
	require.Len(t, snapshot.Catalog(), 26)
	// Frozen expected documents and publishers are independent of the DB reader.
	for _, entry := range aivendors.CIMDDocuments() {
		if !entry.Document.Enabled {
			continue
		}
		clientID := strings.ReplaceAll(entry.Document.URL, "*", "fixture-connector")
		actual, ok := snapshot.CatalogPreset(clientID)
		require.True(t, ok, clientID)
		require.Equal(t, entry.Product.ID, actual.ProductID, clientID)
		require.Equal(t, entry.Product.VendorKey, actual.VendorKey, clientID)
		require.Equal(t, entry.Document.URL, actual.URL, clientID)
	}
	// This exact document also matches the broader ChatGPT pattern.
	actual, ok := snapshot.CatalogPreset("https://chatgpt.com/oauth/codex/client.json")
	require.True(t, ok)
	require.Equal(t, "codex", actual.ProductID)
}

func TestRefreshPublishesEditsWithoutTimestampChanges(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, acceptCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	require.NoError(t, store.Refresh(t.Context(), db, logger, acceptCatalog))
	require.Same(t, initial, store.Snapshot(), "unchanged snapshots are retained")
	// This fixture deliberately does not update updated_at.
	require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed Updated", DeletedAt: pgtype.Timestamptz{}}))
	require.NoError(t, store.Refresh(t.Context(), db, logger, acceptCatalog))
	next := store.Snapshot()
	require.NotEqual(t, initial.Fingerprint(), next.Fingerprint())
	product, ok := productByID(next, "zed")
	require.True(t, ok)
	require.Equal(t, "Zed Updated", product.DisplayName)
	product, ok = productByID(initial, "zed")
	require.True(t, ok)
	require.Equal(t, "Zed", product.DisplayName, "captured snapshots remain unchanged")
}

func TestRefreshFailureRetainsSnapshot(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, acceptCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, store.Refresh(ctx, db, logger, acceptCatalog))
	require.Same(t, initial, store.Snapshot())
}

func TestLoadOmitsInvalidPatternsAndDeletedProducts(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	q := seedrepo.New(db)
	docs, err := q.ListDocuments(t.Context())
	require.NoError(t, err)
	for _, d := range docs {
		if d.Url == "https://chatgpt.com/oauth/*/client.json" {
			require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: d.ID, Url: "https://*.example.com/client.json", DisplayName: d.DisplayName, Enabled: true, DeletedAt: pgtype.Timestamptz{}}))
		}
	}
	require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed", DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}}))
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), acceptCatalog)
	require.NoError(t, err)
	_, ok := snapshot.CatalogPreset("https://chatgpt.com/oauth/fixture-connector/client.json")
	require.False(t, ok)
	_, ok = snapshot.CatalogPreset("https://zed.dev/oauth/client-metadata.json")
	require.False(t, ok)
	_, ok = productByID(snapshot, "zed")
	require.False(t, ok)
	require.False(t, snapshot.BlockableVendorKey("zed"))
	for _, p := range snapshot.Products() {
		for _, d := range p.Documents {
			require.NotEqual(t, "https://*.example.com/client.json", d.URL)
		}
	}
}

func TestLoadOmitsPatternsWithoutRealPath(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	patterns := []string{
		"https://example.com?redirect=/oauth/*/client.json",
		"https://example.com#/oauth/*/client.json",
		"https://:443/*/client.json",
		"https://zed.dev/oauth/no-wildcard/client.json",
	}
	for _, pattern := range patterns {
		_, err = seedrepo.New(db).InsertDocument(t.Context(), seedrepo.InsertDocumentParams{
			ID: uuid.New(), AiProductID: "zed", Url: pattern, DisplayName: "Malformed pattern", Enabled: true, IsPattern: true,
		})
		require.NoError(t, err)
	}
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), acceptCatalog)
	require.NoError(t, err)
	require.Len(t, snapshot.Catalog(), 26)
	for _, pattern := range patterns {
		_, admitted := snapshot.CatalogPreset(strings.ReplaceAll(pattern, "*", "connector"))
		require.False(t, admitted)
		for _, product := range snapshot.Products() {
			for _, document := range product.Documents {
				require.NotEqual(t, pattern, document.URL)
			}
		}
	}
}

func TestRefreshAcceptsIntentionallyEmptyCatalog(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, acceptCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	q := seedrepo.New(db)
	rows, err := q.ListProducts(t.Context())
	require.NoError(t, err)
	for _, p := range rows {
		require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{ID: p.ID, DisplayName: p.DisplayName, DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}}))
	}
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db), "startup does not restore soft-deleted products")
	require.NoError(t, store.Refresh(t.Context(), db, logger, acceptCatalog))
	require.Empty(t, store.Snapshot().Products())
	require.Empty(t, store.Snapshot().Catalog())
	require.NotEqual(t, initial.Fingerprint(), store.Snapshot().Fingerprint())
}

func TestLoadListsDisabledDocumentsWithoutAdmittingThem(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	q := seedrepo.New(db)
	docs, err := q.ListDocuments(t.Context())
	require.NoError(t, err)
	for _, d := range docs {
		if d.AiProductID == "zed" {
			require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: d.ID, Url: d.Url, DisplayName: d.DisplayName, Enabled: false, DeletedAt: pgtype.Timestamptz{}}))
		}
	}
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), acceptCatalog)
	require.NoError(t, err)
	_, ok := snapshot.CatalogPreset("https://zed.dev/oauth/client-metadata.json")
	require.False(t, ok)
	require.False(t, snapshot.BlockableVendorKey("zed"))
	require.Contains(t, snapshot.Catalog(), aivendors.Preset{ProductID: "zed", VendorKey: "zed", DisplayName: "Zed", URL: "https://zed.dev/oauth/client-metadata.json", Enabled: false, Pattern: false})
}

func acceptCatalog(*aivendors.Snapshot) error { return nil }

func productByID(s *aivendors.Snapshot, id string) (aivendors.Product, bool) {
	for _, p := range s.Products() {
		if p.ID == id {
			return p, true
		}
	}
	return aivendors.Product{}, false
}

func TestLoadedSnapshotMatchesCompiledSnapshot(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	loaded, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), acceptCatalog)
	require.NoError(t, err)
	compiled := aivendors.CompiledSnapshot()
	ids := func(s *aivendors.Snapshot) []string {
		var out []string
		for _, p := range s.Products() {
			out = append(out, p.ID)
		}
		return out
	}
	require.ElementsMatch(t, ids(compiled), ids(loaded))
	var probes []string
	for _, entry := range aivendors.CIMDDocuments() {
		probes = append(probes, entry.Document.URL)
		if entry.Document.Pattern {
			for _, segment := range []string{"probe", "codex", "probe/extra"} {
				probes = append(probes, strings.ReplaceAll(entry.Document.URL, "*", segment))
			}
		}
	}
	for _, probe := range probes {
		want, wantOK := compiled.CatalogPreset(probe)
		got, gotOK := loaded.CatalogPreset(probe)
		require.Equal(t, wantOK, gotOK, probe)
		require.Equal(t, want, got, probe)
	}
	// Fingerprints differ only by order: the database lists products by id and
	// documents by URL in byte order, the registry by declaration.
	require.NotEqual(t, compiled.Fingerprint(), loaded.Fingerprint())
	reordered := compiled.Products()
	slices.SortFunc(reordered, func(a, b aivendors.Product) int { return strings.Compare(a.ID, b.ID) })
	for i := range reordered {
		slices.SortFunc(reordered[i].Documents, func(a, b aivendors.Document) int { return strings.Compare(a.URL, b.URL) })
	}
	require.Equal(t, loaded.Fingerprint(), aivendors.NewSnapshot(reordered).Fingerprint())
}

func TestLoadRequiresValidator(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), nil)
	require.Error(t, err)
	require.Nil(t, snapshot)
}

func TestRejectedCatalogIsNotPublished(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	logger := testenv.NewLogger(t)
	errRejected := errors.New("rejected")
	reject := func(*aivendors.Snapshot) error { return errRejected }
	snapshot, err := aivendors.Load(t.Context(), db, logger, reject)
	require.ErrorIs(t, err, errRejected)
	require.Nil(t, snapshot)
	initial, err := aivendors.Load(t.Context(), db, logger, acceptCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed Updated", DeletedAt: pgtype.Timestamptz{}}))
	require.ErrorIs(t, store.Refresh(t.Context(), db, logger, reject), errRejected)
	require.Same(t, initial, store.Snapshot())
}

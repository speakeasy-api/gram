package aivendors_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	seedrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestDatabaseCatalogPreservesShippedAdmissionAndProducts(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.Len(t, snapshot.Products(), 51)
	require.Len(t, snapshot.Catalog(), 24)
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
	require.ElementsMatch(t, aitargets.Defaults(aivendors.NewSnapshot(aivendors.Products())), aitargets.Defaults(snapshot))
	require.NoError(t, aitargets.Validate(snapshot, aitargets.Defaults(snapshot)))
}

func TestRefreshPublishesEditsWithoutTimestampChanges(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	require.Same(t, initial, store.Snapshot(), "unchanged snapshots are retained")
	// This fixture deliberately does not update updated_at.
	require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed Updated", DeletedAt: pgtype.Timestamptz{}}))
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	next := store.Snapshot()
	require.NotEqual(t, initial.Fingerprint(), next.Fingerprint())
	target, ok := aitargets.DefaultByID(next, "zed")
	require.True(t, ok)
	require.Equal(t, "Zed Updated", target.DisplayName)
	target, ok = aitargets.DefaultByID(initial, "zed")
	require.True(t, ok)
	require.Equal(t, "Zed", target.DisplayName, "captured snapshots remain unchanged")
}

func TestRefreshFailureRetainsSnapshot(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, store.Refresh(ctx, db, logger, aitargets.ValidateCatalog))
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
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	_, ok := snapshot.CatalogPreset("https://chatgpt.com/oauth/fixture-connector/client.json")
	require.False(t, ok)
	_, ok = snapshot.CatalogPreset("https://zed.dev/oauth/client-metadata.json")
	require.False(t, ok)
	_, ok = aitargets.DefaultByID(snapshot, "zed")
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
	}
	for _, pattern := range patterns {
		require.NoError(t, seedrepo.New(db).InsertDocument(t.Context(), seedrepo.InsertDocumentParams{
			ID: uuid.New(), AiProductID: "zed", Url: pattern, DisplayName: "Malformed pattern", Enabled: true, IsPattern: true,
		}))
	}
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.Len(t, snapshot.Catalog(), 24)
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
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	q := seedrepo.New(db)
	rows, err := q.ListProducts(t.Context())
	require.NoError(t, err)
	for _, p := range rows {
		require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{ID: p.ID, DisplayName: p.DisplayName, DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}}))
	}
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db), "startup does not restore soft-deleted products")
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
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
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	_, ok := snapshot.CatalogPreset("https://zed.dev/oauth/client-metadata.json")
	require.False(t, ok)
	require.False(t, snapshot.BlockableVendorKey("zed"))
	require.Contains(t, snapshot.Catalog(), aivendors.Preset{ProductID: "zed", VendorKey: "zed", DisplayName: "Zed", URL: "https://zed.dev/oauth/client-metadata.json", Enabled: false, Pattern: false})
	target, ok := aitargets.DefaultByID(snapshot, "zed")
	require.True(t, ok)
	require.True(t, target.GatewayClient.IsZero())
}

func TestInvalidDeviceDefinitionsDoNotReplaceCatalog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		category  pgtype.Text
		binaries  []string
		processes []string
		want      string
	}{
		{name: "missing category", binaries: []string{"fixture"}, want: "unknown category"},
		{name: "unknown category", category: pgtype.Text{String: "unknown", Valid: true}, binaries: []string{"fixture"}, want: "unknown category"},
		{name: "invalid binary", category: pgtype.Text{String: "harness", Valid: true}, binaries: []string{"../fixture"}, want: "bare command name"},
		{name: "process only", category: pgtype.Text{String: "harness", Valid: true}, processes: []string{"Fixture"}, want: "at least one install signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
			logger := testenv.NewLogger(t)
			initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
			require.NoError(t, err)
			store := aivendors.NewStore(initial)
			require.NoError(t, seedrepo.New(db).InsertProduct(t.Context(), seedrepo.InsertProductParams{
				ID: "invalid-fixture", VendorKey: "fixture", DisplayName: "Fixture", Category: tc.category,
				BundleIds: []string{}, Binaries: append([]string{}, tc.binaries...), ConfigDirs: []string{},
				ProcessNames: append([]string{}, tc.processes...), ClientInfoNames: []string{},
			}))
			err = store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog)
			require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
			require.ErrorContains(t, err, tc.want)
			require.Same(t, initial, store.Snapshot(), "a bad definition must not replace the usable catalog")
			loaded, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
			require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
			require.Nil(t, loaded, "initial reads reject the same invalid definitions")
		})
	}
}

func TestOrganizationOverlayRejectsCombinedLimits(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"target count", "encoded size"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			organizationID := "org_" + uuid.NewString()
			_, err = organizationsrepo.New(db).UpsertOrganizationMetadata(t.Context(), organizationsrepo.UpsertOrganizationMetadataParams{
				ID: organizationID, Name: "Overlay limit fixture", Slug: "limit-" + uuid.NewString(),
			})
			require.NoError(t, err)
			custom := aitargets.Target{ID: "custom-fixture", DisplayName: "Custom fixture", Category: aitargets.CategoryHarness,
				Signatures: aitargets.Signatures{Binaries: []string{"fixture"}},
			}
			count := aitargets.MaxTargets
			var dirs []string
			want := "exceeds the maximum"
			if kind == "encoded size" {
				count = 1
				// Each target fits, but two targets exceed the whole-envelope cap.
				dirs = make([]string, aitargets.MaxSignatureEntries)
				for i := range dirs {
					dirs[i] = "/" + strings.Repeat("x", aitargets.MaxEnvelopeBytes/(2*aitargets.MaxSignatureEntries))
				}
				custom.Signatures.ConfigDirs = dirs
				want = "byte cap"
			}
			products := make([]aivendors.Product, 0, count)
			for i := range count {
				products = append(products, aivendors.Product{ID: fmt.Sprintf("builtin-%d", i), VendorKey: "fixture", DisplayName: "Builtin fixture", Category: aivendors.CategoryHarness,
					Signatures: aivendors.Signatures{Binaries: []string{"fixture"}, ConfigDirs: dirs},
				})
			}
			catalog := aivendors.NewSnapshot(products)
			require.NoError(t, aitargets.ValidateCatalog(catalog))
			require.NoError(t, aitargets.ValidateServed(catalog, []aitargets.Target{custom}))
			queries := agentrepo.New(db)
			_, err = queries.UpsertAIScanTarget(t.Context(), aitargets.UpsertParams(organizationID, custom))
			require.NoError(t, err)
			list, err := aitargets.LoadOrganizationList(t.Context(), queries, organizationID, catalog)
			require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
			require.ErrorContains(t, err, want)
			require.Nil(t, list)
		})
	}
}

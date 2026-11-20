package aivendors_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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
	require.False(t, snapshot.BuiltIn(), "the seeded catalog loads from the database")
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
	for _, preset := range snapshot.Catalog() {
		require.Equal(t, strings.Contains(preset.URL, "*"), preset.IsPattern(), preset.URL)
	}
	// Documents load in URL order, so matcher lists may differ in order only.
	require.ElementsMatch(t, sortedMatchers(aitargets.Defaults(aivendors.CompiledSnapshot())), sortedMatchers(aitargets.Defaults(snapshot)))
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
			require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: d.ID, Url: "https://*.example.com/client.json", DisplayName: d.DisplayName, Enabled: false, DeletedAt: pgtype.Timestamptz{}}))
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

func TestLoadOmitsDisabledPatternsWithoutRealPath(t *testing.T) {
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
			ID: uuid.New(), AiProductID: "zed", Url: pattern, DisplayName: "Malformed pattern", Enabled: false, IsPattern: true,
		})
		require.NoError(t, err)
	}
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
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

func TestRefreshRejectsEmptyCatalog(t *testing.T) {
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
	require.ErrorContains(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog), "invalid AI client catalog")
	require.Same(t, initial, store.Snapshot(), "an empty load never replaces a good snapshot")
	loaded, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.Same(t, aivendors.CompiledSnapshot(), loaded, "an empty catalog falls back to the built-in one")
}

func TestLoadFallsBackToTheBuiltInCatalogBeforeTheSeedRuns(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	loaded, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.Same(t, aivendors.CompiledSnapshot(), loaded)
}

func TestRefreshReplacesTheBuiltInCatalogOnceALoadValidates(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	store := aivendors.NewStore(aivendors.CompiledSnapshot())
	require.ErrorContains(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog), "invalid AI client catalog")
	require.Same(t, aivendors.CompiledSnapshot(), store.Snapshot())
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	require.NotSame(t, aivendors.CompiledSnapshot(), store.Snapshot(), "a validated load replaces the built-in catalog even with equal content")
	require.Equal(t, aivendors.CompiledSnapshot().Fingerprint(), store.Snapshot().Fingerprint())
}

func TestRefreshRejectsOverlappingPatternsOfTwoProducts(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	// Neither this nor the ChatGPT pattern is narrower, so attribution would depend on URL order.
	_, err = seedrepo.New(db).InsertDocument(t.Context(), seedrepo.InsertDocumentParams{
		ID: uuid.New(), AiProductID: "zed", Url: "https://chatgpt.com/*/fixture/client.json", DisplayName: "Overlapping pattern", Enabled: true, IsPattern: true,
	})
	require.NoError(t, err)
	err = store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.ErrorContains(t, err, "invalid AI client catalog")
	require.ErrorContains(t, err, "overlap")
	require.Same(t, initial, store.Snapshot())
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
			_, err = seedrepo.New(db).InsertProduct(t.Context(), seedrepo.InsertProductParams{
				ID: "invalid-fixture", VendorKey: "fixture", DisplayName: "Fixture", Category: tc.category,
				BundleIds: []string{}, Binaries: append([]string{}, tc.binaries...), ConfigDirs: []string{},
				ProcessNames: append([]string{}, tc.processes...), ClientInfoNames: []string{},
			})
			require.NoError(t, err)
			err = store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog)
			require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
			require.ErrorContains(t, err, tc.want)
			require.Same(t, initial, store.Snapshot(), "a bad definition must not replace the usable catalog")
			loaded, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
			require.NoError(t, err)
			require.Same(t, aivendors.CompiledSnapshot(), loaded, "initial reads fall back on the same invalid definitions")
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
			require.NoError(t, err)
			err = aitargets.ValidateServed(catalog, list.Snapshot.Targets())
			require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
			require.ErrorContains(t, err, want)
		})
	}
}

func TestLoadedSnapshotMatchesCompiledSnapshot(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	loaded, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.False(t, loaded.BuiltIn(), "the seeded catalog loads from the database")
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
	require.Equal(t, compiled.Fingerprint(), loaded.Fingerprint(), "a freshly seeded database loads the built-in catalog, order included")
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
	require.NoError(t, err)
	require.Same(t, aivendors.CompiledSnapshot(), snapshot)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed Updated", DeletedAt: pgtype.Timestamptz{}}))
	require.ErrorIs(t, store.Refresh(t.Context(), db, logger, reject), errRejected)
	require.Same(t, initial, store.Snapshot())
}

func sortedMatchers(targets []aitargets.Target) []aitargets.Target {
	for i := range targets {
		slices.Sort(targets[i].GatewayClient.CIMDVendorKeys)
		slices.Sort(targets[i].GatewayClient.OAuthClientIDs)
		slices.Sort(targets[i].GatewayClient.ClientInfoNames)
	}
	return targets
}

// The Postgres cutover must not change what devices are told: the loaded
// catalog lists in a different order but serves the same built-in targets.
func TestLoadedSnapshotServesTheCompiledListVersionAndETag(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	loaded, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.False(t, loaded.BuiltIn(), "the seeded catalog loads from the database")
	t.Logf("compiled fingerprint %s, loaded fingerprint %s", aivendors.CompiledSnapshot().Fingerprint(), loaded.Fingerprint())

	want := aitargets.NewOrganizationList(aivendors.CompiledSnapshot(), nil).Snapshot
	got := aitargets.NewOrganizationList(loaded, nil).Snapshot
	require.Equal(t, want.ListVersion, got.ListVersion)
	require.Equal(t, want.ETag, got.ETag)
}

func TestSkippedRowIsLoggedOnceAcrossBootAndRefreshes(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	_, err = seedrepo.New(db).InsertDocument(t.Context(), seedrepo.InsertDocumentParams{
		ID: uuid.New(), AiProductID: "zed", Url: "https://*.example.com/client.json", DisplayName: "Malformed pattern", Enabled: false, IsPattern: true,
	})
	require.NoError(t, err)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, store.Refresh(cancelled, db, logger, aitargets.ValidateCatalog))
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	require.Equal(t, 1, strings.Count(logs.String(), "skip AI catalog row"), "boot logs the row; refreshes and a failed read do not repeat it")
}

func TestMalformedEnabledPatternRejectsTheCatalog(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	docs, err := seedrepo.New(db).ListDocuments(t.Context())
	require.NoError(t, err)
	for _, d := range docs {
		if d.Url == "https://chatgpt.com/oauth/*/client.json" {
			require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: d.ID, Url: "https://chatgpt.com/oauth/x*/client.json", DisplayName: d.DisplayName, Enabled: true, DeletedAt: pgtype.Timestamptz{}}))
		}
	}
	err = store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.ErrorContains(t, err, "invalid AI client catalog")
	require.ErrorContains(t, err, "complete path segment")
	require.Same(t, initial, store.Snapshot(), "a broken enabled pattern keeps the working client")
	loaded, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.True(t, loaded.BuiltIn(), "boot falls back to the built-in catalog")
}

func TestLoadDropsNullArrayElements(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	_, err = db.Exec(t.Context(), "UPDATE ai_products SET client_info_names = ARRAY[NULL, 'Zed']::text[], binaries = array_append(binaries, NULL) WHERE id = 'zed'")
	require.NoError(t, err)
	loaded, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.False(t, loaded.BuiltIn())
	target, ok := aitargets.DefaultByID(loaded, "zed")
	require.True(t, ok)
	require.Equal(t, []string{"Zed"}, target.GatewayClient.ClientInfoNames)
	require.NotContains(t, target.Signatures.Binaries, "")
}

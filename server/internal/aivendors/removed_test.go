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
	removedZedURL       = "https://zed.dev/oauth/client-metadata.json"
	removedPiPattern    = "https://pi.dev/oauth/*/client.json"
	removedPiConnection = "https://pi.dev/oauth/fixture-server/client.json"
)

var removedAt = pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite}

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

// requireRemovedOnlyKeepsBlocks checks a removed client ID is read only to keep blocks applying.
func requireRemovedOnlyKeepsBlocks(t *testing.T, snapshot *aivendors.Snapshot, clientID string, url string) {
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
			setProductDeleted(t, db, "zed", "Zed", removedAt)
		}, clientID: removedZedURL, url: removedZedURL, product: "zed", display: "Zed"},
		{name: "disabled document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, removedZedURL, false, pgtype.Timestamptz{})
		}, clientID: removedZedURL, url: removedZedURL, product: "zed", display: "Zed"},
		{name: "soft-deleted document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, removedZedURL, true, removedAt)
		}, clientID: removedZedURL, url: removedZedURL, product: "zed", display: "Zed"},
		{name: "soft-deleted pattern document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, removedPiPattern, true, removedAt)
		}, clientID: removedPiConnection, url: removedPiPattern, product: "pi", display: "Pi"},
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
			require.Equal(t, tc.product, last.ProductID)
			require.Equal(t, tc.display, last.DisplayName)
			require.Equal(t, tc.url, last.URL, "the matching document is reported")
			require.Equal(t, tc.product, last.VendorKey)
			requireRemovedOnlyKeepsBlocks(t, snapshot, tc.clientID, tc.url)
		})
	}
}

func TestLastKnownIgnoresClientsTheLiveCatalogAdmits(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	for _, clientID := range []string{removedZedURL, removedPiConnection, "https://unknown.example.com/client.json"} {
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
			editDocument(t, db, removedZedURL, true, removedAt)
			require.NoError(t, testrepo.New(db).InsertDocumentFixture(t.Context(), testrepo.InsertDocumentFixtureParams{ID: uuid.New(), AiProductID: "goose", Url: removedZedURL, DisplayName: "Goose", Enabled: tc.enabled, IsPattern: false}))
			snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
			require.NoError(t, err)
			require.False(t, snapshot.BuiltIn())

			last, ok := snapshot.LastKnown(removedZedURL)
			if tc.enabled {
				require.False(t, ok, "live attribution decides")
				preset, known := snapshot.CatalogPreset(removedZedURL)
				require.True(t, known)
				require.Equal(t, "goose", preset.ProductID)
				return
			}
			require.True(t, ok)
			require.Equal(t, "goose", last.ProductID, "the live disabled document wins over the removed one")
		})
	}
}

func TestRemovedDocumentsDoNotCountInValidation(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	// Enabled, this would overlap the ChatGPT pattern and reject the catalog.
	id := uuid.New()
	_, err := seedrepo.New(db).InsertDocument(t.Context(), seedrepo.InsertDocumentParams{
		ID: id, AiProductID: "zed", Url: "https://chatgpt.com/*/fixture/client.json", DisplayName: "Overlapping pattern", Enabled: true, IsPattern: true,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: id, Url: "https://chatgpt.com/*/fixture/client.json", DisplayName: "Overlapping pattern", Enabled: true, DeletedAt: removedAt}))
	// A malformed removed pattern is dropped, not reported.
	malformed := uuid.New()
	require.NoError(t, testrepo.New(db).InsertDocumentFixture(t.Context(), testrepo.InsertDocumentFixtureParams{ID: malformed, AiProductID: "zed", Url: "https://*.example.com/client.json", DisplayName: "Malformed", Enabled: true, IsPattern: true}))
	require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: malformed, Url: "https://*.example.com/client.json", DisplayName: "Malformed", Enabled: true, DeletedAt: removedAt}))

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

	setProductDeleted(t, db, "zed", "Zed", removedAt)
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	last, ok := store.Snapshot().LastKnown(removedZedURL)
	require.True(t, ok)
	require.Equal(t, "zed", last.ProductID)

	setProductDeleted(t, db, "zed", "Zed", pgtype.Timestamptz{})
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	_, ok = store.Snapshot().LastKnown(removedZedURL)
	require.False(t, ok)
	preset, known := store.Snapshot().CatalogPreset(removedZedURL)
	require.True(t, known)
	require.Equal(t, "zed", preset.ProductID)
}

func TestRefreshPublishesAChangeOnlyRemovedDocumentsSee(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	logger := testenv.NewLogger(t)
	setProductDeleted(t, db, "zed", "Zed", removedAt)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	// The product is already gone, so only the removed documents change.
	setProductDeleted(t, db, "zed", "Zed Renamed", removedAt)
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	require.NotEqual(t, initial.Fingerprint(), store.Snapshot().Fingerprint())
	last, ok := store.Snapshot().LastKnown(removedZedURL)
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
			setProductDeleted(t, db, "zed", "Zed", removedAt)
		}},
		{name: "disabled document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, removedZedURL, false, pgtype.Timestamptz{})
		}},
		{name: "soft-deleted document", remove: func(t *testing.T, db *pgxpool.Pool) {
			t.Helper()
			editDocument(t, db, removedZedURL, true, removedAt)
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
			last, ok := loaded.LastKnown(removedZedURL)
			require.True(t, ok)
			require.Equal(t, "zed", last.ProductID)
			requireRemovedOnlyKeepsBlocks(t, loaded, removedZedURL, removedZedURL)
		})
	}
}

func TestLastKnownFollowsTheMostRecentRemoval(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	editDocument(t, db, removedZedURL, true, removedAt)
	goose := uuid.New()
	require.NoError(t, testrepo.New(db).InsertDocumentFixture(t.Context(), testrepo.InsertDocumentFixtureParams{ID: goose, AiProductID: "goose", Url: removedZedURL, DisplayName: "Goose", Enabled: true, IsPattern: false}))
	later := pgtype.Timestamptz{Time: removedAt.Time.Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite}
	require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: goose, Url: removedZedURL, DisplayName: "Goose", Enabled: true, DeletedAt: later}))
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	last, ok := snapshot.LastKnown(removedZedURL)
	require.True(t, ok)
	require.Equal(t, "goose", last.ProductID, "the product that held the URL last decides")
}

func TestBuiltInFallbackNeverCreditsAMovedURLToItsOldProduct(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	editDocument(t, db, removedZedURL, true, removedAt)
	setProductDeleted(t, db, "zed", "Zed", removedAt)
	require.NoError(t, testrepo.New(db).InsertDocumentFixture(t.Context(), testrepo.InsertDocumentFixtureParams{ID: uuid.New(), AiProductID: "goose", Url: removedZedURL, DisplayName: "Goose", Enabled: true, IsPattern: false}))
	insertInvalidProduct(t, db)
	loaded, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.True(t, loaded.BuiltIn())
	_, ok := loaded.LastKnown(removedZedURL)
	require.False(t, ok, "the built-in catalog does not know the URL moved, so the old product must not claim it")
	_, ok = loaded.RemovedProduct("zed")
	require.False(t, ok)
}

func TestRemovedProductAndDisabledDocumentsAreReported(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	setProductDeleted(t, db, "zed", "Zed", removedAt)
	editDocument(t, db, "https://goose-docs.ai/oauth/client-metadata.json", false, pgtype.Timestamptz{})
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	product, ok := snapshot.RemovedProduct("zed")
	require.True(t, ok)
	require.Equal(t, "Zed", product.DisplayName)
	require.Equal(t, aivendors.CategoryHarness, product.Category)
	_, ok = snapshot.RemovedProduct("goose")
	require.False(t, ok, "a live product is not removed")
	require.True(t, snapshot.DocumentsDisabled("goose"))
	require.False(t, snapshot.DocumentsDisabled("claude-code"))
	require.False(t, snapshot.DocumentsDisabled("cursor"), "a product that never published a document has nothing to keep a block applying")
}

func TestRemovedDocumentsStopAtTheCapAndWarn(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	editDocument(t, db, removedZedURL, true, removedAt)
	editDocument(t, db, "https://goose-docs.ai/oauth/client-metadata.json", true, pgtype.Timestamptz{Time: removedAt.Time.Add(time.Hour), Valid: true, InfinityModifier: pgtype.Finite})
	snapshot, skipped, err := aivendors.LoadWithRemovedCap(t.Context(), db, aitargets.ValidateCatalog, 1)
	require.NoError(t, err)
	_, ok := snapshot.LastKnown("https://goose-docs.ai/oauth/client-metadata.json")
	require.True(t, ok, "the most recent removal is kept")
	_, ok = snapshot.LastKnown(removedZedURL)
	require.False(t, ok, "the older removal is past the cap")
	require.Len(t, skipped, 1)
	require.ErrorContains(t, skipped[0], "reached the cap of 1")

	_, skipped, err = aivendors.LoadWithRemovedCap(t.Context(), db, aitargets.ValidateCatalog, 3)
	require.NoError(t, err)
	require.Empty(t, skipped, "below the cap nothing is reported")
}

func TestOrganizationListShowsARemovedProductOnlyForItsDecision(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	setProductDeleted(t, db, "zed", "Zed", removedAt)
	editDocument(t, db, "https://goose-docs.ai/oauth/client-metadata.json", false, pgtype.Timestamptz{})
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	decisionRow := func(id string, decision aitargets.Decision) aitargets.Entry {
		target := aitargets.ZeroTarget()
		target.ID = id
		return aitargets.Entry{Target: target, Source: aitargets.SourceOrganization, Decision: aitargets.DecisionRecord{TargetID: id, Decision: decision, Rationale: ""}}
	}
	withoutRows := aitargets.NewOrganizationList(snapshot, nil)
	list := aitargets.NewOrganizationList(snapshot, []aitargets.Entry{decisionRow("zed", aitargets.DecisionBlocked), decisionRow("goose", aitargets.DecisionBlocked)})

	zed, ok := list.Entry("zed")
	require.True(t, ok)
	require.Equal(t, aitargets.CatalogRemoved, zed.Catalog)
	require.Equal(t, "Zed", zed.DisplayName)
	access := aitargets.SummarizeAccess(zed)
	require.Equal(t, aitargets.AccessBlocked, access.State)
	require.True(t, access.Enforceable)
	require.Equal(t, aitargets.CatalogRemoved, access.Catalog)

	goose, ok := list.Entry("goose")
	require.True(t, ok)
	require.Equal(t, aitargets.CatalogDocumentsDisabled, goose.Catalog)
	require.Equal(t, aitargets.AccessBlocked, aitargets.SummarizeAccess(goose).State)

	_, served := list.Snapshot.ByID("zed")
	require.False(t, served, "a removed product is never served to device agents")
	require.Equal(t, withoutRows.Snapshot.ListVersion, list.Snapshot.ListVersion)

	cleared := aitargets.NewOrganizationList(snapshot, []aitargets.Entry{decisionRow("zed", aitargets.DecisionUnreviewed), decisionRow("goose", aitargets.DecisionUnreviewed)})
	_, ok = cleared.Entry("zed")
	require.False(t, ok, "a removed product with nothing decided is not listed")
	goose, ok = cleared.Entry("goose")
	require.True(t, ok)
	require.False(t, aitargets.SummarizeAccess(goose).Enforceable, "nothing decided means nothing to keep")
}

func insertDocument(t *testing.T, db *pgxpool.Pool, product string, url string, enabled bool, pattern bool, deleted pgtype.Timestamptz) {
	t.Helper()
	id := uuid.New()
	require.NoError(t, testrepo.New(db).InsertDocumentFixture(t.Context(), testrepo.InsertDocumentFixtureParams{ID: id, AiProductID: product, Url: url, DisplayName: product, Enabled: enabled, IsPattern: pattern}))
	if deleted.Valid {
		require.NoError(t, testrepo.New(db).EditDocumentFixture(t.Context(), testrepo.EditDocumentFixtureParams{ID: id, Url: url, DisplayName: product, Enabled: enabled, DeletedAt: deleted}))
	}
}

func TestLastKnownPicksOneCandidateByOneRule(t *testing.T) {
	t.Parallel()
	later := pgtype.Timestamptz{Time: removedAt.Time.Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite}
	for _, tc := range []struct {
		name     string
		arrange  func(t *testing.T, db *pgxpool.Pool)
		clientID string
		want     string
	}{
		{
			// Case (a): the removed exact URL beats a broader disabled live pattern.
			name: "removed exact before disabled live pattern",
			arrange: func(t *testing.T, db *pgxpool.Pool) {
				t.Helper()
				editDocument(t, db, removedZedURL, true, removedAt)
				insertDocument(t, db, "goose", "https://zed.dev/oauth/*", false, true, pgtype.Timestamptz{})
			},
			clientID: removedZedURL, want: "zed",
		},
		{
			// Case (b): nothing records that the pattern covered the URL when it was removed, so exact still wins.
			name: "removed exact before a later removed pattern",
			arrange: func(t *testing.T, db *pgxpool.Pool) {
				t.Helper()
				editDocument(t, db, removedZedURL, true, removedAt)
				insertDocument(t, db, "goose", "https://zed.dev/oauth/*", true, true, later)
			},
			clientID: removedZedURL, want: "zed",
		},
		{
			name: "disabled live pattern before removed pattern",
			arrange: func(t *testing.T, db *pgxpool.Pool) {
				t.Helper()
				editDocument(t, db, removedPiPattern, true, removedAt)
				insertDocument(t, db, "goose", "https://pi.dev/*/fixture-server/client.json", false, true, pgtype.Timestamptz{})
			},
			clientID: removedPiConnection, want: "goose",
		},
		{
			name: "most recently removed pattern, whatever its specificity",
			arrange: func(t *testing.T, db *pgxpool.Pool) {
				t.Helper()
				editDocument(t, db, removedPiPattern, true, removedAt)
				insertDocument(t, db, "goose", "https://pi.dev/*/*/client.json", true, true, later)
			},
			clientID: removedPiConnection, want: "goose",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := seededCatalog(t)
			tc.arrange(t, db)
			snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
			require.NoError(t, err)
			require.False(t, snapshot.BuiltIn())
			last, ok := snapshot.LastKnown(tc.clientID)
			require.True(t, ok)
			require.Equal(t, tc.want, last.ProductID)
		})
	}
}

func TestLastKnownDropsAVendorKeyALiveProductCarries(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	setProductDeleted(t, db, "claude-code", "Claude Code", removedAt)
	setProductDeleted(t, db, "zed", "Zed", removedAt)
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	last, ok := snapshot.LastKnown("https://claude.ai/oauth/claude-code-client-metadata")
	require.True(t, ok)
	require.Equal(t, "claude-code", last.ProductID)
	require.Empty(t, last.VendorKey, "anthropic still names live products")
	last, ok = snapshot.LastKnown(removedZedURL)
	require.True(t, ok)
	require.Equal(t, "zed", last.VendorKey, "no live product carries zed")
}

func TestALiveProductWithOnlyRemovedDocumentsReadsAsDocumentsDisabled(t *testing.T) {
	t.Parallel()
	db := seededCatalog(t)
	editDocument(t, db, removedZedURL, true, removedAt)
	snapshot, err := aivendors.Load(t.Context(), db, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	require.True(t, snapshot.DocumentsDisabled("zed"))
	_, removed := snapshot.RemovedProduct("zed")
	require.False(t, removed, "the product itself is live")
}

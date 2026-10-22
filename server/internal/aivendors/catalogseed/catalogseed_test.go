package catalogseed

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestApplyCompleteAndIdempotent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, Apply(ctx, db))
	q := repo.New(db)
	products, err := q.ListProducts(ctx)
	require.NoError(t, err)
	documents, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	require.Len(t, products, 51)
	require.Len(t, documents, 24)

	var data snapshot
	require.NoError(t, json.Unmarshal(catalog, &data))
	for _, p := range data.Products {
		var found bool
		for _, row := range products {
			if row.ID != p.ID {
				continue
			}
			found = true
			require.Equal(t, p.VendorKey, row.VendorKey)
			require.Equal(t, p.DisplayName, row.DisplayName)
			require.Equal(t, conv.ToPGTextEmpty(p.Category), row.Category)
			require.Equal(t, conv.DefaultSlice(p.Signatures.BundleIDs, []string{}), row.BundleIds)
			require.Equal(t, conv.DefaultSlice(p.Signatures.Binaries, []string{}), row.Binaries)
			require.Equal(t, conv.DefaultSlice(p.Signatures.ConfigDirs, []string{}), row.ConfigDirs)
			require.Equal(t, conv.DefaultSlice(p.Signatures.ProcessNames, []string{}), row.ProcessNames)
			require.Equal(t, conv.ToPGTextEmpty(p.VersionPlistKey), row.VersionPlistKey)
			require.Equal(t, conv.DefaultSlice(p.ClientInfoNames, []string{}), row.ClientInfoNames)
			require.False(t, row.Deleted)
			require.False(t, row.CreatedBy.Valid)
			require.False(t, row.UpdatedBy.Valid)
		}
		require.True(t, found, p.ID)
		for _, d := range p.Documents {
			var found bool
			for _, row := range documents {
				if row.Url != d.URL {
					continue
				}
				found = true
				require.Equal(t, p.ID, row.AiProductID)
				require.Equal(t, d.DisplayName, row.DisplayName)
				require.Equal(t, d.Enabled, row.Enabled)
				require.Equal(t, strings.Contains(d.URL, "*"), row.IsPattern)
				require.False(t, row.VerifiedAt.Valid)
				require.False(t, row.VerificationError.Valid)
			}
			require.True(t, found, d.URL)
		}
	}

	require.NoError(t, Apply(ctx, db))
	afterProducts, err := q.ListProducts(ctx)
	require.NoError(t, err)
	afterDocuments, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	require.Equal(t, products, afterProducts, "including timestamps")
	require.Equal(t, documents, afterDocuments, "including IDs and timestamps")
}

func TestApplyPreservesStaffEditsAndDeletions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, Apply(ctx, db))
	q := repo.New(db)
	require.NoError(t, q.EditProductFixture(ctx, repo.EditProductFixtureParams{
		ID: "claude-code", DisplayName: "Staff title",
		DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}))
	docs, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	for i, d := range docs {
		deletedAt := pgtype.Timestamptz{}
		if i%2 == 0 {
			deletedAt = pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true}
		}
		require.NoError(t, q.EditDocumentFixture(ctx, repo.EditDocumentFixtureParams{
			ID: d.ID, Url: "https://example.com/" + d.ID.String(), DisplayName: "Reviewed document",
			Enabled: false, DeletedAt: deletedAt,
		}))
	}
	beforeProducts, err := q.ListProducts(ctx)
	require.NoError(t, err)
	beforeDocs, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	require.NoError(t, Apply(ctx, db))
	afterProducts, err := q.ListProducts(ctx)
	require.NoError(t, err)
	afterDocs, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	require.Equal(t, beforeProducts, afterProducts)
	require.Equal(t, beforeDocs, afterDocs)
}

func TestApplyConcurrent(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- Apply(t.Context(), db) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	docs, err := repo.New(db).ListDocuments(t.Context())
	require.NoError(t, err)
	require.Len(t, docs, 24)
}

func TestBackfillOnlyUnlinkedBuiltinDecisions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := uuid.NewString()
	_, err = orgrepo.New(db).CreateOrganizationMetadataWithRequests(ctx, orgrepo.CreateOrganizationMetadataWithRequestsParams{
		ID: orgID, Name: "Example", Slug: "example",
	})
	require.NoError(t, err)
	q := agentrepo.New(db)
	_, err = q.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: orgID, ID: "claude-code", Status: "blocked", Rationale: conv.ToPGText("Review pending"),
	})
	require.NoError(t, err)
	_, err = q.UpsertAIScanTarget(ctx, agentrepo.UpsertAIScanTargetParams{
		OrganizationID: orgID, ID: "codex", DisplayName: conv.ToPGText("Organization definition"),
		BundleIds: []string{}, Binaries: []string{"custom"}, ConfigDirs: []string{}, ProcessNames: []string{},
		CimdVendorKeys: []string{}, OauthClientIds: []string{}, ClientInfoNames: []string{},
	})
	require.NoError(t, err)
	_, err = q.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: orgID, ID: "unknown", Status: "blocked",
	})
	require.NoError(t, err)
	before, err := q.ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.NoError(t, EnsureCatalog(ctx, db))
	afterCatalog, err := q.ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, before, afterCatalog, "catalog-only bootstrap never backfills another tenant")
	linked, err := q.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: orgID, ID: "goose", AiProductID: conv.ToPGText("zed"), Status: "approved",
	})
	require.NoError(t, err)
	before = slices.Insert(before, 2, linked)

	require.NoError(t, Apply(ctx, db))
	after, err := q.ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	before[0].AiProductID = conv.ToPGText("claude-code")
	require.Equal(t, before, after, "only the built-in reference changes, not decisions or timestamps")
	require.NoError(t, Apply(ctx, db))
	repeated, err := q.ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, after, repeated)
}

func TestApplyPreservesExistingDocumentURL(t *testing.T) {
	t.Parallel()
	for _, deleted := range []bool{false, true} {
		name := "live"
		if deleted {
			name = "deleted"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			q := repo.New(db)
			require.NoError(t, q.InsertProduct(ctx, repo.InsertProductParams{
				ID: "staff-product", VendorKey: "staff", DisplayName: "Staff product",
				BundleIds: []string{}, Binaries: []string{}, ConfigDirs: []string{},
				ProcessNames: []string{}, ClientInfoNames: []string{},
			}))
			id := uuid.New()
			const url = "https://claude.ai/oauth/claude-code-client-metadata"
			require.NoError(t, q.InsertDocument(ctx, repo.InsertDocumentParams{
				ID: id, AiProductID: "staff-product", Url: url, DisplayName: "Staff document", Enabled: false,
			}))
			if deleted {
				require.NoError(t, q.EditDocumentFixture(ctx, repo.EditDocumentFixtureParams{
					ID: id, Url: url, DisplayName: "Staff document", Enabled: false,
					DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true},
				}))
			}
			before, err := q.ListDocuments(ctx)
			require.NoError(t, err)
			require.NoError(t, Apply(ctx, db))
			after, err := q.ListDocuments(ctx)
			require.NoError(t, err)
			require.Len(t, after, 24)
			require.Contains(t, after, before[0])
		})
	}
}

func TestApplySkipsDocumentsOfDeletedProduct(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	q := repo.New(db)
	require.NoError(t, q.InsertProduct(ctx, repo.InsertProductParams{
		ID: "claude-code", VendorKey: "anthropic", DisplayName: "Staff title",
		BundleIds: []string{}, Binaries: []string{}, ConfigDirs: []string{},
		ProcessNames: []string{}, ClientInfoNames: []string{},
	}))
	require.NoError(t, q.EditProductFixture(ctx, repo.EditProductFixtureParams{
		ID: "claude-code", DisplayName: "Staff title",
		DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}))
	require.NoError(t, Apply(ctx, db))
	docs, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	require.Len(t, docs, 23)
	for _, d := range docs {
		require.NotEqual(t, "claude-code", d.AiProductID)
	}
}

package catalogseed

import (
	"bytes"
	"encoding/json"
	"flag"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestApplyCompleteAndIdempotent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
	q := repo.New(db)
	products, err := q.ListProducts(ctx)
	require.NoError(t, err)
	documents, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	require.Len(t, products, 53)
	require.Len(t, documents, 26)

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

	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
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
	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
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
	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
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
		wg.Go(func() { errs <- Apply(t.Context(), testenv.NewLogger(t), db) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	docs, err := repo.New(db).ListDocuments(t.Context())
	require.NoError(t, err)
	require.Len(t, docs, 26)
}

func TestBackfillOnlyUnlinkedBuiltinDecisions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := uuid.NewString()
	otherOrgID := uuid.NewString()
	for _, id := range []string{orgID, otherOrgID} {
		_, err = orgrepo.New(db).CreateOrganizationMetadataWithRequests(ctx, orgrepo.CreateOrganizationMetadataWithRequestsParams{
			ID: id, Name: "Example " + id, Slug: "example-" + id,
		})
		require.NoError(t, err)
	}
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
	_, err = q.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: otherOrgID, ID: "cursor", Status: "approved",
	})
	require.NoError(t, err)
	_, err = q.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: otherOrgID, ID: "claude", Status: "blocked",
	})
	require.NoError(t, err)
	before, err := q.ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	otherBefore, err := q.ListAIScanTargets(ctx, otherOrgID)
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

	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
	after, err := q.ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	before[0].AiProductID = conv.ToPGText("claude-code")
	require.Equal(t, before, after, "only the built-in reference changes, not decisions, links or timestamps")
	otherAfter, err := q.ListAIScanTargets(ctx, otherOrgID)
	require.NoError(t, err)
	require.Equal(t, "claude", otherAfter[0].ID)
	require.False(t, otherAfter[0].AiProductID.Valid, "a catalog product without signatures is not a scan built-in")
	otherBefore[1].AiProductID = conv.ToPGText("cursor")
	require.Equal(t, otherBefore, otherAfter, "every organization is backfilled")
	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
	repeated, err := q.ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, after, repeated)
}

func TestApplyCommitsCatalogWhenBackfillFails(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := uuid.NewString()
	_, err = orgrepo.New(db).CreateOrganizationMetadataWithRequests(ctx, orgrepo.CreateOrganizationMetadataWithRequestsParams{
		ID: orgID, Name: "Example", Slug: "example",
	})
	require.NoError(t, err)
	_, err = agentrepo.New(db).SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: orgID, ID: "claude-code", Status: "blocked",
	})
	require.NoError(t, err)
	lockTimeout := testenv.NewLockTimeoutPool(t, db, 100*time.Millisecond)
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		_, err := agentrepo.New(tx).GetAIScanTargetForUpdate(ctx, agentrepo.GetAIScanTargetForUpdateParams{
			OrganizationID: orgID, ID: "claude-code",
		})
		require.NoError(t, err)
		testenv.RequireLockNotAvailable(t, Apply(ctx, testenv.NewLogger(t), lockTimeout))
		return nil
	}))
	products, err := repo.New(db).ListProducts(ctx)
	require.NoError(t, err)
	require.Len(t, products, 53)
	documents, err := repo.New(db).ListDocuments(ctx)
	require.NoError(t, err)
	require.Len(t, documents, 26)
}

func TestBackfillSkipsDeletedProduct(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := uuid.NewString()
	_, err = orgrepo.New(db).CreateOrganizationMetadataWithRequests(ctx, orgrepo.CreateOrganizationMetadataWithRequestsParams{
		ID: orgID, Name: "Example", Slug: "example",
	})
	require.NoError(t, err)
	_, err = agentrepo.New(db).SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: orgID, ID: "claude-code", Status: "blocked",
	})
	require.NoError(t, err)
	require.NoError(t, EnsureCatalog(ctx, db))
	require.NoError(t, repo.New(db).EditProductFixture(ctx, repo.EditProductFixtureParams{
		ID: "claude-code", DisplayName: "Claude Code",
		DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}))
	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
	rows, err := agentrepo.New(db).ListAIScanTargets(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.False(t, rows[0].AiProductID.Valid, "a soft-deleted product is no longer in the catalog")
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
			_, err = q.InsertProduct(ctx, repo.InsertProductParams{
				ID: "staff-product", VendorKey: "staff", DisplayName: "Staff product",
				BundleIds: []string{}, Binaries: []string{}, ConfigDirs: []string{},
				ProcessNames: []string{}, ClientInfoNames: []string{},
			})
			require.NoError(t, err)
			id := uuid.New()
			const url = "https://claude.ai/oauth/claude-code-client-metadata"
			_, err = q.InsertDocument(ctx, repo.InsertDocumentParams{
				ID: id, AiProductID: "staff-product", Url: url, DisplayName: "Staff document", Enabled: false,
			})
			require.NoError(t, err)
			if deleted {
				require.NoError(t, q.EditDocumentFixture(ctx, repo.EditDocumentFixtureParams{
					ID: id, Url: url, DisplayName: "Staff document", Enabled: false,
					DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true},
				}))
			}
			before, err := q.ListDocuments(ctx)
			require.NoError(t, err)
			require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
			after, err := q.ListDocuments(ctx)
			require.NoError(t, err)
			require.Len(t, after, 26)
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
	_, err = q.InsertProduct(ctx, repo.InsertProductParams{
		ID: "claude-code", VendorKey: "anthropic", DisplayName: "Staff title",
		BundleIds: []string{}, Binaries: []string{}, ConfigDirs: []string{},
		ProcessNames: []string{}, ClientInfoNames: []string{},
	})
	require.NoError(t, err)
	require.NoError(t, q.EditProductFixture(ctx, repo.EditProductFixtureParams{
		ID: "claude-code", DisplayName: "Staff title",
		DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}))
	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
	docs, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	require.Len(t, docs, 25)
	for _, d := range docs {
		require.NotEqual(t, "claude-code", d.AiProductID)
	}
}

// codeOnly lists registry fields the seed never writes; they stay in code.
var codeOnly = map[string]bool{
	"Aliases": true, // Reported names change with plugin and telemetry code.
	"Family":  true, // Product families change with plugin and telemetry code.
}

// Every registry field except codeOnly must match the file.
func TestCatalogMatchesRegistry(t *testing.T) {
	t.Parallel()
	var data snapshot
	require.NoError(t, json.Unmarshal(catalog, &data))
	raw, err := json.Marshal(aivendors.Products()) //nolint:musttag // The registry's Go field names are the file format.
	require.NoError(t, err)
	var products []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &products))
	var actual bytes.Buffer
	actual.WriteString(`{"Notice":`)
	notice, err := json.Marshal(data.Notice)
	require.NoError(t, err)
	actual.Write(notice)
	actual.WriteString(`,"Products":[`)
	for i, p := range products {
		if i > 0 {
			actual.WriteByte(',')
		}
		actual.Write(withoutCodeOnly(t, p))
	}
	actual.WriteString("]}")
	if f := flag.Lookup("update"); f == nil || f.Value.String() != "true" {
		require.JSONEq(t, actual.String(), string(catalog), "catalog.json differs from the aivendors registry; regenerate it as the README describes")
	}
	testenv.RequireJSONGolden(t, "catalog.json", actual.Bytes())
}

// withoutCodeOnly drops codeOnly keys from a JSON object and keeps the rest in order.
func withoutCodeOnly(t *testing.T, object json.RawMessage) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(object))
	open, err := decoder.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), open)
	out := []byte{'{'}
	for decoder.More() {
		name, err := decoder.Token()
		require.NoError(t, err)
		var value json.RawMessage
		require.NoError(t, decoder.Decode(&value))
		if key, ok := name.(string); ok && codeOnly[key] {
			continue
		}
		if len(out) > 1 {
			out = append(out, ',')
		}
		encoded, err := json.Marshal(name)
		require.NoError(t, err)
		out = append(append(append(out, encoded...), ':'), value...)
	}
	return append(out, '}')
}

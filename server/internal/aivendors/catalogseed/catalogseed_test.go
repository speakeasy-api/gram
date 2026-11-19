package catalogseed

import (
	"context"
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
	aliases, err := q.ListAliases(ctx)
	require.NoError(t, err)
	require.Len(t, products, 53)
	require.Len(t, documents, 26)
	require.Len(t, aliases, 41)

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
			require.Equal(t, conv.ToPGTextEmpty(p.Family), row.Family)
			require.False(t, row.Deleted)
			require.False(t, row.CreatedBy.Valid)
			require.False(t, row.UpdatedBy.Valid)
		}
		require.True(t, found, p.ID)
		for _, a := range p.Aliases {
			var found bool
			for _, row := range aliases {
				if row.Name != a.Name {
					continue
				}
				found = true
				require.Equal(t, p.ID, row.AiProductID)
				require.Equal(t, conv.ToPGTextEmpty(a.Client), row.Client)
				require.False(t, row.Deleted)
				require.False(t, row.CreatedBy.Valid)
			}
			require.True(t, found, a.Name)
		}
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
	afterAliases, err := q.ListAliases(ctx)
	require.NoError(t, err)
	require.Equal(t, products, afterProducts, "including timestamps")
	require.Equal(t, documents, afterDocuments, "including IDs and timestamps")
	require.Equal(t, aliases, afterAliases, "including IDs and timestamps")
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
	require.NoError(t, q.EditProductFixture(ctx, repo.EditProductFixtureParams{
		ID: "claude", DisplayName: "Claude", Family: pgtype.Text{},
	}))
	require.NoError(t, q.EditProductFixture(ctx, repo.EditProductFixtureParams{
		ID: "cursor", DisplayName: "Cursor", Family: conv.ToPGText("staff-family"),
	}))
	aliases, err := q.ListAliases(ctx)
	require.NoError(t, err)
	for i, a := range aliases {
		switch i % 3 {
		case 0:
			require.NoError(t, q.EditAliasFixture(ctx, repo.EditAliasFixtureParams{
				ID: a.ID, Name: a.Name, Client: a.Client,
				DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true},
			}))
		case 1:
			require.NoError(t, q.EditAliasFixture(ctx, repo.EditAliasFixtureParams{
				ID: a.ID, Name: "staff-" + a.ID.String(), Client: conv.ToPGText("web"),
			}))
		}
	}
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
	beforeAliases, err := q.ListAliases(ctx)
	require.NoError(t, err)
	require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
	afterProducts, err := q.ListProducts(ctx)
	require.NoError(t, err)
	afterDocs, err := q.ListDocuments(ctx)
	require.NoError(t, err)
	afterAliases, err := q.ListAliases(ctx)
	require.NoError(t, err)
	require.Equal(t, beforeProducts, afterProducts)
	require.Equal(t, beforeDocs, afterDocs)
	require.Equal(t, beforeAliases, afterAliases)
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
	aliases, err := repo.New(db).ListAliases(t.Context())
	require.NoError(t, err)
	require.Len(t, aliases, 41)
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
	// Holding the row lock makes the backfill outlive its deadline.
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		_, err := agentrepo.New(tx).GetAIScanTargetForUpdate(ctx, agentrepo.GetAIScanTargetForUpdateParams{
			OrganizationID: orgID, ID: "claude-code",
		})
		require.NoError(t, err)
		applyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		require.ErrorContains(t, Apply(applyCtx, testenv.NewLogger(t), db), "backfill AI product decisions")
		return nil
	}))
	products, err := repo.New(db).ListProducts(ctx)
	require.NoError(t, err)
	require.Len(t, products, 53)
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
	aliases, err := q.ListAliases(ctx)
	require.NoError(t, err)
	require.Len(t, aliases, 36)
	for _, a := range aliases {
		require.NotEqual(t, "claude-code", a.AiProductID)
	}
}

func TestApplyPreservesExistingAliasName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		alias   string
		deleted bool
	}{
		{name: "live", alias: "claude-code-cli"},
		{name: "deleted", alias: "claude-code-cli", deleted: true},
		{name: "live respelled", alias: "Claude_Code CLI"},
		{name: "deleted respelled", alias: "Claude_Code CLI", deleted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			_, err = q.InsertAlias(ctx, repo.InsertAliasParams{
				ID: id, AiProductID: "staff-product", Name: tc.alias, Client: conv.ToPGText("ide"),
			})
			require.NoError(t, err)
			if tc.deleted {
				require.NoError(t, q.EditAliasFixture(ctx, repo.EditAliasFixtureParams{
					ID: id, Name: tc.alias, Client: conv.ToPGText("ide"),
					DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Valid: true},
				}))
			}
			before, err := q.ListAliases(ctx)
			require.NoError(t, err)
			require.NoError(t, Apply(ctx, testenv.NewLogger(t), db))
			after, err := q.ListAliases(ctx)
			require.NoError(t, err)
			require.Len(t, after, 41)
			require.Contains(t, after, before[0])
			for _, a := range after {
				require.False(t, a.AiProductID == "claude-code" && a.Name == "claude-code-cli")
			}
		})
	}
}

func TestCatalogReportedNamesBelongToOneProduct(t *testing.T) {
	t.Parallel()
	data, err := load()
	require.NoError(t, err)
	// Same fold as the alias unique index and Identify's hyphen-free key.
	fold := func(name string) string {
		return strings.Map(func(r rune) rune {
			if strings.ContainsRune(" \t_-", r) {
				return -1
			}
			return r
		}, strings.ToLower(name))
	}
	owners := map[string]string{}
	for _, p := range data.Products {
		for _, a := range p.Aliases {
			_, taken := owners[fold(a.Name)]
			require.False(t, taken, "alias %q folds to a name another alias holds", a.Name)
			owners[fold(a.Name)] = p.ID
		}
	}
	for _, p := range data.Products {
		for _, name := range p.ClientInfoNames {
			if owner, ok := owners[fold(name)]; ok {
				require.Equal(t, owner, p.ID, "%q is reported for two products", name)
			}
			owners[fold(name)] = p.ID
		}
	}
}

func TestCatalogMatchesRegistry(t *testing.T) {
	t.Parallel()
	var data snapshot
	require.NoError(t, json.Unmarshal(catalog, &data))
	registry := snapshot{Notice: data.Notice, Products: nil}
	for _, p := range aivendors.Products() {
		var aliases []alias
		for _, a := range p.Aliases {
			aliases = append(aliases, alias{Name: a.Name, Client: string(a.Client)})
		}
		var documents []document
		for _, d := range p.Documents {
			documents = append(documents, document{URL: d.URL, DisplayName: d.DisplayName, Enabled: d.Enabled})
		}
		registry.Products = append(registry.Products, product{
			ID: p.ID, VendorKey: p.VendorKey, Family: p.Family, DisplayName: p.DisplayName,
			Category: string(p.Category), Signatures: signatures(p.Signatures),
			VersionPlistKey: p.VersionPlistKey, ClientInfoNames: p.ClientInfoNames,
			Aliases: aliases, Documents: documents,
		})
	}
	actual, err := json.Marshal(registry)
	require.NoError(t, err)
	if f := flag.Lookup("update"); f == nil || f.Value.String() != "true" {
		require.JSONEq(t, string(actual), string(catalog), "catalog.json differs from the aivendors registry; regenerate it as the README describes")
	}
	testenv.RequireJSONGolden(t, "catalog.json", actual)
}

// Package catalogseed applies the frozen AI client bootstrap without replacing
// staff-managed catalog data. Production becomes authoritative after bootstrap.
package catalogseed

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

//go:embed catalog.json
var catalog []byte

// applyRevision changes when the application logic needs to run again even
// though the frozen bootstrap data is unchanged.
const applyRevision = "1"

// Version hashes both the frozen data and the revision of its application logic.
func Version() string {
	sum := sha256.Sum256(append([]byte(applyRevision+":"), catalog...))
	return hex.EncodeToString(sum[:])
}

// Apply bootstraps the catalog, then repairs existing built-in decisions in a
// separate transaction. It is safe to repeat after old replicas drain.
func Apply(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool) error {
	data, err := load()
	if err != nil {
		return err
	}
	inserted, err := insertCatalog(ctx, db, data)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(data.Products))
	for _, p := range data.Products {
		if len(p.Signatures.BundleIDs)+len(p.Signatures.Binaries)+len(p.Signatures.ConfigDirs)+len(p.Signatures.ProcessNames) > 0 {
			ids = append(ids, p.ID)
		}
	}
	linked, err := repo.New(db).BackfillDecisions(ctx, ids)
	if err != nil {
		return fmt.Errorf("backfill AI product decisions: %w", err)
	}
	logger.InfoContext(ctx, "ai-client-catalog seed applied",
		attr.SlogAICatalogSeedVersion(Version()),
		attr.SlogAICatalogProductsInserted(inserted.products),
		attr.SlogAICatalogDocumentsInserted(inserted.documents),
		attr.SlogAICatalogDecisionsLinked(linked),
	)
	return nil
}

// EnsureCatalog inserts reference data only. Demo provisioning uses this before
// tenant-scoped transactions, without repairing decisions in other organizations.
func EnsureCatalog(ctx context.Context, db *pgxpool.Pool) error {
	data, err := load()
	if err != nil {
		return err
	}
	_, err = insertCatalog(ctx, db, data)
	return err
}

func load() (snapshot, error) {
	var data snapshot
	decoder := json.NewDecoder(bytes.NewReader(catalog))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return data, fmt.Errorf("decode AI catalog bootstrap: %w", err)
	}
	owners := map[string]string{}
	for _, p := range data.Products {
		for _, d := range p.Documents {
			if owner, ok := owners[d.URL]; ok {
				return data, fmt.Errorf("AI catalog bootstrap: document %q belongs to %s and %s", d.URL, owner, p.ID)
			}
			owners[d.URL] = p.ID
		}
	}
	return data, nil
}

type insertedCounts struct {
	products, documents int64
}

func insertCatalog(ctx context.Context, db *pgxpool.Pool, data snapshot) (insertedCounts, error) {
	var counts insertedCounts
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		counts = insertedCounts{products: 0, documents: 0}
		q := repo.New(tx)
		for _, p := range data.Products {
			n, err := q.InsertProduct(ctx, repo.InsertProductParams{
				ID:              p.ID,
				VendorKey:       p.VendorKey,
				DisplayName:     p.DisplayName,
				Category:        conv.ToPGTextEmpty(p.Category),
				BundleIds:       conv.DefaultSlice(p.Signatures.BundleIDs, []string{}),
				Binaries:        conv.DefaultSlice(p.Signatures.Binaries, []string{}),
				ConfigDirs:      conv.DefaultSlice(p.Signatures.ConfigDirs, []string{}),
				ProcessNames:    conv.DefaultSlice(p.Signatures.ProcessNames, []string{}),
				VersionPlistKey: conv.ToPGTextEmpty(p.VersionPlistKey),
				ClientInfoNames: conv.DefaultSlice(p.ClientInfoNames, []string{}),
				Family:          conv.ToPGTextEmpty(p.Family),
			})
			if err != nil {
				return fmt.Errorf("seed AI product %s: %w", p.ID, err)
			}
			counts.products += n
			for _, d := range p.Documents {
				n, err := q.InsertDocument(ctx, repo.InsertDocumentParams{
					ID:          uuid.NewSHA1(uuid.NameSpaceURL, []byte("speakeasy:ai-catalog-bootstrap:"+d.URL)),
					AiProductID: p.ID,
					Url:         d.URL,
					DisplayName: d.DisplayName,
					Enabled:     d.Enabled,
					IsPattern:   strings.Contains(d.URL, "*"),
				})
				if err != nil {
					return fmt.Errorf("seed AI document for %s: %w", p.ID, err)
				}
				counts.documents += n
			}
		}
		return nil
	}); err != nil {
		return counts, fmt.Errorf("apply AI catalog bootstrap: %w", err)
	}
	return counts, nil
}

// Package catalogseed applies the frozen AI client bootstrap without replacing
// staff-managed catalog data. Production becomes authoritative after bootstrap.
package catalogseed

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
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

// Apply atomically bootstraps the catalog and repairs existing built-in decisions.
// It is safe to repeat after old replicas drain during a rolling deployment.
func Apply(ctx context.Context, db *pgxpool.Pool) error {
	return apply(ctx, db, true)
}

// EnsureCatalog inserts reference data only. Demo provisioning uses this before
// tenant-scoped transactions, without repairing decisions in other organizations.
func EnsureCatalog(ctx context.Context, db *pgxpool.Pool) error {
	return apply(ctx, db, false)
}

func apply(ctx context.Context, db *pgxpool.Pool, backfill bool) error {
	var data snapshot
	if err := json.Unmarshal(catalog, &data); err != nil {
		return fmt.Errorf("decode AI catalog bootstrap: %w", err)
	}
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		q := repo.New(tx)
		ids := make([]string, 0, len(data.Products))
		for _, p := range data.Products {
			if err := q.InsertProduct(ctx, repo.InsertProductParams{
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
			}); err != nil {
				return fmt.Errorf("seed AI product %s: %w", p.ID, err)
			}
			for _, d := range p.Documents {
				if err := q.InsertDocument(ctx, repo.InsertDocumentParams{
					ID:          uuid.NewSHA1(uuid.NameSpaceURL, []byte("speakeasy:ai-catalog-bootstrap:"+d.URL)),
					AiProductID: p.ID,
					Url:         d.URL,
					DisplayName: d.DisplayName,
					Enabled:     d.Enabled,
					IsPattern:   strings.Contains(d.URL, "*"),
				}); err != nil {
					return fmt.Errorf("seed AI document for %s: %w", p.ID, err)
				}
			}
			if len(p.Signatures.BundleIDs)+len(p.Signatures.Binaries)+len(p.Signatures.ConfigDirs)+len(p.Signatures.ProcessNames) > 0 {
				ids = append(ids, p.ID)
			}
		}
		if backfill {
			if err := q.BackfillDecisions(ctx, ids); err != nil {
				return fmt.Errorf("backfill AI product decisions: %w", err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("apply AI catalog bootstrap: %w", err)
	}
	return nil
}

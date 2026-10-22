package aivendors

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aivendors/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// RefreshInterval bounds catalog propagation without per-request database reads.
	RefreshInterval = time.Minute

	// loadTimeout bounds one poll so a stalled database does not stop future polls.
	loadTimeout = 10 * time.Second
)

// Load reads one consistent database revision. Invalid patterns are omitted from
// every projection and logged; database or validation errors reject the entire
// replacement. The required validator checks consumer contracts before publication.
func Load(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, validate func(*Snapshot) error) (*Snapshot, error) {
	if validate == nil {
		return nil, fmt.Errorf("AI catalog validator is required")
	}
	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, fmt.Errorf("begin AI catalog read: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	rows, err := q.ListCatalogProducts(ctx)
	if err != nil {
		return nil, fmt.Errorf("read AI catalog products: %w", err)
	}
	docs, err := q.ListCatalogDocuments(ctx)
	if err != nil {
		return nil, fmt.Errorf("read AI catalog documents: %w", err)
	}
	products := make([]Product, 0, len(rows))
	byID := make(map[string]int, len(rows))
	for _, p := range rows {
		byID[p.ID] = len(products)
		products = append(products, Product{
			ID:          p.ID,
			VendorKey:   p.VendorKey,
			DisplayName: p.DisplayName,
			Category:    Category(p.Category.String),
			Signatures: Signatures{
				BundleIDs:    p.BundleIds,
				Binaries:     p.Binaries,
				ConfigDirs:   p.ConfigDirs,
				ProcessNames: p.ProcessNames,
			},
			VersionPlistKey: p.VersionPlistKey.String,
			ClientInfoNames: p.ClientInfoNames,
			Documents:       nil,
		})
	}
	for _, d := range docs {
		if d.IsPattern {
			if err := validatePattern(d.Url); err != nil {
				logger.WarnContext(ctx, "skip invalid AI catalog pattern", attr.SlogError(fmt.Errorf("document %s: %w", d.ID, err)))
				continue
			}
		}
		i, ok := byID[d.AiProductID]
		if !ok {
			continue
		}
		products[i].Documents = append(products[i].Documents, Document{URL: d.Url, DisplayName: d.DisplayName, Enabled: d.Enabled, Pattern: d.IsPattern})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("finish AI catalog read: %w", err)
	}
	snapshot := NewSnapshot(products)
	if err := validate(snapshot); err != nil {
		return nil, fmt.Errorf("validate AI catalog: %w", err)
	}
	return snapshot, nil
}

// Refresh retains the current snapshot when a load fails. Full content hashes,
// never the truncated wire version, decide whether a replacement is needed.
func (s *Store) Refresh(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, validate func(*Snapshot) error) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	next, err := Load(ctx, db, logger, validate)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.Fingerprint() != next.Fingerprint() {
		s.snapshot = next
	}
	return nil
}

// Run refreshes until shutdown. Each process owns its cache; no durable work or
// Temporal actions are needed to maintain an expendable in-memory view.
func (s *Store) Run(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, validate func(*Snapshot) error) {
	s.run(ctx, logger, func(ctx context.Context) error { return s.Refresh(ctx, db, logger, validate) })
}

func (s *Store) run(ctx context.Context, logger *slog.Logger, refresh func(context.Context) error) {
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := refresh(ctx); err != nil {
				logger.WarnContext(ctx, "refresh AI catalog; retaining last snapshot", attr.SlogError(err))
			}
		}
	}
}

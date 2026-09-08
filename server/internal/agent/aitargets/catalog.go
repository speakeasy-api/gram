package aitargets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// DefaultCacheTTL bounds how stale a served snapshot can be on a replica.
const DefaultCacheTTL = 60 * time.Second

// readTimeout bounds one catalog read so a slow database cannot hold the
// plugin poll or scan ingest for longer than it.
const readTimeout = 5 * time.Second

// Revision actions recorded on ai_scan_catalog_revisions.
const (
	ActionSeed    = "seed"
	ActionUpsert  = "upsert"
	ActionEnable  = "enable"
	ActionDisable = "disable"
	ActionDelete  = "delete"
)

// Record is a catalog row with its timestamps, for the management surface.
type Record struct {
	Target

	// CreatedAt is when the id first entered the catalog.
	CreatedAt time.Time

	// UpdatedAt is when the row last changed.
	UpdatedAt time.Time
}

// Catalog reads the served target list from Postgres behind a short
// in-process cache. A failed refresh keeps the last good snapshot so neither
// the plugin poll nor scan ingest fails on catalog trouble.
type Catalog struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	ttl    time.Duration
	now    func() time.Time

	mu       sync.Mutex
	snapshot *Snapshot
	loadedAt time.Time
}

// NewCatalog builds a catalog over db refreshed at most every ttl.
func NewCatalog(logger *slog.Logger, db *pgxpool.Pool, ttl time.Duration) *Catalog {
	return &Catalog{
		logger:   logger.With(attr.SlogComponent("aitargets")),
		db:       db,
		ttl:      ttl,
		now:      time.Now,
		mu:       sync.Mutex{},
		snapshot: nil,
		loadedAt: time.Time{},
	}
}

// Load returns the served snapshot, seeding an empty catalog from Defaults
// on first read. It errors only when no snapshot has ever been read.
func (c *Catalog) Load(ctx context.Context) (*Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.snapshot != nil && c.now().Sub(c.loadedAt) < c.ttl {
		return c.snapshot, nil
	}
	snapshot, err := c.read(ctx)
	if err != nil {
		if c.snapshot != nil {
			c.logger.WarnContext(ctx, "ai scan catalog refresh failed; serving the last good snapshot", attr.SlogError(err))
			return c.snapshot, nil
		}
		return nil, fmt.Errorf("load ai scan catalog: %w", err)
	}
	c.snapshot = snapshot
	c.loadedAt = c.now()
	return snapshot, nil
}

// Invalidate expires the cached snapshot so the next Load reads Postgres.
// The snapshot itself is kept as the fallback for a failed refresh.
func (c *Catalog) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadedAt = time.Time{}
}

// ListRecords returns every non-deleted target, enabled or not, ordered by
// id, together with the list version, read from one database snapshot and
// bypassing the cache.
func (c *Catalog) ListRecords(ctx context.Context) ([]Record, int32, error) {
	tx, err := c.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, 0, fmt.Errorf("begin catalog listing: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	version, err := queries.GetAIScanCatalogListVersion(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("read ai scan catalog version: %w", err)
	}
	rows, err := queries.ListAIScanTargets(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list ai scan targets: %w", err)
	}
	records := make([]Record, 0, len(rows))
	for _, row := range rows {
		records = append(records, RecordFromRow(row))
	}
	return records, version, nil
}

// read loads the version and enabled targets in one repeatable-read
// transaction so a concurrent write cannot pair a new version with an old
// list.
func (c *Catalog) read(ctx context.Context) (*Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	if err := SeedDefaults(ctx, c.db); err != nil {
		return nil, err
	}

	tx, err := c.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, fmt.Errorf("begin catalog read: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	version, err := queries.GetAIScanCatalogListVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("read ai scan catalog version: %w", err)
	}
	rows, err := queries.ListEnabledAIScanTargets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled ai scan targets: %w", err)
	}
	targets := make([]Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, RecordFromRow(row).Target)
	}
	return NewSnapshot(version, targets), nil
}

// SeedDefaults bootstraps an empty catalog from Defaults, one seed revision
// per target. A catalog with any revision is left alone, so deleting every
// target never resurrects the defaults.
func SeedDefaults(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ai scan catalog seed: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := repo.New(tx)
	if err := queries.AcquireAIScanCatalogLock(ctx); err != nil {
		return fmt.Errorf("lock ai scan catalog: %w", err)
	}
	version, err := queries.GetAIScanCatalogListVersion(ctx)
	if err != nil {
		return fmt.Errorf("read ai scan catalog version: %w", err)
	}
	if version > 0 {
		return nil
	}

	defaults := Defaults()
	if err := Validate(defaults); err != nil {
		return fmt.Errorf("validate default ai scan targets: %w", err)
	}
	for _, target := range defaults {
		if _, err := queries.UpsertAIScanTarget(ctx, upsertParams(target)); err != nil {
			return fmt.Errorf("seed ai scan target %q: %w", target.ID, err)
		}
		if _, err := RecordRevision(ctx, queries, Revision{
			TargetID: target.ID,
			Action:   ActionSeed,
			Actor:    Actor{UserID: "", Email: ""},
			Reason:   "bootstrap from aitargets.Defaults",
			Before:   nil,
			After:    &target,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ai scan catalog seed: %w", err)
	}
	return nil
}

// Actor identifies who made a catalog change; empty for the seed.
type Actor struct {
	// UserID is the platform administrator's user id.
	UserID string

	// Email is the platform administrator's email at the time of the change.
	Email string
}

// Revision describes one catalog change to record.
type Revision struct {
	// TargetID is the id the change affected.
	TargetID string

	// Action is one of the Action* constants.
	Action string

	// Actor is who made the change.
	Actor Actor

	// Reason is the free-text justification, if any.
	Reason string

	// Before is the target as it was, nil on creation.
	Before *Target

	// After is the target as it is now, nil on deletion.
	After *Target
}

// RecordRevision appends one revision inside the caller's transaction and
// returns the new list version. Callers must hold the catalog advisory lock.
func RecordRevision(ctx context.Context, queries *repo.Queries, revision Revision) (int32, error) {
	before, err := marshalTarget(revision.Before)
	if err != nil {
		return 0, err
	}
	after, err := marshalTarget(revision.After)
	if err != nil {
		return 0, err
	}
	row, err := queries.InsertAIScanCatalogRevision(ctx, repo.InsertAIScanCatalogRevisionParams{
		TargetID:     revision.TargetID,
		Action:       revision.Action,
		ActorUserID:  conv.ToPGTextEmpty(revision.Actor.UserID),
		ActorEmail:   conv.ToPGTextEmpty(revision.Actor.Email),
		Reason:       conv.ToPGTextEmpty(revision.Reason),
		TargetBefore: before,
		TargetAfter:  after,
	})
	if err != nil {
		return 0, fmt.Errorf("record ai scan catalog revision for %q: %w", revision.TargetID, err)
	}
	return row.Revision, nil
}

func marshalTarget(target *Target) ([]byte, error) {
	if target == nil {
		return nil, nil
	}
	data, err := json.Marshal(target)
	if err != nil {
		return nil, fmt.Errorf("encode ai scan target %q: %w", target.ID, err)
	}
	return data, nil
}

// RecordFromRow converts a catalog row into a Record.
func RecordFromRow(row repo.AiScanTarget) Record {
	var hint *VersionHint
	if row.VersionPlistKey.Valid && row.VersionPlistKey.String != "" {
		hint = &VersionHint{PlistKey: row.VersionPlistKey.String}
	}
	return Record{
		Target: Target{
			ID:          row.ID,
			DisplayName: row.DisplayName,
			Category:    Category(row.Category),
			Signatures: Signatures{
				BundleIDs:    orEmpty(row.BundleIds),
				Binaries:     orEmpty(row.Binaries),
				ConfigDirs:   orEmpty(row.ConfigDirs),
				ProcessNames: orEmpty(row.ProcessNames),
			},
			VersionHint: hint,
			Enabled:     row.Enabled,
		},
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

// upsertParams maps a validated target onto the upsert query.
func upsertParams(target Target) repo.UpsertAIScanTargetParams {
	plistKey := pgtype.Text{String: "", Valid: false}
	if target.VersionHint != nil {
		plistKey = conv.ToPGTextEmpty(target.VersionHint.PlistKey)
	}
	return repo.UpsertAIScanTargetParams{
		ID:              target.ID,
		DisplayName:     target.DisplayName,
		Category:        string(target.Category),
		BundleIds:       orEmpty(target.Signatures.BundleIDs),
		Binaries:        orEmpty(target.Signatures.Binaries),
		ConfigDirs:      orEmpty(target.Signatures.ConfigDirs),
		ProcessNames:    orEmpty(target.Signatures.ProcessNames),
		VersionPlistKey: plistKey,
		Enabled:         target.Enabled,
	}
}

// UpsertParams maps a validated target onto the upsert query.
func UpsertParams(target Target) repo.UpsertAIScanTargetParams {
	return upsertParams(target)
}

// ErrNotFound reports a catalog id that does not exist or is deleted.
var ErrNotFound = errors.New("ai scan target not found")

// orEmpty keeps nil slices out of JSON and Postgres.
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

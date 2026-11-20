package aivendors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

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

// errRejected marks a catalog that was read but is empty or invalid.
var errRejected = errors.New("AI catalog rejected")

// Load reads the catalog, falling back to the built-in one when it is empty or invalid; only a failed read is an error.
func Load(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, validate func(*Snapshot) error) (*Snapshot, error) {
	snapshot, skipped, err := load(ctx, db, validate)
	keys := make([]string, 0, len(skipped))
	for _, row := range skipped {
		logger.WarnContext(ctx, "skip AI catalog row", attr.SlogError(row))
		keys = append(keys, row.Error())
	}
	if errors.Is(err, errRejected) {
		logger.ErrorContext(ctx, "AI catalog rejected; serving the built-in catalog", attr.SlogError(err))
		snapshot = CompiledSnapshot()
		if len(keys) > 0 {
			builtIn := *snapshot
			snapshot = &builtIn
		}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if len(keys) > 0 {
		snapshot.skipped = keys
	}
	return snapshot, nil
}

// load returns the snapshot and the rows it skipped, so callers decide how often to report them.
func load(ctx context.Context, db *pgxpool.Pool, validate func(*Snapshot) error) (*Snapshot, []error, error) {
	if validate == nil {
		return nil, nil, fmt.Errorf("AI catalog validator is required")
	}
	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, nil, fmt.Errorf("begin AI catalog read: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	rows, err := q.ListCatalogProducts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read AI catalog products: %w", err)
	}
	docs, err := q.ListCatalogDocuments(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read AI catalog documents: %w", err)
	}
	products := make([]Product, 0, len(rows))
	byID := make(map[string]int, len(rows))
	for _, p := range rows {
		byID[p.ID] = len(products)
		products = append(products, Product{
			ID:        p.ID,
			VendorKey: p.VendorKey,
			// Families and aliases live in the compiled registry by design.
			Family:      "",
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
			Aliases:         nil,
			Documents:       nil,
		})
	}
	var skipped []error
	for _, d := range docs {
		if d.IsPattern {
			if err := validatePattern(d.Url); err != nil {
				skipped = append(skipped, fmt.Errorf("document %s: %w", d.ID, err))
				continue
			}
		}
		i, ok := byID[d.AiProductID]
		if !ok {
			skipped = append(skipped, fmt.Errorf("document %s: product %s is not live", d.ID, d.AiProductID))
			continue
		}
		products[i].Documents = append(products[i].Documents, Document{URL: d.Url, DisplayName: d.DisplayName, Enabled: d.Enabled, Pattern: d.IsPattern})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("finish AI catalog read: %w", err)
	}
	snapshot := NewSnapshot(products)
	if err := validateCatalog(snapshot); err != nil {
		return nil, skipped, fmt.Errorf("%w: %w", errRejected, err)
	}
	if err := validate(snapshot); err != nil {
		return nil, skipped, fmt.Errorf("%w: %w", errRejected, err)
	}
	return snapshot, skipped, nil
}

// Refresh keeps the current snapshot when a load fails, is empty or is invalid.
func (s *Store) Refresh(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, validate func(*Snapshot) error) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	next, skipped, err := load(ctx, db, validate)
	if err == nil || errors.Is(err, errRejected) {
		reported := make(map[string]struct{}, len(skipped))
		for _, row := range skipped {
			reported[row.Error()] = struct{}{}
			if _, seen := s.reported[row.Error()]; !seen {
				logger.WarnContext(ctx, "skip AI catalog row", attr.SlogError(row))
			}
		}
		s.reported = reported
	}
	if err != nil {
		return err
	}
	s.confirmed.Store(time.Now().UnixNano())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.builtIn || s.snapshot.Fingerprint() != next.Fingerprint() {
		s.snapshot = next
	}
	return nil
}

// Run refreshes until shutdown; each process owns its cache.
func (s *Store) Run(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, meterProvider metric.MeterProvider, validate func(*Snapshot) error) {
	failures, stop := s.refreshMetrics(logger, meterProvider)
	defer stop()
	s.run(ctx, logger, failures, func(ctx context.Context) error { return s.Refresh(ctx, db, logger, validate) })
}

const (
	meterScope = "github.com/speakeasy-api/gram/server/internal/aivendors"

	meterRefreshFailures = "ai_catalog.refresh.failures"

	meterSnapshotAge = "ai_catalog.snapshot.age"

	meterBuiltIn = "ai_catalog.built_in"
)

// refreshMetrics returns the failure counter and registers the snapshot gauges until stop.
func (s *Store) refreshMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) (metric.Int64Counter, func()) {
	meter := meterProvider.Meter(meterScope)
	failures, err := meter.Int64Counter(meterRefreshFailures,
		metric.WithDescription("AI client catalog refreshes that failed and kept the current snapshot."),
		metric.WithUnit("{refresh}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterRefreshFailures), attr.SlogError(err))
		failures = noop.Int64Counter{}
	}
	age, err := meter.Float64ObservableGauge(meterSnapshotAge,
		metric.WithDescription("Seconds since the served AI client catalog snapshot last matched the database."),
		metric.WithUnit("s"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterSnapshotAge), attr.SlogError(err))
		return failures, func() {}
	}
	builtIn, err := meter.Int64ObservableGauge(meterBuiltIn,
		metric.WithDescription("1 while the process serves the built-in AI client catalog instead of the database one."),
		metric.WithUnit("{catalog}"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterBuiltIn), attr.SlogError(err))
		return failures, func() {}
	}
	registration, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveFloat64(age, time.Since(time.Unix(0, s.confirmed.Load())).Seconds())
		var serving int64
		if s.Snapshot().builtIn {
			serving = 1
		}
		o.ObserveInt64(builtIn, serving)
		return nil
	}, age, builtIn)
	if err != nil {
		logger.ErrorContext(context.Background(), "register metric callback", attr.SlogMetricName(meterSnapshotAge), attr.SlogError(err))
		return failures, func() {}
	}
	return failures, func() { _ = registration.Unregister() }
}

func (s *Store) run(ctx context.Context, logger *slog.Logger, failures metric.Int64Counter, refresh func(context.Context) error) {
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()
	lastErr := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := refreshRecovered(ctx, refresh)
			switch {
			case err != nil:
				failures.Add(ctx, 1)
				if err.Error() != lastErr {
					logger.WarnContext(ctx, "refresh AI catalog; keeping the current snapshot", attr.SlogError(err))
				}
				lastErr = err.Error()
			case lastErr != "":
				logger.InfoContext(ctx, "AI catalog refresh recovered")
				lastErr = ""
			}
		}
	}
}

// refreshRecovered turns a panic in one refresh into an error carrying its stack.
func refreshRecovered(ctx context.Context, refresh func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("AI catalog refresh panicked: %v\n%s", r, debug.Stack())
		}
	}()
	return refresh(ctx)
}

// Package demoseed provisions the shared demo organization
// (constants.DemoOrganizationID). The seed is Go code compiled into the server
// binary, so the daily `gram demo-seed` run always applies the seed that
// shipped with the current deploy — no migrations, no runtime file fetches.
// Authoring docs live in seed/demo/.
package demoseed

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

//go:embed openapi.yaml
var openapiDoc []byte

const (
	clickHouseDeleteVisibilityTimeout = 30 * time.Second
	clickHouseDeleteVisibilityPoll    = 250 * time.Millisecond
	demoSeedLockCleanupTimeout        = 5 * time.Second
)

// Run replaces the data for spec's tenant while holding the shared advisory
// lock. The Postgres seed runs in one transaction, so a failed run leaves the
// previous data in place; each statement in it gets its own statement
// timeout. The ClickHouse seed then deletes both source rows and
// materialized-view targets before its inserts incrementally repopulate the
// summaries.
//
// Pass DefaultSpec() for the shared production demo org.
func Run(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, ch driver.Conn, blob assets.BlobStore, spec Spec) (retErr error) {
	logger = logger.With(attr.SlogComponent("demoseed"), attr.SlogOrganizationID(spec.OrgID))

	if err := spec.Validate(); err != nil {
		return err
	}

	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire postgres connection: %w", err)
	}

	const lockName = "gram-demo-seed"
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", lockName).Scan(&locked); err != nil {
		// Cancellation can surface after PostgreSQL acquired the session lock.
		// Destroy the uncertain session instead of returning it to the pool.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), demoSeedLockCleanupTimeout)
		defer cancel()
		if closeErr := conn.Hijack().Close(cleanupCtx); closeErr != nil {
			logger.ErrorContext(cleanupCtx, "close uncertain demo seed lock session", attr.SlogError(closeErr))
		}
		return fmt.Errorf("acquire demo seed advisory lock: %w", err)
	}
	if !locked {
		conn.Release()
		return errors.New("another demo seed run holds the advisory lock; refusing to run concurrently")
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), demoSeedLockCleanupTimeout)
		defer cancel()

		var unlocked bool
		unlockErr := conn.QueryRow(unlockCtx, "SELECT pg_advisory_unlock(hashtext($1))", lockName).Scan(&unlocked)
		if unlockErr == nil && unlocked {
			conn.Release()
			return
		}
		if unlockErr == nil {
			unlockErr = errors.New("demo seed advisory lock was not held by this session")
		}
		logger.ErrorContext(unlockCtx, "release demo seed advisory lock", attr.SlogError(unlockErr))
		if closeErr := conn.Hijack().Close(unlockCtx); closeErr != nil {
			logger.ErrorContext(unlockCtx, "close demo seed lock session", attr.SlogError(closeErr))
		}
		retErr = errors.Join(retErr, fmt.Errorf("release demo seed advisory lock: %w", unlockErr))
	}()

	s, err := seedPostgres(ctx, logger, conn, blob, spec)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "demo seed applied to postgres")

	if err := s.runClickHouse(ctx, ch); err != nil {
		return fmt.Errorf("apply demo seed to clickhouse: %w", err)
	}
	logger.InfoContext(ctx, "demo seed applied to clickhouse")
	return nil
}

// txBeginner starts a transaction. Run passes the *pgxpool.Conn that holds the
// advisory lock, so the seed runs inside the locked session; tests pass the
// *pgxpool.Pool.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// seedPostgres runs the Postgres seed for spec in one transaction on db, so a
// failed run leaves the previous data in place. It returns the seeder, whose
// clock the ClickHouse seed reuses.
func seedPostgres(ctx context.Context, logger *slog.Logger, db txBeginner, blob assets.BlobStore, spec Spec) (*seeder, error) {
	s := &seeder{spec: spec, q: nil, logger: logger, now: time.Time{}, signalSequences: nil}
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		s.q = repo.New(tx)
		if err := s.runPostgres(ctx); err != nil {
			return err
		}
		return s.uploadOpenAPIDoc(ctx, blob)
	}); err != nil {
		return nil, fmt.Errorf("apply demo seed to postgres: %w", err)
	}
	return s, nil
}

// uploadOpenAPIDoc places the Sources page's "OpenAPI Specification" blob,
// which SQL alone cannot: it writes the embedded spec through the asset store
// (fs locally, GCS in prod) and points the seeded assets row at it.
func (s *seeder) uploadOpenAPIDoc(ctx context.Context, blob assets.BlobStore) error {
	docSHA := sha256.Sum256(openapiDoc)
	w, objURL, err := blob.Write(ctx, "demoseed/"+s.spec.OrgSlug+"-openapi.yaml", "application/x-yaml", int64(len(openapiDoc)))
	if err != nil {
		return fmt.Errorf("open demo openapi asset for writing: %w", err)
	}
	if _, err := w.Write(openapiDoc); err != nil {
		defer o11y.NoLogDefer(w.Close)
		return fmt.Errorf("write demo openapi asset: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finalize demo openapi asset: %w", err)
	}
	if err := s.q.PointAssetAtUpload(ctx, repo.PointAssetAtUploadParams{
		Url:           objURL.String(),
		Sha256:        hex.EncodeToString(docSHA[:]),
		ContentLength: int64(len(openapiDoc)),
		ID:            s.assetID(),
		ProjectID:     s.projectID(),
	}); err != nil {
		return fmt.Errorf("point demo assets row at the uploaded spec: %w", err)
	}
	s.logger.InfoContext(ctx, "demo openapi asset uploaded")
	return nil
}

func waitForClickHouseDelete(
	ctx context.Context,
	timeout time.Duration,
	pollInterval time.Duration,
	countRows func(context.Context) (uint64, error),
) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		remaining, err := countRows(waitCtx)
		if err != nil {
			return fmt.Errorf("check demo delete visibility: %w", err)
		}
		if remaining == 0 {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for demo delete visibility with %d rows remaining: %w", remaining, context.Cause(waitCtx))
		case <-ticker.C:
		}
	}
}

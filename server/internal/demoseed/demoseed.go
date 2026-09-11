// Package demoseed provisions the shared demo organization
// (constants.DemoOrganizationID). The SQL sources are embedded so the seed
// versions atomically with the server binary: the daily `gram demo-seed` run
// always applies the seed that shipped with the current deploy — no
// migrations, no runtime file fetches. Authoring docs live in seed/demo/.
package demoseed

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

//go:embed postgres.sql
var postgresSQL string

//go:embed clickhouse.sql
var clickhouseSQL string

//go:embed openapi.yaml
var openapiDoc []byte

const (
	clickHouseDeleteVisibilityTimeout = 30 * time.Second
	clickHouseDeleteVisibilityPoll    = 250 * time.Millisecond
)

// Run applies the seed for spec's tenant: Postgres first (installs and
// executes demo.ensure_demo_org(), whose pre/postflight asserts abort the
// transaction on any isolation violation), then ClickHouse (scoped deletes +
// inserts with throwIf postflights). Both halves are idempotent; ordering
// matters only because ClickHouse rows reference Postgres ids.
//
// Pass DefaultSpec() for the shared production demo org — the scripts are
// written against its literals, so they run through unmodified.
func Run(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, ch driver.Conn, blob assets.BlobStore, spec Spec) error {
	logger = logger.With(attr.SlogComponent("demoseed"), attr.SlogOrganizationID(spec.OrgID))

	if err := spec.Validate(); err != nil {
		return err
	}

	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire postgres connection: %w", err)
	}
	defer conn.Release()

	// Serialize whole runs (Postgres AND ClickHouse) behind one advisory
	// lock: two overlapping runs would interleave the ClickHouse
	// delete+insert phases and leave duplicated telemetry/summaries. The
	// session lock is held on this pooled connection, so it must be released
	// explicitly before the connection returns to the pool.
	// The lock is scoped to the tenant: distinct Specs write disjoint rows, so
	// only same-tenant runs need to exclude each other.
	lockName := "gram-demo-seed:" + spec.OrgID
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", lockName).Scan(&locked); err != nil {
		return fmt.Errorf("acquire demo seed advisory lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("another demo seed run holds the advisory lock; refusing to run concurrently")
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(hashtext($1))", lockName); err != nil {
			logger.ErrorContext(ctx, "release demo seed advisory lock", attr.SlogError(err))
		}
	}()

	// The script is multi-statement (CREATE SCHEMA / CREATE FUNCTION with a
	// dollar-quoted body / SELECT), which requires the simple query protocol.
	if err := conn.Conn().PgConn().Exec(ctx, spec.Rewrite(postgresSQL)).Close(); err != nil {
		return fmt.Errorf("apply demo seed to postgres: %w", err)
	}
	logger.InfoContext(ctx, "demo seed applied to postgres")

	// The Sources page's "OpenAPI Specification" tab serves the asset blob,
	// which SQL alone cannot place — write the embedded spec through the
	// asset store (fs locally, GCS in prod) and point the assets row at it.
	docSHA := sha256.Sum256(openapiDoc)
	docHex := hex.EncodeToString(docSHA[:])
	w, objURL, err := blob.Write(ctx, "demoseed/"+spec.OrgSlug+"-openapi.yaml", "application/x-yaml", int64(len(openapiDoc)))
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
	if _, err := conn.Exec(ctx,
		"UPDATE assets SET url = $1, sha256 = $2, content_length = $3, content_type = 'application/x-yaml' WHERE id = $4 AND project_id = $5",
		objURL.String(), docHex, len(openapiDoc), spec.AssetID(), spec.ProjectID(),
	); err != nil {
		return fmt.Errorf("point demo assets row at the uploaded spec: %w", err)
	}
	logger.InfoContext(ctx, "demo openapi asset uploaded")

	// clickhouse-go executes one statement per call and pools sessions, so the
	// script's SET line cannot carry across statements — the setting rides on
	// the context instead. Statements are split on ';': the seed files keep
	// semicolons out of string literals by convention (see seed/demo/PAGES.md).
	// lightweight_deletes_sync=2 waits for ALL replicas (prod ClickHouse Cloud
	// is replicated; =1 would let the re-insert race a delete still running on
	// another replica). The base client caps max_execution_time at 60s, which
	// prod-sized telemetry_logs deletes can exceed — raise it here.
	chCtx := clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"lightweight_deletes_sync": 2,
		"max_execution_time":       600,
	}))
	deleted := false
	deleteVisible := false
	for _, stmt := range splitStatements(spec.Rewrite(clickhouseSQL)) {
		upperStmt := strings.ToUpper(stmt)
		if strings.HasPrefix(upperStmt, "SET ") {
			continue
		}
		if strings.HasPrefix(upperStmt, "DELETE ") {
			deleted = true
		} else if deleted && !deleteVisible {
			if err := waitForClickHouseTelemetryDelete(
				chCtx,
				clickHouseDeleteVisibilityTimeout,
				clickHouseDeleteVisibilityPoll,
				func(queryCtx context.Context) (uint64, error) {
					var remaining uint64
					err := ch.QueryRow(queryCtx, `
						SELECT count()
						FROM telemetry_logs
						WHERE gram_project_id = toUUID(?)`, spec.ProjectID()).Scan(&remaining)
					if err != nil {
						return 0, fmt.Errorf("count demo telemetry rows: %w", err)
					}
					return remaining, nil
				},
			); err != nil {
				return fmt.Errorf("apply demo seed to clickhouse: %w", err)
			}
			deleteVisible = true
		}
		if err := ch.Exec(chCtx, stmt); err != nil {
			return fmt.Errorf("apply demo seed to clickhouse: %w: %.120s", err, stmt)
		}
	}
	logger.InfoContext(ctx, "demo seed applied to clickhouse")

	return nil
}

func waitForClickHouseTelemetryDelete(
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
			return fmt.Errorf("check demo telemetry delete visibility: %w", err)
		}
		if remaining == 0 {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for demo telemetry delete visibility with %d rows remaining: %w", remaining, context.Cause(waitCtx))
		case <-ticker.C:
		}
	}
}

// splitStatements strips -- line comments and splits the script into
// individual statements on ';'.
func splitStatements(script string) []string {
	var sb strings.Builder
	for line := range strings.Lines(script) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		sb.WriteString(line)
	}

	var out []string
	for stmt := range strings.SplitSeq(sb.String(), ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}

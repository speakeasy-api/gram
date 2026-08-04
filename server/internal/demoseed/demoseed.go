// Package demoseed provisions the shared read-only demo organization
// (constants.DemoOrganizationID). The SQL sources are embedded so the seed
// versions atomically with the server binary: the daily `gram demo-seed` run
// always applies the seed that shipped with the current deploy — no
// migrations, no runtime file fetches. Authoring docs live in seed/demo/.
package demoseed

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

//go:embed postgres.sql
var postgresSQL string

//go:embed clickhouse.sql
var clickhouseSQL string

// Run applies the demo seed: Postgres first (installs and executes
// demo.ensure_demo_org(), whose pre/postflight asserts abort the transaction
// on any isolation violation), then ClickHouse (scoped deletes + inserts with
// throwIf postflights). Both halves are idempotent; ordering matters only
// because ClickHouse rows reference Postgres ids.
func Run(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, ch driver.Conn) error {
	logger = logger.With(attr.SlogComponent("demoseed"))

	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire postgres connection: %w", err)
	}
	defer conn.Release()

	// The script is multi-statement (CREATE SCHEMA / CREATE FUNCTION with a
	// dollar-quoted body / SELECT), which requires the simple query protocol.
	if err := conn.Conn().PgConn().Exec(ctx, postgresSQL).Close(); err != nil {
		return fmt.Errorf("apply demo seed to postgres: %w", err)
	}
	logger.InfoContext(ctx, "demo seed applied to postgres")

	// clickhouse-go executes one statement per call and pools sessions, so the
	// script's SET line cannot carry across statements — the setting rides on
	// the context instead. Statements are split on ';': the seed files keep
	// semicolons out of string literals by convention (see seed/demo/PAGES.md).
	chCtx := clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"mutations_sync": 1,
	}))
	for _, stmt := range splitStatements(clickhouseSQL) {
		if strings.HasPrefix(strings.ToUpper(stmt), "SET ") {
			continue
		}
		if err := ch.Exec(chCtx, stmt); err != nil {
			return fmt.Errorf("apply demo seed to clickhouse: %w: %.120s", err, stmt)
		}
	}
	logger.InfoContext(ctx, "demo seed applied to clickhouse")

	return nil
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

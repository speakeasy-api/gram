// Command migrations back-fills historical data into ClickHouse using a generic
// Source -> Transform -> Sink pipeline (see the pipeline package). Two
// migrations are wired, selected by an optional leading subcommand:
//
//   - riskfindings (default): moves Postgres risk_results rows into the
//     ClickHouse risk_findings event log (see RISK_RESULTS_MIGRATION.md).
//   - riskfindingscols: backfills the message_created_at and assistant_id
//     columns onto existing risk_findings rows via ClickHouse mutations (see
//     RISKFINDINGS_COLS_MIGRATION.md).
//
// It is an offline operator tool, run by hand against production reached
// through Cloud SQL Auth Proxy and a ClickHouse tunnel:
//
// Secrets come from the environment only (never flags, which leak through argv):
//
//	GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
//	CLICKHOUSE_PASSWORD=... \
//	GRAM_RISK_FINGERPRINT_PEPPER_KEYRING='{"current":"v1","keys":{"v1":"<base64>"}}' \
//	  go run ./server/cmd/tools/migrations \
//	  -ch-host 127.0.0.1 -ch-database gram -ch-username gram \
//	  -org org_123 -from 2024-01-01T00:00:00Z -to 2024-06-01T00:00:00Z \
//	  -dry-run=false
//
// Safety properties:
//   - -dry-run defaults to true: a plain run reads and transforms but writes
//     nothing (and skips connecting to ClickHouse).
//   - The read is a keyset scan over risk_results.id (uuidv7, time-ordered). The
//     resume cursor is the sink's last committed id, printed in the final report;
//     an interrupted run exits nonzero and logs that cursor for -cursor.
//   - The full raw match is never written to ClickHouse: only its length, the
//     partial-mask display string (internal/risk/maskdisplay — boundary
//     characters only), and one-way HMAC fingerprints.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/riskfindings"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// clickhouseConfig carries the non-secret ClickHouse connection settings
// shared by every migration subcommand; the password comes from the
// environment.
type clickhouseConfig struct {
	host       string
	database   string
	username   string
	password   string
	nativePort string
	insecure   bool
}

type config struct {
	dbURL         string
	pepperKeyring string
	ch            clickhouseConfig
	orgID         string
	projectID     uuid.NullUUID
	policyID      uuid.NullUUID
	from          *time.Time
	to            *time.Time
	cursor        uuid.NullUUID
	batchSize     int
	bufferSize    int
	dryRun        bool
	liftPartGuard bool
}

func main() {
	os.Exit(run())
}

func run() int {
	// The subcommand is the optional first non-flag argument; a bare flag list
	// keeps the original riskfindings behavior.
	args := os.Args[1:]
	migration := "riskfindings"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		migration = args[0]
		args = args[1:]
	}

	switch migration {
	case "riskfindings":
		return runRiskFindings(args)
	case "riskfindingscols":
		return runRiskFindingsCols(args)
	default:
		// The unrecognized name is deliberately not echoed (log injection).
		log.Printf("unknown migration subcommand (available: riskfindings, riskfindingscols)")
		return 2
	}
}

func runRiskFindings(args []string) int {
	cfg, err := parseFlags(args)
	if err != nil {
		log.Printf("invalid arguments: %v", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fingerprinter, err := risk.ParsePepperKeyRing([]byte(cfg.pepperKeyring))
	if err != nil {
		log.Printf("parse pepper keyring: %v", err)
		return 1
	}

	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres: %v", err)
		return 1
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Printf("ping postgres: %v", err)
		return 1
	}

	var chConn clickhouse.Conn
	if !cfg.dryRun {
		chConn, err = openClickhouse(ctx, cfg.ch)
		if err != nil {
			log.Printf("connect clickhouse: %v", err)
			return 1
		}
		defer func() {
			if cerr := chConn.Close(); cerr != nil {
				log.Printf("close clickhouse: %v", cerr)
			}
		}()
	}

	source := riskfindings.NewSource(pool)
	transformer := riskfindings.NewTransformer(fingerprinter)
	sink := riskfindings.NewSink(chConn, cfg.bufferSize, cfg.batchSize, cfg.dryRun, cfg.liftPartGuard)

	runErr := pipeline.Run[riskfindings.SourceRow, riskfindings.FindingRow](
		ctx, source, transformer, sink, cfg.criteria(), cfg.bufferSize,
	)

	// Resume from the sink's committed id, not the source read position: rows the
	// source read but the sink had not yet flushed on interruption are not
	// durable, and resuming from the read position would skip them.
	printReport(cfg, source.Scanned(), sink.Inserted(), sink.LastCommitted())

	if runErr != nil {
		// Both interruption and hard failure leave an incomplete backfill: exit
		// nonzero and, whenever anything was durably committed, point the operator
		// at the cursor to resume from so shell automation never treats a partial
		// run as done.
		resumeHint := ""
		if committed := sink.LastCommitted(); committed != uuid.Nil {
			resumeHint = fmt.Sprintf("; resume with -cursor %s (repeat the same -from/-to/-org/-project/-policy)", committed)
		}
		if errors.Is(runErr, context.Canceled) {
			log.Printf("migration interrupted before completion%s", resumeHint)
			return 130
		}
		log.Printf("migration failed: %v%s", runErr, resumeHint)
		return 1
	}
	return 0
}

func (c config) criteria() pipeline.Criteria {
	crit := pipeline.Criteria{
		riskfindings.CriteriaBatchSize: c.batchSize,
	}
	if c.orgID != "" {
		crit[riskfindings.CriteriaOrgID] = c.orgID
	}
	if c.projectID.Valid {
		crit[riskfindings.CriteriaProjectID] = c.projectID.UUID
	}
	if c.policyID.Valid {
		crit[riskfindings.CriteriaPolicyID] = c.policyID.UUID
	}
	if c.from != nil {
		crit[riskfindings.CriteriaFrom] = *c.from
	}
	if c.to != nil {
		crit[riskfindings.CriteriaTo] = *c.to
	}
	if c.cursor.Valid {
		crit[riskfindings.CriteriaCursor] = c.cursor.UUID
	}
	return crit
}

func openClickhouse(ctx context.Context, cfg clickhouseConfig) (clickhouse.Conn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Protocol: clickhouse.Native,
		Addr:     []string{net.JoinHostPort(cfg.host, cfg.nativePort)},
		Auth: clickhouse.Auth{
			Database: cfg.database,
			Username: cfg.username,
			Password: cfg.password,
		},
		TLS: &tls.Config{
			InsecureSkipVerify: cfg.insecure, // #nosec G402 -- operator-supplied flag for local/tunnelled use
			MinVersion:         tls.VersionTLS12,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return conn, nil
}

// registerClickhouseFlags declares the shared non-secret ClickHouse flags on
// fs; the returned struct is populated (password included, from the
// environment) once fs.Parse has run.
func registerClickhouseFlags(fs *flag.FlagSet) *clickhouseConfig {
	cfg := &clickhouseConfig{
		host:       "",
		database:   "",
		username:   "",
		password:   "",
		nativePort: "",
		insecure:   false,
	}
	fs.StringVar(&cfg.host, "ch-host", envOr("CLICKHOUSE_HOST", "localhost"), "ClickHouse host")
	fs.StringVar(&cfg.database, "ch-database", envOr("CLICKHOUSE_DATABASE", "default"), "ClickHouse database")
	fs.StringVar(&cfg.username, "ch-username", envOr("CLICKHOUSE_USERNAME", "gram"), "ClickHouse username")
	fs.StringVar(&cfg.nativePort, "ch-native-port", envOr("CLICKHOUSE_NATIVE_PORT", "9440"), "ClickHouse native protocol port")
	fs.BoolVar(&cfg.insecure, "ch-insecure", os.Getenv("CLICKHOUSE_INSECURE") == "true", "skip ClickHouse TLS verification")
	return cfg
}

// defaultFrom is the riskfindings default lower time bound: the start of the
// reveal-metadata re-backfill window (see RISK_RESULTS_MIGRATION.md). Override
// with -from, or pass -from "" to scan from the beginning of the table.
const defaultFrom = "2026-05-01T00:00:00Z"

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("riskfindings", flag.ContinueOnError)

	// Secrets (Postgres URL, ClickHouse password, fingerprint pepper) are read
	// from the environment (or a file, for the pepper) only — never as flag
	// values — so they do not leak through argv / ps.
	var (
		pepperKeyringFile = fs.String("pepper-keyring-file", "", "path to a file holding the JSON pepper keyring (alternative to $GRAM_RISK_FINGERPRINT_PEPPER_KEYRING)")
		chCfg             = registerClickhouseFlags(fs)
		orgID             = fs.String("org", "", "organization_id to scope the migration (optional; all orgs if empty)")
		projectID         = fs.String("project", "", "project_id (uuid) to scope (optional)")
		policyID          = fs.String("policy", "", "risk_policy_id (uuid) to scope (optional)")
		fromStr           = fs.String("from", defaultFrom, "lower time bound, RFC3339 (default is the reveal-metadata re-backfill start; pass an empty string to scan from the beginning)")
		toStr             = fs.String("to", "", "upper time bound, RFC3339 (optional; to the end if empty)")
		cursorStr         = fs.String("cursor", "", "resume after this risk_results id (exclusive); keyset resume position only — still pass the original -from/-to/-org/-project/-policy")
		batchSize         = fs.Int("batch-size", riskfindings.DefaultBatchSize, "rows per source page and sink batch")
		bufferSize        = fs.Int("buffer", riskfindings.DefaultBatchSize, "channel buffer between pipeline stages")
		dryRun            = fs.Bool("dry-run", true, "when true (default) read and transform but do not write; pass -dry-run=false to insert")
		liftPartGuard     = fs.Bool("lift-partition-guard", true, "set max_partitions_per_insert_block=0 on inserts; pass -lift-partition-guard=false when the ClickHouse settings profile constrains that setting (code 452)")
	)
	if err := fs.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}

	pepperKeyring := os.Getenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING")
	if *pepperKeyringFile != "" {
		b, err := os.ReadFile(*pepperKeyringFile) // #nosec G304 -- operator-supplied keyring path
		if err != nil {
			return config{}, fmt.Errorf("read -pepper-keyring-file: %w", err)
		}
		pepperKeyring = strings.TrimSpace(string(b))
	}

	chCfg.password = os.Getenv("CLICKHOUSE_PASSWORD")

	cfg := config{
		dbURL:         os.Getenv("GRAM_DATABASE_URL"),
		pepperKeyring: pepperKeyring,
		ch:            *chCfg,
		orgID:         *orgID,
		projectID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		policyID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		from:          nil,
		to:            nil,
		cursor:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		batchSize:     *batchSize,
		bufferSize:    *bufferSize,
		dryRun:        *dryRun,
		liftPartGuard: *liftPartGuard,
	}

	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	if cfg.pepperKeyring == "" {
		return cfg, errors.New("missing $GRAM_RISK_FINGERPRINT_PEPPER_KEYRING (or -pepper-keyring-file)")
	}
	if cfg.batchSize <= 0 {
		return cfg, errors.New("-batch-size must be positive")
	}

	if *projectID != "" {
		pid, err := uuid.Parse(*projectID)
		if err != nil {
			return cfg, fmt.Errorf("invalid -project: %w", err)
		}
		cfg.projectID = uuid.NullUUID{UUID: pid, Valid: true}
	}
	if *policyID != "" {
		pol, err := uuid.Parse(*policyID)
		if err != nil {
			return cfg, fmt.Errorf("invalid -policy: %w", err)
		}
		cfg.policyID = uuid.NullUUID{UUID: pol, Valid: true}
	}
	if *fromStr != "" {
		from, err := time.Parse(time.RFC3339, *fromStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -from: %w", err)
		}
		cfg.from = &from
	}
	if *toStr != "" {
		to, err := time.Parse(time.RFC3339, *toStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -to: %w", err)
		}
		cfg.to = &to
	}
	if cfg.from != nil && cfg.to != nil && !cfg.from.Before(*cfg.to) {
		return cfg, errors.New("-from must be before -to")
	}
	if *cursorStr != "" {
		cur, err := uuid.Parse(*cursorStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -cursor: %w", err)
		}
		cfg.cursor = uuid.NullUUID{UUID: cur, Valid: true}
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printReport(cfg config, scanned, inserted int64, lastCursor uuid.UUID) {
	mode := "DRY RUN (no writes)"
	if !cfg.dryRun {
		mode = "APPLIED"
	}
	fmt.Println()
	fmt.Println("risk_findings migration summary")
	fmt.Printf("  mode:        %s\n", mode)
	if cfg.orgID != "" {
		fmt.Printf("  org:         %s\n", cfg.orgID)
	} else {
		fmt.Printf("  org:         (all)\n")
	}
	fmt.Printf("  scanned:     %d\n", scanned)
	fmt.Printf("  inserted:    %d\n", inserted)
	// The resume cursor is meaningful only for an applied run: in dry-run nothing
	// is written, so there is no durable checkpoint to resume from.
	if cfg.dryRun {
		fmt.Printf("  last cursor: (dry run — no durable checkpoint)\n")
	} else {
		fmt.Printf("  last cursor: %s\n", lastCursor)
	}
	if cfg.dryRun && scanned > 0 {
		fmt.Println()
		fmt.Println("re-run with -dry-run=false to write to ClickHouse.")
	}
}

// Command migrations back-fills historical data into ClickHouse using a generic
// Source -> Transform -> Sink pipeline (see the pipeline package). The only wired
// migration today moves Postgres risk_results rows into the ClickHouse
// risk_findings event log.
//
// It is an offline operator tool, run by hand against production reached through
// Cloud SQL Auth Proxy and a ClickHouse tunnel:
//
//	GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
//	GRAM_RISK_FINGERPRINT_PEPPER_KEYRING='{"current":"v1","keys":{"v1":"<base64>"}}' \
//	  go run ./server/cmd/tools/migrations \
//	  -ch-host 127.0.0.1 -ch-database gram -ch-username gram -ch-password gram \
//	  -org org_123 -from 2024-01-01T00:00:00Z -to 2024-06-01T00:00:00Z \
//	  -dry-run=false
//
// Safety properties:
//   - -dry-run defaults to true: a plain run reads and transforms but writes
//     nothing (and skips connecting to ClickHouse).
//   - The read is a keyset scan over risk_results.id (uuidv7, time-ordered); on
//     interruption the last processed id is printed so the run resumes with
//     -cursor.
//   - The raw match is never written to ClickHouse: only its length, a redacted
//     display string, and one-way HMAC fingerprints.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/riskfindings"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

type config struct {
	dbURL         string
	pepperKeyring string
	chHost        string
	chDatabase    string
	chUsername    string
	chPassword    string
	chNativePort  string
	chInsecure    bool
	orgID         string
	projectID     uuid.NullUUID
	policyID      uuid.NullUUID
	from          *time.Time
	to            *time.Time
	cursor        uuid.NullUUID
	batchSize     int
	bufferSize    int
	dryRun        bool
}

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := parseFlags()
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
		chConn, err = openClickhouse(ctx, cfg)
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
	sink := riskfindings.NewSink(chConn, cfg.bufferSize, cfg.batchSize, cfg.dryRun)

	runErr := pipeline.Run[riskfindings.SourceRow, riskfindings.FindingRow](
		ctx, source, transformer, sink, cfg.criteria(), cfg.bufferSize,
	)

	printReport(cfg, source.Scanned(), sink.Inserted(), source.LastCursor())

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		log.Printf("migration failed: %v", runErr)
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

func openClickhouse(ctx context.Context, cfg config) (clickhouse.Conn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Protocol: clickhouse.Native,
		Addr:     []string{fmt.Sprintf("%s:%s", cfg.chHost, cfg.chNativePort)},
		Auth: clickhouse.Auth{
			Database: cfg.chDatabase,
			Username: cfg.chUsername,
			Password: cfg.chPassword,
		},
		TLS: &tls.Config{
			InsecureSkipVerify: cfg.chInsecure, // #nosec G402 -- operator-supplied flag for local/tunnelled use
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

func parseFlags() (config, error) {
	var (
		dbURL         = flag.String("db", os.Getenv("GRAM_DATABASE_URL"), "Postgres connection string (defaults to $GRAM_DATABASE_URL)")
		pepperKeyring = flag.String("pepper-keyring", os.Getenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING"), "JSON pepper keyring for fingerprinting (defaults to $GRAM_RISK_FINGERPRINT_PEPPER_KEYRING)")
		chHost        = flag.String("ch-host", envOr("CLICKHOUSE_HOST", "localhost"), "ClickHouse host")
		chDatabase    = flag.String("ch-database", envOr("CLICKHOUSE_DATABASE", "default"), "ClickHouse database")
		chUsername    = flag.String("ch-username", envOr("CLICKHOUSE_USERNAME", "gram"), "ClickHouse username")
		chPassword    = flag.String("ch-password", envOr("CLICKHOUSE_PASSWORD", "gram"), "ClickHouse password")
		chNativePort  = flag.String("ch-native-port", envOr("CLICKHOUSE_NATIVE_PORT", "9440"), "ClickHouse native protocol port")
		chInsecure    = flag.Bool("ch-insecure", os.Getenv("CLICKHOUSE_INSECURE") == "true", "skip ClickHouse TLS verification")
		orgID         = flag.String("org", "", "organization_id to scope the migration (optional; all orgs if empty)")
		projectID     = flag.String("project", "", "project_id (uuid) to scope (optional)")
		policyID      = flag.String("policy", "", "risk_policy_id (uuid) to scope (optional)")
		fromStr       = flag.String("from", "", "lower time bound, RFC3339 (optional; from the beginning if empty)")
		toStr         = flag.String("to", "", "upper time bound, RFC3339 (optional; to the end if empty)")
		cursorStr     = flag.String("cursor", "", "resume after this risk_results id (exclusive); overrides -from")
		batchSize     = flag.Int("batch-size", riskfindings.DefaultBatchSize, "rows per source page and sink batch")
		bufferSize    = flag.Int("buffer", riskfindings.DefaultBatchSize, "channel buffer between pipeline stages")
		dryRun        = flag.Bool("dry-run", true, "when true (default) read and transform but do not write; pass -dry-run=false to insert")
	)
	flag.Parse()

	cfg := config{
		dbURL:         *dbURL,
		pepperKeyring: *pepperKeyring,
		chHost:        *chHost,
		chDatabase:    *chDatabase,
		chUsername:    *chUsername,
		chPassword:    *chPassword,
		chNativePort:  *chNativePort,
		chInsecure:    *chInsecure,
		orgID:         *orgID,
		projectID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		policyID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		from:          nil,
		to:            nil,
		cursor:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		batchSize:     *batchSize,
		bufferSize:    *bufferSize,
		dryRun:        *dryRun,
	}

	if cfg.dbURL == "" {
		return cfg, errors.New("missing -db / $GRAM_DATABASE_URL")
	}
	if cfg.pepperKeyring == "" {
		return cfg, errors.New("missing -pepper-keyring / $GRAM_RISK_FINGERPRINT_PEPPER_KEYRING")
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
	fmt.Printf("  last cursor: %s\n", lastCursor)
	if cfg.dryRun && scanned > 0 {
		fmt.Println()
		fmt.Println("re-run with -dry-run=false to write to ClickHouse.")
	}
}

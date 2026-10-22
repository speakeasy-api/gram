// Command migrations runs one explicitly selected offline migration.
//
//   - riskfindings (default): PostgreSQL to ClickHouse; see RISK_RESULTS_MIGRATION.md.
//   - riskfindingscols: ClickHouse mutations; see RISKFINDINGS_COLS_MIGRATION.md.
//   - openrouter-disable-causes: PostgreSQL-only classification; see
//     OPENROUTER_DISABLE_CAUSES_MIGRATION.md. It defaults to a non-writing dry
//     run. Writes use -apply or -manual-override and require explicit target
//     confirmation.
//   - pro-entitlements: PostgreSQL application-data backfill; see
//     PRO_ENTITLEMENTS_BACKFILL.md. It defaults to a non-writing dry run.
//   - hosted-mcp-wrappers: PostgreSQL application-data backfill; see
//     HOSTED_MCP_WRAPPERS_MIGRATION.md. It defaults to a dry run.
//
// Each guide documents its own connectivity, secret environment variables,
// invocation modes, output, and recovery procedure. Flags are subcommand-local;
// in particular, openrouter-disable-causes does not accept -dry-run=false.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
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
	sources       []string
	policyID      uuid.NullUUID
	from          *time.Time
	to            *time.Time
	cursor        uuid.NullUUID
	gapFillCursor riskfindings.GapFillCursor
	batchSize     int
	bufferSize    int
	dryRun        bool
	gapFill       bool
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
	case "openrouter-disable-causes":
		return runOpenRouterDisableCauses(args, os.Stdin, os.Stdout, os.Getenv)
	case "pro-entitlements":
		return runProEntitlements(args, os.Stdout, os.Getenv)
	case "hosted-mcp-wrappers":
		return runHostedMCPWrappers(args, os.Stdout, os.Getenv)
	default:
		// The unrecognized name is deliberately not echoed (log injection).
		log.Printf("unknown migration subcommand (available: riskfindings, riskfindingscols, openrouter-disable-causes, pro-entitlements, hosted-mcp-wrappers)")
		return 2
	}
}

func runRiskFindings(args []string) int {
	cfg, err := parseFlags(args, time.Now().UTC())
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
	if cfg.gapFill || !cfg.dryRun {
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
	sink := riskfindings.NewSink(chConn, riskfindings.SinkOptions{
		BufferSize:         cfg.bufferSize,
		BatchSize:          cfg.batchSize,
		DryRun:             cfg.dryRun,
		LiftPartitionGuard: cfg.liftPartGuard,
		GapFill:            cfg.gapFill,
	})

	runErr := pipeline.Run[riskfindings.SourceRow, riskfindings.FindingRow](
		ctx, source, transformer, sink, cfg.criteria(), cfg.bufferSize,
	)

	committed := sink.LastCommitted().String()
	if cfg.gapFill {
		committed = sink.LastCommittedGapFill().String()
	}
	if reportErr := printReport(
		os.Stdout,
		cfg,
		source.Scanned(),
		source.ScannedBySource(),
		sink.StatsBySource(),
		committed,
	); reportErr != nil {
		if runErr == nil {
			runErr = fmt.Errorf("write migration report: %w", reportErr)
		} else {
			log.Printf("write migration report: %v", reportErr)
		}
	}

	if runErr != nil {
		// Both interruption and hard failure leave an incomplete backfill: exit
		// nonzero and, whenever anything was durably committed, point the operator
		// at the cursor to resume from so shell automation never treats a partial
		// run as done.
		resumeHint := ""
		resumeFlag := ""
		switch {
		case cfg.gapFill && !sink.LastCommittedGapFill().IsZero():
			resumeFlag = "-gap-fill-cursor " + sink.LastCommittedGapFill().String()
		case !cfg.gapFill && sink.LastCommitted() != uuid.Nil:
			resumeFlag = "-cursor " + sink.LastCommitted().String()
		}
		if resumeFlag != "" {
			resumeHint = fmt.Sprintf("; resume with %s and repeat the same mode, -from, -to, -org, -project, -policy, and -sources", resumeFlag)
			if cfg.from != nil {
				resumeHint += fmt.Sprintf(" (effective -from %s)", cfg.from.Format(time.RFC3339Nano))
			}
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
	if len(c.sources) > 0 {
		crit[riskfindings.CriteriaSources] = c.sources
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
	if c.gapFill {
		crit[riskfindings.CriteriaGapFill] = true
		if !c.gapFillCursor.IsZero() {
			crit[riskfindings.CriteriaGapFillCursor] = c.gapFillCursor
		}
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

// defaultFrom is the lower bound for the original full backfill.
const defaultFrom = "2026-05-01T00:00:00Z"

// clickhouseRetention is the default gap-fill window.
const clickhouseRetention = 90 * 24 * time.Hour

func parseFlags(args []string, now time.Time) (config, error) {
	fs := flag.NewFlagSet("riskfindings", flag.ContinueOnError)

	// Secrets (Postgres URL, ClickHouse password, fingerprint pepper) are read
	// from the environment (or a file, for the pepper) only — never as flag
	// values — so they do not leak through argv / ps.
	var (
		pepperKeyringFile = fs.String("pepper-keyring-file", "", "path to a file holding the JSON pepper keyring (alternative to $GRAM_RISK_FINGERPRINT_PEPPER_KEYRING)")
		chCfg             = registerClickhouseFlags(fs)
		gapFill           = fs.Bool("gap-fill", false, "skip findings already present in ClickHouse and append missing state copies")
		orgID             = fs.String("org", "", "organization_id to scope the migration")
		projectID         = fs.String("project", "", "project_id (uuid) to scope (optional)")
		policyID          = fs.String("policy", "", "risk_policy_id (uuid) to scope (optional)")
		sourcesStr        = fs.String("sources", "", "comma-separated finding sources to scope (optional)")
		fromStr           = fs.String("from", "", "lower time bound, RFC3339")
		toStr             = fs.String("to", "", "upper time bound, RFC3339 (optional)")
		cursorStr         = fs.String("cursor", "", "resume after this risk_results id (exclusive)")
		gapFillCursorStr  = fs.String("gap-fill-cursor", "", "resume a -gap-fill run after <project_id>/<created_at>/<risk_results id> (exclusive)")
		batchSize         = fs.Int("batch-size", riskfindings.DefaultBatchSize, "rows per source page and sink batch")
		bufferSize        = fs.Int("buffer", riskfindings.DefaultBatchSize, "channel buffer between pipeline stages")
		dryRun            = fs.Bool("dry-run", true, "read and report without writing")
		liftPartGuard     = fs.Bool("lift-partition-guard", true, "set max_partitions_per_insert_block=0 on inserts; pass -lift-partition-guard=false when the ClickHouse settings profile constrains that setting (code 452)")
	)
	if err := fs.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}

	fromSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "from" {
			fromSet = true
		}
	})

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
		sources:       parseSources(*sourcesStr),
		from:          nil,
		to:            nil,
		cursor:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		gapFillCursor: riskfindings.GapFillCursor{ProjectID: uuid.Nil, CreatedAt: time.Time{}, ID: uuid.Nil},
		batchSize:     *batchSize,
		bufferSize:    *bufferSize,
		dryRun:        *dryRun,
		gapFill:       *gapFill,
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
	if cfg.gapFill && cfg.orgID == "" {
		return cfg, errors.New("-org is required with -gap-fill")
	}
	if cfg.gapFill && *cursorStr != "" {
		return cfg, errors.New("-cursor does not apply to -gap-fill, which pages by project; resume with -gap-fill-cursor")
	}
	if !cfg.gapFill && *gapFillCursorStr != "" {
		return cfg, errors.New("-gap-fill-cursor requires -gap-fill")
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
	effectiveFrom := *fromStr
	if !fromSet {
		if cfg.gapFill {
			if *gapFillCursorStr != "" {
				return cfg, errors.New("-from is required with -gap-fill-cursor because the default window moves with the clock")
			}
			from := now.Add(-clickhouseRetention)
			cfg.from = &from
		} else {
			effectiveFrom = defaultFrom
		}
	}
	if effectiveFrom != "" {
		from, err := time.Parse(time.RFC3339, effectiveFrom)
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
	if *gapFillCursorStr != "" {
		cur, err := riskfindings.ParseGapFillCursor(*gapFillCursorStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -gap-fill-cursor: %w", err)
		}
		if cfg.projectID.Valid && cfg.projectID.UUID != cur.ProjectID {
			return cfg, errors.New("-gap-fill-cursor names a different project than -project")
		}
		cfg.gapFillCursor = cur
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseSources(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	for source := range strings.SplitSeq(value, ",") {
		source = strings.TrimSpace(source)
		if source != "" {
			seen[source] = struct{}{}
		}
	}
	sources := make([]string, 0, len(seen))
	for source := range seen {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

func printReport(
	w io.Writer,
	cfg config,
	scanned int64,
	scannedBySource map[string]int64,
	statsBySource map[string]riskfindings.SourceStats,
	lastCursor string,
) error {
	mode := "DRY RUN (no writes)"
	if !cfg.dryRun {
		mode = "APPLIED"
	}
	org := cfg.orgID
	if org == "" {
		org = "(all)"
	}
	lines := []string{
		"",
		"risk_findings migration summary",
		fmt.Sprintf("  mode:        %s", mode),
		fmt.Sprintf("  org:         %s", org),
		fmt.Sprintf("  scanned:     %d", scanned),
	}
	if cfg.gapFill {
		lines = append(lines, "  by source:")
		sources := make([]string, 0, len(scannedBySource))
		for source := range scannedBySource {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			stats := statsBySource[source]
			lines = append(lines, fmt.Sprintf(
				"    %s: postgres=%d present_by_id=%d matched_by_key=%d state_copies=%d skipped_ch_newer=%d inserts=%d",
				source,
				scannedBySource[source],
				stats.PresentByID,
				stats.MatchedByKey,
				stats.StateCopies,
				stats.SkippedCHNewer,
				stats.Inserted,
			))
		}
	} else {
		var inserted int64
		for _, stats := range statsBySource {
			inserted += stats.Inserted
		}
		lines = append(lines, fmt.Sprintf("  inserted:    %d", inserted))
	}
	if cfg.dryRun {
		lines = append(lines, "  last cursor: (dry run, no durable checkpoint)")
	} else {
		lines = append(lines, fmt.Sprintf("  last cursor: %s", lastCursor))
	}
	if cfg.dryRun && scanned > 0 {
		lines = append(lines, "", "re-run with -dry-run=false to write to ClickHouse.")
	}
	if _, err := fmt.Fprintln(w, strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

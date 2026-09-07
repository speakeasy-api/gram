package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/legacypolicyscope"
)

type legacyPolicyScopeConfig struct {
	dbURL            string
	environment      string
	mode             legacypolicyscope.Mode
	batchSize        int
	lockTimeout      time.Duration
	statementTimeout time.Duration
}

type legacyPolicyScopeSummary struct {
	Mode        string                    `json:"mode"`
	Environment string                    `json:"environment"`
	Result      string                    `json:"result"`
	ElapsedMS   int64                     `json:"elapsed_ms"`
	Summary     legacypolicyscope.Summary `json:"summary"`
}

func parseLegacyPolicyScopeFlags(args []string, getenv func(string) string) (legacyPolicyScopeConfig, error) {
	fs := flag.NewFlagSet("legacy-policy-scope", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "fold and clear legacy policy scopes")
	validate := fs.Bool("validate", false, "assert no policy still carries a legacy scope")
	environment := fs.String("environment", "", "explicit target environment")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for every write")
	confirmProduction := fs.String("confirm-production", "", "must equal production for a production write")
	batchSize := fs.Int("batch-size", 100, "keyset batch size")
	lockTimeout := fs.Duration("lock-timeout", 2*time.Second, "per-transaction lock timeout")
	statementTimeout := fs.Duration("statement-timeout", 30*time.Second, "per-transaction statement timeout")
	if err := fs.Parse(args); err != nil {
		return legacyPolicyScopeConfig{}, errors.New("invalid legacy-policy-scope flags")
	}
	if fs.NArg() != 0 {
		return legacyPolicyScopeConfig{}, errors.New("unexpected positional arguments")
	}
	if *apply && *validate {
		return legacyPolicyScopeConfig{}, errors.New("select exactly one mode: apply or validate")
	}
	if strings.TrimSpace(*environment) == "" {
		return legacyPolicyScopeConfig{}, errors.New("environment is required")
	}
	target, known := canonicalEnvironment(*environment)
	if !known {
		return legacyPolicyScopeConfig{}, errors.New("unrecognized -environment (want local, dev, staging, or prod)")
	}
	if *batchSize <= 0 || *lockTimeout <= 0 || *statementTimeout <= 0 {
		return legacyPolicyScopeConfig{}, errors.New("batch size and timeouts must be positive")
	}
	if *apply && *confirmEnvironment != *environment {
		return legacyPolicyScopeConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	// The confirmation is keyed off the canonical name, not the literal one:
	// GRAM_ENVIRONMENT spells production "prod", so an exact match on
	// "production" would wave a real production run straight through.
	if *apply && target == environmentProduction && *confirmProduction != environmentProduction {
		return legacyPolicyScopeConfig{}, errors.New("production writes require -confirm-production=production")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if *apply && target != environmentProduction && looksLikeProductionDSN(dbURL) {
		return legacyPolicyScopeConfig{}, errors.New(
			"refusing to write: $GRAM_DATABASE_URL points at a production host but -environment does not name production")
	}

	mode := legacypolicyscope.ModeDryRun
	if *apply {
		mode = legacypolicyscope.ModeApply
	}
	if *validate {
		mode = legacypolicyscope.ModeValidate
	}
	cfg := legacyPolicyScopeConfig{
		dbURL: dbURL, environment: target, mode: mode,
		batchSize: *batchSize, lockTimeout: *lockTimeout, statementTimeout: *statementTimeout,
	}
	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	return cfg, nil
}

// environmentProduction is the canonical name every production alias maps to.
const environmentProduction = "production"

// canonicalEnvironment folds the operator-supplied target name to its
// canonical form and reports whether it is one this tool recognizes. An
// unrecognized name is rejected rather than assumed non-production.
func canonicalEnvironment(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "local":
		return "local", true
	case "dev", "development":
		return "dev", true
	case "staging", "stage":
		return "staging", true
	case "prod", environmentProduction:
		return environmentProduction, true
	default:
		return "", false
	}
}

// looksLikeProductionDSN reports whether the connection target names a
// production host, so a run declaring a lesser environment cannot quietly
// write to production. It inspects the host only: a password or database name
// containing "prod" says nothing about where the connection lands.
func looksLikeProductionDSN(dsn string) bool {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, label := range strings.FieldsFunc(host, func(r rune) bool { return r == '.' || r == '-' }) {
		if label == "prod" || label == environmentProduction {
			return true
		}
	}
	return false
}

func runLegacyPolicyScope(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseLegacyPolicyScopeFlags(args, getenv)
	if err != nil {
		log.Printf("invalid legacy-policy-scope configuration: %v", err)
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres for legacy-policy-scope failed")
		return 1
	}
	defer pool.Close()

	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{
		AddSource: false, Level: slog.LevelInfo, ReplaceAttr: nil,
	}))
	runner, err := legacypolicyscope.NewRunner(pool, logger, legacypolicyscope.Options{
		BatchSize:        cfg.batchSize,
		LockTimeout:      cfg.lockTimeout,
		StatementTimeout: cfg.statementTimeout,
		ApplyAttempts:    0,
		RetryDelay:       0,
	})
	if err != nil {
		log.Printf("build legacy-policy-scope runner: %v", err)
		return 1
	}

	start := time.Now()
	summary, runErr := runner.Run(ctx, cfg.mode)
	result := "ok"
	exit := 0
	if runErr != nil {
		result = "failed"
		exit = 1
		log.Printf("legacy-policy-scope run failed: %v", runErr)
	}

	if err := writeLegacyPolicyScopeSummary(stdout, legacyPolicyScopeSummary{
		Mode:        string(cfg.mode),
		Environment: cfg.environment,
		Result:      result,
		ElapsedMS:   time.Since(start).Milliseconds(),
		Summary:     summary,
	}); err != nil {
		log.Printf("write legacy-policy-scope summary: %v", err)
		return 1
	}
	return exit
}

func writeLegacyPolicyScopeSummary(writer io.Writer, summary legacyPolicyScopeSummary) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode legacy policy scope summary: %w", err)
	}
	return nil
}

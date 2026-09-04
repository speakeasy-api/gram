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
	confirmScopesEnforced := fs.Bool("confirm-recommended-scopes-enabled", false,
		"assert risk-recommended-scopes is enabled for every project in this environment")
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
	if *batchSize <= 0 || *lockTimeout <= 0 || *statementTimeout <= 0 {
		return legacyPolicyScopeConfig{}, errors.New("batch size and timeouts must be positive")
	}
	if *apply && *confirmEnvironment != *environment {
		return legacyPolicyScopeConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	if *apply && *environment == "production" && *confirmProduction != "production" {
		return legacyPolicyScopeConfig{}, errors.New("production writes require -confirm-production=production")
	}
	// Both scanners ignore per-category detection scopes entirely while
	// risk-recommended-scopes is off (risk_analysis.CategoryScope.InScope and
	// CategoryScopes.Masks both short-circuit on it). Folding an enforcing
	// policy into scopes nothing reads would drop its narrowing outright, so
	// applying requires the operator to assert the flag is on.
	if *apply && !*confirmScopesEnforced {
		return legacyPolicyScopeConfig{}, errors.New(
			"writes require -confirm-recommended-scopes-enabled: folded scopes are not enforced while risk-recommended-scopes is off")
	}

	mode := legacypolicyscope.ModeDryRun
	if *apply {
		mode = legacypolicyscope.ModeApply
	}
	if *validate {
		mode = legacypolicyscope.ModeValidate
	}
	cfg := legacyPolicyScopeConfig{
		dbURL: getenv("GRAM_DATABASE_URL"), environment: *environment, mode: mode,
		batchSize: *batchSize, lockTimeout: *lockTimeout, statementTimeout: *statementTimeout,
	}
	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	return cfg, nil
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

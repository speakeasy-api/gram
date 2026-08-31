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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/openrouterdisablecauses"
)

const classifierVersion = "v1"

type openRouterDisableCausesConfig struct {
	dbURL                    string
	environment              string
	codeSHA                  string
	mode                     openrouterdisablecauses.Mode
	manualOverride           bool
	confirmManualOverride    bool
	validateOverrideManifest bool
	batchSize                int
	lockTimeout              time.Duration
	statementTimeout         time.Duration
	maxLockRetries           int
	overrideToken            string
}

type commandSummary struct {
	RunID             string                          `json:"run_id"`
	Mode              string                          `json:"mode"`
	Environment       string                          `json:"environment"`
	CodeSHA           string                          `json:"code_sha"`
	ClassifierVersion string                          `json:"classifier_version"`
	Result            string                          `json:"result"`
	ElapsedMS         int64                           `json:"elapsed_ms"`
	Summary           openrouterdisablecauses.Summary `json:"summary"`
	ManualChanged     *bool                           `json:"manual_changed,omitempty"`
}

func parseOpenRouterDisableCausesFlags(args []string, getenv func(string) string) (openRouterDisableCausesConfig, error) {
	fs := flag.NewFlagSet("openrouter-disable-causes", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "apply safe classifications")
	validate := fs.Bool("validate", false, "prove the complete live population at one snapshot (not a contract handoff)")
	manual := fs.Bool("manual-override", false, "read one protected override from stdin")
	environment := fs.String("environment", "", "explicit target environment")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for every write")
	confirmProduction := fs.String("confirm-production", "", "must equal production for a production write")
	confirmManual := fs.Bool("confirm-manual-override", false, "required for manual override mode")
	validateOverrides := fs.Bool("validate-override-manifest", false, "read the protected manual override manifest from stdin during validation")
	batchSize := fs.Int("batch-size", 100, "keyset batch size")
	lockTimeout := fs.Duration("lock-timeout", 2*time.Second, "per-transaction lock timeout")
	statementTimeout := fs.Duration("statement-timeout", 30*time.Second, "per-transaction statement timeout")
	maxLockRetries := fs.Int("max-lock-retries", 3, "bounded lock retry count")
	if err := fs.Parse(args); err != nil {
		return openRouterDisableCausesConfig{}, errors.New("invalid openrouter-disable-causes flags")
	}
	if fs.NArg() != 0 {
		return openRouterDisableCausesConfig{}, errors.New("unexpected positional arguments")
	}
	selected := 0
	for _, enabled := range []bool{*apply, *validate, *manual} {
		if enabled {
			selected++
		}
	}
	if selected > 1 {
		return openRouterDisableCausesConfig{}, errors.New("select exactly one mode: apply, validate, or manual-override")
	}
	if strings.TrimSpace(*environment) == "" {
		return openRouterDisableCausesConfig{}, errors.New("environment is required")
	}
	if *batchSize <= 0 || *lockTimeout <= 0 || *statementTimeout <= 0 || *maxLockRetries < 0 {
		return openRouterDisableCausesConfig{}, errors.New("batch size and timeouts must be positive and retries nonnegative")
	}
	writeMode := *apply || *manual
	if writeMode && *confirmEnvironment != *environment {
		return openRouterDisableCausesConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	if *environment == "production" && writeMode && *confirmProduction != "production" {
		return openRouterDisableCausesConfig{}, errors.New("production writes require -confirm-production=production")
	}
	if *manual && !*confirmManual {
		return openRouterDisableCausesConfig{}, errors.New("manual override requires -confirm-manual-override")
	}

	mode := openrouterdisablecauses.ModeDryRun
	if *apply {
		mode = openrouterdisablecauses.ModeApply
	}
	if *validate {
		mode = openrouterdisablecauses.ModeValidate
	}
	if *manual {
		mode = openrouterdisablecauses.ModeManualOverride
	}
	cfg := openRouterDisableCausesConfig{
		dbURL: getenv("GRAM_DATABASE_URL"), environment: *environment, codeSHA: getenv("GRAM_CODE_SHA"),
		mode: mode, manualOverride: *manual, confirmManualOverride: *confirmManual, validateOverrideManifest: *validateOverrides, batchSize: *batchSize,
		lockTimeout: *lockTimeout, statementTimeout: *statementTimeout, maxLockRetries: *maxLockRetries,
		overrideToken: getenv("GRAM_OPENROUTER_DISABLE_CAUSES_OVERRIDE_TOKEN"),
	}
	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	if cfg.codeSHA == "" {
		cfg.codeSHA = "unknown"
	}
	if cfg.validateOverrideManifest && cfg.mode != openrouterdisablecauses.ModeValidate {
		return cfg, errors.New("validation override manifest requires validate mode")
	}
	if (cfg.manualOverride || cfg.validateOverrideManifest) && cfg.overrideToken == "" {
		return cfg, errors.New("manual override authorization is not configured")
	}
	return cfg, nil
}

type manualOverrideEnvelope struct {
	AuthorizationToken string   `json:"authorization_token"`
	OrganizationID     string   `json:"organization_id"`
	KeyType            string   `json:"key_type"`
	Causes             []string `json:"causes"`
}

func decodeManualOverride(reader io.Reader, expectedToken string) (openrouterdisablecauses.ManualOverride, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 64*1024))
	decoder.DisallowUnknownFields()
	var envelope manualOverrideEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return openrouterdisablecauses.ManualOverride{}, errors.New("invalid manual override input")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return openrouterdisablecauses.ManualOverride{}, errors.New("manual override input must contain one JSON object")
	}
	if err := openrouterdisablecauses.AuthorizeManualOverride(envelope.AuthorizationToken, expectedToken); err != nil {
		return openrouterdisablecauses.ManualOverride{}, fmt.Errorf("authorize manual override: %w", err)
	}
	if envelope.OrganizationID == "" || envelope.KeyType == "" || envelope.Causes == nil {
		return openrouterdisablecauses.ManualOverride{}, errors.New("manual override fields are required")
	}
	return openrouterdisablecauses.ManualOverride{OrganizationID: envelope.OrganizationID, KeyType: envelope.KeyType, Causes: envelope.Causes}, nil
}

type validationOverrideEnvelope struct {
	AuthorizationToken string                                   `json:"authorization_token"`
	Overrides          []openrouterdisablecauses.ManualOverride `json:"overrides"`
}

func decodeValidationOverrides(reader io.Reader, expectedToken string) ([]openrouterdisablecauses.ManualOverride, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 64*1024))
	decoder.DisallowUnknownFields()
	var envelope validationOverrideEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, errors.New("invalid validation override manifest")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("validation override manifest must contain one JSON object")
	}
	if err := openrouterdisablecauses.AuthorizeManualOverride(envelope.AuthorizationToken, expectedToken); err != nil {
		return nil, fmt.Errorf("authorize validation override manifest: %w", err)
	}
	if envelope.Overrides == nil {
		return nil, errors.New("validation override manifest overrides are required")
	}
	return envelope.Overrides, nil
}

func writeOpenRouterDisableCausesSummary(writer io.Writer, summary commandSummary) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("encode OpenRouter disable causes summary: %w", err)
	}
	return nil
}

func runOpenRouterDisableCauses(args []string, stdin io.Reader, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseOpenRouterDisableCausesFlags(args, getenv)
	if err != nil {
		log.Printf("invalid openrouter-disable-causes configuration: %v", err)
		return 2
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres for openrouter-disable-causes failed")
		return 1
	}
	defer pool.Close()

	runID := uuid.NewString()
	runner := openrouterdisablecauses.NewRunner(pool, slog.Default(), openrouterdisablecauses.Options{
		BatchSize: cfg.batchSize, LockTimeout: cfg.lockTimeout, StatementTimeout: cfg.statementTimeout, MaxLockRetries: cfg.maxLockRetries,
	})
	started := time.Now()
	result := "success"
	summary := openrouterdisablecauses.Summary{
		Mode: cfg.mode, Scanned: 0, Classified: 0, Updated: 0, CauseSets: nil, Ambiguous: nil,
		Validation: nil, SkippedDeleted: 0, Batches: 0, LockRetries: 0, RemainingNulls: 0, Elapsed: 0,
	}
	var manualChanged *bool
	if cfg.manualOverride {
		override, decodeErr := decodeManualOverride(stdin, cfg.overrideToken)
		if decodeErr != nil {
			log.Printf("manual override rejected: %v", decodeErr)
			return 1
		}
		changed, applyErr := runner.ApplyManualOverride(ctx, override)
		manualChanged = &changed
		err = applyErr
	} else if cfg.validateOverrideManifest {
		overrides, decodeErr := decodeValidationOverrides(stdin, cfg.overrideToken)
		if decodeErr != nil {
			log.Printf("validation override manifest rejected")
			return 1
		}
		summary, err = runner.Validate(ctx, overrides)
	} else {
		summary, err = runner.Run(ctx, cfg.mode)
	}
	if err != nil {
		result = "blocked"
	}
	commandResult := commandSummary{
		RunID: runID, Mode: string(cfg.mode), Environment: cfg.environment, CodeSHA: cfg.codeSHA,
		ClassifierVersion: classifierVersion, Result: result, ElapsedMS: time.Since(started).Milliseconds(), Summary: summary, ManualChanged: manualChanged,
	}
	if writeErr := writeOpenRouterDisableCausesSummary(stdout, commandResult); writeErr != nil {
		log.Printf("write aggregate openrouter-disable-causes summary: %v", writeErr)
		return 1
	}
	if err != nil {
		log.Print(blockedOpenRouterDisableCausesLogLine(err))
		return 1
	}
	return 0
}

func blockedOpenRouterDisableCausesLogLine(err error) string {
	category := "unexpected"
	switch {
	case errors.Is(err, openrouterdisablecauses.ErrAmbiguousRows):
		category = "ambiguous_rows"
	case errors.Is(err, openrouterdisablecauses.ErrValidationFailed):
		category = "validation_failed"
	case errors.Is(err, openrouterdisablecauses.ErrManualOverrideConflict):
		category = "override_conflict"
	case isDatabaseOrTimeoutError(err):
		category = "database_or_timeout"
	}
	return "openrouter-disable-causes blocked; error_category=" + category + "; inspect aggregate reason counts"
}

func isDatabaseOrTimeoutError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if _, ok := errors.AsType[*pgconn.PgError](err); ok {
		return true
	}
	var connectErr *pgconn.ConnectError
	return errors.As(err, &connectErr)
}

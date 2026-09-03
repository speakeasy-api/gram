package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/agentmanagementgrants"
)

const agentManagementGrantContractVersion = "v1"

type agentManagementGrantsConfig struct {
	dbURL            string
	environment      string
	codeSHA          string
	mode             agentmanagementgrants.Mode
	batchSize        int
	sampleLimit      int
	lockTimeout      time.Duration
	statementTimeout time.Duration
}

type agentManagementGrantsCommandSummary struct {
	RunID           string                        `json:"run_id"`
	Environment     string                        `json:"environment"`
	CodeSHA         string                        `json:"code_sha"`
	ContractVersion string                        `json:"contract_version"`
	Result          string                        `json:"result"`
	ElapsedMS       int64                         `json:"elapsed_ms"`
	Summary         agentmanagementgrants.Summary `json:"summary"`
}

func parseAgentManagementGrantsFlags(args []string, getenv func(string) string) (agentManagementGrantsConfig, error) {
	fs := flag.NewFlagSet("agent-management-grants", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write missing canonical grants")
	verify := fs.Bool("verify", false, "gate enforcement on complete-population verification")
	environment := fs.String("environment", "", "explicit target environment")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for a write")
	confirmProduction := fs.String("confirm-production", "", "must equal production for a production write")
	batchSize := fs.Int("batch-size", 100, "organization keyset batch size")
	sampleLimit := fs.Int("sample-limit", 20, "maximum defect samples in each category")
	lockTimeout := fs.Duration("lock-timeout", 2*time.Second, "per-transaction lock timeout")
	statementTimeout := fs.Duration("statement-timeout", 30*time.Second, "per-transaction statement timeout")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return agentManagementGrantsConfig{}, errors.New("invalid agent-management-grants flags")
	}
	if *apply && *verify {
		return agentManagementGrantsConfig{}, errors.New("apply and verify modes are mutually exclusive")
	}
	if *environment == "" || strings.IndexFunc(*environment, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return agentManagementGrantsConfig{}, errors.New("environment is required and must not contain control characters")
	}
	if *batchSize <= 0 || *batchSize > 1000 {
		return agentManagementGrantsConfig{}, errors.New("batch-size must be between 1 and 1000")
	}
	if *sampleLimit <= 0 || *sampleLimit > 100 {
		return agentManagementGrantsConfig{}, errors.New("sample-limit must be between 1 and 100")
	}
	if *lockTimeout < time.Millisecond || *statementTimeout < time.Millisecond {
		return agentManagementGrantsConfig{}, errors.New("timeouts must be at least 1ms")
	}
	if *apply {
		runtimeEnvironment := getenv("GRAM_ENVIRONMENT")
		if runtimeEnvironment == "" || runtimeEnvironment != *environment {
			return agentManagementGrantsConfig{}, errors.New("GRAM_ENVIRONMENT must exactly match environment for apply mode")
		}
		if *confirmEnvironment != *environment {
			return agentManagementGrantsConfig{}, errors.New("confirm-environment must exactly match environment for apply mode")
		}
		if *environment == "production" && *confirmProduction != "production" {
			return agentManagementGrantsConfig{}, errors.New("production apply requires confirm-production=production")
		}
	}

	mode := agentmanagementgrants.ModeDryRun
	if *apply {
		mode = agentmanagementgrants.ModeApply
	} else if *verify {
		mode = agentmanagementgrants.ModeVerify
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return agentManagementGrantsConfig{}, errors.New("GRAM_DATABASE_URL is required")
	}
	return agentManagementGrantsConfig{
		dbURL: dbURL, environment: *environment, codeSHA: getenv("GRAM_CODE_SHA"), mode: mode,
		batchSize: *batchSize, sampleLimit: *sampleLimit, lockTimeout: *lockTimeout, statementTimeout: *statementTimeout,
	}, nil
}

func runAgentManagementGrants(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseAgentManagementGrantsFlags(args, getenv)
	if err != nil {
		log.Printf("invalid agent-management-grants configuration: %v", err)
		return 2
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Print("connect postgres for agent-management-grants failed")
		return 1
	}
	defer pool.Close()

	started := time.Now()
	runner := agentmanagementgrants.NewRunner(pool, agentmanagementgrants.Options{
		BatchSize: cfg.batchSize, SampleLimit: cfg.sampleLimit, LockTimeout: cfg.lockTimeout, StatementTimeout: cfg.statementTimeout,
	})
	summary, runErr := runner.Run(ctx, cfg.mode)
	result := "success"
	if runErr != nil {
		result = "blocked"
	}
	commandSummary := agentManagementGrantsCommandSummary{
		RunID: uuid.NewString(), Environment: cfg.environment, CodeSHA: cfg.codeSHA,
		ContractVersion: agentManagementGrantContractVersion, Result: result,
		ElapsedMS: time.Since(started).Milliseconds(), Summary: summary,
	}
	if err := writeAgentManagementGrantsSummary(stdout, commandSummary); err != nil {
		log.Printf("write bounded agent-management-grants summary: %v", err)
		return 1
	}
	if runErr != nil {
		log.Printf("agent-management-grants blocked; error_category=%s; inspect bounded summary", agentManagementGrantsErrorCategory(runErr))
		return 1
	}
	return 0
}

func agentManagementGrantsErrorCategory(err error) string {
	switch {
	case errors.Is(err, agentmanagementgrants.ErrVerificationFailed):
		return "verification_failed"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "database_or_timeout"
	default:
		if _, ok := errors.AsType[*pgconn.PgError](err); ok {
			return "database_or_timeout"
		}
		return "unexpected"
	}
}

func writeAgentManagementGrantsSummary(writer io.Writer, summary agentManagementGrantsCommandSummary) error {
	if err := json.NewEncoder(writer).Encode(summary); err != nil {
		return fmt.Errorf("encode agent management grants summary: %w", err)
	}
	return nil
}

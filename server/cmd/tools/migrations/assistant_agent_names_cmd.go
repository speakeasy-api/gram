package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/assistantagentnames"
)

const assistantAgentNamesCommand = "assistant-agent-names"

type assistantAgentNamesConfig struct {
	dbURL       string
	environment string
	orgID       string
	projectID   string
	apply       bool
}

func parseAssistantAgentNamesFlags(args []string, getenv func(string) string) (assistantAgentNamesConfig, error) {
	fs := flag.NewFlagSet(assistantAgentNamesCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	orgID := fs.String("org", "", "organization_id to scope the backfill (optional)")
	projectID := fs.String("project", "", "project UUID to scope the backfill (optional)")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+assistantAgentNamesCommand+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return assistantAgentNamesConfig{}, errors.New("invalid " + assistantAgentNamesCommand + " flags")
	}
	if *environment == "" {
		return assistantAgentNamesConfig{}, errors.New("environment is required")
	}
	// An explicitly empty -org (for example an unset shell variable) must not
	// silently widen the run to every organization.
	orgSet := false
	fs.Visit(func(f *flag.Flag) { orgSet = orgSet || f.Name == "org" })
	if orgSet && *orgID == "" {
		return assistantAgentNamesConfig{}, errors.New("-org must not be empty; omit it to cover every organization")
	}
	projectSet := false
	fs.Visit(func(f *flag.Flag) { projectSet = projectSet || f.Name == "project" })
	if projectSet {
		if _, err := uuid.Parse(*projectID); err != nil {
			return assistantAgentNamesConfig{}, errors.New("-project must be a nonempty UUID")
		}
	}
	if *apply && *confirmEnvironment != *environment {
		return assistantAgentNamesConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return assistantAgentNamesConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return assistantAgentNamesConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	// pgx dials fallbacks when the primary fails, so a fallback on another host
	// would send confirmed writes to an unconfirmed target.
	for _, fallback := range dbConfig.Fallbacks {
		if fallback.Host != dbConfig.Host || fallback.Port != dbConfig.Port {
			return assistantAgentNamesConfig{}, errors.New("$GRAM_DATABASE_URL must name a single host; fallbacks to other hosts are not allowed")
		}
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return assistantAgentNamesConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != assistantAgentNamesCommand {
		return assistantAgentNamesConfig{}, errors.New("writes require -confirm-apply=" + assistantAgentNamesCommand)
	}
	return assistantAgentNamesConfig{dbURL: dbURL, environment: *environment, orgID: *orgID, projectID: *projectID, apply: *apply}, nil
}

func runAssistantAgentNames(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseAssistantAgentNamesFlags(args, getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "invalid arguments: %v\n", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "migration failed: connect postgres")
		return 1
	}
	defer pool.Close()
	report, err := assistantagentnames.Run(ctx, pool, assistantagentnames.Options{OrganizationID: cfg.orgID, ProjectID: cfg.projectID, Apply: cfg.apply})
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s agents=%d\n", mode, cfg.environment, report.Agents)
	return 0
}

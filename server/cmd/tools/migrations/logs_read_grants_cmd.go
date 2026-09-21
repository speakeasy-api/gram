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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// logsReadLegacyScopes are the scopes that authorized an observability read
// before logs:read existed. A principal holding any of them could already read
// logs, so the backfill gives it an unrestricted logs:read grant and enforcing
// the new scope changes nobody's access.
var logsReadLegacyScopes = []string{
	string(authz.ScopeOrgRead),
	string(authz.ScopeOrgAdmin),
	string(authz.ScopeProjectRead),
	string(authz.ScopeProjectWrite),
}

type logsReadGrantsConfig struct {
	dbURL       string
	environment string
	apply       bool
}

func parseLogsReadGrantsFlags(args []string, getenv func(string) string) (logsReadGrantsConfig, error) {
	fs := flag.NewFlagSet("logs-read-grants", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal logs-read-grants for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return logsReadGrantsConfig{}, errors.New("invalid logs-read-grants flags")
	}
	if *environment == "" {
		return logsReadGrantsConfig{}, errors.New("environment is required")
	}
	if *apply && *confirmEnvironment != *environment {
		return logsReadGrantsConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return logsReadGrantsConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return logsReadGrantsConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return logsReadGrantsConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != "logs-read-grants" {
		return logsReadGrantsConfig{}, errors.New("writes require -confirm-apply=logs-read-grants")
	}
	return logsReadGrantsConfig{dbURL: dbURL, environment: *environment, apply: *apply}, nil
}

type logsReadGrantsReport struct {
	Principals  int
	GrantsAdded int
}

// logsReadGrantsStore is the database surface the backfill needs, narrow enough
// for the unit test to substitute.
type logsReadGrantsStore interface {
	ListPrincipalsMissingScope(context.Context, accessrepo.ListPrincipalsMissingScopeParams) ([]accessrepo.ListPrincipalsMissingScopeRow, error)
	InsertPrincipalGrantIfAbsent(context.Context, accessrepo.InsertPrincipalGrantIfAbsentParams) (int64, error)
}

func backfillLogsReadGrants(ctx context.Context, store logsReadGrantsStore, apply bool) (logsReadGrantsReport, error) {
	principals, err := store.ListPrincipalsMissingScope(ctx, accessrepo.ListPrincipalsMissingScopeParams{
		HeldScopes:  logsReadLegacyScopes,
		TargetScope: string(authz.ScopeLogsRead),
	})
	if err != nil {
		return logsReadGrantsReport{Principals: 0, GrantsAdded: 0}, fmt.Errorf("list principals missing logs:read: %w", err)
	}

	selectors, err := authz.NewSelector(authz.ScopeLogsRead, authz.WildcardResource).MarshalJSON()
	if err != nil {
		return logsReadGrantsReport{Principals: len(principals), GrantsAdded: 0}, fmt.Errorf("marshal logs:read selector: %w", err)
	}

	report := logsReadGrantsReport{Principals: len(principals), GrantsAdded: 0}
	for _, principal := range principals {
		if !apply {
			report.GrantsAdded++
			continue
		}
		affected, err := store.InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{
			OrganizationID: principal.OrganizationID,
			PrincipalUrn:   principal.PrincipalUrn,
			Scope:          string(authz.ScopeLogsRead),
			Selectors:      selectors,
		})
		if err != nil {
			return report, fmt.Errorf("insert logs:read grant: %w", err)
		}
		if affected > 0 {
			report.GrantsAdded++
		}
	}

	return report, nil
}

func runLogsReadGrants(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseLogsReadGrantsFlags(args, getenv)
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
	report, err := backfillLogsReadGrants(ctx, accessrepo.New(pool), cfg.apply)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s principals=%d grants_added=%d\n", mode, cfg.environment, report.Principals, report.GrantsAdded)
	return 0
}

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

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
)

const authPermissionDefaultsName = "auth-permission-defaults"

type authPermissionDefaultsConfig struct {
	dbURL       string
	environment string
	apply       bool
}

func parseAuthPermissionDefaultsFlags(args []string, getenv func(string) string) (authPermissionDefaultsConfig, error) {
	fs := flag.NewFlagSet(authPermissionDefaultsName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+authPermissionDefaultsName+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return authPermissionDefaultsConfig{}, errors.New("invalid " + authPermissionDefaultsName + " flags")
	}
	if *environment == "" {
		return authPermissionDefaultsConfig{}, errors.New("environment is required")
	}
	if *apply && *confirmEnvironment != *environment {
		return authPermissionDefaultsConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return authPermissionDefaultsConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return authPermissionDefaultsConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return authPermissionDefaultsConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != authPermissionDefaultsName {
		return authPermissionDefaultsConfig{}, errors.New("writes require -confirm-apply=" + authPermissionDefaultsName)
	}
	return authPermissionDefaultsConfig{dbURL: dbURL, environment: *environment, apply: *apply}, nil
}

// authPermissionDefaultsReport aggregates every organization's run. It holds
// counts only.
type authPermissionDefaultsReport struct {
	Organizations int
	Granted       map[authclientbackfill.GrantKind]int
	Written       int
	KeysWaiting   int
	Busy          int
	Failed        int
}

func newAuthPermissionDefaultsReport() authPermissionDefaultsReport {
	return authPermissionDefaultsReport{Organizations: 0, Granted: map[authclientbackfill.GrantKind]int{}, Written: 0, KeysWaiting: 0, Busy: 0, Failed: 0}
}

// backfillAuthPermissionDefaults runs the grants phase for each organization
// in turn. An organization another run holds is skipped and counted; any other
// failure stops the backfill, which is safe to rerun.
func backfillAuthPermissionDefaults(organizationIDs []string, run func(organizationID string) (authclientbackfill.Report, error)) (authPermissionDefaultsReport, error) {
	report := newAuthPermissionDefaultsReport()
	for _, organizationID := range organizationIDs {
		report.Organizations++
		result, err := run(organizationID)
		switch {
		case errors.Is(err, authclientbackfill.ErrRunInProgress):
			report.Busy++
			continue
		case err != nil:
			return report, fmt.Errorf("organization %d of %d: %w", report.Organizations, len(organizationIDs), err)
		}
		if len(result.PhaseErrors) > 0 {
			report.Failed++
			continue
		}
		for kind, n := range result.PendingGrantsByKind {
			report.Granted[kind] += n
		}
		report.Written += result.GrantsWritten
		if result.APIKeysWaiting {
			report.KeysWaiting++
		}
	}
	return report, nil
}

func runAuthPermissionDefaults(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseAuthPermissionDefaultsFlags(args, getenv)
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
	organizationIDs, err := authclientbackfill.New(pool).ListOrganizationIDsForGrants(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "migration failed: list organizations")
		return 1
	}
	report, err := backfillAuthPermissionDefaults(organizationIDs, func(organizationID string) (authclientbackfill.Report, error) {
		result, err := authclientbackfill.RunGrants(ctx, pool, authclientbackfill.Options{OrganizationID: organizationID, Apply: cfg.apply, PageSize: 0})
		if err != nil {
			return result, fmt.Errorf("grant default auth permissions: %w", err)
		}
		return result, nil
	})
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s organizations=%d personal_default=%d shared_client=%d api_key=%d written=%d keys_waiting=%d busy=%d failed=%d\n",
		mode, cfg.environment, report.Organizations,
		report.Granted[authclientbackfill.GrantPersonalDefault], report.Granted[authclientbackfill.GrantSharedClient], report.Granted[authclientbackfill.GrantAPIKey],
		report.Written, report.KeysWaiting, report.Busy, report.Failed)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		return 1
	}
	return 0
}

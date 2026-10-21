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
	dbURL          string
	environment    string
	organizationID string
	apply          bool
}

func parseAuthPermissionDefaultsFlags(args []string, getenv func(string) string) (authPermissionDefaultsConfig, error) {
	fs := flag.NewFlagSet(authPermissionDefaultsName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+authPermissionDefaultsName+" for writes")
	organizationID := fs.String("organization", "", "run for this one organization id only")
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
	return authPermissionDefaultsConfig{dbURL: dbURL, environment: *environment, organizationID: *organizationID, apply: *apply}, nil
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

// authPermissionDefaultsProgressEvery is how many organizations pass between
// progress lines.
const authPermissionDefaultsProgressEvery = 100

// backfillAuthPermissionDefaults runs the grants phase for each organization
// in turn. An organization another run holds, or whose grants phase rolled
// back, is counted and skipped; an error that is not an organization's result
// stops the backfill, which is safe to rerun.
func backfillAuthPermissionDefaults(organizationIDs []string, run func(organizationID string) (authclientbackfill.Report, error), progress func(done, total int)) (authPermissionDefaultsReport, error) {
	report := newAuthPermissionDefaultsReport()
	for i, organizationID := range organizationIDs {
		if i > 0 && i%authPermissionDefaultsProgressEvery == 0 {
			progress(i, len(organizationIDs))
		}
		report.Organizations++
		result, err := run(organizationID)
		switch {
		case errors.Is(err, authclientbackfill.ErrRunInProgress):
			report.Busy++
			continue
		case err != nil:
			return report, fmt.Errorf("organization %d of %d: %w", report.Organizations, len(organizationIDs), err)
		}
		// Other phase errors come from the readiness check and only hold
		// key policies back; the grants themselves committed.
		if _, failed := result.PhaseErrors[authclientbackfill.PhaseGrants]; failed {
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
	organizationIDs := []string{cfg.organizationID}
	if cfg.organizationID != "" {
		exists, err := authclientbackfill.New(pool).OrganizationExistsForGrants(ctx, cfg.organizationID)
		if err != nil || !exists {
			_, _ = fmt.Fprintln(stdout, "invalid arguments: -organization names no organization")
			return 2
		}
	} else {
		organizationIDs, err = authclientbackfill.New(pool).ListOrganizationIDsForGrants(ctx)
		if err != nil {
			_, _ = fmt.Fprintln(stdout, "migration failed: list organizations")
			return 1
		}
	}
	report, err := backfillAuthPermissionDefaults(organizationIDs, func(organizationID string) (authclientbackfill.Report, error) {
		result, err := authclientbackfill.RunGrants(ctx, pool, authclientbackfill.Options{OrganizationID: organizationID, Apply: cfg.apply, PageSize: 0})
		if err != nil {
			return result, fmt.Errorf("grant default auth permissions: %w", err)
		}
		return result, nil
	}, func(done, total int) {
		_, _ = fmt.Fprintf(stdout, "progress organizations=%d/%d\n", done, total)
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
	return authPermissionDefaultsExitCode(stdout, report)
}

// authPermissionDefaultsExitCode fails a run that left any organization
// unfinished, so a rerun is never skipped by mistake.
func authPermissionDefaultsExitCode(stdout io.Writer, report authPermissionDefaultsReport) int {
	if report.Failed > 0 || report.Busy > 0 {
		_, _ = fmt.Fprintln(stdout, "migration incomplete: some organizations failed or were busy; rerun")
		return 1
	}
	return 0
}

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
// counts, and ids only for the attachments that need remediation.
type authPermissionDefaultsReport struct {
	Organizations int
	Granted       map[authclientbackfill.GrantKind]int
	Written       int
	KeysWaiting   int
	SharedWaiting int
	// AttachmentsWaiting counts attachments held back by the upstream_client
	// write gate; a run with any is incomplete.
	AttachmentsWaiting int
	Busy               int
	Failed             int
	Remediations       []attachmentRemediation
}

// attachmentRemediation is one agent attachment an owner must reconnect or
// configure, with its organization.
type attachmentRemediation struct {
	OrganizationID string
	authclientbackfill.AttachmentRemediation
}

func newAuthPermissionDefaultsReport() authPermissionDefaultsReport {
	return authPermissionDefaultsReport{Organizations: 0, Granted: map[authclientbackfill.GrantKind]int{}, Written: 0, KeysWaiting: 0, SharedWaiting: 0, AttachmentsWaiting: 0, Busy: 0, Failed: 0, Remediations: nil}
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
		for _, remediation := range result.AttachmentRemediations {
			report.Remediations = append(report.Remediations, attachmentRemediation{OrganizationID: organizationID, AttachmentRemediation: remediation})
		}
		if result.APIKeysWaiting {
			report.KeysWaiting++
		}
		if result.SharedClientsWaiting {
			report.SharedWaiting++
		}
		report.AttachmentsWaiting += result.AttachmentsWaiting
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
	q := authclientbackfill.New(pool)
	organizationIDs, code := authPermissionDefaultsOrganizations(stdout, cfg.organizationID,
		func(id string) (bool, error) { return q.OrganizationExistsForGrants(ctx, id) },
		func() ([]string, error) { return q.ListOrganizationIDsForGrants(ctx) })
	if code != 0 {
		return code
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
	for _, remediation := range report.Remediations {
		_, _ = fmt.Fprintf(stdout, "remediate organization=%s binding=%s agent=%s reason=%s\n", remediation.OrganizationID, remediation.BindingID, remediation.AgentID, remediation.Reason)
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s organizations=%d personal_default=%d shared_client=%d api_key=%d attachment=%d written=%d keys_waiting=%d shared_waiting=%d attachments_waiting=%d attachments_to_remediate=%d busy=%d failed=%d\n",
		mode, cfg.environment, report.Organizations,
		report.Granted[authclientbackfill.GrantPersonalDefault], report.Granted[authclientbackfill.GrantSharedClient], report.Granted[authclientbackfill.GrantAPIKey], report.Granted[authclientbackfill.GrantAttachment],
		report.Written, report.KeysWaiting, report.SharedWaiting, report.AttachmentsWaiting, len(report.Remediations), report.Busy, report.Failed)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		return 1
	}
	return authPermissionDefaultsExitCode(stdout, report)
}

// authPermissionDefaultsOrganizations returns the organizations to run, or a
// nonzero exit code after reporting why not. A lookup that fails is a
// migration failure (1); only a lookup that succeeds and finds no such
// organization is an argument error (2).
func authPermissionDefaultsOrganizations(stdout io.Writer, organizationID string, exists func(string) (bool, error), list func() ([]string, error)) ([]string, int) {
	if organizationID == "" {
		ids, err := list()
		if err != nil {
			_, _ = fmt.Fprintln(stdout, "migration failed: list organizations")
			return nil, 1
		}
		return ids, 0
	}
	found, err := exists(organizationID)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "migration failed: look up organization")
		return nil, 1
	}
	if !found {
		_, _ = fmt.Fprintln(stdout, "invalid arguments: -organization names no organization")
		return nil, 2
	}
	return []string{organizationID}, 0
}

// authPermissionDefaultsExitCode fails a run that left any organization
// unfinished, so a rerun is never skipped by mistake.
func authPermissionDefaultsExitCode(stdout io.Writer, report authPermissionDefaultsReport) int {
	if report.Failed > 0 || report.Busy > 0 {
		_, _ = fmt.Fprintln(stdout, "migration incomplete: some organizations failed or were busy; rerun")
		return 1
	}
	if report.AttachmentsWaiting > 0 {
		_, _ = fmt.Fprintln(stdout, "migration incomplete: attachment grants wait for the upstream_client write gate; rerun once it is enabled")
		return 1
	}
	return 0
}

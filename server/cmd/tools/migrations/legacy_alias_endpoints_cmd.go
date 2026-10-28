package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	legacyaliasrepo "github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const legacyAliasEndpointsName = "legacy-alias-endpoints"

// legacyAliasEndpointsPageSize bounds each candidate page; servers are still written one transaction at a time.
const legacyAliasEndpointsPageSize = 500

type legacyAliasEndpointsConfig struct {
	dbURL          string
	organizationID string
	projectID      uuid.NullUUID
	journalPath    string
	dryRun         bool
}

func parseLegacyAliasEndpointsFlags(args []string, getenv func(string) string) (legacyAliasEndpointsConfig, error) {
	fs := flag.NewFlagSet(legacyAliasEndpointsName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", true, "print the plan without writing")
	organizationID := fs.String("organization", "", "run for this organization id only")
	projectID := fs.String("project", "", "run for this project id only")
	journalPath := fs.String("journal", "", "new file recording every created endpoint; required for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return legacyAliasEndpointsConfig{}, errors.New("invalid " + legacyAliasEndpointsName + " flags")
	}
	cfg := legacyAliasEndpointsConfig{
		dbURL: getenv("GRAM_DATABASE_URL"), organizationID: *organizationID, projectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		journalPath: *journalPath, dryRun: *dryRun,
	}
	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	if *projectID != "" {
		id, err := uuid.Parse(*projectID)
		if err != nil {
			return cfg, errors.New("invalid -project")
		}
		cfg.projectID = uuid.NullUUID{UUID: id, Valid: true}
	}
	if cfg.dryRun {
		return cfg, nil
	}
	if cfg.journalPath == "" {
		return cfg, errors.New("writes require -journal")
	}
	dbConfig, err := pgx.ParseConfig(cfg.dbURL)
	if err != nil {
		return cfg, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *confirmTarget != target {
		return cfg, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	return cfg, nil
}

// legacyAliasEndpointJournalEntry records one created endpoint, enough to find and remove it.
type legacyAliasEndpointJournalEntry struct {
	// EndpointID is the created mcp_endpoints row.
	EndpointID uuid.UUID `json:"endpoint_id"`

	// McpServerID is the canonical server the endpoint points at, equal to the toolset id.
	McpServerID uuid.UUID `json:"mcp_server_id"`

	// ProjectID is the endpoint's project.
	ProjectID uuid.UUID `json:"project_id"`

	// Slug is the endpoint's slug.
	Slug string `json:"slug"`

	// CustomDomainID is the endpoint's custom domain; null on the platform host.
	CustomDomainID *uuid.UUID `json:"custom_domain_id"`
}

// legacyAliasEndpointsTotals counts rows by printed action.
type legacyAliasEndpointsTotals map[string]int

func legacyAliasEndpointsAddress(row mcpendpoints.AliasBackfillRow) string {
	if row.CustomDomainID.Valid {
		return "domain:" + row.CustomDomainID.UUID.String() + "/" + row.Slug
	}
	return "platform/" + row.Slug
}

func legacyAliasEndpointsActionLabel(action mcpendpoints.AliasBackfillAction, dryRun bool) string {
	switch {
	case action == mcpendpoints.AliasBackfillCreate && dryRun:
		return "would_create"
	case action == mcpendpoints.AliasBackfillCreate:
		return "created"
	default:
		return string(action)
	}
}

func printLegacyAliasEndpointsRow(stdout io.Writer, row mcpendpoints.AliasBackfillRow, label string) {
	endpoint := ""
	if row.EndpointID.Valid {
		endpoint = row.EndpointID.UUID.String()
	}
	_, _ = fmt.Fprintf(stdout, "toolset=%s project=%s address=%s platform_for_custom_domain=%t action=%s endpoint=%s reason=%q\n",
		row.ToolsetID, row.ProjectID, legacyAliasEndpointsAddress(row), row.PlatformForCustomDomain, label, endpoint, row.Reason)
}

func runLegacyAliasEndpoints(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseLegacyAliasEndpointsFlags(args, getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "invalid arguments: %v\n", err)
		return 2
	}

	// The journal is opened before any write, so a bad path cannot strand committed endpoints without a record.
	var journal *os.File
	if !cfg.dryRun {
		journal, err = os.OpenFile(cfg.journalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- operator-supplied journal path
		if err != nil {
			_, _ = fmt.Fprintf(stdout, "invalid arguments: open -journal: %v\n", err)
			return 2
		}
		defer o11y.NoLogDefer(journal.Close)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "migration failed: connect postgres")
		return 1
	}
	defer pool.Close()

	totals, runErr := backfillLegacyAliasEndpoints(ctx, pool, cfg, stdout, journal)
	labels := make([]string, 0, len(totals))
	for label := range totals {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	mode := "dry-run"
	if !cfg.dryRun {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s", mode)
	for _, label := range labels {
		_, _ = fmt.Fprintf(stdout, " %s=%d", label, totals[label])
	}
	_, _ = fmt.Fprintln(stdout)
	switch {
	case runErr != nil:
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", runErr)
		return 1
	case totals["failed"] > 0:
		_, _ = fmt.Fprintln(stdout, "migration incomplete: some servers failed; rerun")
		return 1
	}
	return 0
}

// backfillLegacyAliasEndpoints walks every aliased toolset in id order, one server transaction each. A server whose
// transaction fails is reported as failed and the walk goes on; an error listing candidates or writing the journal
// stops it.
func backfillLegacyAliasEndpoints(ctx context.Context, pool *pgxpool.Pool, cfg legacyAliasEndpointsConfig, stdout io.Writer, journal *os.File) (legacyAliasEndpointsTotals, error) {
	totals := legacyAliasEndpointsTotals{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{AddSource: false, Level: slog.LevelError, ReplaceAttr: nil}))
	auditLogger := audit.NewLogger(audit.WithoutOutbox())
	organizationID := pgtype.Text{String: cfg.organizationID, Valid: cfg.organizationID != ""}
	after := uuid.Nil
	for {
		candidates, err := legacyaliasrepo.New(pool).ListAliasEndpointBackfillCandidates(ctx, legacyaliasrepo.ListAliasEndpointBackfillCandidatesParams{
			AfterID: after, OrganizationID: organizationID, ProjectID: cfg.projectID, PageSize: legacyAliasEndpointsPageSize,
		})
		if err != nil {
			return totals, fmt.Errorf("list aliased toolsets: %w", err)
		}
		if len(candidates) == 0 {
			return totals, nil
		}
		for _, candidate := range candidates {
			after = candidate.ID
			rows, err := mcpendpoints.BackfillAliasEndpoints(ctx, pool, logger, auditLogger, mcpendpoints.AliasBackfillCandidate{
				ToolsetID: candidate.ID, ProjectID: candidate.ProjectID, OrganizationID: candidate.OrganizationID,
			}, !cfg.dryRun)
			if err != nil {
				if ctx.Err() != nil {
					return totals, fmt.Errorf("interrupted: %w", ctx.Err())
				}
				totals["failed"]++
				_, _ = fmt.Fprintf(stdout, "toolset=%s project=%s action=failed reason=%q\n", candidate.ID, candidate.ProjectID, err.Error())
				continue
			}
			for _, row := range rows {
				label := legacyAliasEndpointsActionLabel(row.Action, cfg.dryRun)
				totals[label]++
				printLegacyAliasEndpointsRow(stdout, row, label)
				if journal == nil || row.Action != mcpendpoints.AliasBackfillCreate {
					continue
				}
				if err := appendLegacyAliasEndpointJournal(journal, row); err != nil {
					return totals, err
				}
			}
		}
	}
}

func appendLegacyAliasEndpointJournal(journal *os.File, row mcpendpoints.AliasBackfillRow) error {
	var domainID *uuid.UUID
	if row.CustomDomainID.Valid {
		domainID = &row.CustomDomainID.UUID
	}
	line, err := json.Marshal(legacyAliasEndpointJournalEntry{
		EndpointID: row.EndpointID.UUID, McpServerID: row.ToolsetID, ProjectID: row.ProjectID, Slug: row.Slug, CustomDomainID: domainID,
	})
	if err != nil {
		return fmt.Errorf("encode journal entry: %w", err)
	}
	if _, err := journal.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write journal: %w", err)
	}
	if err := journal.Sync(); err != nil {
		return fmt.Errorf("flush journal: %w", err)
	}
	return nil
}

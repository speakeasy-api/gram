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
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	legacyaliasrepo "github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const legacyAliasEndpointsName = "legacy-alias-endpoints"

// legacyAliasEndpointsPageSize bounds each candidate page; servers are still written one transaction at a time.
const legacyAliasEndpointsPageSize = 500

// Labels the output uses beyond the backfill's own actions.
const (
	legacyAliasLabelWouldCreate          = "would_create"
	legacyAliasLabelCreated              = "created"
	legacyAliasLabelFailed               = "failed"
	legacyAliasLabelCommitOutcomeUnknown = "commit_outcome_unknown"
	legacyAliasLabelAliasMoved           = "alias_moved"
	legacyAliasLabelWouldMoveAlias       = "would_move_alias"
)

type legacyAliasEndpointsConfig struct {
	dbURL          string
	environment    string
	organizationID string
	projectID      uuid.NullUUID
	journalPath    string
	apply          bool
}

func parseLegacyAliasEndpointsFlags(args []string, getenv func(string) string) (legacyAliasEndpointsConfig, error) {
	fs := flag.NewFlagSet(legacyAliasEndpointsName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+legacyAliasEndpointsName+" for writes")
	organizationID := fs.String("organization", "", "run for this organization id only")
	projectID := fs.String("project", "", "run for this project id only")
	journalPath := fs.String("journal", "", "new file recording every created endpoint and alias move; required for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return legacyAliasEndpointsConfig{}, errors.New("invalid " + legacyAliasEndpointsName + " flags")
	}
	cfg := legacyAliasEndpointsConfig{
		dbURL: getenv("GRAM_DATABASE_URL"), environment: *environment, organizationID: *organizationID,
		projectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, journalPath: *journalPath, apply: *apply,
	}
	if cfg.environment == "" {
		return cfg, errors.New("environment is required")
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
	if !cfg.apply {
		return cfg, nil
	}
	if *confirmEnvironment != cfg.environment {
		return cfg, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbConfig, err := pgx.ParseConfig(cfg.dbURL)
	if err != nil {
		return cfg, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *confirmTarget != target {
		return cfg, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *confirmApply != legacyAliasEndpointsName {
		return cfg, errors.New("writes require -confirm-apply=" + legacyAliasEndpointsName)
	}
	if cfg.journalPath == "" {
		return cfg, errors.New("writes require -journal")
	}
	return cfg, nil
}

// legacyAliasJournalAddress is one MCP address in the journal.
type legacyAliasJournalAddress struct {
	// Slug is the address slug.
	Slug string `json:"slug"`

	// CustomDomainID is the address's custom domain; null on the platform host.
	CustomDomainID *uuid.UUID `json:"custom_domain_id"`
}

// legacyAliasJournalEndpoint is one endpoint a server's transaction creates.
type legacyAliasJournalEndpoint struct {
	// EndpointID is the created mcp_endpoints row.
	EndpointID uuid.UUID `json:"endpoint_id"`

	// Address is the endpoint's address.
	Address legacyAliasJournalAddress `json:"address"`
}

// legacyAliasJournalMove is an alias move a server's transaction makes.
type legacyAliasJournalMove struct {
	// From is the alias before the move.
	From legacyAliasJournalAddress `json:"from"`

	// To is the alias after the move.
	To legacyAliasJournalAddress `json:"to"`
}

// Journal entry statuses. A server's pending entry is written and flushed before its commit; one of the others
// follows once the commit's outcome is known.
const (
	legacyAliasJournalPending        = "pending"
	legacyAliasJournalCommitted      = "committed"
	legacyAliasJournalRolledBack     = "rolled_back"
	legacyAliasJournalOutcomeUnknown = "commit_outcome_unknown"
)

// legacyAliasJournalEntry records one server's writes, enough to undo them.
type legacyAliasJournalEntry struct {
	// Status is pending, committed, rolled_back or commit_outcome_unknown.
	Status string `json:"status"`

	// McpServerID is the canonical server, equal to the toolset id.
	McpServerID uuid.UUID `json:"mcp_server_id"`

	// ProjectID is the server's project.
	ProjectID uuid.UUID `json:"project_id"`

	// Endpoints are the endpoints the transaction creates; set on the pending entry.
	Endpoints []legacyAliasJournalEndpoint `json:"endpoints,omitempty"`

	// AliasMove is the alias move the transaction makes; set on the pending entry when there is one.
	AliasMove *legacyAliasJournalMove `json:"alias_move,omitempty"`
}

// legacyAliasJournal is where applied writes are recorded; *os.File satisfies it.
type legacyAliasJournal interface {
	io.Writer
	Sync() error
}

func journalAddress(slug string, domain uuid.NullUUID) legacyAliasJournalAddress {
	var domainID *uuid.UUID
	if domain.Valid {
		domainID = &domain.UUID
	}
	return legacyAliasJournalAddress{Slug: slug, CustomDomainID: domainID}
}

func journalAliasAddress(alias legacyalias.Alias) legacyAliasJournalAddress {
	return journalAddress(alias.Slug.String, alias.CustomDomainID)
}

// pendingJournalEntry is the intent of a server's transaction, or nil when it writes nothing.
func pendingJournalEntry(candidate mcpendpoints.AliasBackfillCandidate, result mcpendpoints.AliasBackfillResult) *legacyAliasJournalEntry {
	entry := legacyAliasJournalEntry{Status: legacyAliasJournalPending, McpServerID: candidate.ToolsetID, ProjectID: candidate.ProjectID, Endpoints: nil, AliasMove: nil}
	for _, row := range result.Rows {
		if row.Action == mcpendpoints.AliasBackfillCreate {
			entry.Endpoints = append(entry.Endpoints, legacyAliasJournalEndpoint{EndpointID: row.EndpointID.UUID, Address: journalAddress(row.Slug, row.CustomDomainID)})
		}
	}
	if move := result.AliasMove; move != nil {
		entry.AliasMove = &legacyAliasJournalMove{From: journalAliasAddress(move.From), To: journalAliasAddress(move.To)}
	}
	if len(entry.Endpoints) == 0 && entry.AliasMove == nil {
		return nil
	}
	return &entry
}

func appendLegacyAliasJournal(journal legacyAliasJournal, entry legacyAliasJournalEntry) error {
	line, err := json.Marshal(entry)
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

func legacyAliasAddressText(slug string, domain uuid.NullUUID) string {
	if domain.Valid {
		return "domain:" + domain.UUID.String() + "/" + slug
	}
	return "platform/" + slug
}

// legacyAliasEndpointsRun is one backfill pass, with its database access injected.
type legacyAliasEndpointsRun struct {
	// list returns the next page of candidates after the given toolset id.
	list func(after uuid.UUID) ([]mcpendpoints.AliasBackfillCandidate, error)

	// backfill runs one candidate's transaction, calling beforeCommit with its writes before committing them.
	backfill func(candidate mcpendpoints.AliasBackfillCandidate, beforeCommit func(mcpendpoints.AliasBackfillResult) error) (mcpendpoints.AliasBackfillResult, error)

	// apply reports whether the run writes.
	apply bool

	// stdout receives the per-row plan.
	stdout io.Writer

	// journal records applied writes; nil on a dry run.
	journal legacyAliasJournal
}

// legacyAliasEndpointsTotals counts output rows by label.
type legacyAliasEndpointsTotals map[string]int

// execute walks every candidate in id order. A server whose transaction fails is reported and the walk goes on; an
// error listing candidates or writing the journal stops it.
func (r legacyAliasEndpointsRun) execute() (legacyAliasEndpointsTotals, error) {
	totals := legacyAliasEndpointsTotals{}
	after := uuid.Nil
	for {
		candidates, err := r.list(after)
		if err != nil {
			return totals, fmt.Errorf("list aliased toolsets: %w", err)
		}
		if len(candidates) == 0 {
			return totals, nil
		}
		for _, candidate := range candidates {
			after = candidate.ToolsetID
			if err := r.one(candidate, totals); err != nil {
				return totals, err
			}
		}
	}
}

func (r legacyAliasEndpointsRun) one(candidate mcpendpoints.AliasBackfillCandidate, totals legacyAliasEndpointsTotals) error {
	var pending *legacyAliasJournalEntry
	beforeCommit := func(result mcpendpoints.AliasBackfillResult) error {
		pending = pendingJournalEntry(candidate, result)
		if pending == nil || r.journal == nil {
			return nil
		}
		return appendLegacyAliasJournal(r.journal, *pending)
	}
	outcome := func(status string) error {
		if pending == nil || r.journal == nil {
			return nil
		}
		return appendLegacyAliasJournal(r.journal, legacyAliasJournalEntry{Status: status, McpServerID: candidate.ToolsetID, ProjectID: candidate.ProjectID, Endpoints: nil, AliasMove: nil})
	}

	result, err := r.backfill(candidate, beforeCommit)
	switch {
	case errors.Is(err, mcpendpoints.ErrAliasBackfillCommitUnknown):
		totals[legacyAliasLabelCommitOutcomeUnknown]++
		_, _ = fmt.Fprintf(r.stdout, "toolset=%s project=%s action=%s reason=%q\n", candidate.ToolsetID, candidate.ProjectID, legacyAliasLabelCommitOutcomeUnknown, err.Error())
		return outcome(legacyAliasJournalOutcomeUnknown)
	case errors.Is(err, mcpendpoints.ErrAliasBackfillNotRecorded):
		totals[legacyAliasLabelFailed]++
		_, _ = fmt.Fprintf(r.stdout, "toolset=%s project=%s action=%s reason=%q\n", candidate.ToolsetID, candidate.ProjectID, legacyAliasLabelFailed, err.Error())
		return fmt.Errorf("journal unavailable: %w", err)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("interrupted: %w", err)
	case err != nil:
		totals[legacyAliasLabelFailed]++
		_, _ = fmt.Fprintf(r.stdout, "toolset=%s project=%s action=%s reason=%q\n", candidate.ToolsetID, candidate.ProjectID, legacyAliasLabelFailed, err.Error())
		return outcome(legacyAliasJournalRolledBack)
	}
	if err := outcome(legacyAliasJournalCommitted); err != nil {
		return err
	}

	for _, row := range result.Rows {
		label := string(row.Action)
		if row.Action == mcpendpoints.AliasBackfillCreate {
			label = legacyAliasLabelWouldCreate
			if r.apply {
				label = legacyAliasLabelCreated
			}
		}
		totals[label]++
		endpoint := ""
		if row.EndpointID.Valid {
			endpoint = row.EndpointID.UUID.String()
		}
		_, _ = fmt.Fprintf(r.stdout, "toolset=%s project=%s address=%s platform_for_custom_domain=%t action=%s endpoint=%s reason=%q\n",
			row.ToolsetID, row.ProjectID, legacyAliasAddressText(row.Slug, row.CustomDomainID), row.PlatformForCustomDomain, label, endpoint, row.Reason)
	}
	if move := result.AliasMove; move != nil {
		label := legacyAliasLabelWouldMoveAlias
		if r.apply {
			label = legacyAliasLabelAliasMoved
		}
		totals[label]++
		_, _ = fmt.Fprintf(r.stdout, "toolset=%s project=%s action=%s alias_from=%s alias_to=%s\n", candidate.ToolsetID, candidate.ProjectID, label,
			legacyAliasAddressText(move.From.Slug.String, move.From.CustomDomainID), legacyAliasAddressText(move.To.Slug.String, move.To.CustomDomainID))
	}
	return nil
}

// legacyAliasEndpointsReport prints the totals line and returns the exit code: nonzero while any server failed or
// its commit outcome is unknown.
func legacyAliasEndpointsReport(stdout io.Writer, cfg legacyAliasEndpointsConfig, totals legacyAliasEndpointsTotals, runErr error) int {
	labels := make([]string, 0, len(totals))
	for label := range totals {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	parts := []string{"mode=" + mode, "environment=" + cfg.environment}
	for _, label := range labels {
		parts = append(parts, fmt.Sprintf("%s=%d", label, totals[label]))
	}
	_, _ = fmt.Fprintln(stdout, strings.Join(parts, " "))
	switch {
	case runErr != nil:
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", runErr)
		return 1
	case totals[legacyAliasLabelCommitOutcomeUnknown] > 0:
		_, _ = fmt.Fprintln(stdout, "migration incomplete: some commit outcomes are unknown; check the audit log, then rerun")
		return 1
	case totals[legacyAliasLabelFailed] > 0:
		_, _ = fmt.Fprintln(stdout, "migration incomplete: some servers failed; rerun")
		return 1
	}
	return 0
}

func runLegacyAliasEndpoints(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseLegacyAliasEndpointsFlags(args, getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "invalid arguments: %v\n", err)
		return 2
	}

	// The journal is opened before any write, so a bad path cannot strand committed writes without a record.
	var journal *os.File
	if cfg.apply {
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

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{AddSource: false, Level: slog.LevelError, ReplaceAttr: nil}))
	auditLogger := audit.NewLogger(audit.WithoutOutbox())
	organizationID := pgtype.Text{String: cfg.organizationID, Valid: cfg.organizationID != ""}
	run := legacyAliasEndpointsRun{
		list: func(after uuid.UUID) ([]mcpendpoints.AliasBackfillCandidate, error) {
			rows, err := legacyaliasrepo.New(pool).ListAliasEndpointBackfillCandidates(ctx, legacyaliasrepo.ListAliasEndpointBackfillCandidatesParams{
				AfterID: after, OrganizationID: organizationID, ProjectID: cfg.projectID, PageSize: legacyAliasEndpointsPageSize,
			})
			if err != nil {
				return nil, fmt.Errorf("list candidates: %w", err)
			}
			candidates := make([]mcpendpoints.AliasBackfillCandidate, 0, len(rows))
			for _, row := range rows {
				candidates = append(candidates, mcpendpoints.AliasBackfillCandidate{ToolsetID: row.ID, ProjectID: row.ProjectID, OrganizationID: row.OrganizationID})
			}
			return candidates, nil
		},
		backfill: func(candidate mcpendpoints.AliasBackfillCandidate, beforeCommit func(mcpendpoints.AliasBackfillResult) error) (mcpendpoints.AliasBackfillResult, error) {
			result, err := mcpendpoints.BackfillAliasEndpoints(ctx, pool, logger, auditLogger, candidate, cfg.apply, beforeCommit)
			if err != nil {
				return result, fmt.Errorf("backfill candidate: %w", err)
			}
			return result, nil
		},
		apply:   cfg.apply,
		stdout:  stdout,
		journal: nil,
	}
	// A nil *os.File must not become a non-nil interface.
	if journal != nil {
		run.journal = journal
	}
	totals, runErr := run.execute()
	return legacyAliasEndpointsReport(stdout, cfg, totals, runErr)
}

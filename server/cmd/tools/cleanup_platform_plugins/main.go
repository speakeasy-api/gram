// Command cleanup_platform_plugins previews or applies one authorized batch of
// existing automatic plugin memberships. It is run locally, not by the server.
//
// With GRAM_DATABASE_URL set to an authorized database connection:
//
//	mise exec -- go run ./server/cmd/tools/cleanup_platform_plugins -org <ORG_ID> -project <PROJECT_ID>
//
// Preview is read-only. Follow next_cursor with -after until absent; a page can
// contain no candidates and still have a next cursor. Ambiguous entries (including
// initiating-user Default attachments) are reported but never automatically removed.
// After reviewing exact candidate membership IDs, apply at most 100:
//
//	mise exec -- go run ./server/cmd/tools/cleanup_platform_plugins -org <ORG_ID> -project <PROJECT_ID> -apply -memberships <ID>,<ID> -actor <USER_ID>
//
// Apply rechecks current content/provenance and atomically requests publication.
// Check the reported publication outcome: enqueued is not proof clients refreshed;
// not_configured means no marketplace exists. Installed ZIPs need replacement.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
)

type options struct {
	org         string
	project     uuid.UUID
	after       uuid.UUID
	limit       int
	apply       bool
	memberships []uuid.UUID
	actor       string
}

type candidate struct {
	MembershipID uuid.UUID `json:"membership_id"`
	PluginID     uuid.UUID `json:"plugin_id"`
}

type result struct {
	Candidates       []candidate `json:"candidates,omitempty"`
	Ambiguous        []candidate `json:"ambiguous,omitempty"`
	NextCursor       *uuid.UUID  `json:"next_cursor,omitempty"`
	Scanned          int         `json:"scanned,omitempty"`
	RemovedPluginIDs []uuid.UUID `json:"removed_plugin_ids,omitempty"`
	Publication      string      `json:"publication,omitempty"`
}

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	dsn := os.Getenv("GRAM_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "GRAM_DATABASE_URL is required")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid database configuration")
		return 1
	}
	defer db.Close()
	report, err := execute(ctx, db, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var cfg options
	fs := flag.NewFlagSet("cleanup_platform_plugins", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&cfg.org, "org", "", "exact organization ID (required)")
	project := fs.String("project", "", "exact project UUID (required)")
	after := fs.String("after", "", "preview cursor from the previous page")
	fs.IntVar(&cfg.limit, "limit", roledelivery.PlatformCleanupPageSize, "maximum scanned memberships per preview (1-100)")
	fs.BoolVar(&cfg.apply, "apply", false, "apply explicitly selected membership IDs instead of previewing")
	ids := fs.String("memberships", "", "comma-separated approved membership UUIDs (apply only)")
	fs.StringVar(&cfg.actor, "actor", "", "initiating user ID (required for apply)")
	if err := fs.Parse(args); err != nil {
		return cfg, fmt.Errorf("parse cleanup options: %w", err)
	}
	if fs.NArg() != 0 {
		return cfg, fmt.Errorf("unexpected positional arguments")
	}
	var err error
	cfg.project, err = uuid.Parse(*project)
	if err != nil || cfg.project == uuid.Nil || strings.TrimSpace(cfg.org) == "" {
		return cfg, fmt.Errorf("-org and a nonzero -project UUID are required")
	}
	if cfg.limit < 1 || cfg.limit > roledelivery.PlatformCleanupPageSize {
		return cfg, fmt.Errorf("-limit must be between 1 and 100")
	}
	if *after != "" {
		cfg.after, err = uuid.Parse(*after)
		if err != nil {
			return cfg, fmt.Errorf("invalid preview cursor: %w", err)
		}
	}
	if !cfg.apply {
		if *ids != "" || cfg.actor != "" {
			return cfg, fmt.Errorf("-memberships and -actor require -apply")
		}
		return cfg, nil
	}
	if *after != "" || *ids == "" || strings.TrimSpace(cfg.actor) == "" {
		return cfg, fmt.Errorf("-apply requires -memberships and -actor, and cannot use -after")
	}
	parts := strings.Split(*ids, ",")
	if len(parts) > roledelivery.PlatformCleanupPageSize {
		return cfg, fmt.Errorf("apply accepts at most 100 membership IDs")
	}
	for _, raw := range parts {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || id == uuid.Nil {
			return cfg, fmt.Errorf("every membership ID must be a nonzero UUID")
		}
		cfg.memberships = append(cfg.memberships, id)
	}
	return cfg, nil
}

func execute(ctx context.Context, db *pgxpool.Pool, cfg options) (result, error) {
	mode := pgx.ReadOnly
	if cfg.apply {
		mode = pgx.ReadWrite
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: mode, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return result{}, fmt.Errorf("begin cleanup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var report result
	if cfg.apply {
		report.RemovedPluginIDs, err = roledelivery.ApplyPlatformCleanupAsUser(ctx, tx, cfg.org, cfg.project, cfg.memberships, cfg.actor)
		if err != nil {
			return result{}, fmt.Errorf("apply cleanup: %w", err)
		}
		report.Publication = "not_required"
		if len(report.RemovedPluginIDs) > 0 {
			outcome, err := (plugins.PublicationRequests{Enabled: true}).ProjectWithOutcome(ctx, tx, cfg.org, cfg.project, cfg.actor)
			if err != nil {
				return result{}, fmt.Errorf("request cleanup publication: %w", err)
			}
			report.Publication = string(outcome)
		}
	} else {
		limit := cfg.limit
		if limit < 1 || limit > roledelivery.PlatformCleanupPageSize {
			return result{}, fmt.Errorf("preview limit must be between 1 and 100")
		}
		rows, err := pluginsrepo.New(tx).ListPlatformCleanupMemberships(ctx, pluginsrepo.ListPlatformCleanupMembershipsParams{
			OrganizationID: cfg.org, ProjectID: uuid.NullUUID{UUID: cfg.project, Valid: true}, AfterID: cfg.after, MembershipIds: []uuid.UUID{}, PageSize: int32(limit), ToolsetID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
		if err != nil {
			return result{}, fmt.Errorf("preview memberships: %w", err)
		}
		report.Scanned = len(rows)
		for _, row := range rows {
			membership := row.PluginServer
			platform, err := roledelivery.ContainsPlatformTools(ctx, tx, cfg.org, cfg.project, membership.ToolsetID, membership.McpServerID)
			if err != nil {
				return result{}, fmt.Errorf("classify preview membership: %w", err)
			}
			if !platform {
				continue
			}
			item := candidate{MembershipID: membership.ID, PluginID: membership.PluginID}
			if row.AutomaticProvenance {
				report.Candidates = append(report.Candidates, item)
			} else {
				report.Ambiguous = append(report.Ambiguous, item)
			}
		}
		if len(rows) == cfg.limit {
			report.NextCursor = new(rows[len(rows)-1].PluginServer.ID)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result{}, fmt.Errorf("commit cleanup transaction: %w", err)
	}
	return report, nil
}

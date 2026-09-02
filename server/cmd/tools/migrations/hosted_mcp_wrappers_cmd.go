package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/hostedmcpbackfill"
)

type hostedMCPWrappersConfig struct {
	dbURL      string
	reportPath string
	options    hostedmcpbackfill.Options
}

// Stdout carries counts only, so the transcript never holds customer ids.
type hostedMCPWrappersSummary struct {
	Mode       string                             `json:"mode"`
	Phase      hostedmcpbackfill.Phase            `json:"phase"`
	Scanned    int                                `json:"scanned"`
	Outcomes   map[hostedmcpbackfill.Outcome]int  `json:"outcomes"`
	OauthProxy hostedmcpbackfill.OauthProxyCounts `json:"oauth_proxy_toolsets"`
	LastCursor uuid.UUID                          `json:"last_cursor"`
	ReportPath string                             `json:"report_path,omitempty"`
}

// parseHostedMCPWrappersFlags returns flag.ErrHelp, with usage written to
// helpOut, when -h is passed.
func parseHostedMCPWrappersFlags(args []string, getenv func(string) string, helpOut io.Writer) (hostedMCPWrappersConfig, error) {
	fs := flag.NewFlagSet("hosted-mcp-wrappers", flag.ContinueOnError)
	fs.SetOutput(helpOut)
	apply := fs.Bool("apply", false, "commit writes (default: dry run)")
	ackMirror := fs.Bool("acknowledge-mirror-deployed", false, "required with -apply; see guide")
	moveDependents := fs.Bool("move-dependents", false, "dependent-move phase; see guide")
	retireGrants := fs.Bool("retire-toolset-grants", false, "grant-retirement phase; see guide")
	project := fs.String("project", "", "project_id (uuid) to scope the run")
	cursor := fs.String("cursor", "", "resume after this toolset id")
	limit := fs.Int("limit", 0, "max toolsets to process; 0 means all")
	aliases := fs.String("aliases", "", "comma-separated slug@custom_domain_id platform-alias allowlist")
	reportPath := fs.String("report", "", "per-row JSON report path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return hostedMCPWrappersConfig{}, fmt.Errorf("parse flags: %w", err)
		}
		return hostedMCPWrappersConfig{}, errors.New("invalid hosted-mcp-wrappers flags")
	}
	if fs.NArg() != 0 {
		return hostedMCPWrappersConfig{}, errors.New("unexpected positional arguments")
	}
	if *limit < 0 {
		return hostedMCPWrappersConfig{}, errors.New("limit must be nonnegative")
	}
	if *moveDependents && *retireGrants {
		return hostedMCPWrappersConfig{}, errors.New("choose one of -move-dependents and -retire-toolset-grants")
	}
	if *apply && !*ackMirror {
		return hostedMCPWrappersConfig{}, errors.New("-apply requires -acknowledge-mirror-deployed")
	}

	cfg := hostedMCPWrappersConfig{
		dbURL:      getenv("GRAM_DATABASE_URL"),
		reportPath: *reportPath,
		options: hostedmcpbackfill.Options{
			ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Cursor:    uuid.Nil,
			Limit:     *limit,
			PageSize:  0,
			Aliases:   nil,
			Apply:     *apply,
			Phase:     hostedmcpbackfill.PhaseWrappers,
		},
	}
	switch {
	case *moveDependents:
		cfg.options.Phase = hostedmcpbackfill.PhaseDependents
	case *retireGrants:
		cfg.options.Phase = hostedmcpbackfill.PhaseRetireGrants
	}
	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	if *project != "" {
		id, err := uuid.Parse(*project)
		if err != nil {
			return cfg, fmt.Errorf("invalid -project: %w", err)
		}
		cfg.options.ProjectID = uuid.NullUUID{UUID: id, Valid: true}
	}
	if *cursor != "" {
		id, err := uuid.Parse(*cursor)
		if err != nil {
			return cfg, fmt.Errorf("invalid -cursor: %w", err)
		}
		cfg.options.Cursor = id
	}
	for entry := range strings.SplitSeq(*aliases, ",") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		slug, domain, ok := strings.Cut(entry, "@")
		id, err := uuid.Parse(domain)
		if !ok || slug == "" || err != nil {
			return cfg, fmt.Errorf("invalid -aliases entry %q: want slug@custom_domain_id", entry)
		}
		cfg.options.Aliases = append(cfg.options.Aliases, hostedmcpbackfill.AliasKey{Slug: slug, CustomDomainID: id})
	}
	return cfg, nil
}

func runHostedMCPWrappers(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseHostedMCPWrappersFlags(args, getenv, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		log.Printf("invalid hosted-mcp-wrappers configuration: %v", err)
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres for hosted-mcp-wrappers failed")
		return 1
	}
	defer pool.Close()

	report, runErr := hostedmcpbackfill.NewRunner(pool, cfg.options).Run(ctx)
	summary := hostedMCPWrappersSummary{
		Mode: report.Mode, Phase: report.Phase, Scanned: report.Scanned, Outcomes: report.Outcomes,
		OauthProxy: report.OauthProxy, LastCursor: report.LastCursor, ReportPath: cfg.reportPath,
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		log.Printf("write hosted-mcp-wrappers summary: %v", err)
		return 1
	}
	code := 0
	if runErr != nil {
		code = 1
		if cfg.options.Apply {
			log.Printf("hosted-mcp-wrappers stopped early: %v; resume with -cursor %s", runErr, report.LastCursor)
		} else {
			log.Printf("hosted-mcp-wrappers stopped early: %v; dry-run cursors commit nothing, so rerun without -cursor", runErr)
		}
	}
	if cfg.reportPath != "" {
		if err := writeHostedMCPWrappersReport(cfg.reportPath, report); err != nil {
			log.Printf("write hosted-mcp-wrappers report: %v", err)
			code = 1
		}
	}
	return code
}

func writeHostedMCPWrappersReport(path string, report hostedmcpbackfill.Report) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- operator-supplied report path
	if err != nil {
		return fmt.Errorf("open report: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("chmod report: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		_ = f.Close()
		return fmt.Errorf("encode report: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	return nil
}

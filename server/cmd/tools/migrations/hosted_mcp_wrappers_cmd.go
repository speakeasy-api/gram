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
	"strconv"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/hostedmcpbackfill"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
)

type hostedMCPWrappersConfig struct {
	dbURL       string
	reportPath  string
	journalPath string
	options     hostedmcpbackfill.Options

	// publicationEmitSet records -plugin-publication-emit or its environment default being given, so a resume repeats it.
	publicationEmitSet bool
}

// Stdout carries counts and the resume cursor only, never slugs or names.
type hostedMCPWrappersSummary struct {
	Mode                  string                            `json:"mode"`
	Phase                 string                            `json:"phase"`
	Scanned               int                               `json:"scanned"`
	Toolsets              int                               `json:"toolsets"`
	Writes                int                               `json:"writes"`
	Outcomes              map[hostedmcpbackfill.Outcome]int `json:"outcomes"`
	FreshIDServersPresent int                               `json:"fresh_id_servers_present"`
	LastCursor            uuid.UUID                         `json:"last_cursor"`
	ReportPath            string                            `json:"report_path,omitempty"`
	JournalPath           string                            `json:"journal_path,omitempty"`
}

// parseHostedMCPWrappersFlags returns flag.ErrHelp, with usage written to helpOut, when -h is passed.
func parseHostedMCPWrappersFlags(args []string, getenv func(string) string, helpOut io.Writer) (hostedMCPWrappersConfig, error) {
	fs := flag.NewFlagSet("hosted-mcp-wrappers", flag.ContinueOnError)
	fs.SetOutput(helpOut)
	apply := fs.Bool("apply", false, "commit writes (default: dry run)")
	project := fs.String("project", "", "project_id (uuid) to scope the run")
	cursor := fs.String("cursor", "", "resume after this toolset id")
	limit := fs.Int("limit", 0, "max toolsets to process; 0 means all")
	reportPath := fs.String("report", "", "per-row JSON report path; -apply refuses an existing file, and -move-dependents, -cleanup-orphans, -rekey-removal-records, -delete-leftover-removal-records or -delete-orphan-wrapper-servers -apply also writes <report>.journal.jsonl")
	moveDependents := fs.Bool("move-dependents", false, "re-key toolset-keyed mcp_metadata, plugin_servers and assistant_toolsets onto the canonical wrapper")
	cleanupOrphans := fs.Bool("cleanup-orphans", false, "remove toolset-keyed mcp_metadata, plugin_servers and assistant_toolsets whose toolset or project is deleted")
	rekeyRemovals := fs.Bool("rekey-removal-records", false, "re-key soft-deleted toolset-keyed plugin_servers rows (plugin removal records) onto the canonical server key")
	deleteLeftovers := fs.Bool("delete-leftover-removal-records", false, "hard-delete soft-deleted toolset-keyed plugin_servers rows whose toolset or project is deleted, or whose plugin already has a server-keyed removal record for the canonical server")
	deleteWrappers := fs.Bool("delete-orphan-wrapper-servers", false, "soft-delete live non-canonical mcp_servers rows of deleted toolsets in live projects through the app's server delete; run before -delete-leftover-removal-records")
	emitEnv := getenv("GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED")
	defaultEmit, err := strconv.ParseBool(emitEnv)
	if err != nil && emitEnv != "" {
		return hostedMCPWrappersConfig{}, fmt.Errorf("invalid $GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED: %w", err)
	}
	publicationEmit := fs.Bool("plugin-publication-emit", defaultEmit, "with -cleanup-orphans or -delete-orphan-wrapper-servers, enqueue a plugin publication request per removed member or deleted server of a live project, as the app does (default $GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED)")
	readersCutOver := fs.Bool("readers-cut-over", false, "confirm AIM-16 phase A (dual-key readers) and phase B (writers switched to the server key) are both fully deployed; required for -move-dependents -apply")
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
	phases := 0
	for _, on := range []bool{*moveDependents, *cleanupOrphans, *rekeyRemovals, *deleteLeftovers, *deleteWrappers} {
		if on {
			phases++
		}
	}
	if phases > 1 {
		return hostedMCPWrappersConfig{}, errors.New("-move-dependents, -cleanup-orphans, -rekey-removal-records, -delete-leftover-removal-records and -delete-orphan-wrapper-servers are separate phases")
	}
	// Readers on main before AIM-16 only find toolset-keyed rows, so a moved row would vanish from them.
	if *moveDependents && *apply && !*readersCutOver {
		return hostedMCPWrappersConfig{}, errors.New("-move-dependents -apply requires -readers-cut-over: deploy AIM-16 phases A and B first")
	}
	// The report is the only exact list of moved rows, which a rollback needs.
	if *moveDependents && *apply && *reportPath == "" {
		return hostedMCPWrappersConfig{}, errors.New("-move-dependents -apply requires -report")
	}
	if *cleanupOrphans && *apply && *reportPath == "" {
		return hostedMCPWrappersConfig{}, errors.New("-cleanup-orphans -apply requires -report")
	}
	if *rekeyRemovals && *apply && *reportPath == "" {
		return hostedMCPWrappersConfig{}, errors.New("-rekey-removal-records -apply requires -report")
	}
	if *deleteLeftovers && *apply && *reportPath == "" {
		return hostedMCPWrappersConfig{}, errors.New("-delete-leftover-removal-records -apply requires -report")
	}
	if *deleteWrappers && *apply && *reportPath == "" {
		return hostedMCPWrappersConfig{}, errors.New("-delete-orphan-wrapper-servers -apply requires -report")
	}
	if *readersCutOver && !*moveDependents {
		return hostedMCPWrappersConfig{}, errors.New("-readers-cut-over only applies to -move-dependents")
	}

	emitFlagSet := false
	fs.Visit(func(f *flag.Flag) { emitFlagSet = emitFlagSet || f.Name == "plugin-publication-emit" })
	cfg := hostedMCPWrappersConfig{
		dbURL:              getenv("GRAM_DATABASE_URL"),
		reportPath:         *reportPath,
		journalPath:        "",
		publicationEmitSet: emitFlagSet || emitEnv != "",
		options: hostedmcpbackfill.Options{
			ProjectID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Cursor:                 uuid.Nil,
			Limit:                  *limit,
			PageSize:               0,
			Apply:                  *apply,
			MoveDependents:         *moveDependents,
			CleanupOrphans:         *cleanupOrphans,
			RekeyRemovals:          *rekeyRemovals,
			DeleteLeftoverRemovals: *deleteLeftovers,
			DeleteOrphanWrappers:   *deleteWrappers,
			Publication:            plugins.PublicationRequests{Enabled: *publicationEmit},
			Journal:                nil,
			LockTimeout:            0,
		},
	}
	if phases > 0 && *apply {
		cfg.journalPath = *reportPath + ".journal.jsonl"
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
	// Open the report and journal before any apply, so a bad path cannot strand committed rows without them.
	// An apply never reuses a path, so each batch keeps its own record; a dry run truncates only when writing.
	var reportFile *os.File
	if cfg.reportPath != "" {
		reportFile, err = openHostedMCPWrappersOutput(cfg.reportPath, cfg.options.Apply)
		if err != nil {
			log.Printf("open hosted-mcp-wrappers report: %v", err)
			return 2
		}
		defer o11y.NoLogDefer(reportFile.Close)
	}
	if cfg.journalPath != "" {
		journalFile, err := openHostedMCPWrappersOutput(cfg.journalPath, true)
		if err != nil {
			log.Printf("open hosted-mcp-wrappers journal: %v", err)
			if err := os.Remove(cfg.reportPath); err != nil {
				log.Printf("remove hosted-mcp-wrappers report: %v", err)
			}
			return 2
		}
		defer o11y.NoLogDefer(journalFile.Close)
		cfg.options.Journal = hostedmcpbackfill.NewFileJournal(journalFile)
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
		Mode: report.Mode, Phase: report.Phase, Scanned: report.Scanned, Toolsets: report.Toolsets, Writes: report.Writes, Outcomes: report.Outcomes,
		FreshIDServersPresent: report.FreshIDServersPresent, LastCursor: report.LastCursor, ReportPath: cfg.reportPath, JournalPath: cfg.journalPath,
	}
	code := 0
	// The report is the record of committed rows, so write it before anything that can fail.
	if cfg.reportPath != "" {
		if err := writeHostedMCPWrappersReport(reportFile, report); err != nil {
			log.Printf("write hosted-mcp-wrappers report: %v", err)
			code = 1
		}
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		log.Printf("write hosted-mcp-wrappers summary: %v", err)
		code = 1
	}
	if runErr != nil {
		code = 1
		if cfg.options.Apply {
			log.Printf("hosted-mcp-wrappers %s phase stopped early: %v; resume with the same mode flags (%s), -cursor %s and a new -report path", report.Phase, runErr, hostedMCPWrappersResumeFlags(cfg), report.LastCursor)
		} else {
			log.Printf("hosted-mcp-wrappers stopped early: %v; dry-run cursors commit nothing, so rerun without -cursor", runErr)
		}
	}
	return code
}

// hostedMCPWrappersResumeFlags adds the run's scope and publication choice to its mode flags.
func hostedMCPWrappersResumeFlags(cfg hostedMCPWrappersConfig) string {
	flags := hostedMCPWrappersModeFlags(cfg.options)
	if cfg.options.ProjectID.Valid {
		flags += " -project " + cfg.options.ProjectID.UUID.String()
	}
	if cfg.publicationEmitSet {
		flags += " -plugin-publication-emit=" + strconv.FormatBool(cfg.options.Publication.Enabled)
	}
	return flags
}

// hostedMCPWrappersModeFlags names the flags that select the run's phase, so a resume stays in it.
func hostedMCPWrappersModeFlags(options hostedmcpbackfill.Options) string {
	switch {
	case options.MoveDependents:
		return "-apply -move-dependents -readers-cut-over"
	case options.CleanupOrphans:
		return "-apply -cleanup-orphans"
	case options.RekeyRemovals:
		return "-apply -rekey-removal-records"
	case options.DeleteLeftoverRemovals:
		return "-apply -delete-leftover-removal-records"
	case options.DeleteOrphanWrappers:
		return "-apply -delete-orphan-wrapper-servers"
	default:
		return "-apply"
	}
}

func openHostedMCPWrappersOutput(path string, exclusive bool) (*os.File, error) {
	flags := os.O_CREATE | os.O_WRONLY
	if exclusive {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o600) // #nosec G304 -- operator-supplied report path
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}

func writeHostedMCPWrappersReport(f *os.File, report hostedmcpbackfill.Report) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate report: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind report: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("flush report: %w", err)
	}
	return nil
}

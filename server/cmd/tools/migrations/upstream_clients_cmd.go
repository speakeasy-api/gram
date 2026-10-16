package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/upstreamclientsbackfill"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// upstreamClientsConfirm is the -confirm-apply value writes require.
const upstreamClientsConfirm = "mcp-server-upstream-clients"

type upstreamClientsConfig struct {
	dbURL      string
	reportPath string
	options    upstreamclientsbackfill.Options
}

// upstreamClientsSummary is what stdout carries: counts only. The per-server
// report holds ids and goes to the -report file.
type upstreamClientsSummary struct {
	Mode       string                                  `json:"mode"`
	Servers    int                                     `json:"servers"`
	Outcomes   map[upstreamclientsbackfill.Outcome]int `json:"outcomes"`
	Decided    int                                     `json:"decided"`
	Written    int                                     `json:"written"`
	Skipped    int                                     `json:"skipped"`
	Gaps       int                                     `json:"gaps"`
	ReportPath string                                  `json:"report_path,omitempty"`
}

func parseUpstreamClientsFlags(args []string, getenv func(string) string) (upstreamClientsConfig, error) {
	fs := flag.NewFlagSet(upstreamClientsConfirm, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	confirmation := registerWriteConfirmation(fs, upstreamClientsConfirm)
	organization := fs.String("organization", "", "organization id to scope the run; empty means every organization")
	reportPath := fs.String("report", "", "per-server JSON report path; -apply refuses an existing file")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return upstreamClientsConfig{}, errors.New("invalid " + upstreamClientsConfirm + " flags")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	apply, err := confirmation.check(dbURL)
	if err != nil {
		return upstreamClientsConfig{}, err
	}
	return upstreamClientsConfig{
		dbURL:      dbURL,
		reportPath: *reportPath,
		options: upstreamclientsbackfill.Options{
			OrganizationID: *organization,
			Apply:          apply,
			PageSize:       0,
		},
	}, nil
}

func runUpstreamClients(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseUpstreamClientsFlags(args, getenv)
	if err != nil {
		log.Printf("invalid %s configuration: %v", upstreamClientsConfirm, err)
		return 2
	}
	// Opened before any write so a bad path cannot strand committed rows
	// without their record.
	var reportFile *os.File
	if cfg.reportPath != "" {
		reportFile, err = openReportOutput(cfg.reportPath, cfg.options.Apply)
		if err != nil {
			log.Printf("open %s report: %v", upstreamClientsConfirm, err)
			return 2
		}
		defer o11y.NoLogDefer(reportFile.Close)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres for %s failed", upstreamClientsConfirm)
		return 1
	}
	defer pool.Close()

	report, runErr := upstreamclientsbackfill.Run(ctx, pool, cfg.options)
	code := 0
	if reportFile != nil {
		if err := writeJSONReport(reportFile, report); err != nil {
			log.Printf("write %s report: %v", upstreamClientsConfirm, err)
			code = 1
		}
	}
	summary := upstreamClientsSummary{
		Mode:       report.Mode,
		Servers:    report.Servers,
		Outcomes:   report.Outcomes,
		Decided:    report.Decided,
		Written:    report.Written,
		Skipped:    report.Skipped,
		Gaps:       report.Gaps,
		ReportPath: cfg.reportPath,
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		log.Printf("write %s summary: %v", upstreamClientsConfirm, err)
		code = 1
	}
	if runErr != nil {
		log.Printf("%s stopped early: %v; rerun the same command, the run is idempotent", upstreamClientsConfirm, runErr)
		return 1
	}
	return code
}

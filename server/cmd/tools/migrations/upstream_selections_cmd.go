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

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/upstreamselectionsbackfill"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// upstreamSelectionsConfirm is the subcommand name and the -confirm-apply
// value writes require.
const upstreamSelectionsConfirm = "credential-upstream-selections"

type upstreamSelectionsConfig struct {
	dbURL      string
	reportPath string
	options    upstreamselectionsbackfill.Options
}

// upstreamSelectionsSummary is what stdout carries: counts only. The gap list
// holds ids and goes to the -report file.
type upstreamSelectionsSummary struct {
	Mode           string                                     `json:"mode"`
	CredentialKind string                                     `json:"credential_kind"`
	Credentials    int                                        `json:"credentials"`
	FullyCovered   int                                        `json:"fully_covered"`
	Pairs          map[upstreamselectionsbackfill.Outcome]int `json:"pairs"`
	Written        int64                                      `json:"written"`
	Gaps           int                                        `json:"gaps"`
	ReportPath     string                                     `json:"report_path,omitempty"`
}

func parseUpstreamSelectionsFlags(args []string, getenv func(string) string) (upstreamSelectionsConfig, error) {
	fs := flag.NewFlagSet(upstreamSelectionsConfirm, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	confirmation := registerWriteConfirmation(fs, upstreamSelectionsConfirm)
	organization := fs.String("organization", "", "organization id to scope the run; empty means every organization")
	reportPath := fs.String("report", "", "JSON gap report path; -apply refuses an existing file")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return upstreamSelectionsConfig{}, errors.New("invalid " + upstreamSelectionsConfirm + " flags")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	apply, err := confirmation.check(dbURL)
	if err != nil {
		return upstreamSelectionsConfig{}, err
	}
	return upstreamSelectionsConfig{
		dbURL:      dbURL,
		reportPath: *reportPath,
		options: upstreamselectionsbackfill.Options{
			OrganizationID: *organization,
			Apply:          apply,
			PageSize:       0,
		},
	}, nil
}

func runUpstreamSelections(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseUpstreamSelectionsFlags(args, getenv)
	if err != nil {
		log.Printf("invalid %s configuration: %v", upstreamSelectionsConfirm, err)
		return 2
	}
	var reportFile *os.File
	if cfg.reportPath != "" {
		reportFile, err = openReportOutput(cfg.reportPath, cfg.options.Apply)
		if err != nil {
			log.Printf("open %s report: %v", upstreamSelectionsConfirm, err)
			return 2
		}
		defer o11y.NoLogDefer(reportFile.Close)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres for %s failed", upstreamSelectionsConfirm)
		return 1
	}
	defer pool.Close()

	report, runErr := upstreamselectionsbackfill.Run(ctx, pool, cfg.options)
	code := 0
	if reportFile != nil {
		if err := writeJSONReport(reportFile, report); err != nil {
			log.Printf("write %s report: %v", upstreamSelectionsConfirm, err)
			code = 1
		}
	}
	summary := upstreamSelectionsSummary{
		Mode:           report.Mode,
		CredentialKind: report.CredentialKind,
		Credentials:    report.Credentials,
		FullyCovered:   report.FullyCovered,
		Pairs:          report.Pairs,
		Written:        report.Written,
		Gaps:           len(report.Gaps),
		ReportPath:     cfg.reportPath,
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		log.Printf("write %s summary: %v", upstreamSelectionsConfirm, err)
		code = 1
	}
	if runErr != nil {
		log.Printf("%s stopped early: %v; rerun the same command, the run is idempotent", upstreamSelectionsConfirm, runErr)
		return 1
	}
	return code
}

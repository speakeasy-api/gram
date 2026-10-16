package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/jackc/pgx/v5"
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
	apply := fs.Bool("apply", false, "write the decided associations (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	organization := fs.String("organization", "", "organization id to scope the run; empty means every organization")
	reportPath := fs.String("report", "", "per-server JSON report path; -apply refuses an existing file")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+upstreamClientsConfirm+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return upstreamClientsConfig{}, errors.New("invalid " + upstreamClientsConfirm + " flags")
	}
	if *environment == "" {
		return upstreamClientsConfig{}, errors.New("environment is required")
	}
	if *apply && *confirmEnvironment != *environment {
		return upstreamClientsConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return upstreamClientsConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return upstreamClientsConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return upstreamClientsConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != upstreamClientsConfirm {
		return upstreamClientsConfig{}, errors.New("writes require -confirm-apply=" + upstreamClientsConfirm)
	}
	return upstreamClientsConfig{
		dbURL:      dbURL,
		reportPath: *reportPath,
		options: upstreamclientsbackfill.Options{
			OrganizationID: *organization,
			Apply:          *apply,
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

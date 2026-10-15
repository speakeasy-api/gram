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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

type serverRemoteIssuersConfig struct {
	dbURL          string
	environment    string
	organizationID string
	apply          bool
}

// serverRemoteIssuersReport counts only. The rows it describes name customer
// projects and upstreams, so stdout carries no identifiers.
type serverRemoteIssuersReport struct {
	Servers int
	Stamped int
	Cleared int
	Kept    int
	// Skipped counts writes that lost a race with an identity configuration
	// saved after this run read the server. Rerun to pick up what remains.
	Skipped int
}

func parseServerRemoteIssuersFlags(args []string, getenv func(string) string) (serverRemoteIssuersConfig, error) {
	fs := flag.NewFlagSet("server-remote-issuers", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	organization := fs.String("organization", "", "organization id to scope the run; empty means every organization")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal server-remote-issuers for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return serverRemoteIssuersConfig{}, errors.New("invalid server-remote-issuers flags")
	}
	if *environment == "" {
		return serverRemoteIssuersConfig{}, errors.New("environment is required")
	}
	if *apply && *confirmEnvironment != *environment {
		return serverRemoteIssuersConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return serverRemoteIssuersConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return serverRemoteIssuersConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return serverRemoteIssuersConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != "server-remote-issuers" {
		return serverRemoteIssuersConfig{}, errors.New("writes require -confirm-apply=server-remote-issuers")
	}
	return serverRemoteIssuersConfig{dbURL: dbURL, environment: *environment, organizationID: *organization, apply: *apply}, nil
}

// serverRemoteIssuerTarget is what a server's upstream authorization server
// should be after the backfill, and whether that differs from what is stored.
type serverRemoteIssuerTarget struct {
	provider uuid.NullUUID
	write    bool
}

// decideServerRemoteIssuer picks a server's provider from its own upstream.
//
// A metadata match wins: the server's URL named a protected resource whose
// RFC 9728 authorization_servers list exactly one of the providers its user
// session issuer binds a client for, which is the server's own upstream
// whatever the other servers on that issuer use.
//
// Without a match, a lone server on its user session issuer keeps the stored
// value. The superseded per-issuer derivation wrote the issuer's single
// provider onto every server on it, so with one server it could not have
// borrowed anything, and this is the only evidence available for a hosted
// server, which has no upstream URL or protected resource metadata at all.
//
// Anything else is cleared. The stored value was derived from an issuer shared
// by several servers, so it may be a sibling's provider; NULL is also the
// right answer for a server that works anonymously while still advertising
// OAuth metadata.
func decideServerRemoteIssuer(row mcpserversrepo.ListMCPServerRemoteSessionIssuerBackfillRow) serverRemoteIssuerTarget {
	switch {
	case row.MatchedRemoteSessionIssuerID.Valid:
		return serverRemoteIssuerTarget{
			provider: row.MatchedRemoteSessionIssuerID,
			write:    row.CurrentRemoteSessionIssuerID != row.MatchedRemoteSessionIssuerID,
		}
	case row.IssuerServerCount <= 1:
		return serverRemoteIssuerTarget{provider: row.CurrentRemoteSessionIssuerID, write: false}
	default:
		return serverRemoteIssuerTarget{
			provider: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			write:    row.CurrentRemoteSessionIssuerID.Valid,
		}
	}
}

type serverRemoteIssuersDB interface {
	mcpserversrepo.DBTX
}

func backfillServerRemoteIssuers(ctx context.Context, db serverRemoteIssuersDB, organizationID string, apply bool) (serverRemoteIssuersReport, error) {
	q := mcpserversrepo.New(db)
	rows, err := q.ListMCPServerRemoteSessionIssuerBackfill(ctx, organizationID)
	if err != nil {
		return serverRemoteIssuersReport{}, fmt.Errorf("list MCP server remote session issuer backfill: %w", err)
	}

	report := serverRemoteIssuersReport{Servers: len(rows), Stamped: 0, Cleared: 0, Kept: 0, Skipped: 0}
	for _, row := range rows {
		target := decideServerRemoteIssuer(row)
		if !target.write {
			report.Kept++
			continue
		}
		if !apply {
			if target.provider.Valid {
				report.Stamped++
			} else {
				report.Cleared++
			}
			continue
		}
		// Conditional on the provider this run read: an administrator
		// configuring User Identity on the server in the meantime is the
		// authority on it, and this write loses that race rather than
		// reverting it.
		written, err := q.RepairMCPServerRemoteSessionIssuerIfUnchanged(ctx, mcpserversrepo.RepairMCPServerRemoteSessionIssuerIfUnchangedParams{
			RemoteSessionIssuerID:         target.provider,
			ID:                            row.ID,
			ProjectID:                     row.ProjectID,
			UserSessionIssuerID:           row.UserSessionIssuerID,
			ObservedRemoteSessionIssuerID: row.CurrentRemoteSessionIssuerID,
		})
		if err != nil {
			return report, fmt.Errorf("repair MCP server remote session issuer: %w", err)
		}
		switch {
		case written == 0:
			report.Skipped++
		case target.provider.Valid:
			report.Stamped++
		default:
			report.Cleared++
		}
	}
	return report, nil
}

func runServerRemoteIssuers(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseServerRemoteIssuersFlags(args, getenv)
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
	report, err := backfillServerRemoteIssuers(ctx, pool, cfg.organizationID, cfg.apply)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s servers=%d stamped=%d cleared=%d kept=%d skipped=%d\n", mode, cfg.environment, report.Servers, report.Stamped, report.Cleared, report.Kept, report.Skipped)
	return 0
}

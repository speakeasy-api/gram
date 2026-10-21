package main

import (
	"context"
	"fmt"
	"io"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const riskFailClosedBackfillName = "risk-fail-closed"

type riskFailClosedDB interface {
	riskrepo.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type riskFailClosedCandidate struct {
	OrganizationID    string
	HasFailOpenPolicy bool
}

type riskFailClosedReport struct {
	// Organizations have at least one live prompt policy that stored
	// fail_open = false.
	Organizations int
	// MixedOrganizations also have prompt policies that failed open; once the
	// organization fails closed, those policies fail closed too.
	MixedOrganizations int
	// FeaturesAdded counts organizations whose failure mode this run set (or,
	// in a dry run, would set) to fail closed. An organization that already
	// chose a failure mode keeps it.
	FeaturesAdded int
}

func runRiskFailClosed(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseConfirmedBackfillFlags(riskFailClosedBackfillName, args, getenv)
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
	report, err := backfillRiskFailClosed(ctx, pool, cfg.apply)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s organizations=%d mixed_organizations=%d features_added=%d\n", mode, cfg.environment, report.Organizations, report.MixedOrganizations, report.FeaturesAdded)
	return 0
}

func backfillRiskFailClosed(ctx context.Context, db riskFailClosedDB, apply bool) (riskFailClosedReport, error) {
	rows, err := riskrepo.New(db).ListOrganizationsWithFailClosedPromptPolicies(ctx)
	if err != nil {
		return riskFailClosedReport{}, fmt.Errorf("list organizations with fail-closed prompt policies: %w", err)
	}
	candidates := make([]riskFailClosedCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, riskFailClosedCandidate{OrganizationID: row.OrganizationID, HasFailOpenPolicy: row.HasFailOpenPolicy})
	}

	return backfillRiskFailClosedOrganizations(candidates, apply, func(organizationID string, apply bool) (bool, error) {
		tx, err := db.Begin(ctx)
		if err != nil {
			return false, fmt.Errorf("begin organization transaction: %w", err)
		}
		return seedRiskFailClosed(ctx, tx, organizationID, apply)
	})
}

type riskFailClosedTx interface {
	featurerepo.DBTX
	Commit(context.Context) error
	Rollback(context.Context) error
}

// seedRiskFailClosed turns on the organization's fail-closed failure mode
// unless the organization already has a row for it. A soft-deleted row is an
// administrator's choice to fail open and is never restored.
func seedRiskFailClosed(ctx context.Context, tx riskFailClosedTx, organizationID string, apply bool) (bool, error) {
	added, err := featurerepo.New(tx).EnableFeatureIfNeverConfigured(ctx, featurerepo.EnableFeatureIfNeverConfiguredParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureRiskEnforcementFailClosed),
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		return false, fmt.Errorf("enable %s: %w", productfeatures.FeatureRiskEnforcementFailClosed, err)
	}
	if !apply {
		if err := tx.Rollback(ctx); err != nil {
			return false, fmt.Errorf("roll back dry-run organization failure mode: %w", err)
		}
		return added > 0, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit organization failure mode: %w", err)
	}
	return added > 0, nil
}

func backfillRiskFailClosedOrganizations(candidates []riskFailClosedCandidate, apply bool, seed func(string, bool) (bool, error)) (riskFailClosedReport, error) {
	report := riskFailClosedReport{Organizations: len(candidates), MixedOrganizations: 0, FeaturesAdded: 0}
	for _, candidate := range candidates {
		if candidate.HasFailOpenPolicy {
			report.MixedOrganizations++
		}
		added, err := seed(candidate.OrganizationID, apply)
		if err != nil {
			return report, err
		}
		if added {
			report.FeaturesAdded++
		}
	}
	return report, nil
}

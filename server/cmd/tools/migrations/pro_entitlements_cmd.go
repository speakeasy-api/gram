package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

type proEntitlementsConfig struct {
	dbURL       string
	environment string
	apply       bool
}

func parseProEntitlementsFlags(args []string, getenv func(string) string) (proEntitlementsConfig, error) {
	fs := flag.NewFlagSet("pro-entitlements", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	confirmation := registerWriteConfirmation(fs, "pro-entitlements")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return proEntitlementsConfig{}, errors.New("invalid pro-entitlements flags")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	apply, err := confirmation.check(dbURL)
	if err != nil {
		return proEntitlementsConfig{}, err
	}
	return proEntitlementsConfig{dbURL: dbURL, environment: *confirmation.environment, apply: apply}, nil
}

type proEntitlementsDB interface {
	featurerepo.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type proEntitlementsReport struct {
	Organizations int
	FeaturesAdded int
}

type proOrganizationTx interface {
	LockAndCheckPro(context.Context, string) (bool, error)
	SeedEntitlements(context.Context, string) ([]productfeatures.Feature, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type pgxProOrganizationTx struct{ pgx.Tx }

func (t pgxProOrganizationTx) LockAndCheckPro(ctx context.Context, organizationID string) (bool, error) {
	isPro, err := featurerepo.New(t.Tx).LockAndCheckProOrganization(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("lock and check pro organization: %w", err)
	}
	return isPro, nil
}

func (t pgxProOrganizationTx) SeedEntitlements(ctx context.Context, organizationID string) ([]productfeatures.Feature, error) {
	features, err := productfeatures.SeedEnterpriseAccessEntitlementsTx(ctx, t.Tx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("seed enterprise access entitlements: %w", err)
	}
	return features, nil
}

func backfillProEntitlements(ctx context.Context, db proEntitlementsDB, apply bool) (proEntitlementsReport, error) {
	organizationIDs, err := featurerepo.New(db).ListProOrganizations(ctx)
	if err != nil {
		return proEntitlementsReport{}, fmt.Errorf("list pro organizations: %w", err)
	}

	return backfillProOrganizations(organizationIDs, apply, func(organizationID string, apply bool) (int, error) {
		tx, err := db.Begin(ctx)
		if err != nil {
			return 0, fmt.Errorf("begin organization transaction: %w", err)
		}
		return migrateProOrganization(ctx, pgxProOrganizationTx{Tx: tx}, organizationID, apply)
	})
}

func migrateProOrganization(ctx context.Context, tx proOrganizationTx, organizationID string, apply bool) (int, error) {
	rollback := func() { _ = tx.Rollback(ctx) }
	isPro, err := tx.LockAndCheckPro(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !isPro) {
		rollback()
		return 0, nil
	}
	if err != nil {
		rollback()
		return 0, fmt.Errorf("lock and recheck pro organization: %w", err)
	}
	enabled, err := tx.SeedEntitlements(ctx, organizationID)
	if err != nil {
		rollback()
		return 0, fmt.Errorf("seed pro organization entitlements: %w", err)
	}
	if !apply {
		if err := tx.Rollback(ctx); err != nil {
			return 0, fmt.Errorf("roll back dry-run organization entitlements: %w", err)
		}
		return len(enabled), nil
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit organization entitlements: %w", err)
	}
	return len(enabled), nil
}

func backfillProOrganizations(organizationIDs []string, apply bool, seed func(string, bool) (int, error)) (proEntitlementsReport, error) {
	report := proEntitlementsReport{Organizations: len(organizationIDs), FeaturesAdded: 0}
	for _, organizationID := range organizationIDs {
		added, err := seed(organizationID, apply)
		if err != nil {
			return report, err
		}
		report.FeaturesAdded += added
	}
	return report, nil
}

func runProEntitlements(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseProEntitlementsFlags(args, getenv)
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
	report, err := backfillProEntitlements(ctx, pool, cfg.apply)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s organizations=%d features_added=%d\n", mode, cfg.environment, report.Organizations, report.FeaturesAdded)
	return 0
}

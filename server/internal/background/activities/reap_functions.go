package activities

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/functions"
	funcrepo "github.com/speakeasy-api/gram/server/internal/functions/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type ReapFlyAppsRequest struct {
	Scope FunctionsReaperScope

	ProjectID uuid.NullUUID
}

type ReapFlyAppsResult struct {
	Reaped int
	Errors int
}

type ReapFlyApps struct {
	logger    *slog.Logger
	metrics   *metrics
	db        *pgxpool.Pool
	deployer  functions.Deployer
	keepCount int64
}

func NewReapFlyApps(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	deployer functions.Deployer,
	keepCount int64,
) *ReapFlyApps {
	return &ReapFlyApps{
		logger:    logger.With(attr.SlogComponent("flyio-reaper")),
		metrics:   newMetrics(newMeter(meterProvider), logger),
		db:        db,
		deployer:  deployer,
		keepCount: keepCount,
	}
}

func (r *ReapFlyApps) Do(ctx context.Context, req ReapFlyAppsRequest) (*ReapFlyAppsResult, error) {
	logger := r.logger

	switch {
	case req.Scope == FunctionsReaperScopeProject && req.ProjectID.UUID == uuid.Nil:
		return nil, temporal.NewApplicationErrorWithOptions("project ID must be set for project-scoped reaper", "reaper_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        nil,
		})
	case req.Scope == FunctionsReaperScopeGlobal && req.ProjectID.UUID != uuid.Nil:
		return nil, temporal.NewApplicationErrorWithOptions("project ID must not be set for global reaper", "reaper_error", temporal.ApplicationErrorOptions{
			NonRetryable: true,
			Cause:        nil,
		})
	}

	repo := funcrepo.New(r.db)

	// Get all apps that should be reaped (keeping only the most recent N per project)
	appsToReap, err := repo.GetFlyAppsToReap(ctx, funcrepo.GetFlyAppsToReapParams{
		KeepCount: pgtype.Int8{Int64: r.keepCount, Valid: true},
		// Starting with a small batch size for now and we'll increase later on
		// after some observation.
		BatchSize: pgtype.Int8{Int64: 50, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to query apps to reap").Log(ctx, logger)
	}

	if len(appsToReap) == 0 {
		logger.InfoContext(ctx, "no apps to reap")
		return &ReapFlyAppsResult{
			Reaped: 0,
			Errors: 0,
		}, nil
	}

	result := &ReapFlyAppsResult{
		Reaped: 0,
		Errors: 0,
	}

	for _, app := range appsToReap {
		appLogger := logger.With(
			attr.SlogFlyAppInternalID(app.ID.String()),
			attr.SlogFlyAppName(app.AppName),
			attr.SlogFlyOrgSlug(app.FlyOrgSlug),
			attr.SlogProjectID(app.ProjectID.String()),
			attr.SlogDeploymentID(app.DeploymentID.String()),
			attr.SlogDeploymentFunctionsID(app.FunctionID.String()),
		)

		appLogger.InfoContext(ctx, "reaping fly app")

		if err := r.deployer.Reap(ctx, functions.ReapRequest{
			ProjectID:    app.ProjectID,
			DeploymentID: app.DeploymentID,
			FunctionID:   app.FunctionID,
		}); err != nil {
			appLogger.ErrorContext(ctx, "failed to reap app", attr.SlogError(err))
			result.Errors++
			continue
		}

		result.Reaped++
		appLogger.InfoContext(ctx, "successfully reaped fly app")
	}

	r.metrics.RecordFlyAppReaperReapCount(ctx, int64(result.Reaped), int64(result.Errors))

	return result, nil
}

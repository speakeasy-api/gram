package risk_policy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// CleanArgs identifies the policy whose results should be deleted.
type CleanArgs struct {
	ProjectID uuid.UUID
	PolicyID  uuid.UUID
}

// Cleanup deletes risk_results rows for a soft-deleted policy.
type Cleanup struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
}

func NewCleanup(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *Cleanup {
	return &Cleanup{
		logger: logger.With(attr.SlogComponent("risk-policy-cleanup")),
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_policy"),
		db:     db,
	}
}

func (a *Cleanup) Do(ctx context.Context, args CleanArgs) (err error) {
	ctx, span := a.tracer.Start(ctx, "risk.cleanPolicyResults")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if err = repo.New(a.db).DeleteRiskResultsByPolicy(ctx, repo.DeleteRiskResultsByPolicyParams{
		RiskPolicyID: args.PolicyID,
		ProjectID:    args.ProjectID,
	}); err != nil {
		return fmt.Errorf("delete risk results by policy: %w", err)
	}

	a.logger.InfoContext(ctx, "deleted risk results for policy",
		attr.SlogProjectID(args.ProjectID.String()),
		attr.SlogRiskPolicyID(args.PolicyID.String()),
	)
	return nil
}

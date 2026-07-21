package risk_analysis

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// FetchUnanalyzed retrieves all active policies for a project and the batch
// of chat message IDs that have not yet been marked as analyzed
// (risk_analyzed_at IS NULL) within the configured lookback window.
type FetchUnanalyzed struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
}

func NewFetchUnanalyzed(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *FetchUnanalyzed {
	return &FetchUnanalyzed{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		db:     db,
	}
}

// PolicyForAnalysis carries the policy metadata the coordinator needs to
// construct AnalyzeBatchArgs for each active policy.
type PolicyForAnalysis struct {
	ID               uuid.UUID
	OrganizationID   string
	Version          int64
	Sources          []string
	PresidioEntities []string
	CustomRuleIds    []string
}

type FetchUnanalyzedArgs struct {
	ProjectID    uuid.UUID
	IDLowerBound uuid.UUID // UUIDv7 lower bound derived from lookback window
	BatchLimit   int32
}

type FetchUnanalyzedResult struct {
	MessageIDs []uuid.UUID
	Policies   []PolicyForAnalysis
}

func (a *FetchUnanalyzed) Do(ctx context.Context, args FetchUnanalyzedArgs) (_ *FetchUnanalyzedResult, err error) {
	ctx, span := a.tracer.Start(ctx, "risk.fetchUnanalyzed", trace.WithAttributes(
		attribute.String("risk.project_id", args.ProjectID.String()),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	queries := repo.New(a.db)

	policies, err := queries.ListEnabledRiskPoliciesByProject(ctx, args.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("list enabled risk policies: %w", err)
	}

	if len(policies) == 0 {
		return &FetchUnanalyzedResult{
			MessageIDs: nil,
			Policies:   nil,
		}, nil
	}

	ids, err := queries.FetchUnanalyzedMessageIDs(ctx, repo.FetchUnanalyzedMessageIDsParams{
		ProjectID:    uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		IDLowerBound: args.IDLowerBound,
		BatchLimit:   args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch unanalyzed message IDs: %w", err)
	}

	span.SetAttributes(
		attribute.Int("risk.unanalyzed_count", len(ids)),
		attribute.Int("risk.active_policies", len(policies)),
	)

	result := &FetchUnanalyzedResult{
		MessageIDs: ids,
		Policies:   make([]PolicyForAnalysis, len(policies)),
	}
	for i, p := range policies {
		result.Policies[i] = PolicyForAnalysis{
			ID:               p.ID,
			OrganizationID:   p.OrganizationID,
			Version:          p.Version,
			Sources:          p.Sources,
			PresidioEntities: p.PresidioEntities,
			CustomRuleIds:    p.CustomRuleIds,
		}
	}

	return result, nil
}

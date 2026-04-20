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

// FetchUnanalyzed retrieves a batch of chat message IDs that have not yet been
// analyzed for a given risk policy at its current version.
type FetchUnanalyzed struct {
	logger *slog.Logger
	tracer trace.Tracer
	repo   *repo.Queries
}

func NewFetchUnanalyzed(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *FetchUnanalyzed {
	return &FetchUnanalyzed{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		repo:   repo.New(db),
	}
}

type FetchUnanalyzedArgs struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
	BatchLimit   int32
}

type FetchUnanalyzedResult struct {
	MessageIDs     []uuid.UUID
	OrganizationID string
	PolicyVersion  int64
	Sources        []string
}

func (a *FetchUnanalyzed) Do(ctx context.Context, args FetchUnanalyzedArgs) (_ *FetchUnanalyzedResult, err error) {
	ctx, span := a.tracer.Start(ctx, "risk.fetchUnanalyzed", trace.WithAttributes(
		attribute.String("risk.project_id", args.ProjectID.String()),
		attribute.String("risk.policy_id", args.RiskPolicyID.String()),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	// Always read the current policy to get the latest version and sources.
	policy, err := a.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        args.RiskPolicyID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get risk policy: %w", err)
	}

	span.SetAttributes(attribute.Int64("risk.policy_version", policy.Version))

	if !policy.Enabled {
		// Side-effect: when a policy is disabled, we purge all its results here
		// rather than in a separate activity. This is the only place that runs
		// per-cycle for a disabled policy, and leaving stale results would be
		// misleading in the dashboard (showing findings for a policy that's off).
		if err := a.repo.DeleteStaleRiskResults(ctx, repo.DeleteStaleRiskResultsParams{
			RiskPolicyID:      args.RiskPolicyID,
			ProjectID:         args.ProjectID,
			RiskPolicyVersion: policy.Version + 1, // version+1 deletes everything including current
		}); err != nil {
			return nil, fmt.Errorf("delete results for disabled policy: %w", err)
		}

		span.SetAttributes(attribute.Bool("risk.policy_disabled", true))

		return &FetchUnanalyzedResult{
			MessageIDs:     nil,
			OrganizationID: policy.OrganizationID,
			PolicyVersion:  policy.Version,
			Sources:        policy.Sources,
		}, nil
	}

	ids, err := a.repo.FetchUnanalyzedMessageIDs(ctx, repo.FetchUnanalyzedMessageIDsParams{
		ProjectID:         uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: policy.Version,
		BatchLimit:        args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch unanalyzed message IDs: %w", err)
	}

	span.SetAttributes(attribute.Int("risk.unanalyzed_count", len(ids)))

	return &FetchUnanalyzedResult{
		MessageIDs:     ids,
		OrganizationID: policy.OrganizationID,
		PolicyVersion:  policy.Version,
		Sources:        policy.Sources,
	}, nil
}

package risk_analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	db     *pgxpool.Pool
}

func NewFetchUnanalyzed(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *FetchUnanalyzed {
	return &FetchUnanalyzed{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		db:     db,
	}
}

type FetchUnanalyzedArgs struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
	BatchLimit   int32
}

type FetchUnanalyzedResult struct {
	MessageIDs       []uuid.UUID
	OrganizationID   string
	PolicyVersion    int64
	Sources          []string
	PresidioEntities []string
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
	queries := repo.New(a.db)
	policy, err := queries.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        args.RiskPolicyID,
		ProjectID: args.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Policy was deleted after its drain workflow was signaled. There is
		// no work left for this workflow, so let it complete instead of
		// retrying a deterministic miss.
		span.SetAttributes(attribute.Bool("risk.policy_deleted", true))
		return &FetchUnanalyzedResult{
			MessageIDs:       nil,
			OrganizationID:   "",
			PolicyVersion:    0,
			Sources:          nil,
			PresidioEntities: nil,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get risk policy: %w", err)
	}

	span.SetAttributes(attribute.Int64("risk.policy_version", policy.Version))

	if !policy.Enabled {
		// No work to do — results for disabled policies are already hidden
		// by the list queries (JOIN rp.enabled IS TRUE).
		span.SetAttributes(attribute.Bool("risk.policy_disabled", true))
		return &FetchUnanalyzedResult{
			MessageIDs:       nil,
			OrganizationID:   policy.OrganizationID,
			PolicyVersion:    policy.Version,
			Sources:          policy.Sources,
			PresidioEntities: policy.PresidioEntities,
		}, nil
	}

	ids, err := queries.FetchUnanalyzedMessageIDs(ctx, repo.FetchUnanalyzedMessageIDsParams{
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
		MessageIDs:       ids,
		OrganizationID:   policy.OrganizationID,
		PolicyVersion:    policy.Version,
		Sources:          policy.Sources,
		PresidioEntities: policy.PresidioEntities,
	}, nil
}

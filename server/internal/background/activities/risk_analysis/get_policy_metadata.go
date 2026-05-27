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

// GetRiskPolicyMetadata loads the active fields of a risk policy needed to
// drive AnalyzeBatch without re-fetching unanalyzed message IDs. The
// event-driven workflow (AnalyzeNewMessageWorkflow) already knows which IDs
// to process from its signal queue, so it only needs the policy's current
// version and scanner configuration.
type GetRiskPolicyMetadata struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
}

func NewGetRiskPolicyMetadata(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *GetRiskPolicyMetadata {
	return &GetRiskPolicyMetadata{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		db:     db,
	}
}

type GetRiskPolicyMetadataArgs struct {
	ProjectID    uuid.UUID
	RiskPolicyID uuid.UUID
}

type GetRiskPolicyMetadataResult struct {
	Enabled              bool
	OrganizationID       string
	PolicyVersion        int64
	Sources              []string
	PresidioEntities     []string
	PromptInjectionRules []string
}

func (a *GetRiskPolicyMetadata) Do(ctx context.Context, args GetRiskPolicyMetadataArgs) (_ *GetRiskPolicyMetadataResult, err error) {
	ctx, span := a.tracer.Start(ctx, "risk.getPolicyMetadata", trace.WithAttributes(
		attribute.String("risk.project_id", args.ProjectID.String()),
		attribute.String("risk.policy_id", args.RiskPolicyID.String()),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	policy, err := repo.New(a.db).GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        args.RiskPolicyID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get risk policy: %w", err)
	}

	span.SetAttributes(
		attribute.Int64("risk.policy_version", policy.Version),
		attribute.Bool("risk.policy_enabled", policy.Enabled),
	)

	return &GetRiskPolicyMetadataResult{
		Enabled:              policy.Enabled,
		OrganizationID:       policy.OrganizationID,
		PolicyVersion:        policy.Version,
		Sources:              policy.Sources,
		PresidioEntities:     policy.PresidioEntities,
		PromptInjectionRules: policy.PromptInjectionRules,
	}, nil
}

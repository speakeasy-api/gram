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

	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// dashboardAssistantSourceKind is assistant_threads.source_kind for
// conversations that arrived through the Gram dashboard assistant sidebar.
// Those chats are core dashboard functionality rather than a sold artifact,
// so they are out of scope for risk analysis.
const dashboardAssistantSourceKind = triggers.DefinitionSlugDashboard

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
	MessageTypes     []string
	PresidioEntities []string
	CustomRuleIds    []string
}

type FetchUnanalyzedArgs struct {
	ProjectID    uuid.UUID
	IDLowerBound uuid.UUID // UUIDv7 lower bound derived from lookback window
	BatchLimit   int32
}

type FetchUnanalyzedResult struct {
	MessageIDs     []uuid.UUID
	ContentPartIDs []uuid.UUID
	Policies       []PolicyForAnalysis
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
			MessageIDs:     nil,
			ContentPartIDs: nil,
			Policies:       nil,
		}, nil
	}

	skippedMessages, err := queries.MarkDashboardAssistantMessagesRiskAnalyzed(ctx, repo.MarkDashboardAssistantMessagesRiskAnalyzedParams{
		ProjectID:           uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		IDLowerBound:        args.IDLowerBound,
		DashboardSourceKind: dashboardAssistantSourceKind,
		BatchLimit:          args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("mark dashboard assistant messages analyzed: %w", err)
	}
	skippedContentParts, err := queries.MarkDashboardAssistantContentPartsRiskAnalyzed(ctx, repo.MarkDashboardAssistantContentPartsRiskAnalyzedParams{
		ProjectID:           uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		IDLowerBound:        args.IDLowerBound,
		DashboardSourceKind: dashboardAssistantSourceKind,
		BatchLimit:          args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("mark dashboard assistant content parts analyzed: %w", err)
	}

	messageIDs, err := queries.FetchUnanalyzedMessageIDs(ctx, repo.FetchUnanalyzedMessageIDsParams{
		ProjectID:           uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		IDLowerBound:        args.IDLowerBound,
		DashboardSourceKind: dashboardAssistantSourceKind,
		BatchLimit:          args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch unanalyzed message IDs: %w", err)
	}
	contentPartIDs, err := queries.FetchUnanalyzedContentPartIDs(ctx, repo.FetchUnanalyzedContentPartIDsParams{
		ProjectID:           uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		IDLowerBound:        args.IDLowerBound,
		DashboardSourceKind: dashboardAssistantSourceKind,
		BatchLimit:          args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch unanalyzed content part IDs: %w", err)
	}

	span.SetAttributes(
		attribute.Int("risk.unanalyzed_count", len(messageIDs)+len(contentPartIDs)),
		attribute.Int("risk.unanalyzed_message_count", len(messageIDs)),
		attribute.Int("risk.unanalyzed_content_part_count", len(contentPartIDs)),
		attribute.Int("risk.dashboard_messages_skipped", int(skippedMessages)),
		attribute.Int("risk.dashboard_content_parts_skipped", int(skippedContentParts)),
		attribute.Int("risk.active_policies", len(policies)),
	)

	result := &FetchUnanalyzedResult{
		MessageIDs:     messageIDs,
		ContentPartIDs: contentPartIDs,
		Policies:       make([]PolicyForAnalysis, len(policies)),
	}
	for i, p := range policies {
		result.Policies[i] = PolicyForAnalysis{
			ID:               p.ID,
			OrganizationID:   p.OrganizationID,
			Version:          p.Version,
			Sources:          p.Sources,
			MessageTypes:     p.MessageTypes,
			PresidioEntities: p.PresidioEntities,
			CustomRuleIds:    p.CustomRuleIds,
		}
	}

	return result, nil
}

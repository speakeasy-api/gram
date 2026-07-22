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

// FetchAdhoc pages chat message IDs for an ad-hoc (operator-triggered) risk
// analysis run over an explicit time window, together with the policies to
// scan. Unlike FetchUnanalyzed it ignores risk_analyzed_at entirely: ad-hoc
// runs re-scan already-analyzed messages and must not disturb the live
// coordinator's watermark.
type FetchAdhoc struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
}

func NewFetchAdhoc(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *FetchAdhoc {
	return &FetchAdhoc{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		db:     db,
	}
}

type FetchAdhocArgs struct {
	ProjectID uuid.UUID
	// RiskPolicyID scopes the run to a single enabled policy when set;
	// otherwise all enabled policies for the project are scanned.
	RiskPolicyID uuid.NullUUID
	// IDLowerBound (inclusive) and IDUpperBound (exclusive) are UUIDv7 bounds
	// derived from the requested time window.
	IDLowerBound uuid.UUID
	IDUpperBound uuid.UUID
	// IDCursor is the exclusive keyset cursor; uuid.Nil on the first page.
	IDCursor   uuid.UUID
	BatchLimit int32
}

type FetchAdhocResult struct {
	MessageIDs []uuid.UUID
	Policies   []PolicyForAnalysis
}

func (a *FetchAdhoc) Fetch(ctx context.Context, args FetchAdhocArgs) (_ *FetchAdhocResult, err error) {
	ctx, span := a.tracer.Start(ctx, "risk.fetchAdhoc", trace.WithAttributes(
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

	result := &FetchAdhocResult{
		MessageIDs: nil,
		Policies:   nil,
	}
	for _, p := range policies {
		if args.RiskPolicyID.Valid && p.ID != args.RiskPolicyID.UUID {
			continue
		}
		result.Policies = append(result.Policies, PolicyForAnalysis{
			ID:               p.ID,
			OrganizationID:   p.OrganizationID,
			Version:          p.Version,
			Sources:          p.Sources,
			MessageTypes:     p.MessageTypes,
			PresidioEntities: p.PresidioEntities,
			CustomRuleIds:    p.CustomRuleIds,
		})
	}

	// A requested policy that is no longer enabled (deleted or disabled
	// mid-run) fails the run rather than silently scanning nothing.
	if args.RiskPolicyID.Valid && len(result.Policies) == 0 {
		return nil, fmt.Errorf("risk policy %s is not enabled for project %s", args.RiskPolicyID.UUID, args.ProjectID)
	}
	if len(result.Policies) == 0 {
		return result, nil
	}

	ids, err := queries.ListMessageIDsForAdhocRiskAnalysis(ctx, repo.ListMessageIDsForAdhocRiskAnalysisParams{
		ProjectID:    uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		IDLowerBound: args.IDLowerBound,
		IDCursor:     args.IDCursor,
		IDUpperBound: args.IDUpperBound,
		BatchLimit:   args.BatchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list message ids for adhoc analysis: %w", err)
	}
	result.MessageIDs = ids

	span.SetAttributes(
		attribute.Int("risk.adhoc_message_count", len(ids)),
		attribute.Int("risk.adhoc_policies", len(result.Policies)),
	)

	return result, nil
}

type CountAdhocArgs struct {
	ProjectID    uuid.UUID
	IDLowerBound uuid.UUID
	IDUpperBound uuid.UUID
}

// Count returns the total number of messages in the run's window, computed
// once up front so operators can track progress against a fixed denominator.
func (a *FetchAdhoc) Count(ctx context.Context, args CountAdhocArgs) (int64, error) {
	total, err := repo.New(a.db).CountMessagesForAdhocRiskAnalysis(ctx, repo.CountMessagesForAdhocRiskAnalysisParams{
		ProjectID:    uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		IDLowerBound: args.IDLowerBound,
		IDUpperBound: args.IDUpperBound,
	})
	if err != nil {
		return 0, fmt.Errorf("count messages for adhoc analysis: %w", err)
	}
	return total, nil
}

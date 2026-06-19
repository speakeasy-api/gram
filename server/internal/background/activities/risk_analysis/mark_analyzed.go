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

// MarkMessagesAnalyzed sets risk_analyzed_at on a batch of chat messages
// after all active policies have completed analysis (fan-in step).
type MarkMessagesAnalyzed struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
}

func NewMarkMessagesAnalyzed(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *MarkMessagesAnalyzed {
	return &MarkMessagesAnalyzed{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"),
		db:     db,
	}
}

type MarkMessagesAnalyzedArgs struct {
	ProjectID  uuid.UUID
	MessageIDs []uuid.UUID
}

func (a *MarkMessagesAnalyzed) Do(ctx context.Context, args MarkMessagesAnalyzedArgs) (err error) {
	ctx, span := a.tracer.Start(ctx, "risk.markMessagesAnalyzed", trace.WithAttributes(
		attribute.String("risk.project_id", args.ProjectID.String()),
		attribute.Int("risk.message_count", len(args.MessageIDs)),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if len(args.MessageIDs) == 0 {
		return nil
	}

	if err := repo.New(a.db).MarkMessagesRiskAnalyzed(ctx, repo.MarkMessagesRiskAnalyzedParams{
		ProjectID:  uuid.NullUUID{UUID: args.ProjectID, Valid: true},
		MessageIds: args.MessageIDs,
	}); err != nil {
		return fmt.Errorf("mark messages risk analyzed: %w", err)
	}

	return nil
}

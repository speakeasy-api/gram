package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

type RecordAgentExecutionInput struct {
	ExecutionID string
	ProjectID   uuid.UUID
	Status      string
	StartedAt   time.Time
	CompletedAt *time.Time
}

type RecordAgentExecution struct {
	logger *slog.Logger
	repo   *repo.Queries
}

func NewRecordAgentExecution(logger *slog.Logger, db *pgxpool.Pool) *RecordAgentExecution {
	return &RecordAgentExecution{
		logger: logger.With(attr.SlogComponent("record-agent-execution-activity")),
		repo:   repo.New(db),
	}
}

func (a *RecordAgentExecution) Do(ctx context.Context, input RecordAgentExecutionInput) error {
	var completedAt pgtype.Timestamptz
	if input.CompletedAt != nil {
		completedAt = pgtype.Timestamptz{
			Time:             *input.CompletedAt,
			Valid:            true,
			InfinityModifier: 0,
		}
	}

	_, err := a.repo.UpsertAgentExecution(ctx, repo.UpsertAgentExecutionParams{
		ID:           input.ExecutionID,
		ProjectID:    input.ProjectID,
		DeploymentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, // TODO: this can be provided from investigating a particular tool at a later time
		Status:       input.Status,
		StartedAt:    pgtype.Timestamptz{Time: input.StartedAt, Valid: true, InfinityModifier: 0},
		CompletedAt:  completedAt,
	})

	if err != nil {
		a.logger.ErrorContext(ctx, "failed to record agent execution", attr.SlogError(err))
		return fmt.Errorf("failed to record agent execution: %w", err)
	}

	return nil
}

package activities

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/rag"
)

type GenerateToolsetEmbeddings struct {
	logger     *slog.Logger
	tracer     trace.TracerProvider
	pool       *pgxpool.Pool
	ragService *rag.ToolsetVectorStore
}

type GenerateToolsetEmbeddingsInput struct {
	ToolsetSlug types.Slug
	ProjectID   uuid.UUID
}

func NewGenerateToolsetEmbeddingsActivity(
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	ragService *rag.ToolsetVectorStore,
	logger *slog.Logger,

) *GenerateToolsetEmbeddings {

	return &GenerateToolsetEmbeddings{
		logger:     logger,
		tracer:     tracerProvider,
		pool:       db,
		ragService: ragService,
	}
}

func (a *GenerateToolsetEmbeddings) Do(
	ctx context.Context,
	input GenerateToolsetEmbeddingsInput,
) error {
	toolset, getToolsetErr := mv.DescribeToolset(
		ctx,
		a.logger,
		a.pool,
		mv.ProjectID(input.ProjectID),
		mv.ToolsetSlug(conv.ToLower(input.ToolsetSlug)),
		nil,
	)

	if getToolsetErr != nil {
		a.logger.Error(
			"error fetching toolset",
			slog.String("toolset_slug", string(input.ToolsetSlug)),
			slog.String("project_id", input.ProjectID.String()),
			slog.String("error", getToolsetErr.Error()),
		)
		return getToolsetErr
	}

	err := a.ragService.IndexToolset(
		ctx,
		*toolset,
	)
	if err != nil {
		return err
	}

	return nil
}

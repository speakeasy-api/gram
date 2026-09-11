package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/temporal"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const GenerateToolsetEmbeddingsPermanentErrorType = "GenerateToolsetEmbeddingsPermanent"
const GenerateToolsetEmbeddingsSupersededErrorType = "GenerateToolsetEmbeddingsSuperseded"

type GenerateToolsetEmbeddings struct {
	logger     *slog.Logger
	tracer     trace.TracerProvider
	pool       *pgxpool.Pool
	ragService *rag.ToolsetVectorStore
}

type GenerateToolsetEmbeddingsInput struct {
	ProjectID      uuid.UUID
	ToolsetID      uuid.UUID
	ToolsetSlug    types.Slug
	ToolsetVersion int64
	DeploymentID   uuid.UUID
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
		nil,
	)

	if getToolsetErr != nil {
		a.logger.ErrorContext(
			ctx,
			"error fetching toolset",
			attr.SlogToolsetSlug(string(input.ToolsetSlug)),
			attr.SlogProjectID(input.ProjectID.String()),
			attr.SlogError(getToolsetErr),
		)
		return getToolsetErr
	}

	if err := a.ragService.IndexToolset(ctx, *toolset, rag.ToolsetIndexRevision{
		ToolsetID:      input.ToolsetID,
		ToolsetVersion: input.ToolsetVersion,
		DeploymentID:   input.DeploymentID,
	}); err != nil {
		return newGenerateToolsetEmbeddingsError(err)
	}

	return nil
}

func newGenerateToolsetEmbeddingsError(err error) error {
	wrapped := fmt.Errorf("failed to index toolset: %w", err)
	if errors.Is(err, rag.ErrToolsetIndexRevisionSuperseded) {
		return temporal.NewNonRetryableApplicationError(
			wrapped.Error(),
			GenerateToolsetEmbeddingsSupersededErrorType,
			wrapped,
		)
	}
	if openrouter.IsPermanentError(err) {
		return temporal.NewNonRetryableApplicationError(
			wrapped.Error(),
			GenerateToolsetEmbeddingsPermanentErrorType,
			wrapped,
		)
	}
	return wrapped
}

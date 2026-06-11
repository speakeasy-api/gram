package activities

import (
	"context"
	"fmt"
	"log/slog"

	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

type RecycleAssistantRuntimeImagesResult struct {
	Recycled int
	Skipped  int
	Errors   int
}

type RecycleAssistantRuntimeImages struct {
	logger *slog.Logger
	core   *assistants.ServiceCore
}

func NewRecycleAssistantRuntimeImages(logger *slog.Logger, core *assistants.ServiceCore) *RecycleAssistantRuntimeImages {
	return &RecycleAssistantRuntimeImages{
		logger: logger.With(attr.SlogComponent("assistant_runtime_image_recycle")),
		core:   core,
	}
}

func (r *RecycleAssistantRuntimeImages) Do(ctx context.Context) (*RecycleAssistantRuntimeImagesResult, error) {
	if r.core == nil {
		return nil, fmt.Errorf("assistants core not configured")
	}

	result, err := r.core.RecycleActiveRuntimeImages(ctx, assistants.RecycleAssistantRuntimeImagesParams{
		OnRowProcessed: func() {
			activity.RecordHeartbeat(ctx)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("recycle assistant runtime images: %w", err)
	}

	if result.Recycled > 0 || result.Errors > 0 {
		r.logger.InfoContext(ctx, fmt.Sprintf("assistant runtime image recycle swept recycled=%d skipped=%d errors=%d", result.Recycled, result.Skipped, result.Errors),
			attr.SlogVisibilityInternal(),
		)
	}

	return &RecycleAssistantRuntimeImagesResult{
		Recycled: result.Recycled,
		Skipped:  result.Skipped,
		Errors:   result.Errors,
	}, nil
}

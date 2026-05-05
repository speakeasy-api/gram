package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// ReapInactiveAssistantRuntimesRequest carries one janitor sweep's
// configuration over the Temporal activity boundary.
type ReapInactiveAssistantRuntimesRequest struct {
	InactivityThreshold time.Duration
	BatchSize           int32
}

type ReapInactiveAssistantRuntimesResult struct {
	Reaped int
	Errors int
}

type ReapInactiveAssistantRuntimes struct {
	logger *slog.Logger
	core   *assistants.ServiceCore
}

func NewReapInactiveAssistantRuntimes(logger *slog.Logger, core *assistants.ServiceCore) *ReapInactiveAssistantRuntimes {
	return &ReapInactiveAssistantRuntimes{
		logger: logger.With(attr.SlogComponent("assistant_runtime_janitor")),
		core:   core,
	}
}

func (r *ReapInactiveAssistantRuntimes) Do(ctx context.Context, req ReapInactiveAssistantRuntimesRequest) (*ReapInactiveAssistantRuntimesResult, error) {
	if r.core == nil {
		return nil, fmt.Errorf("assistants core not configured")
	}

	result, err := r.core.ReapInactiveAssistantRuntimes(ctx, assistants.ReapInactiveAssistantRuntimesParams{
		InactivityThreshold: req.InactivityThreshold,
		BatchSize:           req.BatchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("reap inactive assistant runtimes: %w", err)
	}

	if result.Reaped > 0 || result.Errors > 0 {
		r.logger.InfoContext(ctx, fmt.Sprintf("assistant runtime janitor swept reaped=%d errors=%d", result.Reaped, result.Errors),
			attr.SlogVisibilityInternal(),
		)
	}

	return &ReapInactiveAssistantRuntimesResult{
		Reaped: result.Reaped,
		Errors: result.Errors,
	}, nil
}

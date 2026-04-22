package assistants

import (
	"context"

	"github.com/google/uuid"
)

type WorkflowSignaler interface {
	SignalCoordinator(ctx context.Context, assistantID uuid.UUID) error
	SignalThread(ctx context.Context, threadID uuid.UUID) error
}

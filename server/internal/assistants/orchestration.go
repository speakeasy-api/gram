package assistants

import (
	"context"

	"github.com/google/uuid"
)

type WorkflowSignaler interface {
	SignalCoordinator(ctx context.Context, assistantID uuid.UUID) error
	SignalThread(ctx context.Context, threadID, projectID uuid.UUID) error
	// StartRuntimeWarmup kicks off the eager runtime boot workflow for a
	// freshly created assistant. Fire-and-forget: the workflow is keyed on
	// the assistant id, so duplicate starts coalesce onto the running one.
	StartRuntimeWarmup(ctx context.Context, assistantID uuid.UUID) error
}

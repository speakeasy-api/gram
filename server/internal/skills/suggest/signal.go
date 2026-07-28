package suggest

import (
	"context"

	"github.com/google/uuid"
)

// Signaler starts or wakes analysis for one skill.
type Signaler interface {
	Signal(ctx context.Context, projectID, skillID uuid.UUID) error
}

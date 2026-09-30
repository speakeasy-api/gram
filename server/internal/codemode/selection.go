package codemode

import (
	"context"

	"github.com/google/uuid"
)

// SelectionGate checks rollout and runtime availability for new code-mode
// settings and connection overrides. Never use it to interpret stored policy.
type SelectionGate func(context.Context, string, uuid.UUID) bool

// Allows fails closed when the new-selection gate has not been configured.
func (g SelectionGate) Allows(ctx context.Context, organizationID string, projectID uuid.UUID) bool {
	return g != nil && g(ctx, organizationID, projectID)
}

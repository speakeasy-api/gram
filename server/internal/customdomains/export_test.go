package customdomains

import (
	"context"

	"github.com/google/uuid"
)

// EnsureDNSTargetTokenWithSource exposes the injectable token source so
// external tests can force token collisions.
func EnsureDNSTargetTokenWithSource(ctx context.Context, db TxBeginner, organizationID string, id uuid.UUID, newToken func() string) (string, error) {
	return ensureDNSTargetToken(ctx, db, organizationID, id, newToken)
}

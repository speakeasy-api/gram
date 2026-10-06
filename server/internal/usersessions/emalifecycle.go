package usersessions

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/usersessions/lifecycle"
)

func guardUserIssuerEMABindings(ctx context.Context, tx pgx.Tx, organizationID string, id uuid.UUID) error {
	return guardScopedUserIssuerEMABindings(ctx, tx, organizationID, uuid.Nil, id)
}

func guardScopedUserIssuerEMABindings(ctx context.Context, tx pgx.Tx, organizationID string, projectID, id uuid.UUID) error {
	if err := lifecycle.GuardEMABindings(ctx, tx, organizationID, projectID, id); err != nil {
		return fmt.Errorf("guard user issuer identity-chaining bindings: %w", err)
	}
	return nil
}

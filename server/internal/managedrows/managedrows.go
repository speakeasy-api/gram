// Package managedrows refuses organization-tier writes to rows an identity
// provider connection provisioned.
package managedrows

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/managedrows/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Error is the refusal every managed-row guard returns.
func Error(subject string) error {
	return oops.E(oops.CodeConflict, nil, "%s is managed by an identity provider connection", subject)
}

// RequireUnmanaged refuses a row carrying identity_provider_connection_id.
func RequireUnmanaged(id uuid.NullUUID, subject string) error {
	if id.Valid {
		return Error(subject)
	}

	return nil
}

// RequireDeletable refuses a row whose connection is still live. A row left
// behind by a tombstoned connection is the organization's to remove.
func RequireDeletable(ctx context.Context, db repo.DBTX, organizationID string, id uuid.NullUUID, subject string) error {
	if !id.Valid {
		return nil
	}

	deleted, err := repo.New(db).IdentityProviderConnectionIsDeleted(ctx, repo.IdentityProviderConnectionIsDeletedParams{
		ID:             id.UUID,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Error(subject)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "check identity provider connection")
	case !deleted:
		return Error(subject)
	}

	return nil
}

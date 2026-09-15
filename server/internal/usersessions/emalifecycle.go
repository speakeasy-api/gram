package usersessions

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The user-issuer advisory lock is also held by EMA preparation. Retain it
// through commit so a new binding cannot appear between this check and mutation.
func guardUserIssuerEMABindings(ctx context.Context, tx pgx.Tx, organizationID string, id uuid.UUID) error {
	if err := repo.New(tx).LockUserSessionIssuerForOwnerBinding(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock identity-chaining issuer references")
	}
	count, err := remotesessionsrepo.New(tx).CountActiveEMABindingsForUserIssuer(ctx, remotesessionsrepo.CountActiveEMABindingsForUserIssuerParams{IssuerID: id, OrganizationID: organizationID, ProjectID: uuid.Nil})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check identity-chaining issuer bindings")
	}
	if count > 0 {
		return oops.E(oops.CodeConflict, nil, "active identity-chaining bindings reference this issuer; explicitly unlink them before reconfiguration or deletion")
	}
	return nil
}

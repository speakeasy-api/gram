package remotesessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Guards run in the mutation transaction. Preparation takes these same row
// locks before installing a binding, so count-then-write cannot race preparation.
// Callers must authorize the selected object's ownership before invoking them.
func guardEMABindingsForClient(ctx context.Context, r *repo.Queries, organizationID string, clientID uuid.UUID) error {
	if err := lockClientForEMALifecycle(ctx, r, clientID); err != nil {
		return err
	}
	count, err := r.CountActiveEMABindingsForClient(ctx, repo.CountActiveEMABindingsForClientParams{ClientID: conv.ToNullUUID(clientID), OrganizationID: organizationID, ProjectID: uuid.Nil})
	return requireNoEMABindings(count, err)
}

// The caller must authorize ownership before taking this ID-only lock.
func lockClientForEMALifecycle(ctx context.Context, r *repo.Queries, clientID uuid.UUID) error {
	if _, err := r.LockRemoteSessionClientForSessionWrite(ctx, clientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "lock identity-chaining client references")
	}
	return nil
}

func guardEMABindingsForIssuer(ctx context.Context, r *repo.Queries, organizationID string, issuerID uuid.UUID) error {
	_, err := r.GetTenantRemoteSessionIssuerByIDForUpdate(ctx, issuerID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = r.GetGlobalRemoteSessionIssuerByIDForUpdate(ctx, issuerID)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "lock identity-chaining issuer references")
	}
	count, err := r.CountActiveEMABindingsForIssuer(ctx, repo.CountActiveEMABindingsForIssuerParams{IssuerID: issuerID, OrganizationID: organizationID, ProjectID: uuid.Nil})
	return requireNoEMABindings(count, err)
}

func requireNoEMABindings(count int64, err error) error {
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check identity-chaining bindings")
	}
	if count > 0 {
		return oops.E(oops.CodeConflict, nil, "active identity-chaining bindings reference this configuration; explicitly unlink them before changing or deleting it")
	}
	return nil
}

package lifecycle

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// GuardEMABindings protects issuer mutation, including automatic
// orphan cleanup by other services. The caller must retain tx through mutation.
func GuardEMABindings(ctx context.Context, tx pgx.Tx, organizationID string, projectID, id uuid.UUID) error {
	q := repo.New(tx)
	if err := verifyUserIssuerEMAScope(ctx, q, organizationID, projectID, id); err != nil {
		return err
	}
	if err := q.LockUserSessionIssuerForOwnerBinding(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock identity-chaining issuer references")
	}
	if err := verifyUserIssuerEMAScope(ctx, q, organizationID, projectID, id); err != nil {
		return err
	}
	// Revalidate conditional tenant ownership under a row lock as well as the
	// owner advisory lock. Preparation takes this same user-issuer row lock
	// before inserting its binding; the subsequent count therefore sees any
	// preparation that committed while this mutation waited. A remote-issuer
	// advisory lock has a different key and cannot replace this row lock.
	// Hold both until the configuration mutation commits.
	if _, err := remotesessionsrepo.New(tx).LockEMAUserIssuer(ctx, remotesessionsrepo.LockEMAUserIssuerParams{ID: id, ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(organizationID)}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer not found")
		}
		return oops.E(oops.CodeUnexpected, err, "lock user session issuer scope")
	}
	count, err := remotesessionsrepo.New(tx).CountActiveEMABindingsForUserIssuer(ctx, remotesessionsrepo.CountActiveEMABindingsForUserIssuerParams{IssuerID: id, OrganizationID: organizationID, ProjectID: projectID})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check identity-chaining issuer bindings")
	}
	if count > 0 {
		return oops.E(oops.CodeConflict, nil, "active identity-chaining bindings reference this issuer; explicitly unlink them before reconfiguration or deletion")
	}
	return nil
}

// Verify ownership before the UUID-only advisory lock, then again after waiting
// for it, so a foreign identifier cannot lock or reveal another tenant's state.
func verifyUserIssuerEMAScope(ctx context.Context, q *repo.Queries, organizationID string, projectID, id uuid.UUID) error {
	var err error
	if projectID != uuid.Nil {
		_, err = q.GetProjectUserSessionIssuerByID(ctx, repo.GetProjectUserSessionIssuerByIDParams{ID: id, ProjectID: projectID})
	} else {
		_, err = q.GetOrganizationUserSessionIssuerByID(ctx, repo.GetOrganizationUserSessionIssuerByIDParams{ID: id, OrganizationID: organizationID})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "user session issuer not found")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "verify user session issuer ownership")
	}
	return nil
}

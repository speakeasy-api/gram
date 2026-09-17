package bindings

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// ErrNotFound indicates that the requested issuer is not attachable in the
// caller's project or organization.
var ErrNotFound = errors.New("user session issuer not found")

// ValidateAndLock verifies that issuerID is visible to projectID and locks its
// owner-binding key. The second read closes the race with organization issuer
// deletion, which uses the same advisory lock before checking active owners.
func ValidateAndLock(ctx context.Context, tx pgx.Tx, issuerID, projectID uuid.UUID, organizationID string) (repo.UserSessionIssuer, error) {
	if tx == nil || issuerID == uuid.Nil || projectID == uuid.Nil || organizationID == "" {
		return repo.UserSessionIssuer{}, fmt.Errorf("invalid user session issuer binding input")
	}

	queries := repo.New(tx)
	params := repo.GetUserSessionIssuerByIDParams{
		ID:             issuerID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
	}
	if _, err := queries.GetUserSessionIssuerByID(ctx, params); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.UserSessionIssuer{}, ErrNotFound
		}
		return repo.UserSessionIssuer{}, fmt.Errorf("load user session issuer: %w", err)
	}
	if err := queries.LockUserSessionIssuerForOwnerBinding(ctx, issuerID); err != nil {
		return repo.UserSessionIssuer{}, fmt.Errorf("lock user session issuer for owner binding: %w", err)
	}

	issuer, err := queries.GetUserSessionIssuerByID(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.UserSessionIssuer{}, ErrNotFound
		}
		return repo.UserSessionIssuer{}, fmt.Errorf("recheck user session issuer: %w", err)
	}
	return issuer, nil
}

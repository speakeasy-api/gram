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

// ErrGatewayMemberCredentials indicates that the issuer holds several clients
// of one remote issuer for a gateway's members. Only that gateway may consume
// it; any other consumer would resolve credentials ambiguously.
var ErrGatewayMemberCredentials = errors.New("user session issuer holds per-member gateway credentials and cannot be shared")

// ValidateAndLock verifies that issuerID is visible to projectID, locks its
// owner-binding key, and refuses issuers that hold per-member gateway
// credentials. The second read closes the race with organization issuer
// deletion, which uses the same advisory lock before checking active owners.
// Use it when a server or toolset starts consuming the issuer.
func ValidateAndLock(ctx context.Context, tx pgx.Tx, issuerID, projectID uuid.UUID, organizationID string) (repo.UserSessionIssuer, error) {
	issuer, err := validateAndLock(ctx, tx, issuerID, projectID, organizationID)
	if err != nil {
		return repo.UserSessionIssuer{}, err
	}
	if err := RejectGatewayMemberCredentials(ctx, tx, issuerID); err != nil {
		return repo.UserSessionIssuer{}, err
	}
	return issuer, nil
}

// ValidateAndLockOwnerBinding is ValidateAndLock without the gateway member
// credential check, for a gateway that already consumes the issuer.
func ValidateAndLockOwnerBinding(ctx context.Context, tx pgx.Tx, issuerID, projectID uuid.UUID, organizationID string) (repo.UserSessionIssuer, error) {
	return validateAndLock(ctx, tx, issuerID, projectID, organizationID)
}

// RejectGatewayMemberCredentials returns ErrGatewayMemberCredentials when the
// issuer binds more than one live client of the same remote issuer. The caller
// must hold the issuer's owner-binding lock.
func RejectGatewayMemberCredentials(ctx context.Context, tx pgx.Tx, issuerID uuid.UUID) error {
	multi, err := repo.New(tx).UserSessionIssuerHasMultiClientProvider(ctx, issuerID)
	if err != nil {
		return fmt.Errorf("check user session issuer clients: %w", err)
	}
	if multi {
		return ErrGatewayMemberCredentials
	}
	return nil
}

func validateAndLock(ctx context.Context, tx pgx.Tx, issuerID, projectID uuid.UUID, organizationID string) (repo.UserSessionIssuer, error) {
	if issuerID == uuid.Nil {
		return repo.UserSessionIssuer{}, ErrNotFound
	}
	if tx == nil || projectID == uuid.Nil || organizationID == "" {
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

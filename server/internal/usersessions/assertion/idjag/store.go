package idjag

import (
	"context"

	"github.com/google/uuid"
)

// Store supplies the trusted-issuer link and active directory membership.
type Store interface {
	// TrustedIssuer returns the issuer explicitly linked to a user session issuer.
	TrustedIssuer(ctx context.Context, organizationID string, userSessionIssuerID uuid.UUID) (TrustedIssuer, error)
	// ResolveUser maps an asserted email address to an active Gram user.
	ResolveUser(ctx context.Context, organizationID, email string) (string, error)
}

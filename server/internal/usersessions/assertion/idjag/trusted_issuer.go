package idjag

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// JWKSURIConfigured reports whether a remote issuer has a usable jwks_uri.
func JWKSURIConfigured(uri pgtype.Text) bool {
	return uri.Valid && jwks.ValidateURI(uri.String) == nil
}

// TrustedIssuer is the issuer reached through the user session issuer's
// configured trust link, never through a search on an assertion's iss claim.
type TrustedIssuer struct {
	// ID identifies the trusted remote session issuer.
	ID uuid.UUID
	// Issuer is the expected assertion issuer.
	Issuer string
	// JWKSURI is the trusted source for assertion verification keys.
	JWKSURI string
}

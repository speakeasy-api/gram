package idjag

import "github.com/google/uuid"

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

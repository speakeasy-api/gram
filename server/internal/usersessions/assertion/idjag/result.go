package idjag

import (
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

// Result is an accepted Speakeasy user subject and its verified grant claims.
type Result struct {
	// Subject identifies the resolved Speakeasy user.
	Subject urn.SessionSubject
	// TrustedIssuerID identifies the issuer that authenticated the assertion.
	TrustedIssuerID uuid.UUID
	// Claims contains the verified grant claims.
	Claims Claims

	// replayKey is the reservation this result holds on the assertion's jti.
	replayKey replay.Key

	// owner is the per-request token that reservation was made under.
	owner string
}

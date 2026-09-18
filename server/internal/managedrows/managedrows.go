// Package managedrows refuses organization-tier writes to rows an identity
// provider connection provisioned.
package managedrows

import (
	"github.com/google/uuid"

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

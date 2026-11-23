package agents

import (
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/agents/lifecycle"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
)

// Lifecycle is the state derived from an agent's lifecycle timestamps.
type Lifecycle = lifecycle.State

const (
	LifecycleActive    = lifecycle.Active
	LifecycleSuspended = lifecycle.Suspended
	LifecycleRevoked   = lifecycle.Revoked
	LifecycleDeleted   = lifecycle.Deleted
)

// DeriveLifecycle applies the authoritative lifecycle precedence.
func DeriveLifecycle(agent repo.Agent) Lifecycle {
	return lifecycle.Derive(agent)
}

// OwnsUnblocked reports whether userID owns an agent that is not awaiting a
// new owner. Such an owner reads and manages the agent whatever their grants.
// Every surface that admits an agent's owner decides it here, so none admits
// an owner another refuses.
func OwnsUnblocked(userID, ownerUserID string, ownerReassignmentRequiredAt pgtype.Timestamptz) bool {
	return userID != "" && ownerUserID == userID && !ownerReassignmentRequiredAt.Valid
}

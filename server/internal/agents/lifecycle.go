package agents

import "github.com/speakeasy-api/gram/server/internal/agents/repo"

// Lifecycle is the state derived from an agent's lifecycle timestamps.
type Lifecycle string

const (
	LifecycleActive    Lifecycle = "active"
	LifecycleSuspended Lifecycle = "suspended"
	LifecycleRevoked   Lifecycle = "revoked"
	LifecycleDeleted   Lifecycle = "deleted"
)

// DeriveLifecycle applies the authoritative lifecycle precedence.
func DeriveLifecycle(agent repo.Agent) Lifecycle {
	if agent.DeletedAt.Valid {
		return LifecycleDeleted
	}
	if agent.RevokedAt.Valid {
		return LifecycleRevoked
	}
	if agent.SuspendedAt.Valid {
		return LifecycleSuspended
	}
	return LifecycleActive
}

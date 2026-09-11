package agents

import (
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

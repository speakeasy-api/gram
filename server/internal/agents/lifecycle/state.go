package lifecycle

import "github.com/speakeasy-api/gram/server/internal/agents/repo"

// State is the state derived from an agent's lifecycle timestamps.
type State string

const (
	Active    State = "active"
	Suspended State = "suspended"
	Revoked   State = "revoked"
	Deleted   State = "deleted"
)

// Derive applies the authoritative lifecycle precedence.
func Derive(agent repo.Agent) State {
	if agent.DeletedAt.Valid {
		return Deleted
	}
	if agent.RevokedAt.Valid {
		return Revoked
	}
	if agent.SuspendedAt.Valid {
		return Suspended
	}
	return Active
}

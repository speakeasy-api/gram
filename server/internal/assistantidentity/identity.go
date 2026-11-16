// Package assistantidentity connects assistants to the ordinary agent and
// workload identity model. An assistant points at one dedicated agent and each
// of its root triggers points at one workload identity (issuer and subject).
// Those resources stay owned and edited through their own management surfaces;
// this package never treats them as system-owned.
package assistantidentity

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// State is an assistant's workload identity configuration state.
type State string

const (
	// NeverConfigured assistants keep the legacy owner-scoped behaviour.
	NeverConfigured State = "NEVER_CONFIGURED"

	// Active assistants point at a dedicated agent that is currently usable.
	Active State = "ACTIVE"

	// Unavailable assistants point at an agent that is suspended, revoked, or
	// deleted, or a trigger whose workload identity no longer resolves.
	Unavailable State = "UNAVAILABLE"
)

var (
	ErrInvalidIdentity = errors.New("invalid assistant identity")
	ErrActorIneligible = errors.New("assistant identity requires a user actor")
	ErrNotFound        = errors.New("assistant identity resource not found")
	// ErrOwnerUnavailable means a staff-provisioned assistant has no creator
	// who is still an active member of its organization to own its agent.
	ErrOwnerUnavailable = errors.New("assistant creator is not an active organization member")
)

// ProvisionParams identifies the assistant and who configures its identity:
// either the authenticated user ActorUserID, or Staff. The actor is supplied
// by the authorized service, never taken from a request body.
type ProvisionParams struct {
	OrganizationID string
	ProjectID      uuid.UUID
	AssistantID    uuid.UUID
	ActorUserID    string
	// Staff provisions on a Speakeasy staff member's behalf. ActorUserID is
	// then empty, the agent's owner must be the assistant's creator, and the
	// owner's authority alone decides whether the agent inherits inference.
	Staff *StaffActor
}

// StaffActor is the Speakeasy staff member recorded as the audit actor.
type StaffActor struct {
	Principal   urn.Principal
	DisplayName *string
}

// AssistantState is the configuration state of one assistant. AgentID is set
// whenever the assistant points at an agent, including when it is Unavailable.
type AssistantState struct {
	State   State
	AgentID *uuid.UUID
}

// Identity is a resolved workload identity, not a bearer credential.
type Identity struct {
	OrganizationID string    `json:"organization_id"`
	ProjectID      uuid.UUID `json:"project_id"`
	AssistantID    uuid.UUID `json:"assistant_id"`
	AgentID        uuid.UUID `json:"agent_id"`
	TriggerID      uuid.UUID `json:"trigger_id"`
	IssuerID       uuid.UUID `json:"issuer_id"`
	Subject        string    `json:"subject"`
}

// Resolution has an identity only in the active state.
type Resolution struct {
	State    State
	Identity *Identity
}

// DB is the minimal database capability required for coherent read snapshots.
type DB interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

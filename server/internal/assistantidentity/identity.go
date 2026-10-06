// Package assistantidentity connects assistants to the ordinary agent and
// workload identity model. An assistant points at one dedicated agent and each
// of its root triggers points at one workload identity (issuer and subject).
// Those resources stay owned and edited through their own management surfaces;
// this package never treats them as system-owned.
package assistantidentity

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
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
)

// ProvisionParams identifies the assistant and the authenticated user who
// configures its identity. ActorUserID is supplied by the authorized service,
// never taken from a request body.
type ProvisionParams struct {
	OrganizationID string
	ProjectID      uuid.UUID
	AssistantID    uuid.UUID
	ActorUserID    string
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

// CeilingSnapshot owns canonical delegated-policy bytes. It is detached from
// later database changes; callers persist it as immutable execution input.
type CeilingSnapshot struct {
	EncodingVersion runtimepolicy.DelegatedPolicyVersion `json:"encoding_version"`
	Policy          json.RawMessage                      `json:"policy"`
	Digest          string                               `json:"digest"`
}

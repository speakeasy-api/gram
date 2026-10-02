// Package assistantidentity owns durable assistant authority. Management services
// call its transaction-aware mutations; runtime execution is deliberately absent.
package assistantidentity

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
)

// State distinguishes untouched legacy resources from permanently withdrawn authority.
type State string

const (
	NeverConfigured State = "NEVER_CONFIGURED"
	Active          State = "ACTIVE"
	Tombstoned      State = "TOMBSTONED"
)

var (
	ErrInvalidIdentity = errors.New("invalid assistant identity")
	ErrBrokenMapping   = errors.New("broken assistant workload mapping")
	ErrTombstoned      = errors.New("assistant authority is tombstoned")
	ErrActorIneligible = errors.New("assistant identity actor is not an active member")
	ErrNotFound        = errors.New("assistant identity resource not found")
)

// ProvisionParams identifies the authenticated consenting human. ActorUserID is
// supplied by the authorized service, never taken from a request body. The
// original creator is independently loaded from the assistant's durable row.
type ProvisionParams struct {
	OrganizationID string
	ProjectID      uuid.UUID
	AssistantID    uuid.UUID
	ActorUserID    string
}

// Binding identifies the exclusive, project-scoped agent for an assistant.
type Binding struct {
	OrganizationID     string
	ProjectID          uuid.UUID
	AssistantID        uuid.UUID
	AgentID            uuid.UUID
	Generation         int64
	AgentIdentityEpoch int64
}

// Identity is a captured authority incarnation, not a bearer credential.
type Identity struct {
	OrganizationID      string
	ProjectID           uuid.UUID
	AssistantID         uuid.UUID
	AgentID             uuid.UUID
	TriggerID           uuid.UUID
	IssuerID            uuid.UUID
	Subject             string
	AssistantGeneration int64
	TriggerGeneration   int64
	AgentIdentityEpoch  int64
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
	EncodingVersion runtimepolicy.DelegatedPolicyVersion
	Policy          json.RawMessage
	Digest          string
}

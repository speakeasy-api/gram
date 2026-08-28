package killswitches

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	operationResponseVersionV1 = "killswitch-operation-response-v1"
	maxCleanupBatchSize        = int32(1000)
	// Keep selected snapshots and their validation bounded. Use dynamic all-resource scope for broader coverage.
	maxSelectedResourceCount = 1000
)

// PrescriptionState is the durable lifecycle state of a prescription version.
type PrescriptionState string

const (
	PrescriptionStateActive   PrescriptionState = "active"
	PrescriptionStateInactive PrescriptionState = "inactive"
)

var (
	ErrInvalidArgument      = errors.New("invalid killswitch lifecycle argument")
	ErrPrescriptionNotFound = errors.New("killswitch prescription not found")
	ErrInvalidReference     = errors.New("invalid killswitch reference")
	ErrInvalidTransition    = errors.New("invalid killswitch lifecycle transition")
	ErrOperationConflict    = errors.New("killswitch operation conflicts with an existing operation")
	ErrOperationUnavailable = errors.New("killswitch operation is unavailable")
	ErrVersionConflict      = errors.New("killswitch prescription version conflict")
)

// VersionConflictError reports the current version observed under the aggregate lock.
type VersionConflictError struct {
	Expected int64
	Actual   int64
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("%s: expected version %d, current version %d", ErrVersionConflict, e.Expected, e.Actual)
}

func (e *VersionConflictError) Unwrap() error { return ErrVersionConflict }

// MutationContext contains trusted tenancy and actor data plus the organization-wide operation ID.
type MutationContext struct {
	OrganizationID OrganizationID
	ActorUserID    string
	OperationID    uuid.UUID
}

// DesiredVersionInput is the complete desired mutable payload for an active version. Both the raw
// and canonical selected-resource sets are limited to maxSelectedResourceCount entries. Use
// ResourceScopeAll when a prescription must dynamically cover a broader resource population.
type DesiredVersionInput struct {
	ResourceScope          ResourceScope
	SelectedResourceInputs []string
	StartMode              StartMode
	StartsAt               *time.Time
	ExpiresAt              *time.Time
	InternalNote           string
	ExternalNote           string
}

type ActivatePrescriptionRequest struct {
	MutationContext
	Definition     DefinitionKey
	PrincipalKind  PrincipalKind
	PrincipalInput string
	ResourceKind   ResourceKind
	Desired        DesiredVersionInput
}

type ChangePrescriptionRequest struct {
	MutationContext
	PrescriptionID  PrescriptionID
	ExpectedVersion int64
	Desired         DesiredVersionInput
}

type DeactivatePrescriptionRequest struct {
	MutationContext
	PrescriptionID  PrescriptionID
	ExpectedVersion int64
}

type ReactivatePrescriptionRequest struct {
	MutationContext
	PrescriptionID  PrescriptionID
	ExpectedVersion int64
	Desired         DesiredVersionInput
}

// MutationResult is the bounded result persisted in an operation receipt.
type MutationResult struct {
	PrescriptionID PrescriptionID
	Version        int64
	State          PrescriptionState
	Replayed       bool
}

type operationResponseV1 struct {
	ResponseVersion     string            `json:"response_version"`
	PrescriptionID      PrescriptionID    `json:"prescription_id"`
	PrescriptionVersion int64             `json:"prescription_version"`
	State               PrescriptionState `json:"state"`
}

// LifecycleTransactionQueries permits DBTX-style transaction-enlisted domain reads and writes.
// LifecycleService wraps this capability to reject transaction control and multiple statements; it
// alone owns commit and rollback.
type LifecycleTransactionQueries interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CurrentPrincipalReference identifies one canonical principal for authoritative validation.
type CurrentPrincipalReference struct {
	Kind PrincipalKind
	Key  PrincipalKey
}

// CurrentResourceReferences identifies one canonical resource batch for authoritative validation.
type CurrentResourceReferences struct {
	Kind ResourceKind
	Keys []ResourceKey
}

// CurrentReferenceBatch contains canonical references that must still exist in one organization.
type CurrentReferenceBatch struct {
	OrganizationID OrganizationID
	Principal      *CurrentPrincipalReference
	Resources      *CurrentResourceReferences
}

// LifecycleValidator validates authoritative source records through the lifecycle transaction.
// Implementations must validate each batch in a fixed order, must not mutate or retain it, and must
// lock matched rows against deletion or tenant transfer until the transaction finishes. Canonicalization
// remains owned by registered adapters. Invalid or incomplete batches must wrap ErrInvalidReference.
type LifecycleValidator interface {
	ValidateCurrent(context.Context, LifecycleTransactionQueries, CurrentReferenceBatch) error
}

// MutationEvent is passed to the before-commit seam for later audit/outbox integration.
type MutationEvent struct {
	OrganizationID OrganizationID
	ActorUserID    string
	OperationID    uuid.UUID
	Operation      MutationOperation
	Result         MutationResult
}

// BeforeCommitHook may add audit and outbox writes to the lifecycle transaction, but cannot
// commit or roll it back. LifecycleService alone owns transaction completion.
type BeforeCommitHook func(context.Context, LifecycleTransactionQueries, MutationEvent) error

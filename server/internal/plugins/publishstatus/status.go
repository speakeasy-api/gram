// Package publishstatus describes the latest change-driven marketplace publish
// for a project. Publish attempts and their failures are recorded only in the
// Temporal history of the per-project publish workflow, not in Postgres, so
// "what happened to the last publish" is answered by describing that workflow.
// The types live in their own leaf package so the background worker, which
// implements Describer, and the Platform MCP server, which reports it, can
// share them without an import cycle.
package publishstatus

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// State is the coarse state of a project's latest publish attempt.
type State string

const (
	// StateNone means no publish attempt is visible within Temporal's
	// retention window: the project has not changed recently enough to
	// still have one on record.
	StateNone State = "none"

	// StateQueued means a publish is waiting to start, either inside its
	// debounce window or waiting for a worker to pick it up.
	StateQueued State = "queued"

	// StateRunning means a publish is executing its first attempt now.
	StateRunning State = "running"

	// StateRetrying means an earlier attempt of the in-flight publish failed
	// and it is waiting for or executing a retry. FailureCategory names why
	// the previous attempt failed.
	StateRetrying State = "retrying"

	// StateSucceeded means the latest publish finished without error. It may
	// have found the generated package unchanged and pushed nothing.
	StateSucceeded State = "succeeded"

	// StateFailed means the latest publish stopped without succeeding.
	// FailureCategory names why.
	StateFailed State = "failed"
)

// FailureCategory is a bounded, non-secret classification of why a publish
// attempt failed. Raw failure messages are never surfaced: they can carry
// repository names and upstream provider payloads.
type FailureCategory string

const (
	// FailureNone is the zero value for states that carry no failure.
	FailureNone FailureCategory = ""

	// FailureRepositoryConflict means the marketplace repository name computed
	// for the project already belongs to another active project. Retrying
	// cannot resolve it.
	FailureRepositoryConflict FailureCategory = "repository_conflict"

	// FailureTimedOut means the publish did not finish within its time budget.
	FailureTimedOut FailureCategory = "timed_out"

	// FailureCanceled means the publish was canceled or terminated before it
	// finished.
	FailureCanceled FailureCategory = "canceled"

	// FailurePublishFailed covers every other failure, such as an unavailable
	// upstream repository host or an internal error, once retries are
	// exhausted or while they continue.
	FailurePublishFailed FailureCategory = "publish_failed"
)

// Status is the point-in-time state of a project's latest publish attempt.
// Timestamps are nil when they do not apply to the state.
type Status struct {
	// State is the coarse state of the latest attempt.
	State State

	// RequestedAt is when the latest publish was requested. Nil for StateNone.
	RequestedAt *time.Time

	// FinishedAt is when the latest publish closed. Set only for
	// StateSucceeded and StateFailed.
	FinishedAt *time.Time

	// Attempt is the attempt number of the in-flight publish, starting at 1.
	// Zero when no attempt is in flight.
	Attempt int32

	// FailureCategory classifies the failure for StateFailed, and the
	// previous attempt's failure for StateRetrying. FailureNone otherwise.
	FailureCategory FailureCategory
}

// Describer reports the latest publish attempt for a project.
type Describer interface {
	Describe(ctx context.Context, projectID uuid.UUID) (Status, error)
}

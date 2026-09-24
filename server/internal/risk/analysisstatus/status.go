// Package analysisstatus describes the run state of a project's risk
// analysis coordinator workflow. The coordinator is signal-driven rather than
// scheduled, so "when did the Watchdog last run" is answered by describing
// the latest Temporal run of the per-project workflow. The type lives in its
// own leaf package so the risk service, the background worker and the
// Platform MCP server can all share it without an import cycle.
package analysisstatus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
)

// SignalCooldown is the per-project throttle applied to the chat-write signal
// that wakes the risk analysis coordinator. The first write signals at once
// and later writes inside the window coalesce into one trailing signal, so
// analysis starts within about this long of new chat traffic. It lives here
// so the wiring (server and worker) and the administrator-facing explanation
// (Platform MCP) read one value.
const SignalCooldown = 30 * time.Second

// State is the coarse run state of a project's risk analysis coordinator.
type State string

const (
	// StateNever means no coordinator run exists for the project within
	// Temporal's retention window: the project has never been analyzed, or
	// not recently enough to still be visible.
	StateNever State = "never"
	// StateIdle means the latest run has closed and the coordinator is
	// waiting for the next chat write to wake it.
	StateIdle State = "idle"
	// StateRunning means a coordinator run is in flight right now. This
	// includes the retry backoff: a run that saw a failed batch sleeps
	// before continuing as new, and the workflow is running while it waits.
	StateRunning State = "running"
)

// Status is the point-in-time run state of a project's risk analysis
// coordinator. Timestamps are nil when they do not apply to the state.
type Status struct {
	State State
	// RunningSince is the start of the in-flight run. Set only when running.
	RunningSince *time.Time
	// LastRunStartedAt is when the most recent closed run began. Set only
	// when idle.
	LastRunStartedAt *time.Time
	// LastRunAt is when the most recent closed run finished. Set only when
	// idle.
	LastRunAt *time.Time
	// LastRunOutcome is the lower-cased Temporal close status of the most
	// recent closed run (completed, failed, timed_out, canceled, terminated,
	// continued_as_new). Empty unless idle.
	LastRunOutcome string
}

// Describer reports the coordinator run state for a project.
type Describer interface {
	Describe(ctx context.Context, projectID uuid.UUID) (Status, error)
}

// FromDescribe maps a Temporal DescribeWorkflowExecution result to a Status.
// A NotFound error is a valid answer (StateNever), not a failure; any other
// error is returned as-is so callers can decide whether to degrade.
func FromDescribe(resp *workflowservice.DescribeWorkflowExecutionResponse, err error) (Status, error) {
	if err != nil {
		if _, ok := errors.AsType[*serviceerror.NotFound](err); ok {
			return Status{State: StateNever, RunningSince: nil, LastRunStartedAt: nil, LastRunAt: nil, LastRunOutcome: ""}, nil
		}
		return Status{}, fmt.Errorf("describe risk analysis coordinator: %w", err)
	}

	info := resp.GetWorkflowExecutionInfo()
	if info == nil {
		return Status{State: StateNever, RunningSince: nil, LastRunStartedAt: nil, LastRunAt: nil, LastRunOutcome: ""}, nil
	}

	var startedAt *time.Time
	if ts := info.GetStartTime(); ts != nil {
		t := ts.AsTime()
		startedAt = &t
	}

	if info.GetStatus() == enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return Status{State: StateRunning, RunningSince: startedAt, LastRunStartedAt: nil, LastRunAt: nil, LastRunOutcome: ""}, nil
	}

	var closedAt *time.Time
	if ts := info.GetCloseTime(); ts != nil {
		t := ts.AsTime()
		closedAt = &t
	}

	return Status{
		State:            StateIdle,
		RunningSince:     nil,
		LastRunStartedAt: startedAt,
		LastRunAt:        closedAt,
		LastRunOutcome:   outcomeLabel(info.GetStatus()),
	}, nil
}

// outcomeLabel turns a Temporal close status into a stable lower-case label
// that does not leak the enum's WORKFLOW_EXECUTION_STATUS_ prefix to clients.
func outcomeLabel(status enums.WorkflowExecutionStatus) string {
	switch status {
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "completed"
	case enums.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "failed"
	case enums.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "canceled"
	case enums.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "terminated"
	case enums.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return "continued_as_new"
	case enums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "timed_out"
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING, enums.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED:
		return "unknown"
	default:
		return "unknown"
	}
}

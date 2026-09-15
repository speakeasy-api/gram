package triggers

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// These tests cover the trigger workflow helpers that the platform trigger
// tools reach from MCP tool calls. A process without a Temporal environment
// must get an error back, not a nil pointer dereference.

func TestScheduleTriggerCronWorkflowWithoutTemporal(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, ScheduleTriggerCronWorkflow(t.Context(), nil, ScheduleTriggerCronWorkflowOptions{}), tenv.ErrNotConfigured)
}

func TestDeleteTriggerCronWorkflowScheduleWithoutTemporal(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, DeleteTriggerCronWorkflowSchedule(t.Context(), nil, uuid.New()), tenv.ErrNotConfigured)
}

func TestExecuteTriggerDispatchWorkflowWithoutTemporal(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, ExecuteTriggerDispatchWorkflow(t.Context(), nil, TriggerDispatchWorkflowInput{}), tenv.ErrNotConfigured)
}

func TestExecuteTriggerWakeWorkflowWithoutTemporal(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, ExecuteTriggerWakeWorkflow(t.Context(), nil, uuid.New(), time.Now()), tenv.ErrNotConfigured)
}

func TestCancelTriggerWakeWorkflowWithoutTemporal(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, CancelTriggerWakeWorkflow(t.Context(), nil, uuid.New()), tenv.ErrNotConfigured)
}

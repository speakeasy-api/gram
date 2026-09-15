package triggers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// TestWorkflowHelpersWithoutTemporal covers the trigger workflow helpers that
// the platform trigger tools reach from MCP tool calls. A process without a
// Temporal environment must get an error back, not a nil pointer dereference.
func TestWorkflowHelpersWithoutTemporal(t *testing.T) {
	t.Parallel()

	var nilEnv *tenv.Environment
	cases := map[string]func(ctx context.Context) error{
		"schedule cron": func(ctx context.Context) error {
			return ScheduleTriggerCronWorkflow(ctx, nilEnv, ScheduleTriggerCronWorkflowOptions{})
		},
		"delete cron schedule": func(ctx context.Context) error {
			return DeleteTriggerCronWorkflowSchedule(ctx, nilEnv, uuid.New())
		},
		"dispatch": func(ctx context.Context) error {
			return ExecuteTriggerDispatchWorkflow(ctx, nilEnv, TriggerDispatchWorkflowInput{})
		},
		"wake": func(ctx context.Context) error {
			return ExecuteTriggerWakeWorkflow(ctx, nilEnv, uuid.New(), time.Now())
		},
		"cancel wake": func(ctx context.Context) error {
			return CancelTriggerWakeWorkflow(ctx, nilEnv, uuid.New())
		},
	}

	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, run(t.Context()), tenv.ErrNotConfigured)
		})
	}
}

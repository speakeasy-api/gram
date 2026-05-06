package activities_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
)

func TestDispatchTrigger_NilTask_Noop(t *testing.T) {
	t.Parallel()

	// A nil app would normally cause a downstream failure, but Do must
	// short-circuit on a nil Task before reaching the app.
	d := activities.NewDispatchTrigger(nil)
	require.NoError(t, d.Do(t.Context(), activities.DispatchTriggerInput{Task: nil}))
}

func TestDispatchTrigger_NilApp_Errors(t *testing.T) {
	t.Parallel()

	d := activities.NewDispatchTrigger(nil)
	task := &bgtriggers.Task{
		TriggerInstanceID: "instance-1",
		DefinitionSlug:    "slack-message",
		TargetKind:        "tool",
		TargetRef:         "tool-1",
		EventID:           "event-1",
	}
	err := d.Do(t.Context(), activities.DispatchTriggerInput{Task: task})
	require.Error(t, err)
	require.Contains(t, err.Error(), "trigger app is not configured")
}

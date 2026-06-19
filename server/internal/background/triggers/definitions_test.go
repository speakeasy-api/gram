package triggers

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestListIncludesAllDefinitions(t *testing.T) {
	t.Parallel()

	definitions := List()
	// cron, github, linear, slack, wake (dashboard is direct-ingress, excluded).
	require.Len(t, definitions, 5)
	require.Equal(t, "cron", definitions[0].Slug)
	require.Equal(t, "github", definitions[1].Slug)
	require.Equal(t, "linear", definitions[2].Slug)
	require.Equal(t, "slack", definitions[3].Slug)
	require.Equal(t, "wake", definitions[4].Slug)
	for _, d := range definitions {
		require.NotEmpty(t, d.ConfigSchema, d.Slug)
	}
}

func TestCronBuildScheduledEventIsDeterministic(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("cron")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"schedule": "0 * * * *",
	})
	require.NoError(t, err)

	instance := triggerrepo.TriggerInstance{
		ID:             uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		DefinitionSlug: "cron",
	}
	firedAt := time.Date(2026, 4, 9, 12, 0, 0, 123, time.UTC)

	first, err := definition.BuildScheduledEvent(instance, config, firedAt)
	require.NoError(t, err)
	second, err := definition.BuildScheduledEvent(instance, config, firedAt)
	require.NoError(t, err)

	require.Equal(t, first.EventID, second.EventID)
	require.Equal(t, instance.ID.String(), first.CorrelationID)
	require.Equal(t, instance.ID.String(), first.TriggerInstanceID)
}

func TestCronBuildScheduledEventPropagatesNote(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("cron")
	require.True(t, ok)

	note := "run the daily digest"
	config, err := definition.DecodeConfig(map[string]any{
		"schedule": "0 9 * * *",
		"note":     note,
	})
	require.NoError(t, err)

	instance := triggerrepo.TriggerInstance{
		ID:             uuid.MustParse("22222222-3333-4444-5555-666666666666"),
		DefinitionSlug: "cron",
	}
	firedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	envelope, err := definition.BuildScheduledEvent(instance, config, firedAt)
	require.NoError(t, err)

	var event cronTriggerEvent
	require.NoError(t, json.Unmarshal(envelope.RawPayload, &event))
	require.Equal(t, note, event.Note)
	require.Equal(t, "0 9 * * *", event.Schedule)
}

func TestBuildScheduleOptionsUsesSharedScheduleWorkflowInput(t *testing.T) {
	t.Parallel()

	instance := triggerrepo.TriggerInstance{
		ID:     uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Status: "active",
	}

	options := BuildScheduleOptions(instance, "0 * * * *", "trigger-queue", "TriggerCronWorkflow")

	action, ok := options.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	require.Len(t, action.Args, 1)

	input, ok := action.Args[0].(ScheduleWorkflowInput)
	require.True(t, ok)
	require.Equal(t, instance.ID.String(), input.TriggerInstanceID)
	require.Empty(t, input.FiredAt)
}

func TestCronFilterAlwaysReturnsTrue(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("cron")
	require.True(t, ok)

	config, err := definition.DecodeConfig(map[string]any{
		"schedule": "0 * * * *",
	})
	require.NoError(t, err)

	match, err := config.Filter(cronTriggerEvent{
		Schedule: "0 * * * *",
		FiredAt:  "2026-04-09T12:00:00Z",
	})
	require.NoError(t, err)
	require.True(t, match)
}

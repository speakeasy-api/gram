package triggers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeDispatcher struct {
	kind   string
	called bool
	input  Task
	retErr error
}

func (f *fakeDispatcher) Kind() string {
	return f.kind
}

func (f *fakeDispatcher) Dispatch(_ context.Context, input Task) error {
	f.called = true
	f.input = input
	return f.retErr
}

func TestAppDispatchUsesRegisteredDispatcher(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeDispatcher{kind: TargetKindAssistant}
	app := &App{
		dispatchers: map[string]Dispatcher{
			dispatcher.kind: dispatcher,
		},
	}

	input := Task{
		TriggerInstanceID: "11111111-1111-1111-1111-111111111111",
		DefinitionSlug:    "slack",
		TargetKind:        TargetKindAssistant,
		TargetRef:         "assistant-ref",
		TargetDisplay:     "Assistant",
		EventID:           "event-123",
		CorrelationID:     "corr-123",
		RawPayload:        []byte(`{"ok":true}`),
	}

	err := app.Dispatch(t.Context(), input)

	require.NoError(t, err)
	require.True(t, dispatcher.called)
	require.Equal(t, input, dispatcher.input)
}

func TestAppDispatchRejectsUnconfiguredDispatcher(t *testing.T) {
	t.Parallel()

	app := &App{dispatchers: map[string]Dispatcher{}}

	err := app.Dispatch(t.Context(), Task{TargetKind: TargetKindAssistant})

	require.Error(t, err)
	require.ErrorContains(t, err, `trigger dispatcher for target kind "assistant" is not configured`)
}

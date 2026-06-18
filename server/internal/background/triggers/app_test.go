package triggers

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestBoundCorrelationID(t *testing.T) {
	t.Parallel()

	// Within the limit: returned unchanged so routing keys stay readable.
	short := "github:octocat/Hello-World/pr:42"
	require.Equal(t, short, boundCorrelationID(short))

	// A long GitHub push (repo + branch) exceeds the assistant tables' 300-char
	// correlation_id CHECK; it must be bounded, keep a readable prefix, and stay
	// deterministic and distinct from other long ids.
	long := "github:octocat/Hello-World/branch:" + strings.Repeat("a", 400)
	bounded := boundCorrelationID(long)
	require.LessOrEqual(t, utf8.RuneCountInString(bounded), maxCorrelationIDLen)
	require.True(t, strings.HasPrefix(bounded, "github:octocat/Hello-World/branch:"))
	require.Equal(t, bounded, boundCorrelationID(long))

	other := "github:octocat/Hello-World/branch:" + strings.Repeat("b", 400)
	require.NotEqual(t, bounded, boundCorrelationID(other))
}

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

func TestAppCreateRejectsDirectIngressDefinition(t *testing.T) {
	t.Parallel()

	// Direct-ingress definitions (e.g. dashboard) are system-managed; the public
	// create path must refuse them. The guard fires before any DB access.
	app := &App{}

	_, err := app.Create(t.Context(), CreateParams{
		DefinitionSlug: "dashboard",
		Name:           "x",
		TargetKind:     TargetKindAssistant,
		TargetRef:      "assistant-ref",
	})

	require.ErrorIs(t, err, ErrBadRequest)
	require.ErrorContains(t, err, "system-managed")
}

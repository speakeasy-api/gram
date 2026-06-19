package triggers

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestBoundAssistantKey(t *testing.T) {
	t.Parallel()

	// Within the limit: returned unchanged so keys stay readable.
	short := "github:octocat/Hello-World/pr:42"
	require.Equal(t, short, boundAssistantKey(short))

	// A long key (e.g. a GitHub push to a repo + branch with long names)
	// exceeds the assistant tables' 300-char CHECK; it must be bounded, keep a
	// readable prefix, and stay deterministic and distinct from other long keys.
	long := "github:octocat/Hello-World/branch:" + strings.Repeat("a", 400)
	bounded := boundAssistantKey(long)
	require.LessOrEqual(t, utf8.RuneCountInString(bounded), maxAssistantKeyLen)
	require.True(t, strings.HasPrefix(bounded, "github:octocat/Hello-World/branch:"))
	require.Equal(t, bounded, boundAssistantKey(long))

	other := "github:octocat/Hello-World/branch:" + strings.Repeat("b", 400)
	require.NotEqual(t, bounded, boundAssistantKey(other))
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

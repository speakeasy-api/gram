package triggers

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
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

func TestLogTriggerDeliveryStampsTraceContext(t *testing.T) {
	t.Parallel()

	var captured TriggerDeliveryLog
	logger := NewTriggerDeliveryLogger(func(_ context.Context, entry TriggerDeliveryLog) {
		captured = entry
	})

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:  trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
	})
	ctx := trace.ContextWithSpanContext(t.Context(), spanCtx)

	logger.LogTriggerDelivery(ctx, triggerrepo.TriggerInstance{
		ID:             uuid.New(),
		DefinitionSlug: DefinitionSlugSlack,
		TargetKind:     TargetKindAssistant,
	}, EventEnvelope{EventID: "event-123", CorrelationID: "corr-123"}, DeliveryStatusSent, "", nil)

	require.Equal(t, spanCtx.TraceID().String(), captured.Attributes[attr.TraceIDKey])
	require.Equal(t, spanCtx.SpanID().String(), captured.Attributes[attr.SpanIDKey])
}

func TestLogTriggerDeliveryOmitsTraceContextWhenAbsent(t *testing.T) {
	t.Parallel()

	var captured TriggerDeliveryLog
	logger := NewTriggerDeliveryLogger(func(_ context.Context, entry TriggerDeliveryLog) {
		captured = entry
	})

	logger.LogTriggerDelivery(t.Context(), triggerrepo.TriggerInstance{
		ID:             uuid.New(),
		DefinitionSlug: DefinitionSlugSlack,
		TargetKind:     TargetKindAssistant,
	}, EventEnvelope{EventID: "event-123", CorrelationID: "corr-123"}, DeliveryStatusSent, "", nil)

	require.NotContains(t, captured.Attributes, attr.TraceIDKey)
	require.NotContains(t, captured.Attributes, attr.SpanIDKey)
}

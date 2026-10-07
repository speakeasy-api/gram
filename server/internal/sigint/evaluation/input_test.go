package evaluation

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type eventInput struct {
	event    Event
	data     classifier.Entry
	err      error
	resolved int
}

func (in *eventInput) Event() Event                       { return in.event }
func (in *eventInput) MatchingMessage() *matching.Message { return nil }
func (in *eventInput) Resolve(context.Context) (classifier.Entry, error) {
	in.resolved++
	return in.data, in.err
}

func TestEvaluatorAcceptsIndependentEventStreams(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	subject := sigintv1.Reading_Event_builder{Kind: new("mcp.tool_call"), Id: new("execution/opaque-42"), OccurredAt: new("2026-09-30T12:00:00Z")}.Build()
	subject.SetToolCall(sigintv1.Reading_ToolCall_builder{ToolName: new("lookup"), ToolCallId: new("call-1"), McpServerId: new(uuid.NewString())}.Build())
	data, err := classifier.ParseEntry([]byte(`{"arguments":{"count":9007199254740993},"result":{"status":"ok"}}`))
	require.NoError(t, err)
	in := &eventInput{event: Event{OrganizationID: "test-organization", ProjectID: project.String(), Subject: subject, BillingUserID: new("allocated-user")}, data: data}
	var deps dependencies
	deps.Test(t)
	t.Cleanup(func() { deps.AssertExpectations(t) })
	deps.On("IsFeatureEnabled", mock.Anything, in.event.OrganizationID, productfeatures.FeatureSignalsIntelligence).Return(true, nil).Times(3)
	deps.On("Load", mock.Anything, in.event.OrganizationID, project, "mcp.tool_call").Return(partialSensors()[1:], nil).Twice()
	deps.On("Load", mock.Anything, in.event.OrganizationID, project, "custom.audit_event").Return(partialSensors()[1:], nil).Once()
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.MatchedBy(func(req *classifier.Request) bool {
		return reflect.DeepEqual(req.Input, data) && len(req.Questions) == 1
	})).Return(partialResult(false)).Times(3)
	var pub capturePublisher
	evaluator, err := NewEvaluator(testenv.NewLogger(t), testenv.NewMeterProvider(t), &deps, &deps, &pub, c)
	require.NoError(t, err)
	require.NoError(t, evaluator.Evaluate(t.Context(), in))
	require.NoError(t, evaluator.Evaluate(t.Context(), in))
	require.Len(t, pub.readings, 2)
	a, b := pub.readings[0], pub.readings[1]
	require.Equal(t, a.GetId(), b.GetId())
	require.NotEqual(t, a.GetEvaluationAttemptId(), b.GetEvaluationAttemptId())
	require.Equal(t, project.String(), a.GetProjectId())
	require.Equal(t, in.event.OrganizationID, a.GetOrganizationId())
	require.Equal(t, "execution/opaque-42", a.GetEvent().GetId())
	require.False(t, a.GetEvent().HasConversationMessage())
	require.Equal(t, "lookup", a.GetEvent().GetToolCall().GetToolName())
	require.False(t, a.HasActor())
	require.Equal(t, "allocated-user", a.GetBillingUserId())
	encoded, err := proto.Marshal(a)
	require.NoError(t, err)
	decoded := &sigintv1.Reading{}
	require.NoError(t, proto.Unmarshal(encoded, decoded))
	require.True(t, proto.Equal(a, decoded))
	subject.SetKind("custom.audit_event")
	subject.ClearToolCall()
	require.NoError(t, evaluator.Evaluate(t.Context(), in))
	require.Len(t, pub.readings, 3)
	require.NotEqual(t, a.GetId(), pub.readings[2].GetId(), "event kinds namespace otherwise identical identities")
	require.Equal(t, "mcp.tool_call", a.GetEvent().GetKind(), "published metadata is an independent snapshot")
	require.Equal(t, 3, in.resolved)
}

func TestEvaluatorDefersContentUntilEligible(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		enabled     bool
		definitions []Sensor
	}{
		{name: "disabled"},
		{name: "no applicable sensors", enabled: true},
		{name: "draft only", enabled: true, definitions: []Sensor{{ID: "draft"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			project := uuid.New()
			in := &eventInput{event: Event{OrganizationID: "test-organization", ProjectID: project.String(), Subject: sigintv1.Reading_Event_builder{Kind: new("custom.event"), Id: new("event-1"), OccurredAt: new("2026-09-30T12:00:00Z")}.Build()}}
			var deps dependencies
			deps.Test(t)
			t.Cleanup(func() { deps.AssertExpectations(t) })
			deps.On("IsFeatureEnabled", mock.Anything, in.event.OrganizationID, productfeatures.FeatureSignalsIntelligence).Return(tc.enabled, nil).Once()
			if tc.enabled {
				deps.On("Load", mock.Anything, in.event.OrganizationID, project, "custom.event").Return(tc.definitions, nil).Once()
			}
			evaluator, err := NewEvaluator(testenv.NewLogger(t), testenv.NewMeterProvider(t), &deps, &deps, nil, nil)
			require.NoError(t, err)
			require.NoError(t, evaluator.Evaluate(t.Context(), in))
			require.Zero(t, in.resolved)
		})
	}
}

func TestEvaluatorSourceErrorSemantics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"permanent", fmt.Errorf("unsupported payload: %w", ErrInvalidInput), false},
		{"transient", errors.New("source unavailable"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := message()
			// Only metadata is reused; this input has no message payload or resolver.
			in := &eventInput{event: (&conversationInput{message: m}).Event(), err: tc.err}
			h, pub := handler(t, m, sensors(), nil, testenv.NewMeterProvider(t))
			err := h.evaluator.Evaluate(t.Context(), in)
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, pub.readings)
			require.Equal(t, 1, in.resolved)
		})
	}
}

func TestEvaluatorRejectsInvalidEventMetadata(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*Event)
	}{
		{"missing subject", func(e *Event) { e.Subject = nil }},
		{"missing id", func(e *Event) { e.Subject.ClearId() }},
		{"missing kind", func(e *Event) { e.Subject.ClearKind() }},
		{"invalid time", func(e *Event) { e.Subject.SetOccurredAt("yesterday") }},
		{"invalid project", func(e *Event) { e.ProjectID = "not-a-project" }},
		{"missing organization", func(e *Event) { e.OrganizationID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := &eventInput{event: (&conversationInput{message: message()}).Event()}
			tc.mutate(&in.event)
			evaluator, err := NewEvaluator(testenv.NewLogger(t), testenv.NewMeterProvider(t), nil, nil, nil, nil)
			require.NoError(t, err)
			require.NoError(t, evaluator.Evaluate(t.Context(), in))
			require.Zero(t, in.resolved)
		})
	}
}

func TestEvaluatorEnforcesSharedContentLimits(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"null", "oversized"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := message()
			in := &eventInput{event: (&conversationInput{message: m}).Event()}
			if name == "oversized" {
				in.data = classifier.Text(strings.Repeat("x", maxContentBytes))
			}
			h, pub := handler(t, m, sensors(), nil, testenv.NewMeterProvider(t))
			require.NoError(t, h.evaluator.Evaluate(t.Context(), in))
			require.Empty(t, pub.readings)
			require.Equal(t, 1, in.resolved)
		})
	}
}

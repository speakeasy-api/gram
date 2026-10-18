package evaluation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestOutcomeNotApplicableCarriesReadingIdentityAndAttribution(t *testing.T) {
	t.Parallel()
	m := message()
	provenance := &conversationv1.MessageEvent_IngestionContext{}
	provenance.SetBillingUserId("billing-user")
	provenance.SetSource("example-source")
	provenance.SetReplayed(true)
	provenance.SetAttributes(map[string]string{"model_name": "gpt-6.1-sol", "model_family": "openai", "model_provider": "openrouter", "department": "engineering"})
	m.SetIngestion(provenance)
	definitions := partialSensors()
	definitions[0].MatchExpression = `message.role == "assistant"`
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false)).Once()
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))

	require.Len(t, pub.readings, 1)
	require.Len(t, pub.outcomes.messages, 1)
	o, r := pub.outcomes.messages[0], pub.readings[0]
	require.Equal(t, sigintv1.Outcome_STATUS_NOT_APPLICABLE, o.GetStatus())
	require.False(t, o.HasFailureReason())
	require.Equal(t, "multi", o.GetSensorId())
	require.Equal(t, "multi-label", o.GetSensorSlug())
	require.NotEmpty(t, o.GetDefinitionHash())
	require.Equal(t, r.GetEvaluationAttemptId(), o.GetEvaluationAttemptId(), "one attempt covers every sensor decision")
	require.Equal(t, readingID((&conversationInput{message: m}).Event(), "multi"), o.GetId())
	require.Equal(t, m.GetOrganizationId(), o.GetOrganizationId())
	require.Equal(t, m.GetProjectId(), o.GetProjectId())
	require.Equal(t, m.GetMessageId(), o.GetEvent().GetId())
	require.Equal(t, m.GetMessageCreatedAt(), o.GetEvent().GetOccurredAt())
	require.Equal(t, "stream", o.GetTriggerKind())
	require.False(t, o.HasRunId())
	require.Equal(t, m.GetOccurredAt(), o.GetEvaluationRequestedAt())
	require.Equal(t, r.GetEvaluationRequestedAt(), o.GetEvaluationRequestedAt())
	require.Equal(t, m.GetConversationId(), o.GetEvent().GetConversationMessage().GetConversationId())
	require.Equal(t, sigintv1.Outcome_ConversationMessage_ROLE_USER, o.GetEvent().GetConversationMessage().GetRole())
	require.Equal(t, "billing-user", o.GetBillingUserId())
	require.Equal(t, "example-source", o.GetSource())
	require.True(t, o.GetReplayed())
	require.Equal(t, provenance.GetAttributes(), o.GetAttributes())
	require.Equal(t, provenance.GetAttributes(), r.GetAttributes())
	require.False(t, o.HasInput(), "no input is resolved for an inapplicable sensor")
	require.Empty(t, o.GetConfiguredModel())
}

func TestOutcomeDraftHasNoDefinitionHash(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := sensors()[:1]
	definitions[0].Instructions = nil
	h, pub := handler(t, m, definitions, nil, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))

	require.Empty(t, pub.readings)
	require.Len(t, pub.outcomes.messages, 1)
	o := pub.outcomes.messages[0]
	require.Equal(t, sigintv1.Outcome_STATUS_DRAFT, o.GetStatus())
	require.False(t, o.HasFailureReason())
	require.Equal(t, "multi", o.GetSensorId())
	require.Empty(t, o.GetDefinitionHash())
}

func TestOutcomeClassifierRejectionRecordsInputAndModel(t *testing.T) {
	t.Parallel()
	m := message()
	result := partialResult(false)
	result.Metadata.Model = "example-model"
	result.Metadata.Provider = "example-provider"
	result.Metadata.CompilerVersion = "example-compiler"
	result.Models = []string{"example-model-2026"}
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(result).Once()
	h, pub := handler(t, m, partialSensors(), c, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))

	require.Len(t, pub.readings, 1)
	require.Len(t, pub.outcomes.messages, 1)
	o, r := pub.outcomes.messages[0], pub.readings[0]
	require.Equal(t, sigintv1.Outcome_STATUS_FAILED, o.GetStatus())
	require.Equal(t, "classifier_rejected", o.GetFailureReason())
	require.Equal(t, "multi", o.GetSensorId())
	require.Equal(t, r.GetEvaluationAttemptId(), o.GetEvaluationAttemptId())
	require.Equal(t, r.GetEvaluatedAt(), o.GetEvaluatedAt())
	require.Equal(t, "example-model", o.GetConfiguredModel())
	require.Equal(t, "example-provider", o.GetProvider())
	require.Equal(t, "sigint-v1/example-compiler", o.GetCompilerVersion())
	require.Equal(t, []string{"example-model-2026"}, o.GetModels())
	require.Equal(t, InputStrategySingleMessage, o.GetInput().GetStrategy())
	require.Equal(t, int32(1), o.GetInput().GetMessageCount())
}

func TestOutcomeRetryableFailurePublishesNothing(t *testing.T) {
	t.Parallel()
	m := message()
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(true)).Once()
	h, pub := handler(t, m, partialSensors(), c, testenv.NewMeterProvider(t))
	require.Error(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 1)
	require.Empty(t, pub.outcomes.messages)
}

func TestOutcomeInvalidAnswer(t *testing.T) {
	t.Parallel()
	m := message()
	var noul, choice classifier.Answer
	noul.Noul = &classifier.NoulAnswer{Probability: 0.5}
	// A choice answer to a yes/no question cannot become a multi-label reading.
	choice.Choice = &classifier.ChoiceAnswer{Selected: "a", Distribution: []classifier.Probability{{Option: "a", Value: 1}}}
	result := classifier.NewResult([]classifier.QuestionOutcome{
		{Key: "multi/a", Answer: &choice, Failure: nil},
		{Key: "multi/b", Answer: &noul, Failure: nil},
		{Key: "other/a", Answer: &noul, Failure: nil},
	}, nil)
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(result).Once()
	h, pub := handler(t, m, partialSensors(), c, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))

	require.Len(t, pub.readings, 1)
	require.Len(t, pub.outcomes.messages, 1)
	require.Equal(t, "multi", pub.outcomes.messages[0].GetSensorId())
	require.Equal(t, "invalid_answer", pub.outcomes.messages[0].GetFailureReason())
}

func TestOutcomeGroupFailuresCoverEverySensor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		data      classifier.Entry
		err       error
		reason    string
		withInput bool
	}{
		{name: "invalid content", err: fmt.Errorf("unsupported payload: %w", ErrInvalidInput), reason: "invalid_content"},
		{name: "missing asset", err: permanent("missing_asset"), reason: "missing_asset"},
		{name: "content too large", data: classifier.Text(strings.Repeat("x", maxContentBytes)), reason: "content_too_large", withInput: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := message()
			in := &eventInput{event: (&conversationInput{message: m}).Event(), data: tc.data, err: tc.err}
			h, pub := handler(t, m, partialSensors(), nil, testenv.NewMeterProvider(t))
			require.NoError(t, h.evaluator.Evaluate(t.Context(), in))

			require.Empty(t, pub.readings)
			require.Len(t, pub.outcomes.messages, 2)
			for i, sensor := range partialSensors() {
				o := pub.outcomes.messages[i]
				require.Equal(t, sensor.ID, o.GetSensorId())
				require.Equal(t, sigintv1.Outcome_STATUS_FAILED, o.GetStatus())
				require.Equal(t, tc.reason, o.GetFailureReason())
				require.Equal(t, tc.withInput, o.HasInput())
			}
		})
	}
}

func TestOutcomeAttemptsKeepIdentityAcrossRedelivery(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := sensors()[:1]
	definitions[0].MatchExpression = `message.role == "assistant"`
	h, pub := handler(t, m, definitions, nil, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))

	require.Len(t, pub.outcomes.messages, 2)
	a, b := pub.outcomes.messages[0], pub.outcomes.messages[1]
	require.Equal(t, a.GetId(), b.GetId())
	require.NotEqual(t, a.GetEvaluationAttemptId(), b.GetEvaluationAttemptId())
}

func TestOutcomePublishFailureNacks(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := sensors()[:1]
	definitions[0].MatchExpression = `message.role == "assistant"`
	h, pub := handler(t, m, definitions, nil, testenv.NewMeterProvider(t))
	pub.outcomes.err = errors.New("publish unavailable")
	require.ErrorContains(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}), "publish sensor outcome")
}

func TestOutcomeEventLevelFailurePublishesNothing(t *testing.T) {
	t.Parallel()
	var deps dependencies
	deps.Test(t)
	t.Cleanup(func() { deps.AssertExpectations(t) })
	var outcomes outcomeCapture
	evaluator, err := NewEvaluator(testenv.NewLogger(t), testenv.NewMeterProvider(t), &deps, &deps, nil, &outcomes, nil)
	require.NoError(t, err)
	project := uuid.New()
	subject := sigintv1.Reading_Event_builder{Kind: new("custom.event"), Id: new("event-1"), OccurredAt: new("not a timestamp")}.Build()
	in := &eventInput{event: Event{OrganizationID: "test-organization", ProjectID: project.String(), Subject: subject}}
	require.NoError(t, evaluator.Evaluate(t.Context(), in))
	require.Empty(t, outcomes.messages, "no sensors were loaded, so the census must account for the event")
}

func TestOutcomeDropsOversizedLabelsToFitTransport(t *testing.T) {
	t.Parallel()
	m := message()
	labels := &conversationv1.MessageEvent_IngestionContext{}
	labels.SetAttributes(map[string]string{"large": strings.Repeat("x", maxReadingBytes)})
	m.SetIngestion(labels)
	definitions := sensors()[:1]
	definitions[0].MatchExpression = `message.role == "assistant"`
	h, pub := handler(t, m, definitions, nil, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))

	require.Len(t, pub.outcomes.messages, 1)
	o := pub.outcomes.messages[0]
	require.Equal(t, sigintv1.Outcome_STATUS_NOT_APPLICABLE, o.GetStatus())
	require.Empty(t, o.GetAttributes())
	require.LessOrEqual(t, proto.Size(o), maxReadingBytes)
}

package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/sigint/inputconfig"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type windowMessages struct {
	storedMessages
	history []repo.LoadEvaluationMessagesRow
	calls   int
	err     error
}

func (s *windowMessages) LoadHistory(context.Context, string, uuid.UUID, repo.ChatMessage, int) ([]repo.LoadEvaluationMessagesRow, error) {
	s.calls++
	return s.history, s.err
}

func TestWindowConversationPreservesRolesContentAndTarget(t *testing.T) {
	t.Parallel()
	m := message()
	target := storedMessage(m)
	target.ChatMessage.Content = "Yes, that one."
	var history []repo.LoadEvaluationMessagesRow
	for _, role := range []string{"assistant", "tool", "system"} {
		row := storedMessage(m)
		row.ChatMessage.ID = uuid.New()
		row.ChatMessage.Role = role
		row.ChatMessage.Content = role + " context"
		history = append(history, row)
	}
	history[1].ChatMessage.ContentRaw = []byte(`{"value":9007199254740993}`)
	uri := "gs://bucket/" + m.GetProjectId() + "/attachment.txt"
	history[0].AttachmentUris = []string{uri}
	source := &windowMessages{history: history}
	in := &conversationInput{message: m, stored: target, history: source, blobs: memoryBlobs{uri: []byte("attachment context")}}

	state, err := in.ResolveWindow(t.Context(), 5)
	require.NoError(t, err)
	data, err := json.Marshal(state)
	require.NoError(t, err)
	var decoded struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
		Target int `json:"target_message_index"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, 3, decoded.Target)
	require.Len(t, decoded.Messages, 4)
	require.Equal(t, "ROLE_SYSTEM", decoded.Messages[0].Role)
	require.Equal(t, "ROLE_TOOL", decoded.Messages[1].Role)
	require.Equal(t, "ROLE_ASSISTANT", decoded.Messages[2].Role)
	require.Equal(t, "ROLE_USER", decoded.Messages[3].Role)
	require.Equal(t, 1, strings.Count(string(data), "Yes, that one."))
	require.Contains(t, string(data), "9007199254740993")
	require.Contains(t, string(data), "attachment context")

	_, err = in.ResolveWindow(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 1, source.calls)
	require.Len(t, in.resolved, 4)
}

func TestWindowGroupsSensorsAndKeepsReadingIdentityOnRedelivery(t *testing.T) {
	t.Parallel()
	m := message()
	var definitions []Sensor
	var outcomes []classifier.QuestionOutcome
	for i, size := range []int{1, 2, 2, 5} {
		sensor := sensors()[0]
		sensor.ID = string(rune('a' + i))
		sensor.Signals = sensor.Signals[:1]
		if size > 1 {
			sensor.InputStrategy, sensor.WindowSize = inputconfig.SlidingWindow, size
		}
		definitions = append(definitions, sensor)
		outcomes = append(outcomes, classifier.QuestionOutcome{Key: classifier.QuestionKey(sensor.ID + "/a"), Answer: &classifier.Answer{Noul: &classifier.NoulAnswer{Probability: 0.5}}})
	}
	c := classifiertest.NewMock(t)
	var requests []*classifier.Request
	c.On("Classify", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		request, ok := args.Get(1).(*classifier.Request)
		require.True(t, ok)
		requests = append(requests, request)
	}).Return(classifier.NewResult(outcomes, nil)).Times(6)
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	target := storedMessage(m)
	prior := target
	prior.ChatMessage.ID = uuid.New()
	prior.ChatMessage.Content = "First context"
	source := &windowMessages{storedMessages: storedMessages{target.ChatMessage.ID: target}, history: []repo.LoadEvaluationMessagesRow{prior}}
	h.messages = source
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, requests, 3)
	require.Len(t, requests[0].Questions, 1)
	require.Len(t, requests[1].Questions, 2)
	require.Len(t, requests[2].Questions, 1)
	require.Equal(t, 1, source.calls)
	require.Len(t, pub.readings, 4)
	for _, r := range pub.readings {
		require.Equal(t, m.GetMessageId(), r.GetEvent().GetId())
	}
	source.history[0].ChatMessage.Content = "Corrected context"
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 8)
	for i := range 4 {
		require.Equal(t, pub.readings[i].GetId(), pub.readings[i+4].GetId())
		require.NotEqual(t, pub.readings[i].GetEvaluationAttemptId(), pub.readings[i+4].GetEvaluationAttemptId())
	}
	data, err := json.Marshal(requests[4].Input)
	require.NoError(t, err)
	require.Contains(t, string(data), "Corrected context")
}

func TestWindowOlderInvalidContentDoesNotPreventSmallerReading(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := partialSensors()
	definitions[0].InputStrategy, definitions[0].WindowSize = inputconfig.SlidingWindow, 5
	definitions[1].InputStrategy, definitions[1].WindowSize = inputconfig.SlidingWindow, 2
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false)).Once()
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	target := storedMessage(m)
	prior, invalid := storedMessage(message()), storedMessage(message())
	invalid.ChatMessage.ContentRaw = []byte("invalid JSON")
	h.messages = &windowMessages{storedMessages: storedMessages{target.ChatMessage.ID: target}, history: []repo.LoadEvaluationMessagesRow{prior, invalid}}
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 1)
	require.Equal(t, "other", pub.readings[0].GetSensorId())
}

func TestWindowHistoryFailureRetriesAfterPublishingTargetOnly(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := partialSensors()
	definitions[0].InputStrategy = inputconfig.SlidingWindow
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false)).Once()
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	target := storedMessage(m)
	h.messages = &windowMessages{storedMessages: storedMessages{target.ChatMessage.ID: target}, err: errors.New("history unavailable")}
	require.ErrorContains(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}), "history unavailable")
	require.Len(t, pub.readings, 1)
}

func TestWindowAggregateContentLimit(t *testing.T) {
	t.Parallel()
	m := message()
	target := storedMessage(m)
	target.ChatMessage.Content = strings.Repeat("x", maxContentBytes/2)
	prior := storedMessage(message())
	prior.ChatMessage.Content = target.ChatMessage.Content
	in := &conversationInput{message: m, stored: target, history: &windowMessages{history: []repo.LoadEvaluationMessagesRow{prior}}}
	_, err := in.ResolveWindow(t.Context(), 2)
	require.ErrorContains(t, err, "content_too_large")
	require.LessOrEqual(t, in.resolvedBytes, maxContentBytes)
}

func TestWindowEmptyHistoryAndUnusedHistory(t *testing.T) {
	t.Parallel()
	m := message()
	source := &windowMessages{}
	in := &conversationInput{message: m, stored: storedMessage(m), history: source}
	_, err := in.Resolve(t.Context())
	require.NoError(t, err)
	require.Zero(t, source.calls)
	entry, err := in.ResolveWindow(t.Context(), 10)
	require.NoError(t, err)
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	var state conversationState
	require.NoError(t, json.Unmarshal(data, &state))
	require.Len(t, state.Messages, 1)
	require.Zero(t, state.TargetMessageIndex)
}

func TestWindowDefinitionHashAndTargetFraming(t *testing.T) {
	t.Parallel()
	sensor := sensors()[0]
	a, ok := compileSensor(sensor)
	require.True(t, ok)
	sensor.InputStrategy = inputconfig.SlidingWindow
	b, ok := compileSensor(sensor)
	require.True(t, ok)
	sensor.WindowSize = 10
	c, ok := compileSensor(sensor)
	require.True(t, ok)
	require.NotEqual(t, a.hash, b.hash)
	require.NotEqual(t, b.hash, c.hash)
	encoded, err := json.Marshal(c.questions[0].Noul.Instructions)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "Classify only the designated target message")
}

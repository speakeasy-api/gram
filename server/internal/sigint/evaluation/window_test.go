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
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type windowMessages struct {
	storedMessages
	history []repo.LoadEvaluationHistoryRow
	calls   int
	err     error
}

func (s *windowMessages) LoadHistory(context.Context, string, uuid.UUID, repo.LoadEvaluationMessagesRow, int) ([]repo.LoadEvaluationHistoryRow, error) {
	s.calls++
	return s.history, s.err
}

func TestWindowReusesHistoryAndResolvedAttachments(t *testing.T) {
	t.Parallel()
	m := message()
	target := storedMessage(m)
	target.Content = "Yes, that one."
	var history []repo.LoadEvaluationHistoryRow
	for _, role := range []string{"assistant", "tool", "user"} {
		row := targetContent(storedMessage(m))
		row.ID = uuid.New()
		row.Role = role
		row.Content = role + " context"
		history = append(history, row)
	}
	history[1].ContentRaw = []byte(`{"value":9007199254740993}`)
	uri := "gs://bucket/" + m.GetProjectId() + "/attachment.txt"
	history[0].AttachmentUris = []string{uri}
	source := &windowMessages{history: history}
	in := &conversationInput{message: m, stored: target, history: source, blobs: memoryBlobs{uri: []byte("attachment context")}}

	_, err := in.ResolveWindow(t.Context(), 5)
	require.NoError(t, err)
	// A second resolution must succeed even when asset storage is unavailable.
	in.blobs = nil
	_, err = in.ResolveWindow(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 1, source.calls)
}

func TestWindowGroupsSensorsAndKeepsReadingIdentityOnRedelivery(t *testing.T) {
	t.Parallel()
	m := message()
	var definitions []Sensor
	for i, size := range []int{1, 2, 2, 5} {
		sensor := sensors()[0]
		sensor.ID = string(rune('a' + i))
		sensor.Signals = sensor.Signals[:1]
		if size > 1 {
			sensor.InputStrategy, sensor.WindowSize = InputStrategySlidingWindow, size
		}
		definitions = append(definitions, sensor)
	}
	c, requests := newRecordingClassifier(t)
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	target := storedMessage(m)
	prior := targetContent(target)
	prior.ID = uuid.New()
	prior.Content = "First context"
	source := &windowMessages{storedMessages: storedMessages{target.ID: target}, history: []repo.LoadEvaluationHistoryRow{prior}}
	h.messages = source
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Equal(t, 1, source.calls)
	require.Len(t, pub.readings, 4)
	for _, r := range pub.readings {
		require.Equal(t, m.GetMessageId(), r.GetEvent().GetId())
	}
	source.history[0].Content = "Corrected context"
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 8)
	for i := range 4 {
		require.Equal(t, pub.readings[i].GetId(), pub.readings[i+4].GetId())
		require.NotEqual(t, pub.readings[i].GetEvaluationAttemptId(), pub.readings[i+4].GetEvaluationAttemptId())
	}
	testenv.RequireYAMLGolden(t, "testdata/requests/grouped-redelivery.golden.yaml", requests.snapshot(t))
}

func TestWindowOlderInvalidContentDoesNotPreventSmallerReading(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := partialSensors()
	definitions[0].InputStrategy, definitions[0].WindowSize = InputStrategySlidingWindow, 5
	definitions[1].InputStrategy, definitions[1].WindowSize = InputStrategySlidingWindow, 2
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false)).Once()
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	target := storedMessage(m)
	prior, invalid := targetContent(storedMessage(message())), targetContent(storedMessage(message()))
	invalid.ContentRaw = []byte("invalid JSON")
	h.messages = &windowMessages{storedMessages: storedMessages{target.ID: target}, history: []repo.LoadEvaluationHistoryRow{prior, invalid}}
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 1)
	require.Equal(t, "other", pub.readings[0].GetSensorId())
}

func TestWindowHistoryFailureRetriesAfterPublishingTargetOnly(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := partialSensors()
	definitions[0].InputStrategy = InputStrategySlidingWindow
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false)).Once()
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	target := storedMessage(m)
	h.messages = &windowMessages{storedMessages: storedMessages{target.ID: target}, err: errors.New("history unavailable")}
	require.ErrorContains(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}), "history unavailable")
	require.Len(t, pub.readings, 1)
}

func TestWindowAggregateContentLimit(t *testing.T) {
	t.Parallel()
	m := message()
	target := storedMessage(m)
	target.Content = strings.Repeat("x", maxContentBytes/2)
	prior := targetContent(storedMessage(message()))
	prior.Content = target.Content
	in := &conversationInput{message: m, stored: target, history: &windowMessages{history: []repo.LoadEvaluationHistoryRow{prior}}}
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
	testenv.RequireYAMLGolden(t, "testdata/state/empty-history.golden.yaml", data)
}

func TestWindowDefinitionHashIncludesStrategyAndSize(t *testing.T) {
	t.Parallel()
	sensor := sensors()[0]
	a, ok := compileSensor(sensor)
	require.True(t, ok)
	sensor.InputStrategy = InputStrategySlidingWindow
	b, ok := compileSensor(sensor)
	require.True(t, ok)
	sensor.WindowSize = 10
	c, ok := compileSensor(sensor)
	require.True(t, ok)
	require.NotEqual(t, a.hash, b.hash)
	require.NotEqual(t, b.hash, c.hash)
}

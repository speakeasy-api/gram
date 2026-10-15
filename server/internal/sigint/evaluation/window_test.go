package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type windowMessages struct {
	storedMessages
	history []repo.LoadEvaluationHistoryRow
	calls   int
	limits  []int
	err     error
}

func (s *windowMessages) LoadHistory(_ context.Context, _ string, _ uuid.UUID, _ repo.LoadEvaluationMessagesRow, limit int) ([]repo.LoadEvaluationHistoryRow, error) {
	s.calls++
	s.limits = append(s.limits, limit)
	return s.history[:min(limit, len(s.history))], s.err
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
	buffer := in.NewBuffer(5)

	_, err := buffer.Resolve(t.Context(), 5)
	require.NoError(t, err)
	// A second resolution must succeed even when asset storage is unavailable.
	in.blobs = nil
	_, err = buffer.Resolve(t.Context(), 2)
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
	unmatched, draft := sensors()[0], sensors()[0]
	unmatched.ID, unmatched.InputStrategy, unmatched.WindowSize = "unmatched", InputStrategySlidingWindow, 10
	unmatched.MatchExpression = `message.role == "assistant"`
	draft.ID, draft.InputStrategy, draft.WindowSize, draft.Instructions = "draft", InputStrategySlidingWindow, 10, nil
	definitions = append(definitions, unmatched, draft)
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
	require.Equal(t, []int{4}, source.limits, "only matched runnable sensors determine buffer capacity")
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
	_, err := in.NewBuffer(2).Resolve(t.Context(), 2)
	require.ErrorContains(t, err, "content_too_large")
}

func TestWindowEmptyHistoryAndUnusedHistory(t *testing.T) {
	t.Parallel()
	m := message()
	source := &windowMessages{}
	in := &conversationInput{message: m, stored: storedMessage(m), history: source}
	buffer := in.NewBuffer(10)
	_, err := buffer.Resolve(t.Context(), 1)
	require.NoError(t, err)
	require.Zero(t, source.calls)
	entry, err := buffer.Resolve(t.Context(), 10)
	require.NoError(t, err)
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	testenv.RequireYAMLGolden(t, "testdata/state/empty-history.golden.yaml", data)
}

type countingBlobs struct {
	reader BlobReader
	reads  int
}

func (b *countingBlobs) Read(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	b.reads++
	r, err := b.reader.Read(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("read counted asset: %w", err)
	}
	return r, nil
}

func TestTargetFailureIsResolvedOnceAcrossWindowGroups(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		attachment bool
		data       []byte
		retry      bool
	}{
		{name: "invalid content", data: []byte("invalid JSON")},
		{name: "invalid attachment", attachment: true, data: []byte{0xff}},
		{name: "unavailable content", retry: true},
		{name: "unavailable attachment", attachment: true, retry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := message()
			var definitions []Sensor
			for i, size := range []int{1, 2, 5, 10} {
				sensor := sensors()[0]
				sensor.ID = string(rune('a' + i))
				if size > 1 {
					sensor.InputStrategy, sensor.WindowSize = InputStrategySlidingWindow, size
				}
				definitions = append(definitions, sensor)
			}
			reader := sdkmetric.NewManualReader()
			meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meters.Shutdown(context.Background())) })
			h, pub := handler(t, m, definitions, nil, meters)
			target := storedMessage(m)
			uri := "gs://test-bucket/" + m.GetProjectId() + "/target"
			if tc.attachment {
				target.AttachmentUris = []string{uri}
			} else {
				target.ContentAssetUrl = conv.ToPGText(uri)
			}
			blobs := memoryBlobs{}
			if !tc.retry {
				blobs[uri] = tc.data
			}
			counter := &countingBlobs{reader: blobs}
			source := &windowMessages{storedMessages: storedMessages{target.ID: target}}
			h.messages, h.blobs = source, counter
			err := h.Handle(t.Context(), m, gcp.MessageMetadata{})
			if tc.retry {
				require.ErrorContains(t, err, "asset unavailable")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, counter.reads)
			require.Zero(t, source.calls)
			require.Empty(t, pub.readings)
			var metrics metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &metrics))
			var failures int64
			for _, scope := range metrics.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name == "gram.sigint.evaluation.failures" {
						sum, ok := metric.Data.(metricdata.Sum[int64])
						require.True(t, ok)
						for _, point := range sum.DataPoints {
							failures += point.Value
						}
					}
				}
			}
			if tc.retry {
				require.Zero(t, failures)
				blobs[uri] = []byte(`{"text":"available on redelivery"}`)
				h.evaluator.classifier, _ = newRecordingClassifier(t)
				require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
				require.Equal(t, 2, counter.reads, "redelivery creates a fresh buffer")
				require.Len(t, pub.readings, 4)
			} else {
				require.Equal(t, int64(1), failures)
			}
		})
	}
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

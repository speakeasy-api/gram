package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

func (s *windowMessages) LoadHistory(_ context.Context, _ string, _ uuid.UUID, _ repo.LoadEvaluationMessagesRow, limit, _ int) ([]repo.LoadEvaluationHistoryRow, error) {
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
	// Readings record the context actually supplied: one short history serves
	// every window, so the five-message sensor still receives two messages.
	var inputs [][4]any
	for _, r := range pub.readings {
		in := r.GetInput()
		inputs = append(inputs, [4]any{in.GetStrategy(), in.GetRequestedMessageCount(), in.GetMessageCount(), in.GetTruncated()})
	}
	require.Equal(t, [][4]any{
		{InputStrategySingleMessage, int32(1), int32(1), false},
		{InputStrategySlidingWindow, int32(2), int32(2), false},
		{InputStrategySlidingWindow, int32(2), int32(2), false},
		{InputStrategySlidingWindow, int32(5), int32(2), false},
	}, inputs)
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

func TestWindowBudgetFallbackAndObservability(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"overflow marker", "oversized attachment", "short history"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := message()
			var definitions []Sensor
			for i, size := range []int{2, 5, 5, 10} {
				sensor := sensors()[0]
				sensor.ID = string(rune('a' + i))
				sensor.Signals = sensor.Signals[:1]
				sensor.InputStrategy, sensor.WindowSize = InputStrategySlidingWindow, size
				definitions = append(definitions, sensor)
			}
			reader := sdkmetric.NewManualReader()
			meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meters.Shutdown(context.Background())) })
			c, requests := newRecordingClassifier(t)
			h, pub := handler(t, m, definitions, c, meters)
			var logs bytes.Buffer
			evaluator, err := NewEvaluator(slog.New(slog.NewJSONHandler(&logs, nil)), meters, h.evaluator.source, h.evaluator.features, pub, &pub.outcomes, c)
			require.NoError(t, err)
			h.evaluator = evaluator
			target := storedMessage(m)
			target.Content = "Yes, that one."
			prior := targetContent(storedMessage(message()))
			prior.Content, prior.Role = "Most recent context", "assistant"
			history := []repo.LoadEvaluationHistoryRow{prior}
			blobs := &countingBlobs{reader: memoryBlobs{}}
			switch name {
			case "overflow marker":
				history = append(history, repo.LoadEvaluationHistoryRow{ID: uuid.New(), ContentTooLarge: true})
			case "oversized attachment":
				uri := "gs://test-bucket/" + m.GetProjectId() + "/oversized"
				oversized := targetContent(storedMessage(message()))
				oversized.AttachmentUris = []string{uri}
				history = append(history, oversized)
				blobs.reader = memoryBlobs{uri: []byte(strings.Repeat("x", maxContentBytes))}
			}
			if name != "short history" {
				older := targetContent(storedMessage(message()))
				older.Content = "Must not skip over the oversized turn to include this"
				history = append(history, older)
			}
			h.messages = &windowMessages{storedMessages: storedMessages{target.ID: target}, history: history}
			h.blobs = blobs
			require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
			require.Len(t, pub.readings, 4)
			for i, r := range pub.readings {
				in := r.GetInput()
				require.Equal(t, InputStrategySlidingWindow, in.GetStrategy())
				require.Equal(t, int32(definitions[i].WindowSize), in.GetRequestedMessageCount())
				require.Equal(t, int32(2), in.GetMessageCount(), "the budget and short history both leave two messages")
				require.Equal(t, name != "short history" && definitions[i].WindowSize > 2, in.GetTruncated())
			}
			if name == "oversized attachment" {
				require.Equal(t, 1, blobs.reads, "larger windows reuse the truncation boundary")
			}
			testenv.RequireYAMLGolden(t, "testdata/requests/budget-fallback.golden.yaml", requests.snapshot(t))
			var metrics metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &metrics))
			got := make(map[[2]int64]int64)
			for _, scope := range metrics.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name == "gram.sigint.evaluation.window_truncations" {
						sum, ok := metric.Data.(metricdata.Sum[int64])
						require.True(t, ok)
						for _, point := range sum.DataPoints {
							reason, ok := point.Attributes.Value("reason")
							require.True(t, ok)
							require.Equal(t, "content_too_large", reason.AsString())
							requested, ok := point.Attributes.Value("requested_window_size")
							require.True(t, ok)
							effective, ok := point.Attributes.Value("effective_window_size")
							require.True(t, ok)
							got[[2]int64{requested.AsInt64(), effective.AsInt64()}] += point.Value
						}
					}
					if metric.Name == "gram.sigint.evaluation.failures" {
						sum, ok := metric.Data.(metricdata.Sum[int64])
						require.True(t, ok)
						for _, point := range sum.DataPoints {
							require.Zero(t, point.Value, "truncation is not a failed evaluation")
						}
					}
				}
			}
			if name == "short history" {
				require.Empty(t, got)
				require.Empty(t, logs.String())
			} else {
				require.Equal(t, map[[2]int64]int64{{5, 2}: 2, {10, 2}: 1}, got)
				decoder := json.NewDecoder(&logs)
				for i, sensor := range definitions[1:] {
					var record map[string]any
					require.NoError(t, decoder.Decode(&record))
					require.NotEmpty(t, record["time"])
					delete(record, "time")
					require.Equal(t, map[string]any{
						"level":                              "WARN",
						"msg":                                "sensor evaluation window truncated",
						"gram.wide_event":                    true,
						"gram.component":                     "sigint-evaluator",
						"gram.org.id":                        m.GetOrganizationId(),
						"gram.project.id":                    m.GetProjectId(),
						"gram.chat.id":                       m.GetConversationId(),
						"gram.message.id":                    m.GetMessageId(),
						"gram.sigint.event.id":               m.GetMessageId(),
						"gram.sigint.event.kind":             "conversation.message",
						"gram.sigint.sensor.id":              sensor.ID,
						"gram.sigint.sensor.mode":            sensor.Mode,
						"gram.sigint.sensor.definition_hash": pub.readings[i+1].GetDefinitionHash(),
						"gram.sigint.window.requested_size":  float64(sensor.WindowSize),
						"gram.sigint.window.effective_size":  float64(2),
						"gram.sigint.content_budget_bytes":   float64(maxContentBytes),
						"reason":                             "content_too_large",
					}, record)
				}
				var extra map[string]any
				require.ErrorIs(t, decoder.Decode(&extra), io.EOF)
			}
		})
	}
}

func TestWindowAggregateContentLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		targetSize int
		priorSize  int
	}{
		{name: "combined content", targetSize: maxContentBytes / 2, priorSize: maxContentBytes / 2},
		// Both message objects fit without the window envelope, but the complete
		// two-message JSON exceeds the limit. The target-only envelope still fits.
		{name: "window framing", targetSize: maxContentBytes - 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := message()
			target := storedMessage(m)
			target.Content = strings.Repeat("x", tc.targetSize)
			prior := targetContent(storedMessage(message()))
			prior.Content = strings.Repeat("x", tc.priorSize)
			in := &conversationInput{message: m, stored: target, history: &windowMessages{history: []repo.LoadEvaluationHistoryRow{prior}}}
			resolved, err := in.NewBuffer(2).Resolve(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, resolved.Truncated)
			require.Equal(t, 1, resolved.MessageCount)
			data, err := json.Marshal(resolved.State)
			require.NoError(t, err)
			require.LessOrEqual(t, len(data), maxContentBytes)
		})
	}
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
	require.False(t, entry.Truncated, "naturally short histories are not budget truncations")
	data, err := json.Marshal(entry.State)
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
		{name: "oversized attachment", attachment: true, data: []byte(strings.Repeat("x", maxContentBytes))},
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

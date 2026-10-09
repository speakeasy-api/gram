package evaluation

import (
	"context"
	"strings"
	"testing"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSensorMatchingSkipsContentResolution(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := sensors()
	for i := range definitions {
		definitions[i].MatchExpression = `message.role == "assistant"`
	}
	h, pub := handler(t, m, definitions, nil, testenv.NewMeterProvider(t))
	row := storedMessage(m)
	row.ChatMessage.ContentRaw = []byte("invalid json must not be read")
	h.messages = storedMessages{row.ChatMessage.ID: row}
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Empty(t, pub.readings)
}

func TestSensorMatchingIsolatesInvalidPredicates(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := partialSensors()
	definitions[0].MatchExpression = "message.unknown == true"
	definitions[1].MatchExpression = `message.role == "user"`
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.MatchedBy(func(req *classifier.Request) bool { return len(req.Questions) == 1 })).Return(partialResult(false)).Once()
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 1)
}

func TestMatchingExpressionChangesDefinitionNotReadingIdentity(t *testing.T) {
	t.Parallel()
	m := message()
	definitions := partialSensors()[1:]
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false)).Twice()
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	definitions[0].MatchExpression = matching.DefaultExpression
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 2)
	require.NotEqual(t, pub.readings[0].GetDefinitionHash(), pub.readings[1].GetDefinitionHash())
	require.Equal(t, pub.readings[0].GetId(), pub.readings[1].GetId())
}

func TestAssistantSensorMatchesPersistedRole(t *testing.T) {
	t.Parallel()
	m := message()
	m.SetRole(conversationv1.MessageEvent_ROLE_ASSISTANT)
	definitions := partialSensors()
	definitions[0].MatchExpression = matching.DefaultExpression
	definitions[1].MatchExpression = `message.role == "assistant"`
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.MatchedBy(func(req *classifier.Request) bool {
		return len(req.Questions) == 1 && req.Questions[0].Key == "other/a"
	})).Return(partialResult(false)).Once()
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 1)
	require.Equal(t, "other", pub.readings[0].GetSensorId())
}

func TestMatchingCancellationAfterDefinitionLoadRetries(t *testing.T) {
	t.Parallel()
	m := message()
	h, pub := handler(t, m, sensors(), nil, testenv.NewMeterProvider(t))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	deps, ok := h.evaluator.source.(*dependencies)
	require.True(t, ok)
	for _, call := range deps.ExpectedCalls {
		if call.Method == "Load" {
			call.Run(func(mock.Arguments) { cancel() })
		}
	}
	require.ErrorIs(t, h.Handle(ctx, m, gcp.MessageMetadata{}), context.Canceled)
	require.Empty(t, pub.readings)
}

func TestMatchingFailureReasons(t *testing.T) {
	t.Parallel()
	values := "[" + strings.Repeat("1,", 99) + "1]"
	for _, tc := range []struct{ expression, reason string }{
		{`message.unknown == true`, "match_compile"},
		{`message.role`, "match_result_type"},
		{strings.Repeat(" ", matching.MaxExpressionBytes) + "true", "match_expression_size"},
		{`int(message.role) > 0`, "match_evaluation"},
		{values + ".all(x, " + values + ".all(y, x + y > 0))", "match_cost_limit"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meters.Shutdown(context.Background())) })
			m := message()
			definitions := partialSensors()
			definitions[0].MatchExpression = tc.expression
			c := classifiertest.NewMock(t)
			c.On("Classify", mock.Anything, mock.MatchedBy(func(req *classifier.Request) bool {
				return len(req.Questions) == 1 && req.Questions[0].Key == "other/a"
			})).Return(partialResult(false)).Once()
			h, pub := handler(t, m, definitions, c, meters)
			require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
			require.Len(t, pub.readings, 1)
			var data metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &data))
			found := false
			for _, scope := range data.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name != "gram.sigint.evaluation.failures" {
						continue
					}
					sum, ok := metric.Data.(metricdata.Sum[int64])
					require.True(t, ok)
					require.Len(t, sum.DataPoints, 1)
					reason, ok := sum.DataPoints[0].Attributes.Value("reason")
					require.True(t, ok)
					require.Equal(t, tc.reason, reason.AsString())
					require.Equal(t, int64(1), sum.DataPoints[0].Value)
					found = true
				}
			}
			require.True(t, found)
		})
	}
}

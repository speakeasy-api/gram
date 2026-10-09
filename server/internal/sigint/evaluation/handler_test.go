package evaluation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/classifier/jev"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type dependencies struct{ mock.Mock }

func (d *dependencies) Load(ctx context.Context, org string, project uuid.UUID, kind string) ([]Sensor, error) {
	args := d.Called(ctx, org, project, kind)
	value, ok := args.Get(0).([]Sensor)
	if !ok {
		panic("expected []Sensor")
	}
	return value, args.Error(1)
}

func (d *dependencies) IsFeatureEnabled(ctx context.Context, org string, feature productfeatures.Feature) (bool, error) {
	args := d.Called(ctx, org, feature)
	return args.Bool(0), args.Error(1)
}

type capturePublisher struct {
	readings []*sigintv1.Reading
	err      error
}

func (p *capturePublisher) Publish(_ context.Context, reading *sigintv1.Reading, _ ...gcp.PublishOption) gcp.PublishResult {
	p.readings = append(p.readings, reading)
	return gcp.NewErrPublishResult(p.err)
}
func (p *capturePublisher) Stop(context.Context) error { return nil }

func message() *conversationv1.MessageEvent {
	m := &conversationv1.MessageEvent{}
	m.SetId(uuid.NewString())
	m.SetMessageId(uuid.NewString())
	m.SetType(conversationv1.MessageEvent_TYPE_CREATED)
	m.SetConversationId(uuid.NewString())
	m.SetProjectId(uuid.NewString())
	m.SetOrganizationId("test-organization")
	m.SetMessageCreatedAt("2026-09-28T12:00:00.123456789Z")
	m.SetRole(conversationv1.MessageEvent_ROLE_USER)
	return m
}

type storedMessages map[uuid.UUID]repo.LoadEvaluationMessagesRow

func (s storedMessages) LoadMessages(_ context.Context, _ string, project uuid.UUID, ids []uuid.UUID) ([]repo.LoadEvaluationMessagesRow, error) {
	var rows []repo.LoadEvaluationMessagesRow
	for _, id := range ids {
		if row, ok := s[id]; ok && row.ChatMessage.ProjectID.UUID == project {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func storedMessage(m *conversationv1.MessageEvent) repo.LoadEvaluationMessagesRow {
	var row repo.LoadEvaluationMessagesRow
	row.ChatMessage.ID = uuid.MustParse(m.GetMessageId())
	row.ChatMessage.ChatID = uuid.MustParse(m.GetConversationId())
	row.ChatMessage.ProjectID = uuid.NullUUID{UUID: uuid.MustParse(m.GetProjectId()), Valid: true}
	row.ChatMessage.Role = strings.TrimPrefix(strings.ToLower(m.GetRole().String()), "role_")
	row.ChatMessage.Content = "Please help with a failed payment"
	created, _ := time.Parse(time.RFC3339Nano, m.GetMessageCreatedAt())
	row.ChatMessage.CreatedAt = conv.ToPGTimestamptz(created)
	return row
}

func sensors() []Sensor {
	instructions := "Evaluate the message"
	options := []classifier.Option{classifier.NewOption("a", classifier.Text("first")), classifier.NewOption("b", classifier.Text("second"))}
	return []Sensor{
		{ID: "multi", Slug: "multi-label", Mode: "multi_label", MatchExpression: "true", Instructions: &instructions, Signals: options, SignalSlugs: map[classifier.OptionKey]string{"a": "first", "b": "second"}},
		{ID: "choice", Slug: "exclusive", Mode: "exclusive", MatchExpression: "true", Instructions: &instructions, Signals: options, SignalSlugs: map[classifier.OptionKey]string{"a": "first", "b": "second"}},
		{ID: "score", Slug: "ordered-score", Mode: "ordered_score", MatchExpression: "true", Instructions: &instructions, Signals: options, SignalSlugs: map[classifier.OptionKey]string{"a": "first", "b": "second"}},
	}
}

func TestCompileSensorRequiresAllSlugs(t *testing.T) {
	t.Parallel()
	for _, mode := range []int{0, 1, 2} {
		t.Run(sensors()[mode].Mode, func(t *testing.T) {
			t.Parallel()
			sensor := sensors()[mode]
			sensor.Slug = ""
			_, ready := compileSensor(sensor)
			require.False(t, ready)
			sensor = sensors()[mode]
			delete(sensor.SignalSlugs, "b")
			_, ready = compileSensor(sensor)
			require.False(t, ready)
		})
	}
}

func handler(t *testing.T, m *conversationv1.MessageEvent, definitions []Sensor, c classifier.Classifier, meters metric.MeterProvider) (*ConversationHandler, *capturePublisher) {
	t.Helper()
	var deps dependencies
	deps.Test(t)
	t.Cleanup(func() { deps.AssertExpectations(t) })
	deps.On("IsFeatureEnabled", mock.Anything, m.GetOrganizationId(), productfeatures.FeatureSignalsIntelligence).Return(true, nil)
	deps.On("Load", mock.Anything, m.GetOrganizationId(), uuid.MustParse(m.GetProjectId()), ConversationMessageKind).Return(definitions, nil)
	var pub capturePublisher

	evaluator, err := NewEvaluator(testenv.NewLogger(t), meters, &deps, &deps, &pub, c)
	require.NoError(t, err)

	row := storedMessage(m)
	return NewConversationHandler(evaluator, nil, storedMessages{row.ChatMessage.ID: row}), &pub
}

func TestHandlerJevAllModesAndStableRedelivery(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		answers := map[string]any{}
		for key, q := range req.Questions {
			switch q.Type {
			case "noul":
				answers[key] = map[string]any{"type": "noul", "noul": 0.8}
			case "choice":
				answers[key] = map[string]any{"type": "choice", "choice": "1", "probabilities": map[string]float64{"0": 0.2, "1": 0.8}, "confidence": 0.5}
			case "score":
				answers[key] = map[string]any{"type": "score", "score": 0.8, "probabilities": map[string]float64{"0": 0.2, "1": 0.8}, "confidence": 0.5}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test-model", "answers": answers, "usage": map[string]int{"input_tokens": 100, "output_tokens": 20}})
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)

	c, err := jev.New(policy, conv.NewSecret([]byte("test-key")), jev.WithEndpoint(server.URL))
	require.NoError(t, err)
	m := message()
	m.SetRole(conversationv1.MessageEvent_ROLE_ASSISTANT)
	provenance := &conversationv1.MessageEvent_IngestionContext{}
	provenance.SetReplayed(true)
	provenance.SetObservedUserEmail("actor@example.test")
	provenance.SetBillingUserId("billing-user")
	provenance.SetSource("original")
	provenance.SetAssistantId(uuid.NewString())
	accountID := uuid.New()
	provenance.SetAccountType("team")
	provenance.SetBillingMode("flat_rate")
	m.SetIngestion(provenance)
	h, pub := handler(t, m, sensors(), c, testenv.NewMeterProvider(t))
	row := storedMessage(m)
	row.ChatMessage.UserID = conv.ToPGText("message-user")
	row.ChatMessage.ExternalUserID = conv.ToPGText("external-user")
	row.UserAccountID = uuid.NullUUID{UUID: accountID, Valid: true}
	h.messages = storedMessages{row.ChatMessage.ID: row}
	var meta gcp.MessageMetadata
	require.NoError(t, h.Handle(t.Context(), m, meta))
	provenance.SetSource("promoted")
	provenance.SetBillingUserId("promoted-billing-user")
	require.NoError(t, h.Handle(t.Context(), m, meta))
	require.Equal(t, 2, calls)
	require.Len(t, pub.readings, 6)
	for i := range 3 {
		a, b := pub.readings[i], pub.readings[i+3]
		require.Equal(t, a.GetId(), b.GetId())
		require.NotEqual(t, a.GetEvaluationAttemptId(), b.GetEvaluationAttemptId())
		require.Equal(t, sigintv1.Reading_ConversationMessage_ROLE_ASSISTANT, a.GetEvent().GetConversationMessage().GetRole())
		require.Equal(t, []string{"test-model"}, a.GetModels())
		require.Equal(t, "openrouter", a.GetProvider())
		require.Equal(t, m.GetMessageCreatedAt(), a.GetEvent().GetOccurredAt())
		require.Equal(t, "message-user", a.GetActor().GetUserId())
		require.Equal(t, "external-user", a.GetActor().GetExternalUserId())
		require.Equal(t, "actor@example.test", a.GetActor().GetUserEmail())
		require.Equal(t, "billing-user", a.GetBillingUserId())
		require.Equal(t, "promoted-billing-user", b.GetBillingUserId())
		require.Equal(t, "original", a.GetSource())
		require.Equal(t, "promoted", b.GetSource())
		require.Equal(t, provenance.GetAssistantId(), a.GetAssistantId())
		require.Equal(t, accountID.String(), a.GetAccount().GetUserAccountId())
		require.Equal(t, "team", a.GetAccount().GetAccountType())
		require.Equal(t, "flat_rate", a.GetAccount().GetBillingMode())
		require.True(t, a.GetReplayed())
		require.Equal(t, a.GetDefinitionHash(), b.GetDefinitionHash())
	}
	require.Len(t, pub.readings[0].GetMultiLabel().GetSignals(), 2)
	require.Equal(t, "multi-label", pub.readings[0].GetSensorSlug())
	require.Equal(t, "first", pub.readings[0].GetMultiLabel().GetSignals()[0].GetSignalSlug())
	require.Equal(t, "second", pub.readings[0].GetMultiLabel().GetSignals()[1].GetSignalSlug())
	require.Equal(t, "exclusive", pub.readings[1].GetSensorSlug())
	require.Equal(t, "second", pub.readings[1].GetChoice().GetSelectedSignalSlug())
	require.Equal(t, "first", pub.readings[1].GetChoice().GetDistribution()[0].GetSignalSlug())
	require.Equal(t, "second", pub.readings[1].GetChoice().GetDistribution()[1].GetSignalSlug())
	require.Equal(t, "ordered-score", pub.readings[2].GetSensorSlug())
	require.Equal(t, "first", pub.readings[2].GetScore().GetDistribution()[0].GetSignalSlug())
	require.Equal(t, "second", pub.readings[2].GetScore().GetDistribution()[1].GetSignalSlug())
	require.Equal(t, "b", pub.readings[1].GetChoice().GetSelectedSignalId())
	require.InDelta(t, 0.8, pub.readings[2].GetScore().GetExpectedIndex(), 1e-9)
}

func partialResult(retryable bool) classifier.Result {
	var answer classifier.Answer
	answer.Noul = &classifier.NoulAnswer{Probability: 0.5}
	return classifier.NewResult([]classifier.QuestionOutcome{
		{Key: "multi/a", Answer: &answer, Failure: nil},
		{Key: "multi/b", Answer: nil, Failure: &classifier.QuestionFailure{Code: classifier.FailureRateLimited, Message: "failure", Retryable: retryable, RetryAfter: nil}},
		{Key: "other/a", Answer: &answer, Failure: nil},
	}, nil)
}

func partialSensors() []Sensor {
	definitions := sensors()[:1]
	other := definitions[0]
	other.ID = "other"
	other.Signals = other.Signals[:1]
	return append(definitions, other)
}

func TestHandlerPartialSuccessNacksTransientFailure(t *testing.T) {
	t.Parallel()
	m := message()
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(true)).Once()
	h, pub := handler(t, m, partialSensors(), c, testenv.NewMeterProvider(t))
	var meta gcp.MessageMetadata
	require.Error(t, h.Handle(t.Context(), m, meta))
	require.Len(t, pub.readings, 1)
	require.Equal(t, "other", pub.readings[0].GetSensorId())
}

func TestHandlerPermanentFailureAcksAndEmitsMetric(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meters.Shutdown(context.Background())) })
	m := message()
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false)).Once()
	h, pub := handler(t, m, partialSensors(), c, meters)
	var meta gcp.MessageMetadata
	require.NoError(t, h.Handle(t.Context(), m, meta))
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
			require.Equal(t, int64(1), sum.DataPoints[0].Value)
			found = true
		}
	}
	require.True(t, found)
}

func TestHandlerOperationFailureSemantics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"disabled", classifier.ErrDisabled, false},
		{"invalid", classifier.ErrInvalidRequest, false},
		{"unavailable", errors.New("unavailable"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meters.Shutdown(context.Background())) })
			m := message()
			c := classifiertest.NewMock(t)
			partial := partialResult(false)
			// Retain the successful sensor; the other sensor has no outcomes.
			result := classifier.NewResult(partial.Outcomes[len(partial.Outcomes)-1:], fmt.Errorf("wrapped: %w", tc.err))
			c.On("Classify", mock.Anything, mock.Anything).Return(result).Once()
			h, pub := handler(t, m, partialSensors(), c, meters)

			err := h.Handle(t.Context(), m, gcp.MessageMetadata{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, pub.readings, 1)
			require.Equal(t, "other", pub.readings[0].GetSensorId())

			var data metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &data))
			var failures int64
			for _, scope := range data.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name == "gram.sigint.evaluation.failures" {
						data, ok := metric.Data.(metricdata.Sum[int64])
						require.True(t, ok)
						for _, point := range data.DataPoints {
							failures += point.Value
						}
					}
				}
			}
			if tc.retry {
				require.Zero(t, failures)
			} else {
				require.Equal(t, int64(1), failures)
			}
		})
	}
}

func TestHandlerOversizedReadingIsPermanentAndPreservesOtherReadings(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meters.Shutdown(context.Background())) })
	definitions := partialSensors()
	// Inflate one signal identifier to exercise the real serialized-size boundary
	// without constructing hundreds of thousands of classifier outcomes.
	largeKey := classifier.OptionKey(strings.Repeat("a", maxReadingBytes))
	definitions[0].Signals = []classifier.Option{classifier.NewOption(largeKey, classifier.Text("large"))}
	definitions[0].SignalSlugs = map[classifier.OptionKey]string{largeKey: "large"}
	var answer classifier.Answer
	answer.Noul = &classifier.NoulAnswer{Probability: 0.5}
	result := classifier.NewResult([]classifier.QuestionOutcome{
		{Key: classifier.QuestionKey("multi/" + string(largeKey)), Answer: &answer, Failure: nil},
		{Key: "other/a", Answer: &answer, Failure: nil},
	}, nil)
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(result).Once()
	m := message()
	h, pub := handler(t, m, definitions, c, meters)
	var meta gcp.MessageMetadata
	require.NoError(t, h.Handle(t.Context(), m, meta))
	require.Len(t, pub.readings, 1)
	require.Equal(t, "other", pub.readings[0].GetSensorId())
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
			require.Equal(t, int64(1), sum.DataPoints[0].Value)
			reason, ok := sum.DataPoints[0].Attributes.Value("reason")
			require.True(t, ok)
			require.Equal(t, "reading_too_large", reason.AsString())
			found = true
		}
	}
	require.True(t, found)
}

func TestHandlerPublishFailureNacks(t *testing.T) {
	t.Parallel()
	m := message()
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false)).Once()
	h, pub := handler(t, m, partialSensors(), c, testenv.NewMeterProvider(t))
	pub.err = errors.New("publish unavailable")
	var meta gcp.MessageMetadata
	require.ErrorContains(t, h.Handle(t.Context(), m, meta), "publish sensor reading")
}

func TestHandlerSkipsDisabledAndOtherRoles(t *testing.T) {
	t.Parallel()
	var deps dependencies
	deps.Test(t)
	t.Cleanup(func() { deps.AssertExpectations(t) })
	m := message()
	deps.On("IsFeatureEnabled", mock.Anything, m.GetOrganizationId(), productfeatures.FeatureSignalsIntelligence).Return(false, nil).Once()

	evaluator, err := NewEvaluator(testenv.NewLogger(t), testenv.NewMeterProvider(t), &deps, &deps, nil, nil)
	require.NoError(t, err)

	row := storedMessage(m)
	h := NewConversationHandler(evaluator, nil, storedMessages{row.ChatMessage.ID: row})
	var meta gcp.MessageMetadata
	require.NoError(t, h.Handle(t.Context(), m, meta))
	m.SetRole(conversationv1.MessageEvent_ROLE_TOOL)
	require.NoError(t, h.Handle(t.Context(), m, meta))
}

func TestHandlerEntitlementFailureNacks(t *testing.T) {
	t.Parallel()
	m := message()
	var deps dependencies
	deps.Test(t)
	t.Cleanup(func() { deps.AssertExpectations(t) })
	deps.On("IsFeatureEnabled", mock.Anything, mock.Anything, mock.Anything).Return(false, fmt.Errorf("lookup unavailable")).Once()

	evaluator, err := NewEvaluator(testenv.NewLogger(t), testenv.NewMeterProvider(t), &deps, &deps, nil, nil)
	require.NoError(t, err)

	row := storedMessage(m)
	h := NewConversationHandler(evaluator, nil, storedMessages{row.ChatMessage.ID: row})
	var meta gcp.MessageMetadata
	require.ErrorContains(t, h.Handle(t.Context(), m, meta), "check evaluation entitlement")
}

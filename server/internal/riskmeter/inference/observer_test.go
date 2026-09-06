package inference_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/riskmeter"
	"github.com/speakeasy-api/gram/server/internal/riskmeter/inference"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestObserverMapsPhysicalAttemptsFromRiskContext(t *testing.T) {
	t.Parallel()

	publisher := &capturePublisher{messages: nil}
	observer := inference.NewObserver(riskmeter.NewRecorder(testenv.NewLogger(t), publisher))
	startedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	evaluation := riskmeter.Evaluation{
		OrganizationID: "test-org",
		ProjectID:      "b5f82eb4-982d-4db3-8f3a-78784255c4d4",
		OperationID:    "logical-evaluation",
		Detector:       riskmeter.DetectorPromptPolicy,
		ExecutionMode:  riskmeter.ModeRealtime,
		PolicyID:       "policy-test",
		PolicyVersion:  3,
		OccurredAt:     startedAt.Add(-time.Hour),
	}
	ctx := riskmeter.WithEvaluation(t.Context(), evaluation)
	promptTokens := int64(17)
	completionTokens := int64(3)
	costUSD := 0.01

	require.NoError(t, observer.ObserveCompletionAttempt(ctx, openrouter.CompletionAttempt{
		StartedAt:         startedAt,
		CompletedAt:       startedAt.Add(time.Second),
		RequestedModel:    "requested-model",
		ReturnedModel:     "returned-model",
		ProviderRequestID: "provider-success",
		PromptTokens:      &promptTokens,
		CompletionTokens:  &completionTokens,
		CostUSD:           &costUSD,
		Status:            openrouter.CompletionAttemptSucceeded,
		Err:               nil,
	}))
	require.NoError(t, observer.ObserveCompletionAttempt(ctx, openrouter.CompletionAttempt{
		StartedAt:         startedAt.Add(2 * time.Second),
		CompletedAt:       startedAt.Add(3 * time.Second),
		RequestedModel:    "fallback-model",
		ReturnedModel:     "",
		ProviderRequestID: "provider-failed",
		PromptTokens:      nil,
		CompletionTokens:  nil,
		CostUSD:           nil,
		Status:            openrouter.CompletionAttemptFailed,
		Err:               nil,
	}))
	require.NoError(t, observer.ObserveCompletionAttempt(ctx, openrouter.CompletionAttempt{
		StartedAt:         startedAt.Add(4 * time.Second),
		CompletedAt:       startedAt.Add(5 * time.Second),
		RequestedModel:    "cancelled-model",
		ReturnedModel:     "",
		ProviderRequestID: "",
		PromptTokens:      nil,
		CompletionTokens:  nil,
		CostUSD:           nil,
		Status:            openrouter.CompletionAttemptCancelled,
		Err:               context.Canceled,
	}))

	require.Len(t, publisher.messages, 3)
	require.Equal(t, meteringv1.RiskEvaluation_OUTCOME_COMPLETED, publisher.messages[0].GetOutcome())
	require.Equal(t, meteringv1.RiskEvaluation_OUTCOME_FAILED, publisher.messages[1].GetOutcome())
	require.Equal(t, meteringv1.RiskEvaluation_OUTCOME_CANCELLED, publisher.messages[2].GetOutcome())
	require.Equal(t, "returned-model", publisher.messages[0].GetModel())
	require.Equal(t, "fallback-model", publisher.messages[1].GetModel())
	require.Equal(t, startedAt.Format(time.RFC3339Nano), publisher.messages[0].GetOccurredAt())
	require.Equal(t, startedAt.Add(2*time.Second).Format(time.RFC3339Nano), publisher.messages[1].GetOccurredAt())
	require.Equal(t, int64(17), publisher.messages[0].GetPromptTokens())
	require.Equal(t, int64(3), publisher.messages[0].GetCompletionTokens())
	require.InEpsilon(t, 0.01, publisher.messages[0].GetCostUsd(), 1e-12)
	require.False(t, publisher.messages[1].HasPromptTokens())
	require.False(t, publisher.messages[1].HasCompletionTokens())
	require.False(t, publisher.messages[1].HasCostUsd())
	for _, message := range publisher.messages {
		require.Equal(t, riskmeter.RecordKindInference, message.GetRecordKind())
		require.False(t, message.HasStokens())
		require.Equal(t, evaluation.OperationID, message.GetOperationId())
	}
}

func TestObserverIgnoresAttemptsOutsideRiskEvaluation(t *testing.T) {
	t.Parallel()

	publisher := &capturePublisher{messages: nil}
	observer := inference.NewObserver(riskmeter.NewRecorder(testenv.NewLogger(t), publisher))
	require.NoError(t, observer.ObserveCompletionAttempt(t.Context(), openrouter.CompletionAttempt{
		StartedAt:         time.Now().UTC(),
		CompletedAt:       time.Now().UTC(),
		RequestedModel:    "model",
		ReturnedModel:     "model",
		ProviderRequestID: "provider-request",
		PromptTokens:      nil,
		CompletionTokens:  nil,
		CostUSD:           nil,
		Status:            openrouter.CompletionAttemptSucceeded,
		Err:               nil,
	}))
	require.Empty(t, publisher.messages)
}

type capturePublisher struct {
	messages []*meteringv1.RiskEvaluation
}

func (p *capturePublisher) Publish(_ context.Context, message *meteringv1.RiskEvaluation, _ ...gcp.PublishOption) gcp.PublishResult {
	p.messages = append(p.messages, message)
	return gcp.NewSuccessPublishResult()
}

func (*capturePublisher) Stop(context.Context) error { return nil }

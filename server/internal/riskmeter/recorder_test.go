package riskmeter_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/riskmeter"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureRiskPublisher struct {
	messages []*meteringv1.RiskEvaluation
}

func (p *captureRiskPublisher) Publish(_ context.Context, message *meteringv1.RiskEvaluation, _ ...gcp.PublishOption) gcp.PublishResult {
	p.messages = append(p.messages, message)
	return gcp.NewSuccessPublishResult()
}

func (p *captureRiskPublisher) Stop(context.Context) error {
	return nil
}

func TestLogicalIdentityUsesStableLengthFraming(t *testing.T) {
	t.Parallel()

	evaluation := testEvaluation()
	require.Equal(t, "riskop:v1:263386149a3516170cb5ef4abe39a3c21dbb05a97e5085a0a5035b33278ea2d4", evaluation.OperationID)
	require.Equal(t, "6d2ddb8d-5212-5c5c-b7e3-98531f3cc916", riskmeter.EvaluationID(evaluation))
	require.NotEqual(t, riskmeter.OperationID("ab", "c"), riskmeter.OperationID("a", "bc"))
}

func TestCanonicalLabelsRejectUnspecifiedAndUnknownEnums(t *testing.T) {
	t.Parallel()

	detector, ok := riskmeter.DetectorLabel(riskmeter.DetectorGitleaks)
	require.True(t, ok)
	require.Equal(t, "gitleaks", detector)
	mode, ok := riskmeter.ExecutionModeLabel(riskmeter.ModeBatch)
	require.True(t, ok)
	require.Equal(t, "batch", mode)
	outcome, ok := riskmeter.OutcomeLabel(riskmeter.OutcomeCompleted)
	require.True(t, ok)
	require.Equal(t, "completed", outcome)

	_, ok = riskmeter.DetectorLabel(meteringv1.RiskEvaluation_DETECTOR_UNSPECIFIED)
	require.False(t, ok)
	_, ok = riskmeter.ExecutionModeLabel(meteringv1.RiskEvaluation_ExecutionMode(99))
	require.False(t, ok)
	require.Error(t, riskmeter.ValidateOutcome(meteringv1.RiskEvaluation_OUTCOME_UNSPECIFIED))

	evaluation := testEvaluation()
	evaluation.Detector = meteringv1.RiskEvaluation_DETECTOR_UNSPECIFIED
	require.Error(t, riskmeter.ValidateEvaluation(evaluation))
	evaluation = testEvaluation()
	evaluation.ExecutionMode = meteringv1.RiskEvaluation_ExecutionMode(99)
	require.Error(t, riskmeter.ValidateEvaluation(evaluation))
}

func TestRecorderPublishesUnknownScanAfterCallerCancellation(t *testing.T) {
	t.Parallel()

	publisher := &captureRiskPublisher{messages: nil}
	recorder := riskmeter.NewRecorder(testenv.NewLogger(t), publisher)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.NoError(t, recorder.Record(ctx, testEvaluation(), nil, riskmeter.OutcomeCancelled, nil))
	require.Len(t, publisher.messages, 1)
	message := publisher.messages[0]
	require.Equal(t, riskmeter.RecordKindScan, message.GetRecordKind())
	require.False(t, message.GetMeasurementError(), "an unmeasured cancellation is not a tokenizer failure")
	require.False(t, message.HasStokens())
}

func TestRecorderPreservesKnownCleanZero(t *testing.T) {
	t.Parallel()

	publisher := &captureRiskPublisher{messages: nil}
	recorder := riskmeter.NewRecorder(testenv.NewLogger(t), publisher)

	require.NoError(t, recorder.Record(t.Context(), testEvaluation(), []string{}, riskmeter.OutcomeCompleted, nil))
	require.Len(t, publisher.messages, 1)
	message := publisher.messages[0]
	require.True(t, message.HasStokens())
	require.Zero(t, message.GetStokens())
	require.False(t, message.GetMeasurementError())
}

func TestRecorderKeepsUnknownInferenceAccountingNullable(t *testing.T) {
	t.Parallel()

	publisher := &captureRiskPublisher{messages: nil}
	recorder := riskmeter.NewRecorder(testenv.NewLogger(t), publisher)

	inference := &riskmeter.Inference{
		Model:             "openai/gpt-5",
		ProviderRequestID: "",
		PromptTokens:      nil,
		CompletionTokens:  nil,
		CostUSD:           nil,
	}
	require.NoError(t, recorder.RecordInference(t.Context(), testEvaluation(), riskmeter.OutcomeFailed, inference))
	require.Len(t, publisher.messages, 1)
	message := publisher.messages[0]
	require.Equal(t, riskmeter.RecordKindInference, message.GetRecordKind())
	require.False(t, message.HasPromptTokens())
	require.False(t, message.HasCompletionTokens())
	require.False(t, message.HasCostUsd())
	require.False(t, message.HasStokens())
}

func testEvaluation() riskmeter.Evaluation {
	return riskmeter.Evaluation{
		OrganizationID: "org_test",
		ProjectID:      "11111111-1111-1111-1111-111111111111",
		OperationID:    riskmeter.OperationID("message", "a:b", "3"),
		Detector:       riskmeter.DetectorGitleaks,
		ExecutionMode:  riskmeter.ModeBatch,
		PolicyID:       "policy_test",
		PolicyVersion:  3,
		OccurredAt:     time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC),
	}
}

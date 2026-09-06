package consumer_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	meteringchrepo "github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/riskmeter"
	"github.com/speakeasy-api/gram/server/internal/riskmeter/chrepo"
	"github.com/speakeasy-api/gram/server/internal/riskmeter/consumer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureEvaluationInserter struct {
	rows []chrepo.EvaluationRow
}

func (c *captureEvaluationInserter) InsertRiskEvaluations(_ context.Context, rows []chrepo.EvaluationRow) error {
	c.rows = append(c.rows, rows...)
	return nil
}

type captureReadingInserter struct {
	rows []meteringchrepo.ReadingRow
}

func (c *captureReadingInserter) InsertReadings(_ context.Context, rows []meteringchrepo.ReadingRow) error {
	c.rows = append(c.rows, rows...)
	return nil
}

func TestWriterPreservesRetriesAndProjectsLogicalVolumeOnce(t *testing.T) {
	t.Parallel()

	evaluation := testEvaluation()
	first := testRiskEvaluation(uuid.NewString(), evaluation, riskmeter.OutcomeCompleted, riskmeter.RecordKindScan)
	first.SetStokens(12)
	second := testRiskEvaluation(uuid.NewString(), evaluation, riskmeter.OutcomeCompleted, riskmeter.RecordKindScan)
	second.SetStokens(12)
	raw := &captureEvaluationInserter{rows: nil}
	readings := &captureReadingInserter{rows: nil}
	writer := consumer.NewRiskEvaluationCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), raw, readings)

	require.NoError(t, writer.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*meteringv1.RiskEvaluation]{{Message: first}, {Message: second}}))
	require.Len(t, raw.rows, 2)
	require.NotEqual(t, raw.rows[0].ID, raw.rows[1].ID)
	require.Equal(t, raw.rows[0].EvaluationID, raw.rows[1].EvaluationID)
	require.Len(t, readings.rows, 1)
	require.Equal(t, int64(12), readings.rows[0].Value)
	require.Equal(t, evaluation.OperationID, raw.rows[0].OperationID)
	require.Equal(t, "gitleaks", raw.rows[0].Detector)
	require.Equal(t, "batch", raw.rows[0].ExecutionMode)
	require.Equal(t, "completed", raw.rows[0].Outcome)
	require.Equal(t, riskmeter.EvaluationID(evaluation), readings.rows[0].OperationID)
}

func TestWriterPersistsCompletedCleanZeroWithoutVolume(t *testing.T) {
	t.Parallel()

	message := testRiskEvaluation(uuid.NewString(), testEvaluation(), riskmeter.OutcomeCompleted, riskmeter.RecordKindScan)
	message.SetStokens(0)
	raw := &captureEvaluationInserter{rows: nil}
	readings := &captureReadingInserter{rows: nil}
	writer := consumer.NewRiskEvaluationCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), raw, readings)

	require.NoError(t, writer.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*meteringv1.RiskEvaluation]{{Message: message}}))
	require.Len(t, raw.rows, 1)
	require.NotNil(t, raw.rows[0].STokens)
	require.Zero(t, *raw.rows[0].STokens)
	require.Empty(t, readings.rows)
}

func TestWriterPersistsUnknownScanAndAcknowledgesPoison(t *testing.T) {
	t.Parallel()

	message := testRiskEvaluation(uuid.NewString(), testEvaluation(), riskmeter.OutcomeSkipped, riskmeter.RecordKindScan)
	zeroID := testRiskEvaluation(uuid.Nil.String(), testEvaluation(), riskmeter.OutcomeSkipped, riskmeter.RecordKindScan)
	zeroProjectEvaluation := testEvaluation()
	zeroProjectEvaluation.ProjectID = uuid.Nil.String()
	zeroProject := testRiskEvaluation(uuid.NewString(), zeroProjectEvaluation, riskmeter.OutcomeSkipped, riskmeter.RecordKindScan)
	unknownDetector := testRiskEvaluation(uuid.NewString(), testEvaluation(), riskmeter.OutcomeSkipped, riskmeter.RecordKindScan)
	unknownDetector.SetDetector(meteringv1.RiskEvaluation_Detector(99))
	unknownOutcome := testRiskEvaluation(uuid.NewString(), testEvaluation(), riskmeter.OutcomeSkipped, riskmeter.RecordKindScan)
	unknownOutcome.SetOutcome(meteringv1.RiskEvaluation_Outcome(99))
	raw := &captureEvaluationInserter{rows: nil}
	readings := &captureReadingInserter{rows: nil}
	writer := consumer.NewRiskEvaluationCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), raw, readings)

	require.NoError(t, writer.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*meteringv1.RiskEvaluation]{
		{Message: zeroID},
		{Message: zeroProject},
		{Message: unknownDetector},
		{Message: unknownOutcome},
		{Message: message},
	}))
	require.Len(t, raw.rows, 1)
	require.Nil(t, raw.rows[0].STokens)
	require.False(t, raw.rows[0].MeasurementError)
	require.Empty(t, readings.rows)
}

func TestWriterPreservesUnknownInferenceAccountingWithoutVolume(t *testing.T) {
	t.Parallel()

	message := testRiskEvaluation(uuid.NewString(), testEvaluation(), riskmeter.OutcomeFailed, riskmeter.RecordKindInference)
	message.SetModel("openai/gpt-5")
	raw := &captureEvaluationInserter{rows: nil}
	readings := &captureReadingInserter{rows: nil}
	writer := consumer.NewRiskEvaluationCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), raw, readings)

	require.NoError(t, writer.HandleBatchWithResult(t.Context(), []gcp.BatchMessage[*meteringv1.RiskEvaluation]{{Message: message}}))
	require.Len(t, raw.rows, 1)
	require.Nil(t, raw.rows[0].CostUSD)
	require.Nil(t, raw.rows[0].PromptTokens)
	require.Nil(t, raw.rows[0].CompletionTokens)
	require.Nil(t, raw.rows[0].STokens)
	require.Empty(t, readings.rows)
}

func testRiskEvaluation(id string, evaluation riskmeter.Evaluation, outcome meteringv1.RiskEvaluation_Outcome, recordKind string) *meteringv1.RiskEvaluation {
	message := new(meteringv1.RiskEvaluation)
	message.SetId(id)
	message.SetEvaluationId(riskmeter.EvaluationID(evaluation))
	message.SetOrganizationId(evaluation.OrganizationID)
	message.SetProjectId(evaluation.ProjectID)
	message.SetOperationId(evaluation.OperationID)
	message.SetDetector(evaluation.Detector)
	message.SetExecutionMode(evaluation.ExecutionMode)
	message.SetOutcome(outcome)
	message.SetRecordKind(recordKind)
	message.SetOccurredAt(evaluation.OccurredAt.Format(time.RFC3339Nano))
	message.SetProducedAt(evaluation.OccurredAt.Add(time.Second).Format(time.RFC3339Nano))
	message.SetMeasurementMethod(riskmeter.MeasurementMethod)
	message.SetPolicyId(evaluation.PolicyID)
	message.SetPolicyVersion(evaluation.PolicyVersion)
	return message
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

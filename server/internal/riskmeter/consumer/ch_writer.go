// Package consumer persists risk evaluation events and projects billable volume.
package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/metering"
	meteringchrepo "github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/riskmeter"
	riskmeterchrepo "github.com/speakeasy-api/gram/server/internal/riskmeter/chrepo"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

const (
	meterWriterSkipped  = "gram.risk_evaluation_ch_writer.records_skipped"
	meterWriterInserted = "gram.risk_evaluation_ch_writer.records_inserted"
)

// EvaluationInserter persists validated raw risk evaluation measurements.
type EvaluationInserter interface {
	InsertRiskEvaluations(context.Context, []riskmeterchrepo.EvaluationRow) error
}

// RiskEvaluationCHWriter persists raw terminal records and projects positive
// completed scan volume into the existing workload meter ledger.
type RiskEvaluationCHWriter struct {
	logger             *slog.Logger
	evaluationInserter EvaluationInserter
	readingInserter    metering.ReadingInserter
	recordsSkipped     metric.Int64Counter
	recordsInserted    metric.Int64Counter
}

// NewRiskEvaluationCHWriter creates the risk measurement ClickHouse subscriber.
func NewRiskEvaluationCHWriter(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	evaluationInserter EvaluationInserter,
	readingInserter metering.ReadingInserter,
) *RiskEvaluationCHWriter {
	logger = logger.With(attr.SlogComponent("risk-evaluation-ch-writer"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/riskmeter")
	skipped, err := meter.Int64Counter(meterWriterSkipped, metric.WithDescription("Risk measurement messages dropped as unprocessable"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterWriterSkipped), attr.SlogError(err))
	}
	inserted, err := meter.Int64Counter(meterWriterInserted, metric.WithDescription("Risk measurement rows attempted for ClickHouse insertion"))
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterWriterInserted), attr.SlogError(err))
	}
	return &RiskEvaluationCHWriter{
		logger:             logger,
		evaluationInserter: evaluationInserter,
		readingInserter:    readingInserter,
		recordsSkipped:     skipped,
		recordsInserted:    inserted,
	}
}

var _ streams.BatchResultHandler[*meteringv1.RiskEvaluation] = (*RiskEvaluationCHWriter)(nil)

// HandleBatchWithResult acknowledges poison messages and stages retry only for
// valid messages covered by a failed insert.
func (w *RiskEvaluationCHWriter) HandleBatchWithResult(ctx context.Context, batch []gcp.BatchMessage[*meteringv1.RiskEvaluation]) error {
	insertedAt := time.Now().UTC()
	rawRows := make([]riskmeterchrepo.EvaluationRow, 0, len(batch))
	rawCovered := make([]int, 0, len(batch))
	readingByID := make(map[uuid.UUID]int, len(batch))
	readingRows := make([]meteringchrepo.ReadingRow, 0, len(batch))
	readingCovered := make([][]int, 0, len(batch))

	for i, item := range batch {
		raw, reading, err := riskEvaluationRows(item.Message, insertedAt)
		if err != nil {
			messageID := ""
			if item.Message != nil {
				messageID = item.Message.GetId()
			}
			w.logger.ErrorContext(ctx, "skipping unprocessable risk measurement", attr.SlogMessageID(messageID), attr.SlogError(err))
			if w.recordsSkipped != nil {
				w.recordsSkipped.Add(ctx, 1)
			}
			continue
		}
		rawRows = append(rawRows, raw)
		rawCovered = append(rawCovered, i)
		if reading == nil {
			continue
		}
		if prior, duplicate := readingByID[reading.ID]; duplicate {
			readingCovered[prior] = append(readingCovered[prior], i)
			if reading.ProducedAt.After(readingRows[prior].ProducedAt) {
				readingRows[prior] = *reading
			}
			continue
		}
		readingByID[reading.ID] = len(readingRows)
		readingRows = append(readingRows, *reading)
		readingCovered = append(readingCovered, []int{i})
	}

	if len(rawRows) > 0 {
		err := w.evaluationInserter.InsertRiskEvaluations(ctx, rawRows)
		w.recordInsert(ctx, int64(len(rawRows)), err)
		if err != nil {
			err = fmt.Errorf("insert raw risk evaluations: %w", err)
			w.stageFailure(ctx, batch, rawCovered, err)
			return nil
		}
	}
	if len(readingRows) > 0 {
		err := w.readingInserter.InsertReadings(ctx, readingRows)
		if err != nil {
			err = fmt.Errorf("insert risk evaluation meter readings: %w", err)
			covered := make([]int, 0, len(readingRows))
			for _, indices := range readingCovered {
				covered = append(covered, indices...)
			}
			w.stageFailure(ctx, batch, covered, err)
		}
	}
	return nil
}

func (w *RiskEvaluationCHWriter) recordInsert(ctx context.Context, count int64, err error) {
	if w.recordsInserted != nil {
		w.recordsInserted.Add(ctx, count, metric.WithAttributes(attr.Outcome(o11y.OutcomeFromError(err))))
	}
}

func (w *RiskEvaluationCHWriter) stageFailure(ctx context.Context, batch []gcp.BatchMessage[*meteringv1.RiskEvaluation], covered []int, err error) {
	w.logger.ErrorContext(ctx, "insert risk measurement batch", attr.SlogError(err))
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	for _, i := range covered {
		batch[i].Fail(err)
	}
}

func riskEvaluationRows(message *meteringv1.RiskEvaluation, insertedAt time.Time) (riskmeterchrepo.EvaluationRow, *meteringchrepo.ReadingRow, error) {
	if message == nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("message is nil")
	}
	id, err := uuid.Parse(message.GetId())
	if err != nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("parse id: %w", err)
	}
	if id == uuid.Nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("id must not be zero")
	}
	evaluationID, err := uuid.Parse(message.GetEvaluationId())
	if err != nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("parse evaluation id: %w", err)
	}
	projectID, err := uuid.Parse(message.GetProjectId())
	if err != nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("parse project id: %w", err)
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, message.GetOccurredAt())
	if err != nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("parse occurred at: %w", err)
	}
	producedAt, err := time.Parse(time.RFC3339Nano, message.GetProducedAt())
	if err != nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("parse produced at: %w", err)
	}
	if producedAt.Before(occurredAt) {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("produced at precedes occurred at")
	}
	evaluation := riskmeter.Evaluation{
		OrganizationID: message.GetOrganizationId(), ProjectID: message.GetProjectId(), OperationID: message.GetOperationId(),
		Detector: message.GetDetector(), ExecutionMode: message.GetExecutionMode(), PolicyID: message.GetPolicyId(),
		PolicyVersion: message.GetPolicyVersion(), OccurredAt: occurredAt,
	}
	if err := riskmeter.ValidateEvaluation(evaluation); err != nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("validate evaluation: %w", err)
	}
	detector, _ := riskmeter.DetectorLabel(evaluation.Detector)
	executionMode, _ := riskmeter.ExecutionModeLabel(evaluation.ExecutionMode)
	if evaluationID.String() != riskmeter.EvaluationID(evaluation) {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("evaluation id does not match logical identity")
	}
	if err := riskmeter.ValidateOutcome(message.GetOutcome()); err != nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("validate outcome: %w", err)
	}
	outcome, _ := riskmeter.OutcomeLabel(message.GetOutcome())
	if message.GetMeasurementMethod() != riskmeter.MeasurementMethod {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("invalid measurement method %q", message.GetMeasurementMethod())
	}
	if message.HasStokens() && message.GetStokens() < 0 {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("stokens must not be negative")
	}
	if message.HasPromptTokens() && message.GetPromptTokens() < 0 {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("prompt tokens must not be negative")
	}
	if message.HasCompletionTokens() && message.GetCompletionTokens() < 0 {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("completion tokens must not be negative")
	}
	if message.HasCostUsd() && (message.GetCostUsd() < 0 || math.IsNaN(message.GetCostUsd()) || math.IsInf(message.GetCostUsd(), 0)) {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("cost usd must be finite and nonnegative")
	}
	switch message.GetRecordKind() {
	case riskmeter.RecordKindScan:
		if message.HasStokens() && message.GetMeasurementError() {
			return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("scan with measurement error must not contain known stokens")
		}
		if message.HasPromptTokens() || message.HasCompletionTokens() || message.HasCostUsd() || message.GetModel() != "" || message.GetProviderRequestId() != "" {
			return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("scan must not contain inference accounting")
		}
	case riskmeter.RecordKindInference:
		if message.HasStokens() || message.GetMeasurementError() {
			return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("inference must not contain scan volume")
		}
	default:
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("invalid record kind %q", message.GetRecordKind())
	}

	var stokenCount, promptTokens, completionTokens *int64
	var costUSD *float64
	if message.HasStokens() {
		value := message.GetStokens()
		stokenCount = &value
	}
	if message.HasPromptTokens() {
		value := message.GetPromptTokens()
		promptTokens = &value
	}
	if message.HasCompletionTokens() {
		value := message.GetCompletionTokens()
		completionTokens = &value
	}
	if message.HasCostUsd() {
		value := message.GetCostUsd()
		costUSD = &value
	}
	raw := riskmeterchrepo.EvaluationRow{
		ID: id, EvaluationID: evaluationID, OrganizationID: evaluation.OrganizationID, ProjectID: projectID,
		OperationID: evaluation.OperationID, Detector: detector, ExecutionMode: executionMode,
		Outcome: outcome, RecordKind: message.GetRecordKind(), OccurredAt: occurredAt, ProducedAt: producedAt,
		InsertedAt: insertedAt, STokens: stokenCount, MeasurementMethod: message.GetMeasurementMethod(), Model: message.GetModel(),
		ProviderRequestID: message.GetProviderRequestId(), PromptTokens: promptTokens, CompletionTokens: completionTokens,
		CostUSD: costUSD, MeasurementError: message.GetMeasurementError(), PolicyID: evaluation.PolicyID, PolicyVersion: evaluation.PolicyVersion,
	}
	if message.GetRecordKind() != riskmeter.RecordKindScan || message.GetOutcome() != riskmeter.OutcomeCompleted || stokenCount == nil || *stokenCount == 0 {
		return raw, nil, nil
	}
	definition, ok := metering.RiskEvaluationDefinition(detector, executionMode)
	if !ok {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("risk meter definition is not registered")
	}
	attributes := map[string]string{
		"policy_id":      evaluation.PolicyID,
		"policy_version": strconv.FormatInt(evaluation.PolicyVersion, 10),
	}
	usage, err := metering.NewUsage(metering.UsageInput{
		Meter: definition, Scope: metering.ProjectScope(evaluation.OrganizationID, projectID), OperationID: evaluationID.String(),
		Value: *stokenCount, OccurredAt: occurredAt.UTC(), ProducedAt: producedAt.UTC(), Source: "risk_evaluation",
		Attributes: attributes,
	})
	if err != nil {
		return riskmeterchrepo.EvaluationRow{}, nil, fmt.Errorf("build risk meter reading: %w", err)
	}
	meterID, _ := metering.RiskEvaluationMeterID(detector, executionMode)
	reading := &meteringchrepo.ReadingRow{
		ID: usage.ID(), OrganizationID: evaluation.OrganizationID, ProjectID: projectID, MeterID: string(meterID),
		OperationID: evaluationID.String(), Unit: string(metering.UnitSTokens), MeasurementMethod: string(metering.MeasurementTiktokenO200kBase),
		Value: *stokenCount, OccurredAt: occurredAt.UTC(), ProducedAt: producedAt.UTC(), InsertedAt: insertedAt,
		CorrectsReadingID: nil, Attributes: attributes,
	}
	return raw, reading, nil
}

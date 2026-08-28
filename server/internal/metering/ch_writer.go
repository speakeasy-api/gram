package metering

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/google/uuid"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

// ReadingInserter persists validated workload readings.
type ReadingInserter interface {
	InsertReadings(context.Context, []chrepo.ReadingRow) error
}

// MeterReadingCHWriter validates Pub/Sub readings and writes them to ClickHouse.
type MeterReadingCHWriter struct {
	logger   *slog.Logger
	inserter ReadingInserter
}

// NewMeterReadingCHWriter creates a ClickHouse workload reading subscriber.
func NewMeterReadingCHWriter(logger *slog.Logger, inserter ReadingInserter) *MeterReadingCHWriter {
	return &MeterReadingCHWriter{
		logger:   logger.With(attr.SlogComponent("meter-reading-ch-writer")),
		inserter: inserter,
	}
}

var _ streams.BatchHandler[*meteringv1.MeterReading] = (*MeterReadingCHWriter)(nil)

// HandleBatch acknowledges malformed poison messages and retries ClickHouse failures.
func (w *MeterReadingCHWriter) HandleBatch(ctx context.Context, messages []*meteringv1.MeterReading, _ []gcp.MessageMetadata) error {
	insertedAt := time.Now().UTC()
	rows := make([]chrepo.ReadingRow, 0, len(messages))
	seen := make(map[uuid.UUID]struct{}, len(messages))

	for _, message := range messages {
		row, reason := meterReadingRow(message, insertedAt)
		if reason != "" {
			messageID := ""
			if message != nil {
				messageID = message.GetId()
			}
			w.logger.ErrorContext(ctx, "skipping unprocessable meter reading",
				attr.SlogReason(reason),
				attr.SlogMessageID(messageID),
			)
			continue
		}
		if _, duplicate := seen[row.ID]; duplicate {
			continue
		}
		seen[row.ID] = struct{}{}
		rows = append(rows, row)
	}

	if err := w.inserter.InsertReadings(ctx, rows); err != nil {
		return fmt.Errorf("insert meter readings: %w", err)
	}
	return nil
}

func meterReadingRow(message *meteringv1.MeterReading, insertedAt time.Time) (chrepo.ReadingRow, string) {
	var zero chrepo.ReadingRow
	if message == nil {
		return zero, "nil_reading"
	}

	id, err := uuid.Parse(message.GetId())
	if err != nil || id == uuid.Nil {
		return zero, "invalid_id"
	}
	projectID, err := uuid.Parse(message.GetProjectId())
	if err != nil || projectID == uuid.Nil {
		return zero, "invalid_project_id"
	}
	definition, ok := LookupDefinition(MeterID(message.GetMeterId()), message.GetMeterVersion())
	if !ok {
		return zero, "unknown_meter_definition"
	}
	if message.GetUnit() != string(definition.unit) {
		return zero, "invalid_unit"
	}
	if message.GetMeasurementMethod() != string(definition.measurementMethod) {
		return zero, "invalid_measurement_method"
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, message.GetOccurredAt())
	if err != nil {
		return zero, "invalid_occurred_at"
	}
	producedAt, err := time.Parse(time.RFC3339Nano, message.GetProducedAt())
	if err != nil {
		return zero, "invalid_produced_at"
	}

	scope := ProjectScope(message.GetOrganizationId(), projectID)
	var reading Reading
	var correctsReadingID *uuid.UUID
	switch message.GetKind() {
	case meteringv1.MeterReading_KIND_USAGE:
		if message.GetCorrectsReadingId() != "" || message.GetAdjustmentReason() != "" {
			return zero, "invalid_usage"
		}
		reading, err = NewUsage(UsageInput{
			Meter:       definition,
			Scope:       scope,
			OperationID: message.GetOperationId(),
			Value:       message.GetValue(),
			OccurredAt:  occurredAt,
			ProducedAt:  producedAt,
			Source:      message.GetSource(),
			Attributes:  message.GetAttributes(),
		})
		if err != nil {
			return zero, "invalid_usage"
		}
	case meteringv1.MeterReading_KIND_ADJUSTMENT:
		correctionID, parseErr := uuid.Parse(message.GetCorrectsReadingId())
		if parseErr != nil {
			return zero, "invalid_correction_id"
		}
		reading, err = NewAdjustment(AdjustmentInput{
			Meter:             definition,
			Scope:             scope,
			OperationID:       message.GetOperationId(),
			Value:             message.GetValue(),
			OccurredAt:        occurredAt,
			ProducedAt:        producedAt,
			CorrectsReadingID: correctionID,
			Reason:            message.GetAdjustmentReason(),
			Source:            message.GetSource(),
			Attributes:        message.GetAttributes(),
		})
		if err != nil {
			return zero, "invalid_adjustment"
		}
		correctsReadingID = &correctionID
	default:
		return zero, "invalid_kind"
	}
	if reading.ID() != id {
		return zero, "non_deterministic_id"
	}

	attributes := maps.Clone(message.GetAttributes())
	if attributes == nil {
		attributes = make(map[string]string, 1)
	}
	if definition.unit == UnitSTokens {
		attributes["codec"] = string(definition.measurementMethod)
	}

	return chrepo.ReadingRow{
		ID:                id,
		OrganizationID:    scope.organizationID,
		ProjectID:         scope.projectID,
		MeterID:           string(definition.id),
		OperationID:       message.GetOperationId(),
		Unit:              string(definition.unit),
		Value:             message.GetValue(),
		OccurredAt:        occurredAt,
		InsertedAt:        insertedAt,
		CorrectsReadingID: correctsReadingID,
		Attributes:        attributes,
	}, ""
}

// Package metering owns workload meter definitions, readings, and publication.
package metering

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	outboxrepo "github.com/speakeasy-api/gram/server/internal/outbox/repo"
)

// Enqueue atomically stages validated readings through the caller's transaction.
func Enqueue(ctx context.Context, dbtx outboxrepo.DBTX, readings []Reading) error {
	if len(readings) == 0 {
		return nil
	}

	organizationID := readings[0].scope.organizationID
	messages := make([]outbox.Message, 0, len(readings))
	for i, reading := range readings {
		if err := reading.validate(); err != nil {
			return fmt.Errorf("validate meter reading %d: %w", i, err)
		}
		if reading.scope.organizationID != organizationID {
			return fmt.Errorf("meter reading %d organization id does not match batch organization", i)
		}

		readingID := reading.ID()
		message := toProto(reading, readingID)
		messages = append(messages, outbox.Message{
			Proto:      message,
			PublicID:   readingID,
			Attributes: nil,
		})
	}

	if _, err := outbox.PublishBatch(ctx, dbtx, organizationID, messages); err != nil {
		return fmt.Errorf("enqueue meter readings: %w", err)
	}
	return nil
}

func toProto(reading Reading, readingID uuid.UUID) *meteringv1.MeterReading {
	message := &meteringv1.MeterReading{}
	message.SetId(readingID.String())
	message.SetOrganizationId(reading.scope.organizationID)
	if reading.scope.kind == scopeKindProject {
		message.SetProjectId(reading.scope.projectID.String())
	}
	message.SetMeterId(string(reading.meter.id))
	message.SetOperationId(reading.operationID)
	message.SetUnit(string(reading.meter.unit))
	message.SetValue(reading.value)
	message.SetOccurredAt(reading.occurredAt.Format(time.RFC3339Nano))
	message.SetAttributes(maps.Clone(reading.attributes))
	message.SetMeterVersion(reading.meter.version)
	message.SetProducedAt(reading.producedAt.Format(time.RFC3339Nano))
	message.SetMeasurementMethod(string(reading.meter.measurementMethod))
	message.SetSource(reading.source)

	switch reading.kind {
	case readingKindUsage:
		message.SetKind(meteringv1.MeterReading_KIND_USAGE)
	case readingKindAdjustment:
		message.SetKind(meteringv1.MeterReading_KIND_ADJUSTMENT)
		message.SetCorrectsReadingId(reading.correctsReadingID.String())
		message.SetAdjustmentReason(reading.adjustmentReason)
	}

	return message
}

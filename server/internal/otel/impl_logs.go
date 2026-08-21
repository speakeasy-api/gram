package otel

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/proto"

	gen "github.com/speakeasy-api/gram/server/gen/otel"
)

func (s *Service) Logs(ctx context.Context, payload *gen.LogsPayload, body io.ReadCloser) error {
	return ingestOTLPExport(ctx, s.logger, otlpIngestSpec[*otelv1.InboundLogRecord]{
		signal:          "log",
		contentEncoding: payload.ContentEncoding,
		body:            body,
		decode: func(raw []byte, tenant otlpIngestTenant) ([]*otelv1.InboundLogRecord, error) {
			provenance := (&otelv1.InboundLogRecord_Provenance_builder{
				Source:         new(otelProvenanceSource),
				OrganizationId: &tenant.organizationID,
				ProjectId:      &tenant.projectID,
			}).Build()
			return decodeOTLPLogExport(raw, provenance)
		},
		validate:  validateLogRecord,
		publisher: s.logPublisher,
	})
}

func decodeOTLPLogExport(raw []byte, provenance *otelv1.InboundLogRecord_Provenance) ([]*otelv1.InboundLogRecord, error) {
	request := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: nil}
	if err := proto.Unmarshal(raw, request); err != nil {
		return nil, fmt.Errorf("decode OTLP log export: %w", err)
	}

	records := make([]*otelv1.InboundLogRecord, 0)
	for _, resourceLogs := range request.GetResourceLogs() {
		if resourceLogs == nil {
			continue
		}

		var resource *otelv1.InboundLogRecord_Resource
		if source := resourceLogs.GetResource(); source != nil {
			resource = &otelv1.InboundLogRecord_Resource{}
			if err := transcodeOTLPMessage(source, resource); err != nil {
				return nil, fmt.Errorf("convert OTLP resource: %w", err)
			}
		}

		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			if scopeLogs == nil {
				continue
			}

			var scope *otelv1.InboundLogRecord_InstrumentationScope
			if source := scopeLogs.GetScope(); source != nil {
				scope = &otelv1.InboundLogRecord_InstrumentationScope{}
				if err := transcodeOTLPMessage(source, scope); err != nil {
					return nil, fmt.Errorf("convert OTLP instrumentation scope: %w", err)
				}
			}

			for _, record := range scopeLogs.GetLogRecords() {
				if record == nil {
					continue
				}

				converted := &otelv1.InboundLogRecord{}
				if err := transcodeOTLPMessage(record, converted); err != nil {
					return nil, fmt.Errorf("convert OTLP log record: %w", err)
				}

				converted.SetRecordId(uuid.NewString())
				converted.SetResource(resource)
				converted.SetProvenance(provenance)
				converted.SetScope(scope)

				if schemaURL := resourceLogs.GetSchemaUrl(); schemaURL != "" {
					converted.SetResourceSchemaUrl(schemaURL)
				}
				if schemaURL := scopeLogs.GetSchemaUrl(); schemaURL != "" {
					converted.SetScopeSchemaUrl(schemaURL)
				}

				records = append(records, converted)
			}
		}
	}

	return records, nil
}

func validateLogRecord(record *otelv1.InboundLogRecord) error {
	if record == nil {
		return errors.New("log record is required")
	}
	if record.GetRecordId() == "" {
		return errors.New("log record ID is required")
	}
	if size := proto.Size(record); size > maxOTLPLogRecordBytes {
		return fmt.Errorf("log record exceeds maximum size of %d bytes: got %d bytes", maxOTLPLogRecordBytes, size)
	}
	if size := len(record.GetTraceId()); size != 0 && size != otlpTraceIDSize {
		return fmt.Errorf("trace ID must be empty or %d bytes, got %d", otlpTraceIDSize, size)
	}
	if size := len(record.GetSpanId()); size != 0 && size != otlpSpanIDSize {
		return fmt.Errorf("span ID must be empty or %d bytes, got %d", otlpSpanIDSize, size)
	}

	return nil
}

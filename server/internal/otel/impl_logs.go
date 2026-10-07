package otel

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/proto"

	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/gramotel"
)

func (s *Service) Logs(ctx context.Context, payload *gen.LogsPayload, body io.ReadCloser) error {
	var export *collectorlogsv1.ExportLogsServiceRequest
	err := ingestOTLPExport(ctx, s.logger, otlpIngestSpec[*otelv1.InboundLogRecord]{
		signal:          "log",
		contentEncoding: payload.ContentEncoding,
		body:            body,
		decode: func(raw []byte, tenant otlpIngestTenant) ([]*otelv1.InboundLogRecord, error) {
			provenance := (&otelv1.InboundLogRecord_Provenance_builder{
				Source:         new(gramotel.ProvenanceSource),
				OrganizationId: &tenant.organizationID,
				ProjectId:      &tenant.projectID,
			}).Build()
			request, err := unmarshalOTLPLogExport(raw)
			if err != nil {
				return nil, err
			}
			export = request
			return inboundLogRecordsFromExport(request, provenance)
		},
		publish: func(ctx context.Context, records []*otelv1.InboundLogRecord) error {
			return gramotel.PublishLogs(ctx, s.records, s.logPublisher, records)
		},
	})
	if err != nil {
		return err
	}
	// Only after the event feed publish is durable: an exporter retry after a
	// failed publish must not write the hooks telemetry rows twice.
	s.forwardLogsToHooks(ctx, export)
	return nil
}

func unmarshalOTLPLogExport(raw []byte) (*collectorlogsv1.ExportLogsServiceRequest, error) {
	request := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: nil}
	if err := proto.Unmarshal(raw, request); err != nil {
		return nil, fmt.Errorf("decode OTLP log export: %w", err)
	}
	return request, nil
}

func inboundLogRecordsFromExport(request *collectorlogsv1.ExportLogsServiceRequest, provenance *otelv1.InboundLogRecord_Provenance) ([]*otelv1.InboundLogRecord, error) {
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
				// OTLP receivers stamp observed time when the producer did
				// not. Stamping before the first publish keeps the value
				// stable across Pub/Sub redeliveries, which downstream
				// ClickHouse writers rely on for a deterministic dedup key.
				if converted.GetObservedTimeUnixNano() == 0 {
					converted.SetObservedTimeUnixNano(uint64(time.Now().UnixNano()))
				}
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

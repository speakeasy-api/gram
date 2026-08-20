package otel

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/proto"

	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) Logs(ctx context.Context, payload *gen.LogsPayload, body io.ReadCloser) (err error) {
	logger := s.logger

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	defer o11y.NoLogDefer(func() error { return body.Close() })

	reader := io.LimitReader(body, maxOTLPExportBytes+1)
	switch encoding := strings.ToLower(strings.TrimSpace(conv.PtrValOr(payload.ContentEncoding, ""))); encoding {
	case "", "identity":
	case "gzip":
		decompressed, err := gzip.NewReader(reader)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "unable to read gzipped OTLP log export").LogError(ctx, logger)
		}
		defer o11y.NoLogDefer(func() error { return decompressed.Close() })

		reader = io.LimitReader(decompressed, maxOTLPExportBytes+1)
	default:
		return oops.E(oops.CodeUnsupportedMedia, nil, "unsupported OTLP content encoding %q", encoding)
	}

	raw, err := io.ReadAll(reader)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "unable to read OTLP log export").LogError(ctx, logger)
	}
	if len(raw) > maxOTLPExportBytes {
		return oops.E(oops.CodeRequestTooLarge, nil, "OTLP log export exceeds %d MiB", maxOTLPExportBytes/constants.MiB)
	}

	provenance := (&otelv1.InboundLogRecord_Provenance_builder{
		Source:         new(otelProvenanceSource),
		OrganizationId: &authCtx.ActiveOrganizationID,
		ProjectId:      new(authCtx.ProjectID.String()),
	}).Build()

	records, err := decodeOTLPLogExport(raw, provenance)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid OTLP log export").LogError(ctx, logger)
	}

	for _, record := range records {
		if err := validateLogRecord(record); err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid OTLP log export").LogError(ctx, logger)
		}
	}

	results := make([]gcp.PublishResult, 0, len(records))
	for _, record := range records {
		results = append(results, s.logPublisher.Publish(ctx, record))
	}

	var publishErr error
	for _, result := range results {
		if _, err := result.Get(ctx); err != nil {
			publishErr = errors.Join(publishErr, err)
		}
	}
	if publishErr != nil {
		return oops.E(oops.CodeUnexpected, publishErr, "unable to accept OTLP log export").LogError(ctx, logger)
	}

	return nil
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
	if size := len(record.GetTraceId()); size != 0 && size != otlpTraceIDSize {
		return fmt.Errorf("trace ID must be empty or %d bytes, got %d", otlpTraceIDSize, size)
	}
	if size := len(record.GetSpanId()); size != 0 && size != otlpSpanIDSize {
		return fmt.Errorf("span ID must be empty or %d bytes, got %d", otlpSpanIDSize, size)
	}

	return nil
}

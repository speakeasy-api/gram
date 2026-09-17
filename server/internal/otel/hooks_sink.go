package otel

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	hookssrv "github.com/speakeasy-api/gram/server/gen/http/hooks/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// HooksSink receives every OTLP export this service accepts, in the payload
// shape of the hooks OTLP endpoints, so the hooks telemetry writers
// (telemetry_logs, session and account attribution) run for producers that
// export to /otel/v1/* as well. Without it those exports only reach the event
// feed and never count toward usage, cost, or identity pages.
//
// The sink is called after the export is authenticated and durably published,
// with the request's auth context, so it never widens access and an exporter
// retry after a failed publish cannot write the telemetry rows twice.
type HooksSink interface {
	IngestOTLPLogs(ctx context.Context, payload *hooksgen.LogsPayload)
	IngestOTLPMetrics(ctx context.Context, payload *hooksgen.MetricsPayload)
}

// SetHooksSink wires the hooks service in after construction.
func (s *Service) SetHooksSink(sink HooksSink) {
	s.hooksSink = sink
}

func (s *Service) forwardLogsToHooks(ctx context.Context, export *collectorlogsv1.ExportLogsServiceRequest) {
	if s.hooksSink == nil || export == nil {
		return
	}
	payload, err := hooksLogsPayload(export)
	if err != nil {
		s.logger.ErrorContext(ctx, "convert OTLP logs export for hooks telemetry", attr.SlogError(err))
		return
	}
	s.hooksSink.IngestOTLPLogs(ctx, payload)
}

func (s *Service) forwardMetricsToHooks(ctx context.Context, export *collectormetricsv1.ExportMetricsServiceRequest) {
	if s.hooksSink == nil || export == nil {
		return
	}
	payload, err := hooksMetricsPayload(export)
	if err != nil {
		s.logger.ErrorContext(ctx, "convert OTLP metrics export for hooks telemetry", attr.SlogError(err))
		return
	}
	s.hooksSink.IngestOTLPMetrics(ctx, payload)
}

// hooksLogsPayload converts a protobuf export into the hooks endpoint's
// payload by round-tripping through OTLP/JSON and the generated request body
// decoder, so both ingest edges parse identical shapes. protojson renders
// trace and span ids as base64 where OTLP/JSON producers send hex; those are
// rewritten so downstream id handling matches the hooks path.
func hooksLogsPayload(export *collectorlogsv1.ExportLogsServiceRequest) (*hooksgen.LogsPayload, error) {
	var body hookssrv.LogsRequestBody
	if err := transcodeToRequestBody(export, &body); err != nil {
		return nil, err
	}
	for _, resourceLog := range body.ResourceLogs {
		if resourceLog == nil {
			continue
		}
		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}
			for _, record := range scopeLog.LogRecords {
				if record == nil {
					continue
				}
				record.TraceID = base64IDToHex(record.TraceID)
				record.SpanID = base64IDToHex(record.SpanID)
			}
		}
	}
	if err := hookssrv.ValidateLogsRequestBody(&body); err != nil {
		return nil, fmt.Errorf("validate hooks logs payload: %w", err)
	}
	return hookssrv.NewLogsPayload(&body, nil, nil), nil
}

func hooksMetricsPayload(export *collectormetricsv1.ExportMetricsServiceRequest) (*hooksgen.MetricsPayload, error) {
	var body hookssrv.MetricsRequestBody
	if err := transcodeToRequestBody(export, &body); err != nil {
		return nil, err
	}
	if err := hookssrv.ValidateMetricsRequestBody(&body); err != nil {
		return nil, fmt.Errorf("validate hooks metrics payload: %w", err)
	}
	return hookssrv.NewMetricsPayload(&body, nil, nil), nil
}

func transcodeToRequestBody(export proto.Message, body any) error {
	raw, err := protojson.Marshal(export)
	if err != nil {
		return fmt.Errorf("encode OTLP export as JSON: %w", err)
	}
	if err := json.Unmarshal(raw, body); err != nil {
		return fmt.Errorf("decode OTLP JSON export as hooks payload: %w", err)
	}
	return nil
}

func base64IDToHex(raw *string) *string {
	if raw == nil || *raw == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(*raw)
	if err != nil {
		return raw
	}
	encoded := hex.EncodeToString(decoded)
	return &encoded
}

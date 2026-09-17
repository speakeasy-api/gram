package otel

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

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

// transcodeToRequestBody renders the export as OTLP/JSON and decodes it with
// the hooks request-body types. protojson writes non-finite doubles as the
// strings "NaN" and "Infinity", which those types cannot decode; such values
// are cleared first so one bad datapoint does not drop the whole export.
func transcodeToRequestBody(export proto.Message, body any) error {
	clearNonFiniteFloats(export.ProtoReflect())
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

// clearNonFiniteFloats clears singular non-finite float fields in place and
// reports whether the message is still usable. A non-finite value inside a
// repeated float field (a histogram's explicit_bounds) cannot be removed
// without desynchronising the parallel bucket_counts array, so such a message
// is reported unusable and callers drop it whole: list items are removed,
// singular fields are cleared.
func clearNonFiniteFloats(message protoreflect.Message) bool {
	if !message.IsValid() {
		return true
	}
	keep := true
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.IsMap():
			if isProtobufMessage(field.MapValue().Kind()) {
				var drop []protoreflect.MapKey
				value.Map().Range(func(key protoreflect.MapKey, item protoreflect.Value) bool {
					if !clearNonFiniteFloats(item.Message()) {
						drop = append(drop, key)
					}
					return true
				})
				for _, key := range drop {
					value.Map().Clear(key)
				}
			}
		case field.IsList():
			switch {
			case isProtobufMessage(field.Kind()):
				dropUnusableMessages(value.List())
			case isProtobufFloat(field.Kind()):
				if hasNonFiniteFloat(value.List()) {
					keep = false
				}
			}
		case isProtobufMessage(field.Kind()):
			if !clearNonFiniteFloats(value.Message()) {
				message.Clear(field)
			}
		case isProtobufFloat(field.Kind()) && !isFinite(value.Float()):
			message.Clear(field)
		}
		return true
	})
	return keep
}

func dropUnusableMessages(list protoreflect.List) {
	kept := 0
	for i := range list.Len() {
		item := list.Get(i)
		if clearNonFiniteFloats(item.Message()) {
			list.Set(kept, item)
			kept++
		}
	}
	list.Truncate(kept)
}

func hasNonFiniteFloat(list protoreflect.List) bool {
	for i := range list.Len() {
		if !isFinite(list.Get(i).Float()) {
			return true
		}
	}
	return false
}

func isProtobufFloat(kind protoreflect.Kind) bool {
	return kind == protoreflect.DoubleKind || kind == protoreflect.FloatKind
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

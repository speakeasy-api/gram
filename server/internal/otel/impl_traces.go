package otel

import (
	"context"
	"fmt"
	"io"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	gen "github.com/speakeasy-api/gram/server/gen/otel"
)

func (s *Service) Traces(ctx context.Context, payload *gen.TracesPayload, body io.ReadCloser) error {
	return ingestOTLPExport(ctx, s.logger, otlpIngestSpec[*otelv1.InboundSpan]{
		signal:          "trace",
		contentEncoding: payload.ContentEncoding,
		body:            body,
		decode: func(raw []byte, tenant otlpIngestTenant) ([]*otelv1.InboundSpan, error) {
			provenance := (&otelv1.InboundSpan_Provenance_builder{
				Source:         new(otelProvenanceSource),
				OrganizationId: &tenant.organizationID,
				ProjectId:      &tenant.projectID,
			}).Build()
			return decodeOTLPTraceExport(raw, provenance)
		},
		validate:  func(span *otelv1.InboundSpan) error { return validateSpan(span) },
		publisher: s.spanPublisher,
	})
}

func decodeOTLPTraceExport(raw []byte, provenance *otelv1.InboundSpan_Provenance) ([]*otelv1.InboundSpan, error) {
	request := &collectortracev1.ExportTraceServiceRequest{ResourceSpans: nil}
	if err := proto.Unmarshal(raw, request); err != nil {
		return nil, fmt.Errorf("decode OTLP trace export: %w", err)
	}

	spans := make([]*otelv1.InboundSpan, 0)
	for _, resourceSpans := range request.GetResourceSpans() {
		if resourceSpans == nil {
			continue
		}

		// One converted copy per resource and scope, shared by every span
		// underneath it: the messages are only read from here on.
		var resource *otelv1.InboundSpan_Resource
		if source := resourceSpans.GetResource(); source != nil {
			resource = &otelv1.InboundSpan_Resource{}
			if err := transcodeOTLPMessage(source, resource); err != nil {
				return nil, fmt.Errorf("convert OTLP resource: %w", err)
			}
		}

		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			if scopeSpans == nil {
				continue
			}

			var scope *otelv1.InboundSpan_InstrumentationScope
			if source := scopeSpans.GetScope(); source != nil {
				scope = &otelv1.InboundSpan_InstrumentationScope{}
				if err := transcodeOTLPMessage(source, scope); err != nil {
					return nil, fmt.Errorf("convert OTLP instrumentation scope: %w", err)
				}
			}

			for _, span := range scopeSpans.GetSpans() {
				if span == nil {
					continue
				}

				converted := &otelv1.InboundSpan{}
				if err := transcodeOTLPMessage(span, converted); err != nil {
					return nil, fmt.Errorf("convert OTLP span: %w", err)
				}

				converted.SetResource(resource)
				converted.SetProvenance(provenance)
				converted.SetScope(scope)

				if schemaURL := resourceSpans.GetSchemaUrl(); schemaURL != "" {
					converted.SetResourceSchemaUrl(schemaURL)
				}
				if schemaURL := scopeSpans.GetSchemaUrl(); schemaURL != "" {
					converted.SetScopeSchemaUrl(schemaURL)
				}

				spans = append(spans, converted)
			}
		}
	}

	return spans, nil
}

// transcodeOTLPMessage re-parses src's encoded bytes into dst. Valid only
// between messages that agree on field numbers and wire types, which is the
// compatibility guarantee the gram.otel.v1 protos keep against their
// opentelemetry.proto counterparts.
func transcodeOTLPMessage(src proto.Message, dst proto.Message) error {
	encoded, err := proto.Marshal(src)
	if err != nil {
		return fmt.Errorf("marshal OTLP message: %w", err)
	}

	if err := proto.Unmarshal(encoded, dst); err != nil {
		return fmt.Errorf("unmarshal OTLP message as gram.otel.v1: %w", err)
	}

	return nil
}

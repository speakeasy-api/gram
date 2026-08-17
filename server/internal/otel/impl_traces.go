package otel

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) Traces(ctx context.Context, payload *gen.TracesPayload, body io.ReadCloser) (err error) {
	logger := s.logger

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	defer o11y.NoLogDefer(func() error { return body.Close() })

	// Both the encoded body and, for a compressed one, what it expands to are
	// capped: the size of an export as sent says nothing about how much of it a
	// gzip stream unpacks into.
	reader := io.LimitReader(body, maxOTLPTraceExportBytes+1)
	switch encoding := strings.ToLower(strings.TrimSpace(conv.PtrValOr(payload.ContentEncoding, ""))); encoding {
	case "", "identity":
	case "gzip":
		decompressed, err := gzip.NewReader(reader)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "unable to read gzipped OTLP trace export").LogError(ctx, logger)
		}
		defer o11y.NoLogDefer(func() error { return decompressed.Close() })

		reader = io.LimitReader(decompressed, maxOTLPTraceExportBytes+1)
	default:
		return oops.E(oops.CodeUnsupportedMedia, nil, "unsupported OTLP content encoding %q", encoding)
	}

	raw, err := io.ReadAll(reader)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "unable to read OTLP trace export").LogError(ctx, logger)
	}
	if len(raw) > maxOTLPTraceExportBytes {
		return oops.E(oops.CodeRequestTooLarge, nil, "OTLP trace export exceeds %d MiB", maxOTLPTraceExportBytes/constants.MiB)
	}

	// Tenancy comes from the authenticated request, never from the export's own
	// resource attributes: those are producer-controlled and a client could
	// claim any organization or project it liked.
	provenance := (&otelv1.InboundSpan_Provenance_builder{
		Source:         new(otelTracesProvenanceSource),
		OrganizationId: &authCtx.ActiveOrganizationID,
		ProjectId:      new(authCtx.ProjectID.String()),
	}).Build()

	spans, err := decodeOTLPTraceExport(raw, provenance)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid OTLP trace export").LogError(ctx, logger)
	}

	// Enqueue the whole export before waiting on any result so the batch flushes
	// as one round trip, then settle every result: answering the exporter before
	// its spans are durable would turn a Pub/Sub failure into silent data loss,
	// since OTLP clients only retry what the server rejects.
	results := make([]gcp.PublishResult, 0, len(spans))
	for _, span := range spans {
		if err := validateSpan(span); err != nil {
			logger.WarnContext(ctx, "invalid otlp span", attr.SlogError(err))
			continue
		}

		results = append(results, s.spanPublisher.Publish(ctx, span))
	}

	var publishErr error
	for _, result := range results {
		if _, err := result.Get(ctx); err != nil {
			publishErr = errors.Join(publishErr, err)
		}
	}
	if publishErr != nil {
		return oops.E(oops.CodeUnexpected, publishErr, "unable to accept OTLP trace export").LogError(ctx, logger)
	}

	return nil
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

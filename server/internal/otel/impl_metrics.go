package otel

import (
	"context"
	"errors"
	"fmt"
	"io"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"

	gen "github.com/speakeasy-api/gram/server/gen/otel"
)

func (s *Service) Metrics(ctx context.Context, payload *gen.MetricsPayload, body io.ReadCloser) error {
	return ingestOTLPExport(ctx, s.logger, otlpIngestSpec[*otelv1.InboundMetric]{
		signal:          "metric",
		contentEncoding: payload.ContentEncoding,
		body:            body,
		decode: func(raw []byte, tenant otlpIngestTenant) ([]*otelv1.InboundMetric, error) {
			provenance := (&otelv1.InboundMetric_Provenance_builder{
				Source:         new(ProvenanceSource),
				OrganizationId: &tenant.organizationID,
				ProjectId:      &tenant.projectID,
			}).Build()
			return decodeOTLPMetricExport(raw, provenance)
		},
		validate:  validateInboundMetric,
		publisher: s.metricPublisher,
	})
}

func decodeOTLPMetricExport(raw []byte, provenance *otelv1.InboundMetric_Provenance) ([]*otelv1.InboundMetric, error) {
	request := &collectormetricsv1.ExportMetricsServiceRequest{ResourceMetrics: nil}
	if err := proto.Unmarshal(raw, request); err != nil {
		return nil, fmt.Errorf("decode OTLP metric export: %w", err)
	}

	metrics := make([]*otelv1.InboundMetric, 0)
	for _, resourceMetrics := range request.GetResourceMetrics() {
		if resourceMetrics == nil {
			continue
		}

		var resource *otelv1.InboundMetric_Resource
		if source := resourceMetrics.GetResource(); source != nil {
			resource = new(otelv1.InboundMetric_Resource)
			if err := transcodeOTLPMessage(source, resource); err != nil {
				return nil, fmt.Errorf("convert OTLP metric resource: %w", err)
			}
		}

		for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
			if scopeMetrics == nil {
				continue
			}

			var scope *otelv1.InboundMetric_InstrumentationScope
			if source := scopeMetrics.GetScope(); source != nil {
				scope = new(otelv1.InboundMetric_InstrumentationScope)
				if err := transcodeOTLPMessage(source, scope); err != nil {
					return nil, fmt.Errorf("convert OTLP metric instrumentation scope: %w", err)
				}
			}

			for _, metric := range scopeMetrics.GetMetrics() {
				if metric == nil {
					continue
				}

				converted := new(otelv1.InboundMetric)
				if err := transcodeOTLPMessage(metric, converted); err != nil {
					return nil, fmt.Errorf("convert OTLP metric: %w", err)
				}

				converted.SetResource(resource)
				converted.SetScope(scope)
				converted.SetProvenance(provenance)
				if schemaURL := resourceMetrics.GetSchemaUrl(); schemaURL != "" {
					converted.SetResourceSchemaUrl(schemaURL)
				}
				if schemaURL := scopeMetrics.GetSchemaUrl(); schemaURL != "" {
					converted.SetScopeSchemaUrl(schemaURL)
				}
				metrics = append(metrics, converted)
			}
		}
	}

	return metrics, nil
}

func validateInboundMetric(metric *otelv1.InboundMetric) error {
	if metric == nil {
		return errors.New("metric is required")
	}
	if metric.GetName() == "" {
		return errors.New("metric name is required")
	}
	if metric.WhichData() == otelv1.InboundMetric_Data_not_set_case {
		return errors.New("metric data is required")
	}
	if size := proto.Size(metric); size > maxOTLPMetricBytes {
		return fmt.Errorf("metric exceeds maximum size of %d bytes: got %d bytes", maxOTLPMetricBytes, size)
	}

	return nil
}

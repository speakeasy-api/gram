package otlp

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	otlpmetrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTLPMetricsUnmarshalAsGram(t *testing.T) {
	t.Parallel()

	for _, src := range otlpMetricFixtures() {
		raw, err := proto.Marshal(src)
		require.NoError(t, err, "marshal OTLP metric %s", src.GetName())

		for _, copy := range metricCopies() {
			gramMetric := copy.newMetric()
			require.NoError(t, proto.Unmarshal(raw, gramMetric), "%s: unmarshal OTLP metric %s as Gram", copy.name, src.GetName())

			roundTripRaw, err := proto.Marshal(gramMetric)
			require.NoError(t, err, "%s: marshal Gram metric %s", copy.name, src.GetName())

			var got otlpmetrics.Metric
			require.NoError(t, proto.Unmarshal(roundTripRaw, &got), "%s: unmarshal Gram metric %s as OTLP", copy.name, src.GetName())
			require.True(t, proto.Equal(src, &got), "%s: %s changed across OTLP → Gram → OTLP", copy.name, src.GetName())
		}
	}
}

func TestGramMetricFieldsSurviveOTLPRoundTrip(t *testing.T) {
	t.Parallel()

	src := (&otelv1.Metric_builder{
		Name: new("requests"),
		Gauge: (&otelv1.Metric_Gauge_builder{
			DataPoints: []*otelv1.Metric_NumberDataPoint{
				(&otelv1.Metric_NumberDataPoint_builder{TimeUnixNano: new(uint64(200)), AsInt: new(int64(1))}).Build(),
			},
		}).Build(),
		Resource: (&otelv1.Metric_Resource_builder{
			Attributes: []*otelv1.Metric_KeyValue{
				(&otelv1.Metric_KeyValue_builder{
					Key:   new("service.name"),
					Value: (&otelv1.Metric_AnyValue_builder{StringValue: new("svc")}).Build(),
				}).Build(),
			},
		}).Build(),
		ResourceSchemaUrl: new("https://opentelemetry.io/schemas/1.27.0"),
		Scope:             (&otelv1.Metric_InstrumentationScope_builder{Name: new("scope")}).Build(),
		ScopeSchemaUrl:    new("https://opentelemetry.io/schemas/1.28.0"),
		Provenance: (&otelv1.Metric_Provenance_builder{
			Source:         new("test"),
			OrganizationId: new("org"),
			ProjectId:      new("project"),
		}).Build(),
	}).Build()

	raw, err := proto.Marshal(src)
	require.NoError(t, err)

	var viaOTLP otlpmetrics.Metric
	require.NoError(t, proto.Unmarshal(raw, &viaOTLP))
	reencoded, err := proto.Marshal(&viaOTLP)
	require.NoError(t, err)

	var got otelv1.Metric
	require.NoError(t, proto.Unmarshal(reencoded, &got))
	require.Equal(t, "org", got.GetProvenance().GetOrganizationId())
	require.Equal(t, "project", got.GetProvenance().GetProjectId())
	require.Equal(t, "scope", got.GetScope().GetName())
	require.Equal(t, "https://opentelemetry.io/schemas/1.27.0", got.GetResourceSchemaUrl())
	require.Equal(t, "https://opentelemetry.io/schemas/1.28.0", got.GetScopeSchemaUrl())
	require.Equal(t, "svc", got.GetResource().GetAttributes()[0].GetValue().GetStringValue())
}

func otlpMetricFixtures() []*otlpmetrics.Metric {
	attributes := []*otlpcommon.KeyValue{{
		Key: "model",
		Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{
			StringValue: "test-model",
		}},
	}}
	exemplar := &otlpmetrics.Exemplar{
		FilteredAttributes: attributes,
		TimeUnixNano:       150,
		Value:              &otlpmetrics.Exemplar_AsDouble{AsDouble: 1.5},
		SpanId:             []byte("01234567"),
		TraceId:            []byte("0123456789abcdef"),
	}
	metadata := []*otlpcommon.KeyValue{{
		Key: "prometheus.type",
		Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{
			StringValue: "counter",
		}},
	}}

	return []*otlpmetrics.Metric{
		{
			Name:     "gauge",
			Metadata: metadata,
			Data: &otlpmetrics.Metric_Gauge{Gauge: &otlpmetrics.Gauge{DataPoints: []*otlpmetrics.NumberDataPoint{{
				Attributes:        attributes,
				StartTimeUnixNano: 100,
				TimeUnixNano:      200,
				Value:             &otlpmetrics.NumberDataPoint_AsDouble{AsDouble: 2.5},
				Exemplars:         []*otlpmetrics.Exemplar{exemplar},
				Flags:             uint32(otlpmetrics.DataPointFlags_DATA_POINT_FLAGS_NO_RECORDED_VALUE_MASK),
			}}}},
		},
		{
			Name: "sum",
			Data: &otlpmetrics.Metric_Sum{Sum: &otlpmetrics.Sum{
				DataPoints: []*otlpmetrics.NumberDataPoint{{
					Attributes:        attributes,
					StartTimeUnixNano: 100,
					TimeUnixNano:      200,
					Value:             &otlpmetrics.NumberDataPoint_AsInt{AsInt: 42},
				}},
				AggregationTemporality: otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
				IsMonotonic:            true,
			}},
		},
		{
			Name: "histogram",
			Data: &otlpmetrics.Metric_Histogram{Histogram: &otlpmetrics.Histogram{
				DataPoints: []*otlpmetrics.HistogramDataPoint{{
					Attributes:        attributes,
					StartTimeUnixNano: 100,
					TimeUnixNano:      200,
					Count:             2,
					Sum:               new(0.3),
					BucketCounts:      []uint64{1, 1},
					ExplicitBounds:    []float64{0.25},
					Exemplars:         []*otlpmetrics.Exemplar{exemplar},
					Min:               new(0.1),
					Max:               new(0.2),
				}},
				AggregationTemporality: otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
			}},
		},
		{
			Name: "exponential-histogram",
			Data: &otlpmetrics.Metric_ExponentialHistogram{ExponentialHistogram: &otlpmetrics.ExponentialHistogram{
				DataPoints: []*otlpmetrics.ExponentialHistogramDataPoint{{
					Attributes:        attributes,
					StartTimeUnixNano: 100,
					TimeUnixNano:      200,
					Count:             3,
					Sum:               new(0.6),
					Scale:             2,
					ZeroCount:         1,
					Positive:          &otlpmetrics.ExponentialHistogramDataPoint_Buckets{Offset: -1, BucketCounts: []uint64{1}},
					Negative:          &otlpmetrics.ExponentialHistogramDataPoint_Buckets{Offset: 0, BucketCounts: []uint64{1}},
					Exemplars:         []*otlpmetrics.Exemplar{exemplar},
					Min:               new(-0.2),
					Max:               new(0.4),
					ZeroThreshold:     0.01,
				}},
				AggregationTemporality: otlpmetrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
			}},
		},
		{
			Name: "summary",
			Data: &otlpmetrics.Metric_Summary{Summary: &otlpmetrics.Summary{DataPoints: []*otlpmetrics.SummaryDataPoint{{
				Attributes:        attributes,
				StartTimeUnixNano: 100,
				TimeUnixNano:      200,
				Count:             2,
				Sum:               0.3,
				QuantileValues: []*otlpmetrics.SummaryDataPoint_ValueAtQuantile{
					{Quantile: 0, Value: 0.1},
					{Quantile: 1, Value: 0.2},
				},
			}}}},
		},
	}
}

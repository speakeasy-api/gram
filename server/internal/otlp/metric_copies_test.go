package otlp

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// metricCopy describes one self-contained Gram copy of the OTLP metric schema.
type metricCopy struct {
	name                        string
	newMetric                   func() proto.Message
	gauge                       proto.Message
	sum                         proto.Message
	histogram                   proto.Message
	exponentialHistogram        proto.Message
	summary                     proto.Message
	numberDataPoint             proto.Message
	histogramDataPoint          proto.Message
	exponentialHistogramPoint   proto.Message
	exponentialHistogramBuckets proto.Message
	summaryDataPoint            proto.Message
	valueAtQuantile             proto.Message
	exemplar                    proto.Message
	resource                    proto.Message
	instrumentationScope        proto.Message
	anyValue                    proto.Message
	arrayValue                  proto.Message
	keyValueList                proto.Message
	keyValue                    proto.Message
	aggregationTemporality      protoreflect.EnumDescriptor
	dataPointFlags              protoreflect.EnumDescriptor
}

func metricCopies() []metricCopy {
	return []metricCopy{
		{
			name:                        "Metric",
			newMetric:                   func() proto.Message { return new(otelv1.Metric) },
			gauge:                       new(otelv1.Metric_Gauge),
			sum:                         new(otelv1.Metric_Sum),
			histogram:                   new(otelv1.Metric_Histogram),
			exponentialHistogram:        new(otelv1.Metric_ExponentialHistogram),
			summary:                     new(otelv1.Metric_Summary),
			numberDataPoint:             new(otelv1.Metric_NumberDataPoint),
			histogramDataPoint:          new(otelv1.Metric_HistogramDataPoint),
			exponentialHistogramPoint:   new(otelv1.Metric_ExponentialHistogramDataPoint),
			exponentialHistogramBuckets: new(otelv1.Metric_ExponentialHistogramDataPoint_Buckets),
			summaryDataPoint:            new(otelv1.Metric_SummaryDataPoint),
			valueAtQuantile:             new(otelv1.Metric_SummaryDataPoint_ValueAtQuantile),
			exemplar:                    new(otelv1.Metric_Exemplar),
			resource:                    new(otelv1.Metric_Resource),
			instrumentationScope:        new(otelv1.Metric_InstrumentationScope),
			anyValue:                    new(otelv1.Metric_AnyValue),
			arrayValue:                  new(otelv1.Metric_ArrayValue),
			keyValueList:                new(otelv1.Metric_KeyValueList),
			keyValue:                    new(otelv1.Metric_KeyValue),
			aggregationTemporality:      otelv1.Metric_AGGREGATION_TEMPORALITY_UNSPECIFIED.Descriptor(),
			dataPointFlags:              otelv1.Metric_DATA_POINT_FLAGS_DO_NOT_USE_UNSPECIFIED.Descriptor(),
		},
		{
			name:                        "InboundMetric",
			newMetric:                   func() proto.Message { return new(otelv1.InboundMetric) },
			gauge:                       new(otelv1.InboundMetric_Gauge),
			sum:                         new(otelv1.InboundMetric_Sum),
			histogram:                   new(otelv1.InboundMetric_Histogram),
			exponentialHistogram:        new(otelv1.InboundMetric_ExponentialHistogram),
			summary:                     new(otelv1.InboundMetric_Summary),
			numberDataPoint:             new(otelv1.InboundMetric_NumberDataPoint),
			histogramDataPoint:          new(otelv1.InboundMetric_HistogramDataPoint),
			exponentialHistogramPoint:   new(otelv1.InboundMetric_ExponentialHistogramDataPoint),
			exponentialHistogramBuckets: new(otelv1.InboundMetric_ExponentialHistogramDataPoint_Buckets),
			summaryDataPoint:            new(otelv1.InboundMetric_SummaryDataPoint),
			valueAtQuantile:             new(otelv1.InboundMetric_SummaryDataPoint_ValueAtQuantile),
			exemplar:                    new(otelv1.InboundMetric_Exemplar),
			resource:                    new(otelv1.InboundMetric_Resource),
			instrumentationScope:        new(otelv1.InboundMetric_InstrumentationScope),
			anyValue:                    new(otelv1.InboundMetric_AnyValue),
			arrayValue:                  new(otelv1.InboundMetric_ArrayValue),
			keyValueList:                new(otelv1.InboundMetric_KeyValueList),
			keyValue:                    new(otelv1.InboundMetric_KeyValue),
			aggregationTemporality:      otelv1.InboundMetric_AGGREGATION_TEMPORALITY_UNSPECIFIED.Descriptor(),
			dataPointFlags:              otelv1.InboundMetric_DATA_POINT_FLAGS_DO_NOT_USE_UNSPECIFIED.Descriptor(),
		},
	}
}

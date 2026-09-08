package otlp

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// logCopy describes one self-contained Gram copy of the OTLP log schema.
// Wire round-trip and descriptor drift tests range over this registry.
type logCopy struct {
	name                 string
	newRecord            func() proto.Message
	resource             proto.Message
	instrumentationScope proto.Message
	anyValue             proto.Message
	arrayValue           proto.Message
	keyValueList         proto.Message
	keyValue             proto.Message
	severityNumber       protoreflect.EnumDescriptor
}

func logCopies() []logCopy {
	return []logCopy{
		{
			name:                 "LogRecord",
			newRecord:            func() proto.Message { return &otelv1.LogRecord{} },
			resource:             &otelv1.LogRecord_Resource{},
			instrumentationScope: &otelv1.LogRecord_InstrumentationScope{},
			anyValue:             &otelv1.LogRecord_AnyValue{},
			arrayValue:           &otelv1.LogRecord_ArrayValue{},
			keyValueList:         &otelv1.LogRecord_KeyValueList{},
			keyValue:             &otelv1.LogRecord_KeyValue{},
			severityNumber:       otelv1.LogRecord_SEVERITY_NUMBER_UNSPECIFIED.Descriptor(),
		},
		{
			name:                 "InboundLogRecord",
			newRecord:            func() proto.Message { return &otelv1.InboundLogRecord{} },
			resource:             &otelv1.InboundLogRecord_Resource{},
			instrumentationScope: &otelv1.InboundLogRecord_InstrumentationScope{},
			anyValue:             &otelv1.InboundLogRecord_AnyValue{},
			arrayValue:           &otelv1.InboundLogRecord_ArrayValue{},
			keyValueList:         &otelv1.InboundLogRecord_KeyValueList{},
			keyValue:             &otelv1.InboundLogRecord_KeyValue{},
			severityNumber:       otelv1.InboundLogRecord_SEVERITY_NUMBER_UNSPECIFIED.Descriptor(),
		},
	}
}

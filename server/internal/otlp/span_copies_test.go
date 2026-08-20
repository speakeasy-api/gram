package otlp

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// spanCopy describes one self-contained Gram copy of the OTLP span schema.
// Both wire round-trip and descriptor drift tests range over this registry.
type spanCopy struct {
	name                 string
	newSpan              func() proto.Message
	event                proto.Message
	link                 proto.Message
	status               proto.Message
	resource             proto.Message
	instrumentationScope proto.Message
	anyValue             proto.Message
	arrayValue           proto.Message
	keyValueList         proto.Message
	keyValue             proto.Message
	spanKind             protoreflect.EnumDescriptor
	statusCode           protoreflect.EnumDescriptor
}

func spanCopies() []spanCopy {
	return []spanCopy{
		{
			name:                 "Span",
			newSpan:              func() proto.Message { return &otelv1.Span{} },
			event:                &otelv1.Span_Event{},
			link:                 &otelv1.Span_Link{},
			status:               &otelv1.Span_Status{},
			resource:             &otelv1.Span_Resource{},
			instrumentationScope: &otelv1.Span_InstrumentationScope{},
			anyValue:             &otelv1.Span_AnyValue{},
			arrayValue:           &otelv1.Span_ArrayValue{},
			keyValueList:         &otelv1.Span_KeyValueList{},
			keyValue:             &otelv1.Span_KeyValue{},
			spanKind:             otelv1.Span_SPAN_KIND_UNSPECIFIED.Descriptor(),
			statusCode:           otelv1.Span_STATUS_CODE_UNSPECIFIED.Descriptor(),
		},
		{
			name:                 "InboundSpan",
			newSpan:              func() proto.Message { return &otelv1.InboundSpan{} },
			event:                &otelv1.InboundSpan_Event{},
			link:                 &otelv1.InboundSpan_Link{},
			status:               &otelv1.InboundSpan_Status{},
			resource:             &otelv1.InboundSpan_Resource{},
			instrumentationScope: &otelv1.InboundSpan_InstrumentationScope{},
			anyValue:             &otelv1.InboundSpan_AnyValue{},
			arrayValue:           &otelv1.InboundSpan_ArrayValue{},
			keyValueList:         &otelv1.InboundSpan_KeyValueList{},
			keyValue:             &otelv1.InboundSpan_KeyValue{},
			spanKind:             otelv1.InboundSpan_SPAN_KIND_UNSPECIFIED.Descriptor(),
			statusCode:           otelv1.InboundSpan_STATUS_CODE_UNSPECIFIED.Descriptor(),
		},
	}
}

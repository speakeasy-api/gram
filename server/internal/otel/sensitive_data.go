package otel

import (
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
)

const redactedSensitiveDataValue = "[REDACTED]"

func redactSensitiveOTLP(message proto.Message) {
	if message == nil {
		return
	}

	switch value := message.(type) {
	case *logsv1.LogRecord:
		redactUnkeyedLogBody(value.GetBody())
	case *commonv1.KeyValue:
		if dialect.ClassifySensitiveDataKey(value.GetKey()) != dialect.SensitivityNone {
			value.Value = &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{StringValue: redactedSensitiveDataValue},
			}
			return
		}
	}

	reflected := message.ProtoReflect()
	if !reflected.IsValid() {
		return
	}
	reflected.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		redactSensitiveField(field, value)
		return true
	})
}

func redactSensitiveField(field protoreflect.FieldDescriptor, value protoreflect.Value) {
	switch {
	case field.IsMap():
		if isProtobufMessage(field.MapValue().Kind()) {
			value.Map().Range(func(_ protoreflect.MapKey, value protoreflect.Value) bool {
				redactSensitiveOTLP(value.Message().Interface())
				return true
			})
		}
	case field.IsList():
		if isProtobufMessage(field.Kind()) {
			values := value.List()
			for i := range values.Len() {
				redactSensitiveOTLP(values.Get(i).Message().Interface())
			}
		}
	case isProtobufMessage(field.Kind()):
		redactSensitiveOTLP(value.Message().Interface())
	}
}

func isProtobufMessage(kind protoreflect.Kind) bool {
	return kind == protoreflect.MessageKind || kind == protoreflect.GroupKind
}

func redactUnkeyedLogBody(value *commonv1.AnyValue) {
	if value == nil {
		return
	}
	switch body := value.GetValue().(type) {
	case nil, *commonv1.AnyValue_KvlistValue:
		return
	case *commonv1.AnyValue_ArrayValue:
		for _, item := range body.ArrayValue.GetValues() {
			redactUnkeyedLogBody(item)
		}
	default:
		value.Value = &commonv1.AnyValue_StringValue{StringValue: redactedSensitiveDataValue}
	}
}

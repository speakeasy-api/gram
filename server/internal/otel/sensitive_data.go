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
	redactSensitiveMessage(message.ProtoReflect())
}

func redactSensitiveMessage(message protoreflect.Message) {
	if !message.IsValid() {
		return
	}
	if record, ok := message.Interface().(*logsv1.LogRecord); ok {
		redactUnkeyedLogBody(record.GetBody())
	}
	if attribute, ok := message.Interface().(*commonv1.KeyValue); ok && dialect.ClassifySensitiveDataKey(attribute.GetKey()) != dialect.SensitivityNone {
		attribute.Value = &commonv1.AnyValue{
			Value: &commonv1.AnyValue_StringValue{StringValue: redactedSensitiveDataValue},
		}
		return
	}

	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			if field.MapValue().Kind() == protoreflect.MessageKind || field.MapValue().Kind() == protoreflect.GroupKind {
				value.Map().Range(func(_ protoreflect.MapKey, item protoreflect.Value) bool {
					redactSensitiveMessage(item.Message())
					return true
				})
			}
			return true
		}
		if field.IsList() {
			if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
				items := value.List()
				for i := range items.Len() {
					redactSensitiveMessage(items.Get(i).Message())
				}
			}
			return true
		}
		if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
			redactSensitiveMessage(value.Message())
		}
		return true
	})
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

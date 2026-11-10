package attr

import "go.opentelemetry.io/otel/attribute"

const (
	// StorageProtoMessageKey uses the storage metrics' compact, stable label contract.
	StorageProtoMessageKey = attribute.Key("proto_message")

	// StorageReasonKey is restricted by callers to finite failure/drop enums.
	StorageReasonKey = attribute.Key("reason")
)

func StorageProtoMessage(v string) attribute.KeyValue { return StorageProtoMessageKey.String(v) }

func StorageReason[S ~string](v S) attribute.KeyValue { return StorageReasonKey.String(string(v)) }

// Package storage runs generated analytical Pub/Sub storage subscriptions.
package storage

import (
	"github.com/parquet-go/parquet-go"
	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"google.golang.org/protobuf/proto"
)

// MappingVersion identifies the frozen protobuf-to-Parquet mapping contract.
const MappingVersion = "1"

// Metadata is the transport identity stored alongside every payload row.
type Metadata struct {
	// MessageID supports deduplication of Pub/Sub redelivery.
	MessageID string

	// ReceivedMicros is the UTC receipt time as Unix epoch microseconds.
	ReceivedMicros int64
}

// Definition is emitted by gen-storage. Applications install it without a
// message-processing callback. Schema and Decode must come from the same build.
type Definition struct {
	// Marker is the dedicated storage subscription's generated protobuf type.
	Marker proto.Message

	// Payload is the generated type of the schema-bound topic.
	Payload proto.Message

	// ProtoName is the fixed object prefix and marker's fully qualified name.
	ProtoName string

	// SubscriptionID is the resolved transport subscription, including overrides.
	SubscriptionID string

	// TopicID is the resolved transport topic recorded in row metadata.
	TopicID string

	// Bucket is the logical bucket resolved by deployment configuration.
	Bucket string

	// Partitioning selects daily, hourly or publisher-supplied Hive directories.
	Partitioning pubsubv1.StoragePartitioning

	// PartitionAttribute contains external Hive routing metadata.
	PartitionAttribute string

	// PartitionKeys is the immutable ordered external partition schema.
	PartitionKeys []string

	// Schema explicitly describes each physical and logical Parquet column.
	Schema *parquet.Schema

	// Fingerprint identifies the generated schema and field-number mapping.
	Fingerprint string

	// Decode unmarshals a protobuf and uses generated, statically typed accessors
	// to append a row. It never infers an analytical schema at runtime.
	Decode func([]byte, Metadata) (parquet.Row, error)
}

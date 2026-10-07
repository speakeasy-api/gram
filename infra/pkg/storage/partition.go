package storage

import (
	"regexp"
	"strings"
	"time"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
)

const (
	// partitionLimit bounds publisher-controlled object suffixes to 512 ASCII bytes.
	partitionLimit = 512

	// partitionValueLimit caps a component without relying on a regexp engine limit.
	partitionValueLimit = 128

	// partitionKeyLimit matches the descriptor validator's maximum Hive depth.
	partitionKeyLimit = 8
)

var partitionValue = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)

// DropReason is a finite-cardinality permanent routing failure.
type DropReason string

const (
	// DropNone indicates a valid route.
	DropNone DropReason = ""

	// DropMissing indicates the publisher omitted the declared attribute.
	DropMissing DropReason = "missing_partition_attribute"

	// DropMalformed indicates invalid syntax, key order or reserved values.
	DropMalformed DropReason = "malformed_partition_attribute"

	// DropLimit indicates the attribute exceeds a component, byte or depth limit.
	DropLimit DropReason = "partition_attribute_limit_exceeded"
)

func partition(def Definition, attributes map[string]string, received time.Time) (string, DropReason) {
	if def.Partitioning != pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL {
		layout := "part__year=2006/part__month=01/part__day=02"
		if def.Partitioning == pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_HOURLY {
			layout += "/part__hour=15"
		}
		return received.UTC().Format(layout), DropNone
	}
	value, ok := attributes[def.PartitionAttribute]
	if !ok {
		return "", DropMissing
	}
	if len(value) > partitionLimit {
		return "", DropLimit
	}
	parts := strings.Split(value, "/")
	if len(parts) > partitionKeyLimit {
		return "", DropLimit
	}
	if len(parts) != len(def.PartitionKeys) {
		return "", DropMalformed
	}
	for i, part := range parts {
		key, val, ok := strings.Cut(part, "=")
		if !ok || key != def.PartitionKeys[i] {
			return "", DropMalformed
		}
		if len(val) > partitionValueLimit {
			return "", DropLimit
		}
		if !partitionValue.MatchString(val) || strings.EqualFold(val, "NULL") || strings.EqualFold(val, "__HIVE_DEFAULT_PARTITION__") {
			return "", DropMalformed
		}
	}
	return value, DropNone
}

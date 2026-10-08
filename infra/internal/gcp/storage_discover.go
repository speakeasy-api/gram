package gcp

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
)

// maxPartitionKeys bounds the depth of an external Hive partition suffix.
const maxPartitionKeys = 8

var (
	// Logical names leave room for a project-number prefix within GCS's
	// 63-character single-component bucket name limit. Deployment validates
	// the complete physical name, including any environment prefix.
	logicalBucketPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)
	partitionKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
)

// DesiredStorage is the normalized configuration owned by a storage runner.
// Codec and Partitioning are resolved, never UNSPECIFIED.
type DesiredStorage struct {
	// Bucket is a deployment-resolved logical bucket name.
	Bucket string

	// Codec is the generated analytical encoding.
	Codec pubsubv1.StorageCodec

	// Partitioning selects the directories below the marker's proto full name.
	Partitioning pubsubv1.StoragePartitioning

	// PartitionAttribute names the publisher attribute for external routing.
	PartitionAttribute string

	// PartitionKeys is the exact ordered external partition schema.
	PartitionKeys []string
}

// StorageOptionsFromMessage extracts only the storage-consumer declaration;
// ordinary subscriber option resolution intentionally does not recognize it.
func StorageOptionsFromMessage(message protoreflect.MessageDescriptor) (*pubsubv1.StorageSubscriptionOptions, bool) {
	options, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || options == nil || !proto.HasExtension(options, pubsubv1.E_StorageSubscription) {
		return nil, false
	}
	value, ok := proto.GetExtension(options, pubsubv1.E_StorageSubscription).(*pubsubv1.StorageSubscriptionOptions)
	return value, ok && value != nil
}

func desiredStorageSubscription(message protoreflect.MessageDescriptor, opts *pubsubv1.StorageSubscriptionOptions) (DesiredSubscription, error) {
	if !logicalBucketPattern.MatchString(opts.GetBucket()) {
		return DesiredSubscription{}, fmt.Errorf("bucket %q must be a logical name of 3-40 lowercase letters, digits or hyphens, starting with a letter and ending with a letter or digit", opts.GetBucket())
	}
	if err := validateStorageDurations(opts); err != nil {
		return DesiredSubscription{}, err
	}
	codec := opts.GetCodec()
	switch codec {
	case pubsubv1.StorageCodec_STORAGE_CODEC_UNSPECIFIED:
		codec = pubsubv1.StorageCodec_STORAGE_CODEC_PARQUET
	case pubsubv1.StorageCodec_STORAGE_CODEC_PARQUET:
	default:
		return DesiredSubscription{}, fmt.Errorf("unsupported storage codec %d", codec)
	}
	partitioning := opts.GetPartitioning()
	switch partitioning {
	case pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_UNSPECIFIED:
		partitioning = pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_DAILY
	case pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_DAILY,
		pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_HOURLY,
		pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL:
	default:
		return DesiredSubscription{}, fmt.Errorf("unsupported storage partitioning %d", partitioning)
	}
	if partitioning == pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL {
		attribute := opts.GetPartitionAttribute()
		if attribute == "" || strings.TrimSpace(attribute) != attribute || strings.HasPrefix(strings.ToLower(attribute), "goog") || len(attribute) > 256 {
			return DesiredSubscription{}, fmt.Errorf("partition_attribute must be a nonempty Pub/Sub attribute key of at most 256 bytes without surrounding whitespace or the case-insensitive reserved goog prefix")
		}
		keys := opts.GetPartitionKeys()
		if len(keys) == 0 || len(keys) > maxPartitionKeys {
			return DesiredSubscription{}, fmt.Errorf("HIVE_EXTERNAL requires 1-%d partition_keys", maxPartitionKeys)
		}
		seen := make(map[string]bool, len(keys))
		for _, key := range keys {
			if !partitionKeyPattern.MatchString(key) {
				return DesiredSubscription{}, fmt.Errorf("invalid partition key %q: expected [a-z][a-z0-9_]{0,62}", key)
			}
			if seen[key] {
				return DesiredSubscription{}, fmt.Errorf("duplicate partition key %q", key)
			}
			seen[key] = true
		}
	} else if opts.HasPartitionAttribute() || len(opts.GetPartitionKeys()) != 0 {
		return DesiredSubscription{}, fmt.Errorf("partition_attribute and partition_keys are only valid with HIVE_EXTERNAL")
	}
	sub := desiredSubscriptionFromOptions(message, opts)
	sub.Storage = &DesiredStorage{
		Bucket:             opts.GetBucket(),
		Codec:              codec,
		Partitioning:       partitioning,
		PartitionAttribute: opts.GetPartitionAttribute(),
		PartitionKeys:      slices.Clone(opts.GetPartitionKeys()),
	}
	return sub, nil
}

func validateStorageDurations(opts *pubsubv1.StorageSubscriptionOptions) error {
	// Validate before AsDuration, which saturates overflow and normalizes
	// malformed nanos. Config Connector's transport fields use whole seconds.
	for _, field := range []struct {
		name  string
		value *durationpb.Duration
	}{
		{"retention", opts.GetRetention()},
		{"ack_deadline", opts.GetAckDeadline()},
		{"expiration_ttl", opts.GetExpirationTtl()},
		{"retry_policy.minimum_backoff", opts.GetRetryPolicy().GetMinimumBackoff()},
		{"retry_policy.maximum_backoff", opts.GetRetryPolicy().GetMaximumBackoff()},
	} {
		if field.value == nil {
			continue
		}
		if err := field.value.CheckValid(); err != nil {
			return fmt.Errorf("invalid %s: %w", field.name, err)
		}
		if field.value.GetNanos() != 0 || field.value.GetSeconds() < 0 {
			return fmt.Errorf("%s must be a nonnegative whole-second duration", field.name)
		}
	}
	if deadline := opts.GetAckDeadline(); deadline != nil {
		if d := deadline.AsDuration(); d < 10*time.Second || d > 600*time.Second {
			return fmt.Errorf("ack_deadline must be between 10s and 600s")
		}
	}
	return nil
}

func validateStorageSubscriptions(files *protoregistry.Files, topics []DesiredTopic, subs []DesiredSubscription) error {
	topicByMessage := make(map[string]DesiredTopic, len(topics))
	for _, topic := range topics {
		topicByMessage[topic.ProtoMessage] = topic
	}
	for _, sub := range subs {
		if sub.Storage == nil {
			continue
		}
		if topicByMessage[sub.TopicMessage].NameOverridden {
			return fmt.Errorf("storage subscription %s: topic %s overrides its name and has no attached schema", sub.ProtoMessage, sub.TopicMessage)
		}
		descriptor, err := files.FindDescriptorByName(protoreflect.FullName(sub.TopicMessage))
		if err != nil {
			return fmt.Errorf("storage subscription %s: resolve payload: %w", sub.ProtoMessage, err)
		}
		payload, ok := descriptor.(protoreflect.MessageDescriptor)
		if !ok {
			return fmt.Errorf("storage subscription %s: %s is not a message", sub.ProtoMessage, sub.TopicMessage)
		}
		if err := validateStoragePayload(payload); err != nil {
			return fmt.Errorf("storage subscription %s: %w", sub.ProtoMessage, err)
		}
		for _, key := range sub.Storage.PartitionKeys {
			for i := 0; i < payload.Fields().Len(); i++ {
				if field := payload.Fields().Get(i); strings.EqualFold(key, string(field.Name())) {
					return fmt.Errorf("storage subscription %s: partition key %q collides with payload column %s", sub.ProtoMessage, key, field.FullName())
				}
			}
		}
	}
	return nil
}

func validateStoragePayload(root protoreflect.MessageDescriptor) error {
	active := make(map[protoreflect.FullName]bool)
	complete := make(map[protoreflect.FullName]bool)
	var visit func(protoreflect.MessageDescriptor, []string) error
	visit = func(message protoreflect.MessageDescriptor, path []string) error {
		if active[message.FullName()] {
			return fmt.Errorf("recursive payload schema: %s -> %s", strings.Join(path, " -> "), message.FullName())
		}
		if complete[message.FullName()] {
			return nil
		}
		active[message.FullName()] = true
		for i := 0; i < message.Fields().Len(); i++ {
			field := message.Fields().Get(i)
			edge := string(field.FullName())
			if field.IsMap() {
				edge += "{value}"
				field = field.MapValue()
			} else if field.IsList() {
				edge += "[]"
			}
			if oneof := field.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
				edge += " (oneof " + string(oneof.Name()) + ")"
			}
			var target protoreflect.Descriptor
			if field.Message() != nil {
				target = field.Message()
			} else if field.Enum() != nil {
				target = field.Enum()
			} else {
				continue
			}
			next := append(slices.Clone(path), edge)
			// Attached Pub/Sub schemas are single-file, self-contained payloads.
			// Enforce this directly on descriptors, including repo-local imports,
			// rather than relying on a package prefix or a later compiler error.
			if target.ParentFile().Path() != root.ParentFile().Path() {
				return fmt.Errorf("external payload type: %s -> %s (%s); storage payloads must be self-contained in %s", strings.Join(next, " -> "), target.FullName(), target.ParentFile().Path(), root.ParentFile().Path())
			}
			if child := field.Message(); child != nil {
				if err := visit(child, next); err != nil {
					return err
				}
			}
		}
		delete(active, message.FullName())
		complete[message.FullName()] = true
		return nil
	}
	return visit(root, nil)
}

// ValidateStorageSchemas ensures every storage consumer's topic actually gets
// its schema attached in the emitted topology, rather than merely having a
// payload descriptor available to the generator.
func ValidateStorageSchemas(topics []DesiredTopic, subs []DesiredSubscription, schemas []DesiredSchema) error {
	attached := make(map[string]bool, len(topics))
	for _, topic := range topics {
		if topic.NameOverridden {
			continue
		}
		for _, schema := range schemas {
			if schema.ProtoMessage == topic.ProtoMessage {
				attached[topic.ProtoMessage] = true
				break
			}
		}
	}
	for _, sub := range subs {
		if sub.Storage != nil && !attached[sub.TopicMessage] {
			return fmt.Errorf("storage subscription %s: topic %s has no attached schema", sub.ProtoMessage, sub.TopicMessage)
		}
	}
	return nil
}

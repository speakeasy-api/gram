package gcp

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"sigs.k8s.io/yaml"
)

func storageFixture(t *testing.T) (*descriptorpb.FileDescriptorSet, *descriptorpb.DescriptorProto, *descriptorpb.DescriptorProto, *pubsubv1.StorageSubscriptionOptions) {
	t.Helper()
	payload := &descriptorpb.DescriptorProto{Name: new("Event"), Options: topicOptions(t), Field: []*descriptorpb.FieldDescriptorProto{stringField("id", 1)}}
	opts := pubsubv1.StorageSubscriptionOptions_builder{Topic: new("test.storage.v1.Event"), Bucket: new("event-archive")}.Build()
	marker := &descriptorpb.DescriptorProto{Name: new("EventArchive"), Options: &descriptorpb.MessageOptions{}}
	proto.SetExtension(marker.Options, pubsubv1.E_StorageSubscription, opts)
	set := &descriptorpb.FileDescriptorSet{File: append(wellKnownSchemaDeps(),
		&descriptorpb.FileDescriptorProto{
			Name: new("test/storage/v1/event.proto"), Package: new("test.storage.v1"), Syntax: new("proto3"),
			Dependency: []string{"gcp/pubsub/v1/options.proto"}, MessageType: []*descriptorpb.DescriptorProto{payload},
		},
		&descriptorpb.FileDescriptorProto{
			Name: new("test/storage/v1/archive.proto"), Package: new("test.storage.v1"), Syntax: new("proto3"),
			Dependency: []string{"gcp/pubsub/v1/options.proto"}, MessageType: []*descriptorpb.DescriptorProto{marker},
		},
	)}
	return set, payload, marker, opts
}

func discoverStorageFixture(t *testing.T, set *descriptorpb.FileDescriptorSet) ([]DesiredTopic, []DesiredSubscription, error) {
	t.Helper()
	raw, err := proto.Marshal(set)
	require.NoError(t, err)
	return DiscoverPubSub(raw)
}

func TestStorageSubscription_DefaultsAndTransport(t *testing.T) {
	t.Parallel()
	set, _, _, opts := storageFixture(t)
	opts.SetRetention(durationpb.New(24 * time.Hour))
	opts.SetAckDeadline(durationpb.New(60 * time.Second))
	opts.SetRetryPolicy(pubsubv1.RetryPolicy_builder{}.Build())
	opts.SetDeadLetter(pubsubv1.DeadLetterPolicy_builder{MaxDeliveryAttempts: new(int32(10))}.Build())
	topics, subs, err := discoverStorageFixture(t, set)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Len(t, topics, 2)
	sub := subs[0]
	require.Equal(t, "test-storage-v1-event-archive", sub.Name)
	require.Equal(t, "test-storage-v1-event", sub.Topic)
	require.Equal(t, time.Minute, sub.AckDeadline)
	require.Equal(t, 24*time.Hour, sub.Retention)
	require.Equal(t, 10*time.Second, sub.RetryPolicy.MinimumBackoff)
	require.Equal(t, 600*time.Second, sub.RetryPolicy.MaximumBackoff)
	require.Equal(t, "test-storage-v1-event-archive-dlq", sub.DeadLetterTopic)
	require.Equal(t, int32(10), sub.MaxDeliveryAttempts)
	require.Equal(t, pubsubv1.StorageCodec_STORAGE_CODEC_PARQUET, sub.Storage.Codec)
	require.Equal(t, pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_DAILY, sub.Storage.Partitioning)
	require.Equal(t, "event-archive", sub.Storage.Bucket)
}

func TestStorageSubscription_ExplicitModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []pubsubv1.StoragePartitioning{
		pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_DAILY,
		pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_HOURLY,
		pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			set, _, _, opts := storageFixture(t)
			opts.SetCodec(pubsubv1.StorageCodec_STORAGE_CODEC_PARQUET)
			opts.SetPartitioning(mode)
			if mode == pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL {
				opts.SetPartitionAttribute("storage_partition")
				opts.SetPartitionKeys([]string{"region", "date"})
			}
			_, subs, err := discoverStorageFixture(t, set)
			require.NoError(t, err)
			require.Equal(t, mode, subs[0].Storage.Partitioning)
			require.Equal(t, opts.GetPartitionKeys(), subs[0].Storage.PartitionKeys)
		})
	}
}

func TestStorageSubscription_InvalidOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*pubsubv1.StorageSubscriptionOptions)
		want string
	}{
		{"empty bucket", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetBucket("") }, "logical name"},
		{"physical bucket URI", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetBucket("gs://example") }, "logical name"},
		{"bucket normalization", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetBucket(" Event-archive ") }, "logical name"},
		{"codec", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetCodec(pubsubv1.StorageCodec(99)) }, "unsupported storage codec 99"},
		{"partitioning", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetPartitioning(pubsubv1.StoragePartitioning(99)) }, "unsupported storage partitioning 99"},
		{"built-in attribute", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetPartitionAttribute("") }, "only valid with HIVE_EXTERNAL"},
		{"built-in keys", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetPartitionKeys([]string{"region"}) }, "only valid with HIVE_EXTERNAL"},
		{"unknown topic", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetTopic("test.storage.v1.Missing") }, "unknown topic message"},
		{"negative retention", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetRetention(durationpb.New(-time.Second)) }, "nonnegative whole-second"},
		{"fractional deadline", func(o *pubsubv1.StorageSubscriptionOptions) {
			o.SetAckDeadline(durationpb.New(15*time.Second + time.Nanosecond))
		}, "whole-second"},
		{"short deadline", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetAckDeadline(durationpb.New(time.Second)) }, "between 10s and 600s"},
		{"long deadline", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetAckDeadline(durationpb.New(601 * time.Second)) }, "between 10s and 600s"},
		{"bad duration", func(o *pubsubv1.StorageSubscriptionOptions) {
			o.SetRetention(&durationpb.Duration{Nanos: 2_000_000_000})
		}, "invalid retention"},
		{"short retention", func(o *pubsubv1.StorageSubscriptionOptions) { o.SetRetention(durationpb.New(time.Second)) }, "invalid retention"},
		{"invalid DLQ", func(o *pubsubv1.StorageSubscriptionOptions) {
			o.SetDeadLetter(pubsubv1.DeadLetterPolicy_builder{MaxDeliveryAttempts: new(int32(1))}.Build())
		}, "max delivery attempts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			set, _, _, opts := storageFixture(t)
			tt.edit(opts)
			_, _, err := discoverStorageFixture(t, set)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestStorageSubscription_InvalidExternalConfiguration(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		attribute string
		keys      []string
		want      string
	}{
		{"missing attribute", "", []string{"region"}, "partition_attribute"},
		{"reserved attribute", "googPartition", []string{"region"}, "partition_attribute"},
		{"missing keys", "partition", nil, "requires 1-8"},
		{"too many keys", "partition", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, "requires 1-8"},
		{"duplicate", "partition", []string{"region", "region"}, "duplicate partition key"},
		{"uppercase", "partition", []string{"Region"}, "invalid partition key"},
		{"slash", "partition", []string{"a/b"}, "invalid partition key"},
		{"payload collision", "partition", []string{"id"}, "collides with payload column"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			set, _, _, opts := storageFixture(t)
			opts.SetPartitioning(pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL)
			opts.SetPartitionAttribute(tt.attribute)
			opts.SetPartitionKeys(tt.keys)
			_, _, err := discoverStorageFixture(t, set)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestStorageSubscription_ExclusiveOptions(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"topic", "subscription"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			set, _, marker, _ := storageFixture(t)
			if kind == "topic" {
				proto.SetExtension(marker.Options, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())
			} else {
				proto.SetExtension(marker.Options, pubsubv1.E_Subscription, pubsubv1.SubscriptionOptions_builder{Topic: new("test.storage.v1.Event")}.Build())
			}
			_, _, err := discoverStorageFixture(t, set)
			require.ErrorContains(t, err, "declare them on separate marker messages")
		})
	}
}

func TestStorageSubscription_CollidesWithOrdinarySubscription(t *testing.T) {
	t.Parallel()
	set, _, _, opts := storageFixture(t)
	opts.SetName("same-name")
	ordinary := &descriptorpb.DescriptorProto{Name: new("Processor"), Options: &descriptorpb.MessageOptions{}}
	proto.SetExtension(ordinary.Options, pubsubv1.E_Subscription, pubsubv1.SubscriptionOptions_builder{Name: new("same-name"), Topic: new("test.storage.v1.Event")}.Build())
	set.File[len(set.File)-1].MessageType = append(set.File[len(set.File)-1].MessageType, ordinary)
	_, _, err := discoverStorageFixture(t, set)
	require.ErrorContains(t, err, `subscription "same-name" is declared multiple times`)
}

func TestStorageSubscription_RequiresAttachedSchema(t *testing.T) {
	t.Parallel()
	set, payload, _, _ := storageFixture(t)
	proto.SetExtension(payload.Options, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{Name: new("shared-topic")}.Build())
	_, _, err := discoverStorageFixture(t, set)
	require.ErrorContains(t, err, "overrides its name and has no attached schema")

	proto.SetExtension(payload.Options, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())
	topics, subs, err := discoverStorageFixture(t, set)
	require.NoError(t, err)
	require.ErrorContains(t, ValidateStorageSchemas(topics, subs, nil), "has no attached schema")
	require.NoError(t, ValidateStorageSchemas(topics, subs, []DesiredSchema{{ProtoMessage: "test.storage.v1.Event"}}))
}

func storageMessageField(name string, number int32, target string) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{Name: new(name), Number: new(number), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: new(target)}
}

func TestStorageSubscription_RejectsExternalPayloadTypes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		file   *descriptorpb.FileDescriptorProto
		target string
		enum   bool
	}{
		{"timestamp", protodesc.ToFileDescriptorProto(timestamppb.File_google_protobuf_timestamp_proto), ".google.protobuf.Timestamp", false},
		{"duration", protodesc.ToFileDescriptorProto(durationpb.File_google_protobuf_duration_proto), ".google.protobuf.Duration", false},
		{"external enum", &descriptorpb.FileDescriptorProto{Name: new("dependency/types.proto"), Package: new("dependency"), Syntax: new("proto3"), EnumType: []*descriptorpb.EnumDescriptorProto{{Name: new("Kind"), Value: []*descriptorpb.EnumValueDescriptorProto{{Name: new("UNSPECIFIED"), Number: new(int32(0))}}}}}, ".dependency.Kind", true},
	} {
		for _, edge := range []string{"singular", "repeated", "oneof", "map"} {
			t.Run(tt.name+"/"+edge, func(t *testing.T) {
				t.Parallel()
				set, payload, _, _ := storageFixture(t)
				field := storageMessageField("external", 2, tt.target)
				if tt.enum {
					field.Type = descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum()
				}
				switch edge {
				case "repeated":
					field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
				case "oneof":
					payload.OneofDecl = []*descriptorpb.OneofDescriptorProto{{Name: new("choice")}}
					field.OneofIndex = new(int32(0))
				case "map":
					field.Name = new("value")
					field.Number = new(int32(2))
					payload.NestedType = []*descriptorpb.DescriptorProto{{Name: new("ExternalEntry"), Options: &descriptorpb.MessageOptions{MapEntry: new(true)}, Field: []*descriptorpb.FieldDescriptorProto{stringField("key", 1), field}}}
					field = storageMessageField("external", 2, ".test.storage.v1.Event.ExternalEntry")
					field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
				}
				payload.Field = append(payload.Field, field)
				payloadFile := set.File[len(set.File)-2]
				payloadFile.Dependency = append(payloadFile.Dependency, tt.file.GetName())
				if !slices.ContainsFunc(set.File, func(f *descriptorpb.FileDescriptorProto) bool { return f.GetName() == tt.file.GetName() }) {
					set.File = append(set.File, tt.file)
				}
				_, _, err := discoverStorageFixture(t, set)
				require.ErrorContains(t, err, "storage subscription test.storage.v1.EventArchive")
				require.ErrorContains(t, err, "external payload type")
				require.ErrorContains(t, err, "Event.external")
				require.ErrorContains(t, err, tt.file.GetName())
			})
		}
	}
}

func TestStorageSubscription_RejectsRecursionWithPath(t *testing.T) {
	t.Parallel()
	for _, edge := range []string{"direct", "repeated", "oneof", "map"} {
		t.Run(edge, func(t *testing.T) {
			t.Parallel()
			set, payload, _, _ := storageFixture(t)
			child := &descriptorpb.DescriptorProto{Name: new("Child"), Field: []*descriptorpb.FieldDescriptorProto{storageMessageField("parent", 1, ".test.storage.v1.Event")}}
			field := storageMessageField("child", 2, ".test.storage.v1.Event.Child")
			switch edge {
			case "direct":
				field.TypeName = new(".test.storage.v1.Event")
			case "repeated":
				field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
			case "oneof":
				payload.OneofDecl = []*descriptorpb.OneofDescriptorProto{{Name: new("choice")}}
				field.OneofIndex = new(int32(0))
			case "map":
				child.Name = new("ChildEntry")
				child.Options = &descriptorpb.MessageOptions{MapEntry: new(true)}
				child.Field = []*descriptorpb.FieldDescriptorProto{stringField("key", 1), storageMessageField("value", 2, ".test.storage.v1.Event")}
				field.TypeName = new(".test.storage.v1.Event.ChildEntry")
				field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
			}
			payload.NestedType = []*descriptorpb.DescriptorProto{child}
			payload.Field = append(payload.Field, field)
			_, _, err := discoverStorageFixture(t, set)
			require.ErrorContains(t, err, "recursive payload schema")
			require.ErrorContains(t, err, "Event.child")
			require.ErrorContains(t, err, " -> test.storage.v1.Event")
		})
	}
}

func TestStorageSubscription_AllowsSharedAcyclicTypes(t *testing.T) {
	t.Parallel()
	set, payload, _, _ := storageFixture(t)
	payload.NestedType = []*descriptorpb.DescriptorProto{{Name: new("Child"), Field: []*descriptorpb.FieldDescriptorProto{stringField("value", 1)}}}
	payload.Field = append(payload.Field, storageMessageField("left", 2, ".test.storage.v1.Event.Child"), storageMessageField("right", 3, ".test.storage.v1.Event.Child"))
	_, _, err := discoverStorageFixture(t, set)
	require.NoError(t, err)
}

func TestStorageBuckets_DeduplicatedPrivateAndPreserved(t *testing.T) {
	t.Parallel()
	subs := []DesiredSubscription{
		{Name: "archive-one", Storage: &DesiredStorage{Bucket: "zebra-archive"}},
		{Name: "archive-two", Storage: &DesiredStorage{Bucket: "alpha-archive"}},
		{Name: "archive-three", Storage: &DesiredStorage{Bucket: "zebra-archive"}},
		{Name: "ordinary"},
	}
	doc := buildPubSubValues(t.Context(), slog.New(slog.DiscardHandler), nil, subs, nil)
	require.Equal(t, []string{storageAPI}, doc.Storage.APIs)
	require.Len(t, doc.Storage.Buckets, 2)
	require.Equal(t, "alpha-archive", doc.Storage.Buckets[0].Name)
	require.Equal(t, "zebra-archive", doc.Storage.Buckets[1].Name)
	for _, bucket := range doc.Storage.Buckets {
		require.Equal(t, "abandon", bucket.Annotations["cnrm.cloud.google.com/deletion-policy"])
		require.Equal(t, "false", bucket.Annotations["cnrm.cloud.google.com/force-destroy"])
		require.Equal(t, "enforced", *bucket.Spec.PublicAccessPrevention)
		require.True(t, *bucket.Spec.UniformBucketLevelAccess)
		require.Nil(t, bucket.Spec.ResourceID)
		require.Empty(t, bucket.Spec.LifecycleRule)
	}
	first, err := yaml.Marshal(doc)
	require.NoError(t, err)
	slices.Reverse(subs)
	second, err := yaml.Marshal(buildPubSubValues(t.Context(), slog.New(slog.DiscardHandler), nil, subs, nil))
	require.NoError(t, err)
	require.Equal(t, string(first), string(second))
	withoutStorage, err := yaml.Marshal(buildPubSubValues(t.Context(), slog.New(slog.DiscardHandler), nil, nil, nil))
	require.NoError(t, err)
	require.NotContains(t, string(withoutStorage), "storage:")
}

func TestStorageTopology_GenerateAndPreserveOutputOnError(t *testing.T) {
	t.Parallel()
	requireBuf(t)
	set, _, _, opts := storageFixture(t)
	root := t.TempDir()
	source := filepath.Join(root, "test/storage/v1/event.proto")
	require.NoError(t, os.MkdirAll(filepath.Dir(source), 0o750))
	require.NoError(t, os.WriteFile(source, []byte(`syntax = "proto3";
package test.storage.v1;
import "gcp/pubsub/v1/options.proto";
message Event {
  string id = 1;
  option (gcp.pubsub.v1.topic) = {};
}
`), 0o600))
	raw, err := proto.Marshal(set)
	require.NoError(t, err)
	out := filepath.Join(root, "kcc.yaml")
	cc := NewCCPubSub(slog.New(slog.DiscardHandler), out, raw, root)
	require.NoError(t, cc.Generate(t.Context()))
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(data), "storage:")
	require.Contains(t, string(data), "name: event-archive")
	require.Contains(t, string(data), "name: test-storage-v1-event-archive")
	require.Contains(t, string(data), "schemaSettings:")
	require.Contains(t, string(data), "publicAccessPrevention: enforced")
	require.Contains(t, string(data), "cnrm.cloud.google.com/deletion-policy: abandon")

	opts.SetCodec(pubsubv1.StorageCodec(99))
	cc.descriptors, err = proto.Marshal(set)
	require.NoError(t, err)
	require.ErrorContains(t, cc.Generate(t.Context()), "unsupported storage codec")
	after, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, data, after)
}

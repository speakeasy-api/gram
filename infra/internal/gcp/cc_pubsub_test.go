package gcp

import (
	"log/slog"
	"os"
	"testing"
	"time"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestBuildPubSubValues_StableOrder(t *testing.T) {
	t.Parallel()

	topics := []DesiredTopic{
		{
			Name:         "zebra-topic",
			Retention:    24 * time.Hour,
			Labels:       map[string]string{"managed_by": managedByLabel},
			ProtoMessage: "example.v1.Zebra",
		},
		{
			Name:         "alpha-topic",
			Retention:    0,
			Labels:       map[string]string{"managed_by": managedByLabel},
			ProtoMessage: "example.v1.Alpha",
		},
	}

	subs := []DesiredSubscription{
		{
			Name:                "zebra-sub",
			Topic:               "outbox-event",
			TopicMessage:        "example.v1.Event",
			Retention:           7 * 24 * time.Hour,
			RetainAckedMessages: true,
			AckDeadline:         30 * time.Second,
			ExpirationTTL:       0,
			RetryPolicy:         nil,
			Labels:              map[string]string{"managed_by": managedByLabel},
			Filter:              "",
			DeadLetterTopic:     "",
			MaxDeliveryAttempts: 0,
			ProtoMessage:        "example.v1.ZebraProcessor",
		},
		{
			Name:                "alpha-sub",
			Topic:               "outbox-event",
			TopicMessage:        "example.v1.Event",
			Retention:           0,
			RetainAckedMessages: false,
			AckDeadline:         0,
			ExpirationTTL:       48 * time.Hour,
			RetryPolicy: &DesiredRetryPolicy{
				MinimumBackoff: 10 * time.Second,
				MaximumBackoff: 60 * time.Second,
			},
			Labels:              map[string]string{"managed_by": managedByLabel},
			Filter:              `attributes.env="prod"`,
			DeadLetterTopic:     "outbox-event-dlq",
			MaxDeliveryAttempts: 5,
			ProtoMessage:        "example.v1.AlphaProcessor",
		},
	}

	doc := buildPubSubValues(t.Context(), slog.New(slog.DiscardHandler), topics, subs, []DesiredSchema{})

	require.True(t, doc.PubSub.Enabled)
	require.Equal(t, []string{pubsubAPI}, doc.PubSub.APIs)

	// Topics sorted alphabetically.
	require.Len(t, doc.PubSub.Topics, 2)
	require.Equal(t, "alpha-topic", doc.PubSub.Topics[0].Name)
	require.Equal(t, "zebra-topic", doc.PubSub.Topics[1].Name)
	// Retention rendered only when set.
	require.Nil(t, doc.PubSub.Topics[0].Spec.MessageRetentionDuration)
	require.NotNil(t, doc.PubSub.Topics[1].Spec.MessageRetentionDuration)
	require.Equal(t, "86400s", *doc.PubSub.Topics[1].Spec.MessageRetentionDuration)
	// proto_message label carries the protobuf message name sanitized into a
	// valid GCP label value (kebab-cased) alongside the existing managed_by label.
	require.Equal(t, "example-v1-alpha", doc.PubSub.Topics[0].Labels[protoMessageLabel])
	require.Equal(t, "example-v1-zebra", doc.PubSub.Topics[1].Labels[protoMessageLabel])
	require.Equal(t, managedByLabel, doc.PubSub.Topics[0].Labels["managed_by"])

	// Subscriptions sorted alphabetically.
	require.Len(t, doc.PubSub.Subscriptions, 2)
	require.Equal(t, "alpha-sub", doc.PubSub.Subscriptions[0].Name)
	require.Equal(t, "zebra-sub", doc.PubSub.Subscriptions[1].Name)
	require.Equal(t, "example-v1-alpha-processor", doc.PubSub.Subscriptions[0].Labels[protoMessageLabel])
	require.Equal(t, "example-v1-zebra-processor", doc.PubSub.Subscriptions[1].Labels[protoMessageLabel])
	// Subscriptions also carry the consumed topic's message name.
	require.Equal(t, "example-v1-event", doc.PubSub.Subscriptions[0].Labels[topicMessageLabel])
	require.Equal(t, "example-v1-event", doc.PubSub.Subscriptions[1].Labels[topicMessageLabel])
	// Topics do not get a topic_message label.
	require.NotContains(t, doc.PubSub.Topics[0].Labels, topicMessageLabel)

	alpha := doc.PubSub.Subscriptions[0].Spec
	require.Equal(t, "outbox-event", alpha.TopicRef.Name)
	require.NotNil(t, alpha.ExpirationPolicy)
	require.Equal(t, "172800s", alpha.ExpirationPolicy.Ttl)
	require.NotNil(t, alpha.RetryPolicy)
	require.Equal(t, "10s", *alpha.RetryPolicy.MinimumBackoff)
	require.Equal(t, "60s", *alpha.RetryPolicy.MaximumBackoff)
	require.NotNil(t, alpha.DeadLetterPolicy)
	require.Equal(t, "outbox-event-dlq", alpha.DeadLetterPolicy.DeadLetterTopicRef.Name)
	require.Equal(t, `attributes.env="prod"`, *alpha.Filter)

	zebra := doc.PubSub.Subscriptions[1].Spec
	require.NotNil(t, zebra.AckDeadlineSeconds)
	require.Equal(t, int64(30), *zebra.AckDeadlineSeconds)
	require.NotNil(t, zebra.MessageRetentionDuration)
	require.Equal(t, "604800s", *zebra.MessageRetentionDuration)
}

// TestBuildPubSubValues_SchemaAttachment verifies that a topic derived from the
// same proto message as a generated schema gets that schema attached via
// schemaSettings, unless the topic sets an explicit name option (in which case
// the schema is left unattached).
func TestBuildPubSubValues_SchemaAttachment(t *testing.T) {
	t.Parallel()

	topics := []DesiredTopic{
		{
			Name:           "example-v1-event",
			Labels:         map[string]string{"managed_by": managedByLabel},
			ProtoMessage:   "example.v1.Event",
			NameOverridden: false,
		},
		{
			// Same proto message as a schema, but an explicit name override.
			Name:           "shared-topic",
			Labels:         map[string]string{"managed_by": managedByLabel},
			ProtoMessage:   "example.v1.Shared",
			NameOverridden: true,
		},
		{
			// No matching schema (e.g. a synthesized DLQ topic).
			Name:         "example-v1-event-dlq",
			Labels:       map[string]string{"managed_by": managedByLabel, "dlq_for": "example-v1-processor"},
			ProtoMessage: "example.v1.Processor",
		},
	}

	schemas := []DesiredSchema{
		{
			Name:         "example-v1-event",
			ProtoMessage: "example.v1.Event",
			Definition:   "edition = \"2024\";\n",
			Labels:       map[string]string{"managed_by": managedByLabel},
		},
		{
			Name:         "example-v1-shared",
			ProtoMessage: "example.v1.Shared",
			Definition:   "edition = \"2024\";\n",
			Labels:       map[string]string{"managed_by": managedByLabel},
		},
	}

	doc := buildPubSubValues(t.Context(), slog.New(slog.DiscardHandler), topics, nil, schemas)

	byName := map[string]pubSubTopicValue{}
	for _, topic := range doc.PubSub.Topics {
		byName[topic.Name] = topic
	}

	// Topic with no name override gets the matching schema attached.
	event := byName["example-v1-event"].Spec
	require.NotNil(t, event.SchemaSettings)
	require.Equal(t, "example-v1-event", event.SchemaSettings.SchemaRef.Name)
	require.NotNil(t, event.SchemaSettings.Encoding)
	require.Equal(t, schemaEncodingBinary, *event.SchemaSettings.Encoding)

	// Topic with an explicit name override is left unattached.
	require.Nil(t, byName["shared-topic"].Spec.SchemaSettings)

	// Topic without a matching schema is left unattached.
	require.Nil(t, byName["example-v1-event-dlq"].Spec.SchemaSettings)
}

func TestCCPubSub_WriteValues(t *testing.T) {
	t.Parallel()

	out := t.TempDir() + "/pubsub-values.yaml"
	cc := NewCCPubSub(slog.New(slog.DiscardHandler), out, nil, "")

	topics := []DesiredTopic{
		{Name: "outbox-event", Retention: 7 * 24 * time.Hour, Labels: map[string]string{"managed_by": managedByLabel}, ProtoMessage: "gram.outbox.v1.Event"},
	}
	subs := []DesiredSubscription{
		{Name: "outbox-processor", Topic: "outbox-event", TopicMessage: "gram.outbox.v1.Event", AckDeadline: 30 * time.Second, Labels: map[string]string{"managed_by": managedByLabel}, ProtoMessage: "gram.outbox.v1.Processor"},
	}

	err := cc.writeValues(t.Context(), buildPubSubValues(t.Context(), slog.New(slog.DiscardHandler), topics, subs, []DesiredSchema{}))
	require.NoError(t, err)

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	content := string(data)

	require.Contains(t, content, "DO NOT EDIT")
	require.Contains(t, content, "pubsub:")
	require.Contains(t, content, "enabled: true")
	require.Contains(t, content, pubsubAPI)
	require.Contains(t, content, "outbox-event")
	require.Contains(t, content, "outbox-processor")
	require.Contains(t, content, "604800s")
	require.Contains(t, content, "ackDeadlineSeconds: 30")
	require.Contains(t, content, "proto_message: gram-outbox-v1-event")
	require.Contains(t, content, "proto_message: gram-outbox-v1-processor")
	require.Contains(t, content, "topic_proto_message: gram-outbox-v1-event")

	// Per-resource metadata now lives in the chart template, not the values doc.
	require.NotContains(t, content, "{{ .Values")
	require.NotContains(t, content, "cnrm.cloud.google.com/project-id")
	require.NotContains(t, content, "apiVersion")
	require.NotContains(t, content, "PubSubTopic")
}

// TestDiscoverPubSub_DeprecatedOption verifies that a marker message carrying
// the standard protobuf `option deprecated = true` produces a "deprecated"
// label on the generated topic/subscription (and the synthesized DLQ), while a
// non-deprecated message gets no such label.
func TestDiscoverPubSub_DeprecatedOption(t *testing.T) {
	t.Parallel()

	const pkg = "test.deprecated.v1"

	deprecatedTopicOpts := &descriptorpb.MessageOptions{Deprecated: new(true)}
	proto.SetExtension(deprecatedTopicOpts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{
		RetentionHint: durationpb.New(24 * time.Hour),
	}.Build())

	activeTopicOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(activeTopicOpts, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())

	deprecatedSubOpts := &descriptorpb.MessageOptions{Deprecated: new(true)}
	proto.SetExtension(deprecatedSubOpts, pubsubv1.E_Subscription, pubsubv1.SubscriptionOptions_builder{
		Topic: new(pkg + ".Event"),
		DeadLetter: pubsubv1.DeadLetterPolicy_builder{
			MaxDeliveryAttempts: new(int32(5)),
		}.Build(),
	}.Build())

	fileProto := &descriptorpb.FileDescriptorProto{
		Name:       new("test/deprecated/v1/test.proto"),
		Package:    new(pkg),
		Syntax:     new("proto3"),
		Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: new("Event"), Options: deprecatedTopicOpts},
			{Name: new("ActiveEvent"), Options: activeTopicOpts},
			{Name: new("Processor"), Options: deprecatedSubOpts},
		},
	}

	set := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
			protodesc.ToFileDescriptorProto(durationpb.File_google_protobuf_duration_proto),
			protodesc.ToFileDescriptorProto(pubsubv1.File_gcp_pubsub_v1_options_proto),
			fileProto,
		},
	}
	raw, err := proto.Marshal(set)
	require.NoError(t, err)

	topics, subs, err := DiscoverPubSub(raw)
	require.NoError(t, err)

	topicsByName := map[string]DesiredTopic{}
	for _, topic := range topics {
		topicsByName[topic.Name] = topic
	}
	subsByName := map[string]DesiredSubscription{}
	for _, sub := range subs {
		subsByName[sub.Name] = sub
	}

	// Deprecated topic carries the label; active topic does not.
	require.Equal(t, deprecatedLabelValue, topicsByName["test-deprecated-v1-event"].Labels[deprecatedLabelKey])
	require.NotContains(t, topicsByName["test-deprecated-v1-active-event"].Labels, deprecatedLabelKey)

	// Deprecated subscription carries the label, and the label propagates to its
	// synthesized dead-letter topic.
	require.Equal(t, deprecatedLabelValue, subsByName["test-deprecated-v1-processor"].Labels[deprecatedLabelKey])
	require.Equal(t, deprecatedLabelValue, topicsByName["test-deprecated-v1-processor-dlq"].Labels[deprecatedLabelKey])
}

func TestDurationToGCPString(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0s", durationToGCPString(0))
	require.Equal(t, "30s", durationToGCPString(30*time.Second))
	require.Equal(t, "86400s", durationToGCPString(24*time.Hour))
	require.Equal(t, "604800s", durationToGCPString(7*24*time.Hour))
}

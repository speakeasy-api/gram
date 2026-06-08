package gcp

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

	doc := buildPubSubValues(topics, subs)

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

func TestCCPubSub_WriteValues(t *testing.T) {
	t.Parallel()

	out := t.TempDir() + "/pubsub-values.yaml"
	cc := NewCCPubSub(slog.New(slog.DiscardHandler), out, nil)

	topics := []DesiredTopic{
		{Name: "outbox-event", Retention: 7 * 24 * time.Hour, Labels: map[string]string{"managed_by": managedByLabel}, ProtoMessage: "gram.outbox.v1.Event"},
	}
	subs := []DesiredSubscription{
		{Name: "outbox-processor", Topic: "outbox-event", TopicMessage: "gram.outbox.v1.Event", AckDeadline: 30 * time.Second, Labels: map[string]string{"managed_by": managedByLabel}, ProtoMessage: "gram.outbox.v1.Processor"},
	}

	err := cc.writeValues(t.Context(), buildPubSubValues(topics, subs))
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

func TestDurationToGCPString(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0s", durationToGCPString(0))
	require.Equal(t, "30s", durationToGCPString(30*time.Second))
	require.Equal(t, "86400s", durationToGCPString(24*time.Hour))
	require.Equal(t, "604800s", durationToGCPString(7*24*time.Hour))
}

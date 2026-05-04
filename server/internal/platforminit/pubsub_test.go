package platforminit

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDedupeAndValidate_HappyPathWithSynthesizedDLQ(t *testing.T) {
	t.Parallel()

	topics := []DesiredTopic{
		{
			Name:         "outbox-event",
			Retention:    7 * 24 * time.Hour,
			Labels:       map[string]string{"managed_by": managedByLabel},
			ProtoMessage: "outbox.event.v1.Event",
		},
	}
	subs := []DesiredSubscription{
		{
			Name:                "outbox-processor",
			Topic:               "",
			TopicMessage:        "outbox.event.v1.Event",
			Retention:           0,
			RetainAckedMessages: false,
			AckDeadline:         30 * time.Second,
			ExpirationTTL:       0,
			RetryPolicy:         nil,
			Labels:              map[string]string{"managed_by": managedByLabel},
			Filter:              "",
			DeadLetterTopic:     "outbox-processor-dlq",
			MaxDeliveryAttempts: 5,
			ProtoMessage:        "outbox.event.v1.OutboxProcessor",
		},
	}

	gotTopics, gotSubs, err := dedupeAndValidate(topics, subs)
	require.NoError(t, err)
	require.Len(t, gotSubs, 1)
	require.Equal(t, "outbox-event", gotSubs[0].Topic, "Topic ID should be resolved from TopicMessage")
	require.Len(t, gotTopics, 2, "expected primary topic plus synthesised DLQ topic")

	byName := map[string]DesiredTopic{}
	for _, topic := range gotTopics {
		byName[topic.Name] = topic
	}

	dlq, ok := byName["outbox-processor-dlq"]
	require.True(t, ok, "synthesised DLQ topic missing")
	require.Equal(t, managedByLabel, dlq.Labels["managed_by"])
	require.Equal(t, "outbox-processor", dlq.Labels["dlq_for"])
}

func TestDedupeAndValidate_MissingSubscriptionName(t *testing.T) {
	t.Parallel()

	topics := []DesiredTopic{
		{Name: "outbox-event", Retention: 0, Labels: nil, ProtoMessage: "outbox.event.v1.Event"},
	}
	subs := []DesiredSubscription{
		{
			Name:                "",
			Topic:               "",
			TopicMessage:        "outbox.event.v1.Event",
			Retention:           0,
			RetainAckedMessages: false,
			AckDeadline:         0,
			ExpirationTTL:       0,
			RetryPolicy:         nil,
			Labels:              nil,
			Filter:              "",
			DeadLetterTopic:     "",
			MaxDeliveryAttempts: 0,
			ProtoMessage:        "outbox.event.v1.OutboxProcessor",
		},
	}

	_, _, err := dedupeAndValidate(topics, subs)
	require.ErrorContains(t, err, "missing a name")
}

func TestDedupeAndValidate_DuplicateSubscriptionName(t *testing.T) {
	t.Parallel()

	topics := []DesiredTopic{
		{Name: "outbox-event", Retention: 0, Labels: nil, ProtoMessage: "outbox.event.v1.Event"},
		{Name: "billing-event", Retention: 0, Labels: nil, ProtoMessage: "billing.event.v1.Event"},
	}
	subs := []DesiredSubscription{
		{
			Name: "shared-name", Topic: "", TopicMessage: "outbox.event.v1.Event",
			ProtoMessage: "outbox.event.v1.OutboxProcessor",
			Retention:    0, RetainAckedMessages: false, AckDeadline: 0, ExpirationTTL: 0,
			RetryPolicy: nil, Labels: nil, Filter: "", DeadLetterTopic: "", MaxDeliveryAttempts: 0,
		},
		{
			Name: "shared-name", Topic: "", TopicMessage: "billing.event.v1.Event",
			ProtoMessage: "billing.event.v1.BillingProcessor",
			Retention:    0, RetainAckedMessages: false, AckDeadline: 0, ExpirationTTL: 0,
			RetryPolicy: nil, Labels: nil, Filter: "", DeadLetterTopic: "", MaxDeliveryAttempts: 0,
		},
	}

	_, _, err := dedupeAndValidate(topics, subs)
	require.ErrorContains(t, err, "is declared multiple times")
}

func TestDedupeAndValidate_SubscriptionReferencesUnknownTopicMessage(t *testing.T) {
	t.Parallel()

	subs := []DesiredSubscription{
		{
			Name: "orphan", Topic: "", TopicMessage: "missing.v1.Topic",
			ProtoMessage: "outbox.event.v1.OutboxProcessor",
			Retention:    0, RetainAckedMessages: false, AckDeadline: 0, ExpirationTTL: 0,
			RetryPolicy: nil, Labels: nil, Filter: "", DeadLetterTopic: "", MaxDeliveryAttempts: 0,
		},
	}

	_, _, err := dedupeAndValidate(nil, subs)
	require.ErrorContains(t, err, "references unknown topic message")
}

func TestDedupeAndValidate_SubscriptionMissingTopicReference(t *testing.T) {
	t.Parallel()

	topics := []DesiredTopic{
		{Name: "outbox-event", Retention: 0, Labels: nil, ProtoMessage: "outbox.event.v1.Event"},
	}
	subs := []DesiredSubscription{
		{
			Name: "outbox-processor", Topic: "", TopicMessage: "",
			ProtoMessage: "outbox.event.v1.OutboxProcessor",
			Retention:    0, RetainAckedMessages: false, AckDeadline: 0, ExpirationTTL: 0,
			RetryPolicy: nil, Labels: nil, Filter: "", DeadLetterTopic: "", MaxDeliveryAttempts: 0,
		},
	}

	_, _, err := dedupeAndValidate(topics, subs)
	require.ErrorContains(t, err, "missing a topic reference")
}

func TestDedupeAndValidate_DLQCollidesWithDeclaredTopic(t *testing.T) {
	t.Parallel()

	topics := []DesiredTopic{
		{Name: "outbox-event", Retention: 0, Labels: nil, ProtoMessage: "outbox.event.v1.Event"},
		{Name: "outbox-processor-dlq", Retention: 0, Labels: nil, ProtoMessage: "other.v1.Other"},
	}
	subs := []DesiredSubscription{
		{
			Name: "outbox-processor", Topic: "", TopicMessage: "outbox.event.v1.Event",
			ProtoMessage:    "outbox.event.v1.OutboxProcessor",
			DeadLetterTopic: "outbox-processor-dlq", MaxDeliveryAttempts: 5,
			Retention: 0, RetainAckedMessages: false, AckDeadline: 0, ExpirationTTL: 0,
			RetryPolicy: nil, Labels: nil, Filter: "",
		},
	}

	_, _, err := dedupeAndValidate(topics, subs)
	require.ErrorContains(t, err, "dead-letter topic")
	require.ErrorContains(t, err, "collides")
}

func TestDedupeAndValidate_SubscriptionNameTooLongForDLQSuffix(t *testing.T) {
	t.Parallel()

	longName := "a" + strings.Repeat("b", maxTopicIDLen-len(dlqSuffix))

	topics := []DesiredTopic{
		{Name: "outbox-event", Retention: 0, Labels: nil, ProtoMessage: "outbox.event.v1.Event"},
	}
	subs := []DesiredSubscription{
		{
			Name: longName, Topic: "", TopicMessage: "outbox.event.v1.Event",
			ProtoMessage: "outbox.event.v1.OutboxProcessor",
			Retention:    0, RetainAckedMessages: false, AckDeadline: 0, ExpirationTTL: 0,
			RetryPolicy: nil, Labels: nil, Filter: "", DeadLetterTopic: "", MaxDeliveryAttempts: 0,
		},
	}

	_, _, err := dedupeAndValidate(topics, subs)
	require.ErrorContains(t, err, "leave room")
}

func TestDurationToSeconds(t *testing.T) {
	t.Parallel()

	require.Equal(t, int32(0), durationToSeconds(0))
	require.Equal(t, int32(0), durationToSeconds(-1*time.Second))
	require.Equal(t, int32(30), durationToSeconds(30*time.Second))
	require.Equal(t, int32(31), durationToSeconds(30500*time.Millisecond))
	require.Equal(t, int32(30), durationToSeconds(30499*time.Millisecond))
	require.Equal(t, int32(math.MaxInt32), durationToSeconds(math.MaxInt64))
	require.Equal(t, int32(math.MaxInt32), durationToSeconds(time.Duration(math.MaxInt32)*time.Second))
	require.Equal(t, int32(math.MaxInt32-1), durationToSeconds(time.Duration(math.MaxInt32-1)*time.Second))
}

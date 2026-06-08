package gcp

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/googleapis/gax-go/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

var (
	gcpLabelKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	gcpLabelValuePattern = regexp.MustCompile(`^$|^[a-z0-9][a-z0-9_-]{0,62}$`)
)

type pubSubTopicCreator interface {
	CreateTopic(context.Context, *pubsubpb.Topic, ...gax.CallOption) (*pubsubpb.Topic, error)
}

type pubSubSubscriptionCreator interface {
	CreateSubscription(context.Context, *pubsubpb.Subscription, ...gax.CallOption) (*pubsubpb.Subscription, error)
}

type pubSubEmulatorAdminClients struct {
	topicCreator        pubSubTopicCreator
	subscriptionCreator pubSubSubscriptionCreator
}

type fakeTopicCreator struct {
	requests []*pubsubpb.Topic
	errs     []error
}

func (f *fakeTopicCreator) CreateTopic(_ context.Context, req *pubsubpb.Topic, _ ...gax.CallOption) (*pubsubpb.Topic, error) {
	f.requests = append(f.requests, req)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return nil, err
	}
	return req, nil
}

type fakeSubscriptionCreator struct {
	requests []*pubsubpb.Subscription
	errs     []error
}

func (f *fakeSubscriptionCreator) CreateSubscription(_ context.Context, req *pubsubpb.Subscription, _ ...gax.CallOption) (*pubsubpb.Subscription, error) {
	f.requests = append(f.requests, req)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return nil, err
	}
	return req, nil
}

func TestPubSubTopologyCreatableWithEmulator(t *testing.T) {
	if os.Getenv("GRAM_TEST_PUBSUB_EMULATOR") != "1" {
		t.Skip("GRAM_TEST_PUBSUB_EMULATOR is not set")
	}

	emulatorHost := strings.TrimSpace(os.Getenv("PUBSUB_EMULATOR_HOST"))
	if emulatorHost == "" {
		t.Skip("PUBSUB_EMULATOR_HOST is not set")
	}

	descriptorBytes, err := os.ReadFile(filepath.Join("..", "..", "cmd", "infra", "descriptors.pb"))
	require.NoError(t, err)
	require.NotEmpty(t, descriptorBytes)

	topics, subs, err := discoverPubSubFromDescriptor(descriptorBytes)
	require.NoError(t, err)

	requireGeneratedPubSubLabelsValid(t, topics, subs)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	projectID := "gram-pubsub-validation"
	client, err := pubsub.NewClient(
		ctx,
		projectID,
		option.WithEndpoint(emulatorHost),
		option.WithoutAuthentication(),
	)
	require.NoError(t, err)
	defer client.Close()

	err = validatePubSubEmulatorTopology(ctx, projectID, topics, subs, pubSubEmulatorAdminClients{
		topicCreator:        client.TopicAdminClient,
		subscriptionCreator: client.SubscriptionAdminClient,
	})
	require.NoError(t, err)
}

func requireGeneratedPubSubLabelsValid(t *testing.T, topics []DesiredTopic, subs []DesiredSubscription) {
	t.Helper()

	for _, topic := range topics {
		require.NoError(t, validateGCPLabels(labelsWithProtoMessage(topic.Labels, topic.ProtoMessage)), "invalid generated labels for topic %q", topic.Name)
	}

	for _, sub := range subs {
		labels := labelsWithProtoMessage(sub.Labels, sub.ProtoMessage)
		if sub.TopicMessage != "" {
			labels[topicMessageLabel] = sub.TopicMessage
		}
		require.NoError(t, validateGCPLabels(labels), "invalid generated labels for subscription %q", sub.Name)
	}
}

func TestValidatePubSubEmulatorTopology_CreatesTopicsBeforeSubscriptions(t *testing.T) {
	t.Parallel()

	topicCreator := &fakeTopicCreator{}
	subscriptionCreator := &fakeSubscriptionCreator{}

	err := validatePubSubEmulatorTopology(
		t.Context(),
		"test-project",
		[]DesiredTopic{
			{
				Name:         "z-topic",
				Retention:    24 * time.Hour,
				Labels:       map[string]string{"proto_message": "example_v1_ztopic"},
				ProtoMessage: "example_v1_ztopic",
			},
			{
				Name:         "a-topic",
				Retention:    0,
				Labels:       map[string]string{"proto_message": "example_v1_atopic"},
				ProtoMessage: "example_v1_atopic",
			},
		},
		[]DesiredSubscription{
			{
				Name:                "z-sub",
				Topic:               "z-topic",
				TopicMessage:        "example_v1_ztopic",
				Retention:           time.Hour,
				RetainAckedMessages: true,
				AckDeadline:         1500 * time.Millisecond,
				ExpirationTTL:       48 * time.Hour,
				RetryPolicy: &DesiredRetryPolicy{
					MinimumBackoff: 10 * time.Second,
					MaximumBackoff: 60 * time.Second,
				},
				Labels:              map[string]string{"proto_message": "example_v1_zsub"},
				Filter:              `attributes.env="prod"`,
				DeadLetterTopic:     "z-sub-dlq",
				MaxDeliveryAttempts: 5,
				ProtoMessage:        "example_v1_zsub",
			},
		},
		pubSubEmulatorAdminClients{
			topicCreator:        topicCreator,
			subscriptionCreator: subscriptionCreator,
		},
	)

	require.NoError(t, err)
	require.Len(t, topicCreator.requests, 2)
	require.Equal(t, "projects/test-project/topics/a-topic", topicCreator.requests[0].Name)
	require.Nil(t, topicCreator.requests[0].MessageRetentionDuration)
	require.Equal(t, "projects/test-project/topics/z-topic", topicCreator.requests[1].Name)
	require.Equal(t, int64(86400), topicCreator.requests[1].MessageRetentionDuration.GetSeconds())

	require.Len(t, subscriptionCreator.requests, 1)
	sub := subscriptionCreator.requests[0]
	require.Equal(t, "projects/test-project/subscriptions/z-sub", sub.Name)
	require.Equal(t, "projects/test-project/topics/z-topic", sub.Topic)
	require.Equal(t, int32(2), sub.AckDeadlineSeconds)
	require.Equal(t, int64(3600), sub.MessageRetentionDuration.GetSeconds())
	require.Equal(t, int64(172800), sub.ExpirationPolicy.GetTtl().GetSeconds())
	require.Equal(t, int64(10), sub.RetryPolicy.GetMinimumBackoff().GetSeconds())
	require.Equal(t, int64(60), sub.RetryPolicy.GetMaximumBackoff().GetSeconds())
	require.Equal(t, "projects/test-project/topics/z-sub-dlq", sub.DeadLetterPolicy.GetDeadLetterTopic())
	require.Equal(t, int32(5), sub.DeadLetterPolicy.GetMaxDeliveryAttempts())
	require.Equal(t, `attributes.env="prod"`, sub.Filter)
}

func TestValidatePubSubEmulatorTopology_IgnoresAlreadyExists(t *testing.T) {
	t.Parallel()

	topicCreator := &fakeTopicCreator{
		errs: []error{status.Error(codes.AlreadyExists, "topic exists")},
	}
	subscriptionCreator := &fakeSubscriptionCreator{
		errs: []error{status.Error(codes.AlreadyExists, "subscription exists")},
	}

	err := validatePubSubEmulatorTopology(
		t.Context(),
		"test-project",
		[]DesiredTopic{
			{
				Name:         "a-topic",
				Retention:    0,
				Labels:       map[string]string{},
				ProtoMessage: "example_v1_atopic",
			},
		},
		[]DesiredSubscription{
			{
				Name:                "a-sub",
				Topic:               "a-topic",
				TopicMessage:        "example_v1_atopic",
				Retention:           0,
				RetainAckedMessages: false,
				AckDeadline:         0,
				ExpirationTTL:       0,
				RetryPolicy:         nil,
				Labels:              map[string]string{},
				Filter:              "",
				DeadLetterTopic:     "",
				MaxDeliveryAttempts: 0,
				ProtoMessage:        "example_v1_asub",
			},
		},
		pubSubEmulatorAdminClients{
			topicCreator:        topicCreator,
			subscriptionCreator: subscriptionCreator,
		},
	)

	require.NoError(t, err)
	require.Len(t, topicCreator.requests, 1)
	require.Len(t, subscriptionCreator.requests, 1)
}

func TestValidatePubSubEmulatorTopology_ReturnsResourceNameOnCreateError(t *testing.T) {
	t.Parallel()

	topicCreator := &fakeTopicCreator{
		errs: []error{status.Error(codes.InvalidArgument, "invalid label value")},
	}

	err := validatePubSubEmulatorTopology(
		t.Context(),
		"test-project",
		[]DesiredTopic{
			{
				Name:         "a-topic",
				Retention:    0,
				Labels:       map[string]string{"proto_message": "example_v1_atopic"},
				ProtoMessage: "example_v1_atopic",
			},
		},
		nil,
		pubSubEmulatorAdminClients{
			topicCreator:        topicCreator,
			subscriptionCreator: &fakeSubscriptionCreator{},
		},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, `create pubsub emulator topic "projects/test-project/topics/a-topic"`)
	require.ErrorContains(t, err, "invalid label value")
}

func TestValidatePubSubEmulatorTopology_ReturnsResourceNameOnInvalidLabels(t *testing.T) {
	t.Parallel()

	err := validatePubSubEmulatorTopology(
		t.Context(),
		"test-project",
		[]DesiredTopic{
			{
				Name:         "a-topic",
				Retention:    0,
				Labels:       map[string]string{},
				ProtoMessage: "example.v1.ATopic",
			},
		},
		nil,
		pubSubEmulatorAdminClients{
			topicCreator:        &fakeTopicCreator{},
			subscriptionCreator: &fakeSubscriptionCreator{},
		},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, `invalid labels for pubsub emulator topic "projects/test-project/topics/a-topic"`)
	require.ErrorContains(t, err, `label "proto_message" value "example.v1.ATopic"`)
}

func validatePubSubEmulatorTopology(ctx context.Context, projectID string, topics []DesiredTopic, subs []DesiredSubscription, clients pubSubEmulatorAdminClients) error {
	if clients.topicCreator == nil {
		return fmt.Errorf("pubsub topic creator is nil")
	}
	if clients.subscriptionCreator == nil {
		return fmt.Errorf("pubsub subscription creator is nil")
	}

	sortedTopics := slices.Clone(topics)
	slices.SortFunc(sortedTopics, func(a DesiredTopic, b DesiredTopic) int {
		return cmp.Compare(a.Name, b.Name)
	})

	for _, topic := range sortedTopics {
		req := emulatorTopicRequest(projectID, topic)
		if err := validateGCPLabels(req.Labels); err != nil {
			return fmt.Errorf("invalid labels for pubsub emulator topic %q: %w", req.Name, err)
		}
		if _, err := clients.topicCreator.CreateTopic(ctx, req); err != nil {
			if status.Code(err) == codes.AlreadyExists {
				continue
			}
			return fmt.Errorf("create pubsub emulator topic %q: %w", req.Name, err)
		}
	}

	sortedSubs := slices.Clone(subs)
	slices.SortFunc(sortedSubs, func(a DesiredSubscription, b DesiredSubscription) int {
		return cmp.Compare(a.Name, b.Name)
	})

	for _, sub := range sortedSubs {
		req := emulatorSubscriptionRequest(projectID, sub)
		if err := validateGCPLabels(req.Labels); err != nil {
			return fmt.Errorf("invalid labels for pubsub emulator subscription %q: %w", req.Name, err)
		}
		if _, err := clients.subscriptionCreator.CreateSubscription(ctx, req); err != nil {
			if status.Code(err) == codes.AlreadyExists {
				continue
			}
			return fmt.Errorf("create pubsub emulator subscription %q: %w", req.Name, err)
		}
	}

	return nil
}

func validateGCPLabels(labels map[string]string) error {
	for key, value := range labels {
		if !gcpLabelKeyPattern.MatchString(key) {
			return fmt.Errorf("label key %q must start with a lowercase letter and contain only lowercase letters, numbers, underscores, or dashes", key)
		}
		if !gcpLabelValuePattern.MatchString(value) {
			return fmt.Errorf("label %q value %q must be empty or contain only lowercase letters, numbers, underscores, or dashes", key, value)
		}
	}
	return nil
}

func emulatorTopicRequest(projectID string, topic DesiredTopic) *pubsubpb.Topic {
	req := &pubsubpb.Topic{
		Name:                        fmt.Sprintf("projects/%s/topics/%s", projectID, topic.Name),
		Labels:                      labelsWithProtoMessage(topic.Labels, topic.ProtoMessage),
		MessageStoragePolicy:        nil,
		KmsKeyName:                  "",
		SchemaSettings:              nil,
		SatisfiesPzs:                false,
		MessageRetentionDuration:    nil,
		State:                       0,
		IngestionDataSourceSettings: nil,
		MessageTransforms:           nil,
	}

	if topic.Retention > 0 {
		req.MessageRetentionDuration = durationpb.New(topic.Retention)
	}

	return req
}

func emulatorSubscriptionRequest(projectID string, sub DesiredSubscription) *pubsubpb.Subscription {
	labels := labelsWithProtoMessage(sub.Labels, sub.ProtoMessage)
	if sub.TopicMessage != "" {
		labels[topicMessageLabel] = sub.TopicMessage
	}

	req := &pubsubpb.Subscription{
		Name:                          fmt.Sprintf("projects/%s/subscriptions/%s", projectID, sub.Name),
		Topic:                         fmt.Sprintf("projects/%s/topics/%s", projectID, sub.Topic),
		PushConfig:                    nil,
		AckDeadlineSeconds:            durationToPubSubSeconds(sub.AckDeadline),
		RetainAckedMessages:           sub.RetainAckedMessages,
		MessageRetentionDuration:      nil,
		Labels:                        labels,
		EnableMessageOrdering:         false,
		ExpirationPolicy:              nil,
		Filter:                        sub.Filter,
		DeadLetterPolicy:              nil,
		RetryPolicy:                   nil,
		Detached:                      false,
		EnableExactlyOnceDelivery:     false,
		TopicMessageRetentionDuration: nil,
		BigqueryConfig:                nil,
		CloudStorageConfig:            nil,
		AnalyticsHubSubscriptionInfo:  nil,
		MessageTransforms:             nil,
		State:                         0,
	}

	if sub.Retention > 0 {
		req.MessageRetentionDuration = durationpb.New(sub.Retention)
	}

	if sub.ExpirationTTL > 0 {
		req.ExpirationPolicy = &pubsubpb.ExpirationPolicy{
			Ttl: durationpb.New(sub.ExpirationTTL),
		}
	}

	if sub.RetryPolicy != nil {
		req.RetryPolicy = &pubsubpb.RetryPolicy{
			MinimumBackoff: durationpb.New(sub.RetryPolicy.MinimumBackoff),
			MaximumBackoff: durationpb.New(sub.RetryPolicy.MaximumBackoff),
		}
	}

	if sub.DeadLetterTopic != "" {
		req.DeadLetterPolicy = &pubsubpb.DeadLetterPolicy{
			DeadLetterTopic:     fmt.Sprintf("projects/%s/topics/%s", projectID, sub.DeadLetterTopic),
			MaxDeliveryAttempts: sub.MaxDeliveryAttempts,
		}
	}

	return req
}

func durationToPubSubSeconds(d time.Duration) int32 {
	if d <= 0 {
		return 0
	}

	const cap32 = time.Duration(math.MaxInt32) * time.Second
	if d >= cap32-500*time.Millisecond {
		return math.MaxInt32
	}

	return int32((d + 500*time.Millisecond) / time.Second)
}

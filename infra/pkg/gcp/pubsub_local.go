package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/speakeasy-api/gram/infra/internal/attr"
	"github.com/speakeasy-api/gram/infra/internal/gcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// emulatorMaxRetention caps how long the local emulator retains messages.
// Unlike real Pub/Sub, the emulator does not yet support high message
// retention periods, so we clamp the configured retention down to this value.
const emulatorMaxRetention = 72 * time.Hour

type EmulatedPubSubBroker struct {
	logger      *slog.Logger
	projectID   string
	client      *pubsub.Client
	descriptors []byte
}

var _ SubscriberBroker = (*EmulatedPubSubBroker)(nil)
var _ PublisherBroker = (*EmulatedPubSubBroker)(nil)

func NewEmulatedPubSub(logger *slog.Logger, projectID string, client *pubsub.Client, descriptors []byte) *EmulatedPubSubBroker {
	return &EmulatedPubSubBroker{
		logger:      logger,
		projectID:   projectID,
		client:      client,
		descriptors: descriptors,
	}
}

func (e *EmulatedPubSubBroker) PublisherForMessage(ctx context.Context, msg proto.Message) (*pubsub.Publisher, error) {
	descriptor := msg.ProtoReflect().Descriptor()

	topicOptions, ok := gcp.TopicOptionsFromMessage(descriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub topic", descriptor.FullName())
	}

	topicName := gcp.ResolveTopicName(descriptor, topicOptions)
	if err := e.reconcileTopic(ctx, topicName, topicOptions); err != nil {
		return nil, fmt.Errorf("reconcile topic: %w", err)
	}

	publisher := e.client.Publisher(topicName)

	return publisher, nil
}

func (e *EmulatedPubSubBroker) reconcileTopic(ctx context.Context, topicName string, options *pubsubv1.TopicOptions) error {
	qname := fmt.Sprintf("projects/%s/topics/%s", e.projectID, topicName)

	topic := &pubsubpb.Topic{
		Name:                        qname,
		Labels:                      options.GetLabels(),
		MessageStoragePolicy:        nil,
		KmsKeyName:                  "",
		SchemaSettings:              nil,
		SatisfiesPzs:                false,
		MessageRetentionDuration:    nil,
		State:                       0,
		IngestionDataSourceSettings: nil,
		MessageTransforms:           nil,
	}

	var retention time.Duration
	if options.GetRetentionHint() != nil {
		retention = options.GetRetentionHint().AsDuration()
	}
	if retention > emulatorMaxRetention {
		retention = emulatorMaxRetention
	}
	if retention > 0 {
		topic.MessageRetentionDuration = durationpb.New(retention)
	}

	_, err := e.client.TopicAdminClient.CreateTopic(ctx, topic)
	switch {
	case status.Code(err) == codes.AlreadyExists:
		e.logger.InfoContext(ctx, "topic already exists", attr.SlogGCPTopicQualifiedName(qname))
	case err != nil:
		return fmt.Errorf("create topic %q: %w", qname, err)
	default:
		e.logger.InfoContext(ctx, "topic created", attr.SlogGCPTopicQualifiedName(qname))
	}

	return nil
}

func (e *EmulatedPubSubBroker) SubscriberForMessage(ctx context.Context, msg proto.Message, subt proto.Message) (*pubsub.Subscriber, error) {
	subDescriptor := subt.ProtoReflect().Descriptor()

	subOptions, ok := gcp.SubscriptionOptionsFromMessage(subDescriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub subscription", subDescriptor.FullName())
	}

	msgDescriptor := msg.ProtoReflect().Descriptor()
	topicOptions, ok := gcp.TopicOptionsFromMessage(msgDescriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub topic", msgDescriptor.FullName())
	}

	subName := gcp.ResolveSubscriptionName(subDescriptor, subOptions)
	topicName := gcp.ResolveTopicName(msgDescriptor, topicOptions)

	// The emulator has no Config Connector to provision resources ahead of
	// time, so the topic must exist before its subscription can be created;
	// otherwise CreateSubscription fails with NOT_FOUND when a subscriber
	// initializes before any publisher has reconciled the topic.
	if err := e.reconcileTopic(ctx, topicName, topicOptions); err != nil {
		return nil, fmt.Errorf("reconcile topic: %w", err)
	}

	if err := e.reconcileSubscriptions(ctx, subName, topicName, subOptions); err != nil {
		return nil, fmt.Errorf("reconcile subscription: %w", err)
	}

	sub := e.client.Subscriber(subName)

	return sub, nil
}

func (e *EmulatedPubSubBroker) reconcileSubscriptions(ctx context.Context, subName string, topicName string, options *pubsubv1.SubscriptionOptions) error {
	qname := fmt.Sprintf("projects/%s/subscriptions/%s", e.projectID, subName)
	topicName = fmt.Sprintf("projects/%s/topics/%s", e.projectID, topicName)

	var ackDeadline time.Duration
	if options.GetAckDeadline() != nil {
		ackDeadline = options.GetAckDeadline().AsDuration()
	}

	var expPolicy *pubsubpb.ExpirationPolicy
	if options.GetExpirationTtl() != nil && options.GetExpirationTtl().AsDuration() > 0 {
		expPolicy = &pubsubpb.ExpirationPolicy{
			Ttl: options.GetExpirationTtl(),
		}
	}

	var retryPolicy *pubsubpb.RetryPolicy
	if options.GetRetryPolicy() != nil {
		retryPolicy = &pubsubpb.RetryPolicy{
			MinimumBackoff: options.GetRetryPolicy().GetMinimumBackoff(),
			MaximumBackoff: options.GetRetryPolicy().GetMaximumBackoff(),
		}
	}

	retention := options.GetRetention()
	if retention != nil && retention.AsDuration() > emulatorMaxRetention {
		retention = durationpb.New(emulatorMaxRetention)
	}

	var deadLetterPolicy *pubsubpb.DeadLetterPolicy
	if dl := options.GetDeadLetter(); dl != nil {
		dlqName := gcp.ResolveDeadLetterTopicName(subName, dl)

		// As with the source topic above, the emulator has no Config Connector
		// to provision the auto-derived DLQ topic ahead of time, so it must be
		// created before the subscription can reference it.
		if err := e.reconcileTopic(ctx, dlqName, &pubsubv1.TopicOptions{}); err != nil {
			return fmt.Errorf("reconcile dead-letter topic: %w", err)
		}

		deadLetterPolicy = &pubsubpb.DeadLetterPolicy{
			DeadLetterTopic:     fmt.Sprintf("projects/%s/topics/%s", e.projectID, dlqName),
			MaxDeliveryAttempts: dl.GetMaxDeliveryAttempts(),
		}
	}

	sub := &pubsubpb.Subscription{
		Name:                          qname,
		Topic:                         topicName,
		AckDeadlineSeconds:            durationToSeconds(ackDeadline),
		RetainAckedMessages:           options.GetRetainAckedMessages(),
		MessageRetentionDuration:      retention,
		Labels:                        options.GetLabels(),
		ExpirationPolicy:              expPolicy,
		Filter:                        options.GetFilter(),
		DeadLetterPolicy:              deadLetterPolicy,
		RetryPolicy:                   retryPolicy,
		EnableMessageOrdering:         false,
		PushConfig:                    nil,
		BigqueryConfig:                nil,
		CloudStorageConfig:            nil,
		Detached:                      false,
		EnableExactlyOnceDelivery:     false,
		TopicMessageRetentionDuration: nil,
		AnalyticsHubSubscriptionInfo:  nil,
		MessageTransforms:             nil,
		State:                         0,
	}

	_, err := e.client.SubscriptionAdminClient.CreateSubscription(ctx, sub)
	switch {
	case status.Code(err) == codes.AlreadyExists:
		e.logger.InfoContext(ctx, "subscription already exists", attr.SlogGCPSubscriptionQualifiedName(qname))
	case err != nil:
		return fmt.Errorf("create subscription %q: %w", qname, err)
	default:
		e.logger.InfoContext(ctx, "subscription created", attr.SlogGCPSubscriptionQualifiedName(qname))
	}

	return nil
}

func durationToSeconds(d time.Duration) int32 {
	if d <= 0 {
		return 0
	}

	// Clamp before any arithmetic so neither the round-half-up addition nor
	// the int32 conversion can overflow.
	const cap32 = time.Duration(math.MaxInt32) * time.Second
	if d >= cap32-500*time.Millisecond {
		return math.MaxInt32
	}

	return int32((d + 500*time.Millisecond) / time.Second)
}

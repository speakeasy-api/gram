package platforminit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/ettle/strcase"
	pubsubv1 "github.com/speakeasy-api/gram/protogen/infra/pubsub/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	managedByLabel = "proto_pubsub_orchestrator"
	dlqSuffix      = "-dlq"
	maxTopicIDLen  = 255
)

type DesiredTopic struct {
	Name         string
	Retention    time.Duration
	Labels       map[string]string
	ProtoMessage string
}

type DesiredRetryPolicy struct {
	MinimumBackoff time.Duration
	MaximumBackoff time.Duration
}

type DesiredSubscription struct {
	Name                string
	Topic               string
	TopicMessage        string
	Retention           time.Duration
	RetainAckedMessages bool
	AckDeadline         time.Duration
	ExpirationTTL       time.Duration
	RetryPolicy         *DesiredRetryPolicy
	Labels              map[string]string
	Filter              string
	DeadLetterTopic     string
	MaxDeliveryAttempts int32
	ProtoMessage        string
}

func DiscoverPubSubFromBytes(descriptorBytes []byte) ([]DesiredTopic, []DesiredSubscription, error) {
	var descriptorSet descriptorpb.FileDescriptorSet

	if err := proto.Unmarshal(descriptorBytes, &descriptorSet); err != nil {
		return nil, nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}

	files, err := protodesc.NewFiles(&descriptorSet)
	if err != nil {
		return nil, nil, fmt.Errorf("build proto file registry: %w", err)
	}

	var (
		topics  []DesiredTopic
		subs    []DesiredSubscription
		walkErr error
	)

	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		walkErr = collectFromMessages(file.Messages(), &topics, &subs)
		return walkErr == nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}

	return dedupeAndValidate(topics, subs)
}

func collectFromMessages(messages protoreflect.MessageDescriptors, topics *[]DesiredTopic, subs *[]DesiredSubscription) error {
	for i := 0; i < messages.Len(); i++ {
		message := messages.Get(i)

		topicOptions, hasTopic := topicOptionsFromMessage(message)
		subOptions, hasSub := subscriptionOptionsFromMessage(message)

		if hasTopic && hasSub {
			return fmt.Errorf("message %s declares both a topic and a subscription option; declare them on separate marker messages", message.FullName())
		}

		if hasTopic {
			*topics = append(*topics, desiredTopicFromOptions(message, topicOptions))
		}

		if hasSub {
			*subs = append(*subs, desiredSubscriptionFromOptions(message, subOptions))
		}

		if err := collectFromMessages(message.Messages(), topics, subs); err != nil {
			return err
		}
	}
	return nil
}

func topicOptionsFromMessage(message protoreflect.MessageDescriptor) (*pubsubv1.TopicOptions, bool) {
	options, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || options == nil {
		return nil, false
	}

	if !proto.HasExtension(options, pubsubv1.E_Topic) {
		return nil, false
	}

	topicOptions, ok := proto.GetExtension(options, pubsubv1.E_Topic).(*pubsubv1.TopicOptions)
	if !ok || topicOptions == nil {
		return nil, false
	}

	return topicOptions, true
}

func subscriptionOptionsFromMessage(message protoreflect.MessageDescriptor) (*pubsubv1.SubscriptionOptions, bool) {
	options, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || options == nil {
		return nil, false
	}

	if !proto.HasExtension(options, pubsubv1.E_Subscription) {
		return nil, false
	}

	subOptions, ok := proto.GetExtension(options, pubsubv1.E_Subscription).(*pubsubv1.SubscriptionOptions)
	if !ok || subOptions == nil {
		return nil, false
	}

	return subOptions, true
}

func resolveSubscriptionName(message protoreflect.MessageDescriptor, opts *pubsubv1.SubscriptionOptions) string {
	name := strings.TrimSpace(opts.GetName())
	if name == "" {
		name = string(message.Name())
	}
	return strcase.ToKebab(name)
}

func resolveTopicName(message protoreflect.MessageDescriptor, topicOptions *pubsubv1.TopicOptions) string {
	topicName := strings.TrimSpace(topicOptions.GetName())
	if topicName == "" {
		topicName = string(message.FullName())
	}
	return strcase.ToKebab(topicName)
}

func desiredTopicFromOptions(message protoreflect.MessageDescriptor, topicOptions *pubsubv1.TopicOptions) DesiredTopic {
	inlabels := topicOptions.GetLabels()
	labels := make(map[string]string, len(inlabels)+1)
	maps.Copy(labels, inlabels)
	labels["managed_by"] = managedByLabel

	return DesiredTopic{
		Name:         resolveTopicName(message, topicOptions),
		Retention:    topicOptions.GetRetentionHint().AsDuration(),
		Labels:       labels,
		ProtoMessage: string(message.FullName()),
	}
}

func desiredSubscriptionFromOptions(message protoreflect.MessageDescriptor, subOptions *pubsubv1.SubscriptionOptions) DesiredSubscription {
	inlabels := subOptions.GetLabels()
	labels := make(map[string]string, len(inlabels)+1)
	maps.Copy(labels, inlabels)
	labels["managed_by"] = managedByLabel

	subName := resolveSubscriptionName(message, subOptions)

	desired := DesiredSubscription{
		Name:                subName,
		Topic:               "",
		TopicMessage:        strings.TrimSpace(subOptions.GetTopic()),
		Retention:           subOptions.GetRetention().AsDuration(),
		RetainAckedMessages: subOptions.GetRetainAckedMessages(),
		AckDeadline:         subOptions.GetAckDeadline().AsDuration(),
		ExpirationTTL:       subOptions.GetExpirationTtl().AsDuration(),
		RetryPolicy:         nil,
		Labels:              labels,
		Filter:              subOptions.GetFilter(),
		DeadLetterTopic:     "",
		MaxDeliveryAttempts: 0,
		ProtoMessage:        string(message.FullName()),
	}

	if rp := subOptions.GetRetryPolicy(); rp != nil {
		desired.RetryPolicy = &DesiredRetryPolicy{
			MinimumBackoff: rp.GetMinimumBackoff().AsDuration(),
			MaximumBackoff: rp.GetMaximumBackoff().AsDuration(),
		}
	}

	if dl := subOptions.GetDeadLetter(); dl != nil {
		dlqName := strcase.ToKebab(strings.TrimSpace(dl.GetName()))
		if dlqName == "" {
			dlqName = subName + dlqSuffix
		}
		desired.DeadLetterTopic = dlqName
		desired.MaxDeliveryAttempts = dl.GetMaxDeliveryAttempts()
	}

	return desired
}

func dedupeAndValidate(topics []DesiredTopic, subs []DesiredSubscription) ([]DesiredTopic, []DesiredSubscription, error) {
	topicByName := map[string]DesiredTopic{}
	topicByFullName := map[string]DesiredTopic{}

	for _, topic := range topics {
		if err := validateTopicID(topic.Name); err != nil {
			return nil, nil, fmt.Errorf("invalid topic name %q from %s: %w", topic.Name, topic.ProtoMessage, err)
		}

		if existing, exists := topicByName[topic.Name]; exists {
			return nil, nil, fmt.Errorf(
				"topic %q is declared multiple times: %s and %s",
				topic.Name,
				existing.ProtoMessage,
				topic.ProtoMessage,
			)
		}

		topicByName[topic.Name] = topic
		topicByFullName[topic.ProtoMessage] = topic
	}

	subByName := map[string]DesiredSubscription{}
	resolvedSubs := make([]DesiredSubscription, 0, len(subs))

	for _, sub := range subs {
		if strings.TrimSpace(sub.Name) == "" {
			return nil, nil, fmt.Errorf("subscription on %s is missing a name", sub.ProtoMessage)
		}

		if err := validateSubscriptionID(sub.Name); err != nil {
			return nil, nil, fmt.Errorf("invalid subscription name %q from %s: %w", sub.Name, sub.ProtoMessage, err)
		}

		if existing, exists := subByName[sub.Name]; exists {
			return nil, nil, fmt.Errorf(
				"subscription %q is declared multiple times: %s and %s",
				sub.Name,
				existing.ProtoMessage,
				sub.ProtoMessage,
			)
		}

		if sub.TopicMessage == "" {
			return nil, nil, fmt.Errorf("subscription %q on %s is missing a topic reference", sub.Name, sub.ProtoMessage)
		}

		parentTopic, exists := topicByFullName[sub.TopicMessage]
		if !exists {
			return nil, nil, fmt.Errorf(
				"subscription %q on %s references unknown topic message %q",
				sub.Name,
				sub.ProtoMessage,
				sub.TopicMessage,
			)
		}
		sub.Topic = parentTopic.Name

		subByName[sub.Name] = sub
		resolvedSubs = append(resolvedSubs, sub)
	}

	for _, sub := range resolvedSubs {
		if sub.DeadLetterTopic == "" {
			continue
		}

		if err := validateTopicID(sub.DeadLetterTopic); err != nil {
			return nil, nil, fmt.Errorf(
				"invalid dead-letter topic name %q for subscription %q: %w",
				sub.DeadLetterTopic,
				sub.Name,
				err,
			)
		}

		if existing, exists := topicByName[sub.DeadLetterTopic]; exists {
			return nil, nil, fmt.Errorf(
				"dead-letter topic %q for subscription %q collides with topic declared on %s",
				sub.DeadLetterTopic,
				sub.Name,
				existing.ProtoMessage,
			)
		}

		dlqTopic := DesiredTopic{
			Name:      sub.DeadLetterTopic,
			Retention: 0,
			Labels: map[string]string{
				"managed_by": managedByLabel,
				"dlq_for":    sub.Name,
			},
			ProtoMessage: sub.ProtoMessage,
		}
		topicByName[dlqTopic.Name] = dlqTopic
	}

	resultTopics := make([]DesiredTopic, 0, len(topicByName))
	for _, topic := range topicByName {
		resultTopics = append(resultTopics, topic)
	}

	return resultTopics, resolvedSubs, nil
}

func validateSubscriptionID(subID string) error {
	if err := validateTopicID(subID); err != nil {
		return err
	}

	if maxLen := maxTopicIDLen - len(dlqSuffix); len(subID) > maxLen {
		return fmt.Errorf("subscription ID must be at most %d characters to leave room for the %q suffix used when auto-deriving dead-letter topic names", maxLen, dlqSuffix)
	}

	return nil
}

func validateTopicID(topicID string) error {
	if strings.HasPrefix(topicID, "projects/") {
		return errors.New("use topic ID only, not full resource name")
	}

	if strings.HasPrefix(strings.ToLower(topicID), "goog") {
		return errors.New("topic ID must not start with goog")
	}

	validTopicID := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._~+%-]{2,254}$`)
	if !validTopicID.MatchString(topicID) {
		return errors.New("topic ID must start with a letter and be 3-255 characters using letters, numbers, dashes, underscores, periods, tildes, plus signs or percent signs")
	}

	return nil
}

func ReconcileTopics(ctx context.Context, logger *slog.Logger, projectID string, client *pubsub.Client, desiredTopics []DesiredTopic) error {
	for _, desired := range desiredTopics {
		qname := fmt.Sprintf("projects/%s/topics/%s", projectID, desired.Name)

		topic := &pubsubpb.Topic{
			Name:                        qname,
			Labels:                      desired.Labels,
			MessageStoragePolicy:        nil,
			KmsKeyName:                  "",
			SchemaSettings:              nil,
			SatisfiesPzs:                false,
			MessageRetentionDuration:    nil,
			State:                       0,
			IngestionDataSourceSettings: nil,
			MessageTransforms:           nil,
		}
		if desired.Retention > 0 {
			topic.MessageRetentionDuration = durationpb.New(desired.Retention)
		}

		_, err := client.TopicAdminClient.CreateTopic(ctx, topic)
		switch {
		case status.Code(err) == codes.AlreadyExists:
			continue
		case err != nil:
			return fmt.Errorf("create topic %q: %w", desired.Name, err)
		default:
			logger.InfoContext(ctx, "topic created", attr.SlogName(qname))
		}
	}

	return nil
}

func ReconcileSubscriptions(ctx context.Context, logger *slog.Logger, projectID string, client *pubsub.Client, desiredSubs []DesiredSubscription) error {
	for _, desired := range desiredSubs {
		qname := fmt.Sprintf("projects/%s/subscriptions/%s", projectID, desired.Name)
		topicName := fmt.Sprintf("projects/%s/topics/%s", projectID, desired.Topic)

		sub := &pubsubpb.Subscription{
			Name:                          qname,
			Topic:                         topicName,
			PushConfig:                    nil,
			BigqueryConfig:                nil,
			CloudStorageConfig:            nil,
			AckDeadlineSeconds:            durationToSeconds(desired.AckDeadline),
			RetainAckedMessages:           desired.RetainAckedMessages,
			MessageRetentionDuration:      nil,
			Labels:                        desired.Labels,
			EnableMessageOrdering:         false,
			ExpirationPolicy:              nil,
			Filter:                        desired.Filter,
			DeadLetterPolicy:              nil,
			RetryPolicy:                   nil,
			Detached:                      false,
			EnableExactlyOnceDelivery:     false,
			TopicMessageRetentionDuration: nil,
			State:                         0,
			AnalyticsHubSubscriptionInfo:  nil,
			MessageTransforms:             nil,
		}

		if desired.Retention > 0 {
			sub.MessageRetentionDuration = durationpb.New(desired.Retention)
		}

		if desired.ExpirationTTL > 0 {
			sub.ExpirationPolicy = &pubsubpb.ExpirationPolicy{
				Ttl: durationpb.New(desired.ExpirationTTL),
			}
		}

		if desired.RetryPolicy != nil {
			sub.RetryPolicy = &pubsubpb.RetryPolicy{
				MinimumBackoff: durationpb.New(desired.RetryPolicy.MinimumBackoff),
				MaximumBackoff: durationpb.New(desired.RetryPolicy.MaximumBackoff),
			}
		}

		if desired.DeadLetterTopic != "" {
			sub.DeadLetterPolicy = &pubsubpb.DeadLetterPolicy{
				DeadLetterTopic:     fmt.Sprintf("projects/%s/topics/%s", projectID, desired.DeadLetterTopic),
				MaxDeliveryAttempts: desired.MaxDeliveryAttempts,
			}
		}

		_, err := client.SubscriptionAdminClient.CreateSubscription(ctx, sub)
		switch {
		case status.Code(err) == codes.AlreadyExists:
			continue
		case err != nil:
			return fmt.Errorf("create subscription %q: %w", desired.Name, err)
		default:
			logger.InfoContext(ctx, "subscription created", attr.SlogName(qname))
		}
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

// PublisherForMessage returns a *pubsub.Publisher for the topic declared by
// msg's (infra.pubsub.v1.topic) message option. It errors if msg does not
// declare a topic.
func PublisherForMessage(client *pubsub.Client, msg proto.Message) (*pubsub.Publisher, error) {
	descriptor := msg.ProtoReflect().Descriptor()

	topicOptions, ok := topicOptionsFromMessage(descriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub topic", descriptor.FullName())
	}

	return client.Publisher(resolveTopicName(descriptor, topicOptions)), nil
}

// SubscriberForMessage returns a *pubsub.Subscriber for the subscription
// declared by msg's (infra.pubsub.v1.subscription) message option. It errors
// if msg does not declare a subscription. The Go type of msg is the static
// identity of the subscription — there is no name string at the call site.
func SubscriberForMessage(client *pubsub.Client, msg proto.Message) (*pubsub.Subscriber, error) {
	descriptor := msg.ProtoReflect().Descriptor()

	subOptions, ok := subscriptionOptionsFromMessage(descriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub subscription", descriptor.FullName())
	}

	return client.Subscriber(resolveSubscriptionName(descriptor, subOptions)), nil
}

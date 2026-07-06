package gcp

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"

	"github.com/ettle/strcase"
	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	managedByLabel = "proto-pubsub-orchestrator"
	dlqSuffix      = "-dlq"
	maxTopicIDLen  = 255

	// deprecatedLabelKey is the metadata label key stamped on topics and
	// subscriptions whose marker message carries the standard protobuf
	// `option deprecated = true`. Its value is always "true".
	deprecatedLabelKey = "deprecated"

	// deprecatedLabelValue is the value stamped under deprecatedLabelKey. GCP
	// label values must match [\p{Ll}\p{Lo}\p{N}_-]{0,63}, which "true"
	// satisfies.
	deprecatedLabelValue = "true"

	// GCP requires a dead-letter policy's max delivery attempts to fall within
	// this inclusive range; values outside it are rejected at reconcile time.
	minDeliveryAttempts = 5
	maxDeliveryAttempts = 100

	// GCP requires message retention to fall within this inclusive range
	// (10 minutes to 31 days); values outside it are rejected at reconcile time.
	minRetention = 10 * time.Minute
	maxRetention = 31 * 24 * time.Hour

	// GCP requires a subscription's expiration TTL to fall within this inclusive
	// range (1 day to 31 days); values outside it are rejected at reconcile time.
	minExpirationTTL = 24 * time.Hour
	maxExpirationTTL = 31 * 24 * time.Hour

	// GCP caps retry backoff at 600 seconds, and defaults an unspecified bound
	// to 10 seconds (minimum) or 600 seconds (maximum).
	maxBackoff        = 600 * time.Second
	defaultMinBackoff = 10 * time.Second
	defaultMaxBackoff = 600 * time.Second
)

type DesiredTopic struct {
	Name         string
	Retention    time.Duration
	Labels       map[string]string
	ProtoMessage string
	// NameOverridden reports whether the topic's `name` option was explicitly
	// set (rather than the ID being derived from the proto full name). A topic
	// with an explicit name may map onto a shared, externally-owned topic, so a
	// generated schema — whose identity is tied to the proto message — must not
	// be attached to it.
	NameOverridden bool
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

func DiscoverPubSub(descriptorBytes []byte) ([]DesiredTopic, []DesiredSubscription, error) {
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

		topicOptions, hasTopic := TopicOptionsFromMessage(message)
		subOptions, hasSub := SubscriptionOptionsFromMessage(message)

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

func TopicOptionsFromMessage(message protoreflect.MessageDescriptor) (*pubsubv1.TopicOptions, bool) {
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

func SubscriptionOptionsFromMessage(message protoreflect.MessageDescriptor) (*pubsubv1.SubscriptionOptions, bool) {
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

// messageDeprecated reports whether the marker message carries the standard
// protobuf `option deprecated = true` message option.
func messageDeprecated(message protoreflect.MessageDescriptor) bool {
	options, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || options == nil {
		return false
	}
	return options.GetDeprecated()
}

func ResolveSubscriptionName(message protoreflect.MessageDescriptor, opts *pubsubv1.SubscriptionOptions) string {
	name := strings.TrimSpace(opts.GetName())
	if name == "" {
		name = string(message.FullName())
	}
	return strcase.ToKebab(name)
}

// ResolveDeadLetterTopicName returns the DLQ topic ID for a subscription's
// dead-letter policy: the kebab-cased explicit name when set, otherwise the
// subscription name with the "-dlq" suffix. Since dead_letter.name is optional,
// callers must apply the policy whenever the dead_letter block is present, not
// only when a name is given.
func ResolveDeadLetterTopicName(subName string, dl *pubsubv1.DeadLetterPolicy) string {
	dlqName := strcase.ToKebab(strings.TrimSpace(dl.GetName()))
	if dlqName == "" {
		dlqName = subName + dlqSuffix
	}
	return dlqName
}

func ResolveTopicName(message protoreflect.MessageDescriptor, topicOptions *pubsubv1.TopicOptions) string {
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
	if messageDeprecated(message) {
		labels[deprecatedLabelKey] = deprecatedLabelValue
	}

	return DesiredTopic{
		Name:           ResolveTopicName(message, topicOptions),
		Retention:      topicOptions.GetRetentionHint().AsDuration(),
		Labels:         labels,
		ProtoMessage:   string(message.FullName()),
		NameOverridden: strings.TrimSpace(topicOptions.GetName()) != "",
	}
}

func desiredSubscriptionFromOptions(message protoreflect.MessageDescriptor, subOptions *pubsubv1.SubscriptionOptions) DesiredSubscription {
	inlabels := subOptions.GetLabels()
	labels := make(map[string]string, len(inlabels)+1)
	maps.Copy(labels, inlabels)
	labels["managed_by"] = managedByLabel
	if messageDeprecated(message) {
		labels[deprecatedLabelKey] = deprecatedLabelValue
	}

	subName := ResolveSubscriptionName(message, subOptions)

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
		// Fill GCP's documented defaults for any bound left unset so we emit
		// concrete values rather than "0s" (which would override the default
		// with zero) and so the min <= max check below is meaningful.
		resolvedMin := defaultMinBackoff
		if rp.GetMinimumBackoff() != nil {
			resolvedMin = rp.GetMinimumBackoff().AsDuration()
		}
		resolvedMax := defaultMaxBackoff
		if rp.GetMaximumBackoff() != nil {
			resolvedMax = rp.GetMaximumBackoff().AsDuration()
		}
		desired.RetryPolicy = &DesiredRetryPolicy{
			MinimumBackoff: resolvedMin,
			MaximumBackoff: resolvedMax,
		}
	}

	if dl := subOptions.GetDeadLetter(); dl != nil {
		desired.DeadLetterTopic = ResolveDeadLetterTopicName(subName, dl)
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

		if err := validateRetention(topic.Retention); err != nil {
			return nil, nil, fmt.Errorf("invalid retention for topic %q from %s: %w", topic.Name, topic.ProtoMessage, err)
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

		if err := validateRetention(sub.Retention); err != nil {
			return nil, nil, fmt.Errorf("invalid retention for subscription %q on %s: %w", sub.Name, sub.ProtoMessage, err)
		}

		if err := validateExpirationTTL(sub.ExpirationTTL); err != nil {
			return nil, nil, fmt.Errorf("invalid expiration TTL for subscription %q on %s: %w", sub.Name, sub.ProtoMessage, err)
		}

		if err := validateRetryPolicy(sub.RetryPolicy); err != nil {
			return nil, nil, fmt.Errorf("invalid retry policy for subscription %q on %s: %w", sub.Name, sub.ProtoMessage, err)
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

		if err := validateMaxDeliveryAttempts(sub.MaxDeliveryAttempts); err != nil {
			return nil, nil, fmt.Errorf(
				"invalid dead-letter policy for subscription %q on %s: %w",
				sub.Name,
				sub.ProtoMessage,
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

		dlqLabels := map[string]string{
			"managed_by": managedByLabel,
			"dlq_for":    sub.Name,
		}
		// A DLQ inherits its subscription's deprecation: marking the consumer
		// deprecated should mark its dead-letter sink the same way.
		if sub.Labels[deprecatedLabelKey] == deprecatedLabelValue {
			dlqLabels[deprecatedLabelKey] = deprecatedLabelValue
		}

		dlqTopic := DesiredTopic{
			Name:         sub.DeadLetterTopic,
			Retention:    0,
			Labels:       dlqLabels,
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

func validateRetention(d time.Duration) error {
	if d == 0 {
		// Unset: GCP applies its default retention, so leave it to the server.
		return nil
	}
	if d < minRetention || d > maxRetention {
		return fmt.Errorf(
			"message retention must be between %s and %s, got %s",
			minRetention,
			maxRetention,
			d,
		)
	}
	return nil
}

func validateExpirationTTL(d time.Duration) error {
	if d == 0 {
		// Unset: the subscription never expires, which GCP allows.
		return nil
	}
	if d < minExpirationTTL || d > maxExpirationTTL {
		return fmt.Errorf(
			"expiration TTL must be between %s and %s, got %s",
			minExpirationTTL,
			maxExpirationTTL,
			d,
		)
	}
	return nil
}

func validateRetryPolicy(rp *DesiredRetryPolicy) error {
	if rp == nil {
		return nil
	}
	if rp.MinimumBackoff < 0 || rp.MinimumBackoff > maxBackoff {
		return fmt.Errorf("retry minimum backoff must be between 0s and %s, got %s", maxBackoff, rp.MinimumBackoff)
	}
	if rp.MaximumBackoff < 0 || rp.MaximumBackoff > maxBackoff {
		return fmt.Errorf("retry maximum backoff must be between 0s and %s, got %s", maxBackoff, rp.MaximumBackoff)
	}
	if rp.MinimumBackoff > rp.MaximumBackoff {
		return fmt.Errorf("retry minimum backoff %s must not exceed maximum backoff %s", rp.MinimumBackoff, rp.MaximumBackoff)
	}
	return nil
}

func validateMaxDeliveryAttempts(attempts int32) error {
	if attempts < minDeliveryAttempts || attempts > maxDeliveryAttempts {
		return fmt.Errorf(
			"max delivery attempts must be between %d and %d, got %d",
			minDeliveryAttempts,
			maxDeliveryAttempts,
			attempts,
		)
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

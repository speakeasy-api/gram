package platforminit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
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

type DesiredTopic struct {
	Name         string
	Labels       map[string]string
	ProtoMessage string
}

func ReconcileTopics(ctx context.Context, logger *slog.Logger, projectID string, client *pubsub.Client, desiredTopics []DesiredTopic) error {
	for _, desired := range desiredTopics {
		qname := fmt.Sprintf("projects/%s/topics/%s", projectID, desired.Name)

		_, err := client.TopicAdminClient.CreateTopic(ctx, &pubsubpb.Topic{
			Name:                     qname,
			Labels:                   desired.Labels,
			MessageRetentionDuration: durationpb.New(7 * 24 * time.Hour),
		})
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

func DiscoverTopicsFromBytes(descriptorBytes []byte) ([]DesiredTopic, error) {
	var descriptorSet descriptorpb.FileDescriptorSet

	if err := proto.Unmarshal(descriptorBytes, &descriptorSet); err != nil {
		return nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}

	files, err := protodesc.NewFiles(&descriptorSet)
	if err != nil {
		return nil, fmt.Errorf("build proto file registry: %w", err)
	}

	var discovered []DesiredTopic

	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		collectTopicsFromMessages(file.Messages(), &discovered)
		return true
	})

	return dedupeAndValidateTopics(discovered)
}

func collectTopicsFromMessages(messages protoreflect.MessageDescriptors, discovered *[]DesiredTopic) {
	for i := 0; i < messages.Len(); i++ {
		message := messages.Get(i)

		if topic, ok := topicFromMessage(message); ok {
			*discovered = append(*discovered, topic)
		}

		collectTopicsFromMessages(message.Messages(), discovered)
	}
}

func topicFromMessage(message protoreflect.MessageDescriptor) (DesiredTopic, bool) {
	topicOptions, ok := topicOptionsFromMessage(message)
	if !ok {
		return DesiredTopic{}, false
	}

	inlabels := topicOptions.GetLabels()
	labels := make(map[string]string, len(inlabels))
	maps.Copy(labels, inlabels)

	labels["managed_by"] = "proto_pubsub_orchestrator"

	return DesiredTopic{
		Name:         resolveTopicName(message, topicOptions),
		Labels:       labels,
		ProtoMessage: string(message.FullName()),
	}, true
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

func resolveTopicName(message protoreflect.MessageDescriptor, topicOptions *pubsubv1.TopicOptions) string {
	topicName := strings.TrimSpace(topicOptions.GetName())
	if topicName == "" {
		topicName = string(message.FullName())
	}
	return strcase.ToKebab(topicName)
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

func dedupeAndValidateTopics(topics []DesiredTopic) ([]DesiredTopic, error) {
	byName := map[string]DesiredTopic{}

	for _, topic := range topics {
		if err := validateTopicID(topic.Name); err != nil {
			return nil, fmt.Errorf("invalid topic name %q from %s: %w", topic.Name, topic.ProtoMessage, err)
		}

		if existing, exists := byName[topic.Name]; exists {
			return nil, fmt.Errorf(
				"topic %q is declared multiple times: %s and %s",
				topic.Name,
				existing.ProtoMessage,
				topic.ProtoMessage,
			)
		}

		byName[topic.Name] = topic
	}

	result := make([]DesiredTopic, 0, len(byName))

	for _, topic := range byName {
		result = append(result, topic)
	}

	return result, nil
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

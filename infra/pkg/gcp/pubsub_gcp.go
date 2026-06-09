package gcp

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/pubsub/v2"
	"github.com/speakeasy-api/gram/infra/internal/gcp"
	"google.golang.org/protobuf/proto"
)

type PubSubBroker struct {
	logger      *slog.Logger
	client      *pubsub.Client
	descriptors []byte
}

var _ SubscriberBroker = (*PubSubBroker)(nil)
var _ PublisherBroker = (*PubSubBroker)(nil)

func NewPubSubBroker(logger *slog.Logger, client *pubsub.Client, descriptors []byte) *PubSubBroker {
	return &PubSubBroker{
		logger:      logger,
		client:      client,
		descriptors: descriptors,
	}
}

func (p *PubSubBroker) PublisherForMessage(ctx context.Context, msg proto.Message) (*pubsub.Publisher, error) {
	descriptor := msg.ProtoReflect().Descriptor()

	topicOptions, ok := gcp.TopicOptionsFromMessage(descriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub topic", descriptor.FullName())
	}

	topicName := gcp.ResolveTopicName(descriptor, topicOptions)
	publisher := p.client.Publisher(topicName)

	return publisher, nil
}

func (p *PubSubBroker) SubscriberForMessage(ctx context.Context, msg proto.Message, subt proto.Message) (*pubsub.Subscriber, error) {
	subDescriptor := subt.ProtoReflect().Descriptor()

	subOptions, ok := gcp.SubscriptionOptionsFromMessage(subDescriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub subscription", subDescriptor.FullName())
	}

	// Validate that the message type the subscription consumes declares a
	// topic, mirroring the emulator broker. Without this the GCP broker would
	// silently accept an invalid message/subscription pairing that local
	// development rejects.
	msgDescriptor := msg.ProtoReflect().Descriptor()
	if _, ok := gcp.TopicOptionsFromMessage(msgDescriptor); !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub topic", msgDescriptor.FullName())
	}

	sub := p.client.Subscriber(gcp.ResolveSubscriptionName(subDescriptor, subOptions))

	return sub, nil
}

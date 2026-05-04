package infra

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/protobuf/proto"
)

type PublishResult interface {
	Ready() <-chan struct{}
	Get(ctx context.Context) (serverID string, err error)
}

type errPublishResult struct {
	err error
}

func (e *errPublishResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (e *errPublishResult) Get(ctx context.Context) (serverID string, err error) {
	return "", e.err
}

type Publisher[M any] interface {
	Publish(ctx context.Context, msg M) PublishResult
}

type psPublisherOptions struct {
	publishSettings *pubsub.PublishSettings
}

func WithPubSubPublishSettings(settings *pubsub.PublishSettings) func(*psPublisherOptions) {
	return func(o *psPublisherOptions) {
		if settings != nil {
			o.publishSettings = settings
		}
	}
}

type psPublisher[M proto.Message] struct {
	pub *pubsub.Publisher
}

// PubSubPublisherForMessage returns a *pubsub.Publisher for the topic declared by
// msg's (infra.pubsub.v1.topic) message option. It errors if msg does not
// declare a topic.
func PubSubPublisherForMessage[M proto.Message](client *pubsub.Client, msg M, opts ...func(*psPublisherOptions)) (Publisher[M], error) {
	descriptor := msg.ProtoReflect().Descriptor()

	topicOptions, ok := topicOptionsFromMessage(descriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub topic", descriptor.FullName())
	}

	var o psPublisherOptions
	for _, opt := range opts {
		opt(&o)
	}

	publisher := client.Publisher(resolveTopicName(descriptor, topicOptions))
	if o.publishSettings != nil {
		publisher.PublishSettings = *o.publishSettings
	}

	return &psPublisher[M]{pub: publisher}, nil
}

func (p *psPublisher[M]) Publish(ctx context.Context, msg M) PublishResult {
	bs, err := proto.Marshal(msg)
	if err != nil {
		return &errPublishResult{err: fmt.Errorf("marshal proto: %w", err)}
	}

	res := p.pub.Publish(ctx, &pubsub.Message{
		Data: bs,
		Attributes: map[string]string{
			"content-type": "application/x-protobuf",
			"schema":       string(proto.MessageName(msg)),
		},
	})

	return res
}

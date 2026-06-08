package gcp

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/protobuf/proto"
)

type SubscriberBroker interface {
	SubscriberForMessage(ctx context.Context, msg proto.Message, subscription proto.Message) (*pubsub.Subscriber, error)
}

type psSubscriberOptions struct {
	receiveSettings *pubsub.ReceiveSettings
}

func WithPubSubReceiveSettings(settings *pubsub.ReceiveSettings) func(*psSubscriberOptions) {
	return func(opts *psSubscriberOptions) {
		opts.receiveSettings = settings
	}
}

type PubSubAckResult interface {
	Ready() <-chan struct{}
	Get(ctx context.Context) (res pubsub.AcknowledgeStatus, err error)
}

type MessageMetadata struct {
	// ID is the unique identifier for the message, assigned by message broker.
	ID string
	// Attributes contains the attributes of the message that were carried along
	// with the payload.
	Attributes map[string]string
	// DeliveryAttempt is the number of times a message has been delivered. This
	// is part of the dead lettering feature that forwards messages that fail to
	// be processed (from nack/ack deadline timeout) to a dead letter topic. If
	// dead lettering is enabled, this will be set on all attempts, starting
	// with value 1. Otherwise, the value will be nil.
	DeliveryAttempt *int
}

type Subscriber[M proto.Message] interface {
	Receive(context.Context, func(context.Context, M, MessageMetadata) error) error
}

type psSubscriber[M proto.Message] struct {
	sub *pubsub.Subscriber
	new func() M
}

func (s *psSubscriber[M]) Receive(ctx context.Context, f func(context.Context, M, MessageMetadata) error) error {
	err := s.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		defer m.Ack()

		msg := s.new()
		if err := proto.Unmarshal(m.Data, msg); err != nil {
			m.Nack()
			return
		}
		if err := f(ctx, msg, MessageMetadata{
			ID:              m.ID,
			Attributes:      m.Attributes,
			DeliveryAttempt: m.DeliveryAttempt,
		}); err != nil {
			m.Nack()
			return
		}
	})
	if err != nil {
		return fmt.Errorf("receive: %w", err)
	}

	return nil
}

func PubSubSubscriberForMessage[M proto.Message](ctx context.Context, broker SubscriberBroker, msg M, subscription proto.Message, options ...func(*psSubscriberOptions)) (Subscriber[M], error) {
	if isNilMessage(msg) {
		return nil, fmt.Errorf("message must not be nil")
	}
	if isNilMessage(subscription) {
		return nil, fmt.Errorf("subscription marker message must not be nil")
	}

	descriptor := subscription.ProtoReflect().Descriptor()
	msgref := msg.ProtoReflect()

	if _, ok := msgref.New().Interface().(M); !ok {
		return nil, fmt.Errorf("proto message %s cannot be constructed as %T", descriptor.FullName(), msg)
	}

	sub, err := broker.SubscriberForMessage(ctx, msg, subscription)
	if err != nil {
		return nil, fmt.Errorf("get subscriber for message: %w", err)
	}

	var opts psSubscriberOptions
	for _, opt := range options {
		opt(&opts)
	}
	if opts.receiveSettings != nil {
		sub.ReceiveSettings = *opts.receiveSettings
	}

	mt := msgref.Type()
	return &psSubscriber[M]{
		sub: sub,
		new: func() M { return mt.New().Interface().(M) },
	}, nil
}

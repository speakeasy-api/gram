package infra

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/protobuf/proto"
)

type psSubscriberOptions struct {
	receiveSettings *pubsub.ReceiveSettings
}

func WithReceiveSettings(settings *pubsub.ReceiveSettings) func(*psSubscriberOptions) {
	return func(opts *psSubscriberOptions) {
		opts.receiveSettings = settings
	}
}

type AckResult interface {
	Ready() <-chan struct{}
	Get(ctx context.Context) (res pubsub.AcknowledgeStatus, err error)
}

type Message[M any] interface {
	Ack()
	AckWithResult() AckResult
	Nack()
	NackWithResult() AckResult

	ID() string
	Data() M
	Attributes() map[string]string
	DeliveryAttempt() *int
}

type psMessage[M any] struct {
	msg  *pubsub.Message
	data M
}

func (m *psMessage[M]) Ack() {
	m.msg.Ack()
}

func (m *psMessage[M]) AckWithResult() AckResult {
	return m.msg.AckWithResult()
}

func (m *psMessage[M]) Nack() {
	m.msg.Nack()
}

func (m *psMessage[M]) NackWithResult() AckResult {
	return m.msg.NackWithResult()
}

func (m *psMessage[M]) ID() string {
	return m.msg.ID
}

func (m *psMessage[M]) Data() M {
	return m.data
}

func (m *psMessage[M]) Attributes() map[string]string {
	return m.msg.Attributes
}

func (m *psMessage[M]) DeliveryAttempt() *int {
	return m.msg.DeliveryAttempt
}

type Subscriber[M proto.Message] interface {
	Receive(context.Context, func(context.Context, Message[M])) error
}

type psSubscriber[M proto.Message] struct {
	sub *pubsub.Subscriber
	new func() M
}

func (s *psSubscriber[M]) Receive(ctx context.Context, f func(context.Context, Message[M])) error {
	err := s.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		msg := s.new()
		if err := proto.Unmarshal(m.Data, msg); err != nil {
			return
		}
		f(ctx, &psMessage[M]{msg: m, data: msg})
	})
	if err != nil {
		return fmt.Errorf("receive: %w", err)
	}

	return nil
}

func PubSubSubscriberForMessage[M proto.Message](client *pubsub.Client, msg M, subt proto.Message, options ...func(*psSubscriberOptions)) (Subscriber[M], error) {
	descriptor := subt.ProtoReflect().Descriptor()

	subOptions, ok := subscriptionOptionsFromMessage(descriptor)
	if !ok {
		return nil, fmt.Errorf("proto message %s does not declare a pubsub subscription", descriptor.FullName())
	}

	if _, ok := msg.ProtoReflect().New().Interface().(M); !ok {
		return nil, fmt.Errorf("proto message %s cannot be constructed as %T", descriptor.FullName(), msg)
	}

	var opts psSubscriberOptions
	for _, opt := range options {
		opt(&opts)
	}

	sub := client.Subscriber(resolveSubscriptionName(descriptor, subOptions))
	if opts.receiveSettings != nil {
		sub.ReceiveSettings = *opts.receiveSettings
	}

	mt := msg.ProtoReflect().Type()
	return &psSubscriber[M]{
		sub: sub,
		new: func() M { return mt.New().Interface().(M) },
	}, nil
}

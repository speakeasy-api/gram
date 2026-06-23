package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"cloud.google.com/go/pubsub/v2"
	"github.com/speakeasy-api/gram/infra/internal/attr"
	"github.com/speakeasy-api/gram/infra/internal/gcp"
	"google.golang.org/protobuf/proto"
)

type SubscriberBroker interface {
	SubscriberForMessage(ctx context.Context, msg proto.Message, subscription proto.Message) (*pubsub.Subscriber, error)
}

type psSubscriberOptions struct {
	receiveSettings *pubsub.ReceiveSettings
	logger          *slog.Logger
}

func WithPubSubReceiveSettings(settings *pubsub.ReceiveSettings) func(*psSubscriberOptions) {
	return func(opts *psSubscriberOptions) {
		opts.receiveSettings = settings
	}
}

// WithSubscriberLogger sets the logger used to report panics recovered while
// processing messages. When unset the subscriber falls back to slog.Default().
func WithSubscriberLogger(logger *slog.Logger) func(*psSubscriberOptions) {
	return func(opts *psSubscriberOptions) {
		if logger != nil {
			opts.logger = logger
		}
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

// incomingMessage decouples per-message processing from the concrete
// *pubsub.Message so the handling logic (including panic recovery) can be
// exercised in tests without a live subscriber.
type incomingMessage struct {
	id              string
	data            []byte
	attributes      map[string]string
	deliveryAttempt *int
	ack             func()
	nack            func()
}

type psSubscriber[M proto.Message] struct {
	sub                   *pubsub.Subscriber
	new                   func() M
	logger                *slog.Logger
	topicProtoName        string
	subscriptionProtoName string
}

func (s *psSubscriber[M]) Receive(ctx context.Context, f func(context.Context, M, MessageMetadata) error) error {
	err := s.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		s.handle(ctx, incomingMessage{
			id:              m.ID,
			data:            m.Data,
			attributes:      m.Attributes,
			deliveryAttempt: m.DeliveryAttempt,
			ack:             m.Ack,
			nack:            m.Nack,
		}, f)
	})
	if err != nil {
		return fmt.Errorf("receive: %w", err)
	}

	return nil
}

func (s *psSubscriber[M]) handle(ctx context.Context, m incomingMessage, f func(context.Context, M, MessageMetadata) error) {
	defer m.ack()

	// Recover from panics in the handler so a single bad message does not take
	// down the receive goroutine. Log with diagnostic context so the crash is
	// visible instead of becoming a silent redelivery loop, then nack so the
	// message is redelivered (and eventually dead-lettered if it keeps
	// panicking).
	defer func() {
		if r := recover(); r != nil {
			deliveryAttempt := 0
			if m.deliveryAttempt != nil {
				deliveryAttempt = *m.deliveryAttempt
			}

			s.logger.ErrorContext(ctx, "panic recovered while processing pubsub message",
				attr.SlogErrorMessage(fmt.Sprintf("%v", r)),
				attr.SlogErrorStack(string(debug.Stack())),
				attr.SlogTopicProtoName(s.topicProtoName),
				attr.SlogSubscriptionProtoName(s.subscriptionProtoName),
				slog.String("message_id", m.id),
				slog.Int("delivery_attempt", deliveryAttempt),
			)
			m.nack()
		}
	}()

	msg := s.new()
	if err := proto.Unmarshal(m.data, msg); err != nil {
		m.nack()
		return
	}
	if err := f(ctx, msg, MessageMetadata{
		ID:              m.id,
		Attributes:      m.attributes,
		DeliveryAttempt: m.deliveryAttempt,
	}); err != nil {
		m.nack()
		return
	}
}

type SubscriberOption func(*psSubscriberOptions)

func PubSubSubscriberForMessage[M proto.Message](ctx context.Context, broker SubscriberBroker, msg M, subscription proto.Message, options ...SubscriberOption) (Subscriber[M], error) {
	if isNilMessage(msg) {
		return nil, fmt.Errorf("message must not be nil")
	}
	if isNilMessage(subscription) {
		return nil, fmt.Errorf("subscription marker message must not be nil")
	}

	descriptor := subscription.ProtoReflect().Descriptor()
	msgref := msg.ProtoReflect()

	// A BigQuery export sink delivers to a table, not to application code, so it
	// is not consumable. Reject it here rather than letting a caller wire up a
	// receive loop that would never get messages.
	if subOptions, ok := gcp.SubscriptionOptionsFromMessage(descriptor); ok && subOptions.GetBigquery() != nil {
		return nil, fmt.Errorf("subscription %s is a BigQuery export sink and cannot be consumed", descriptor.FullName())
	}

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
	logger := opts.logger
	if logger == nil {
		logger = slog.Default()
	}

	mt := msgref.Type()
	return &psSubscriber[M]{
		sub:                   sub,
		new:                   func() M { return mt.New().Interface().(M) },
		logger:                logger,
		topicProtoName:        string(msgref.Descriptor().FullName()),
		subscriptionProtoName: string(descriptor.FullName()),
	}, nil
}

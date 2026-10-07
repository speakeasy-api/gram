package gcp

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub/v2"
	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/speakeasy-api/gram/infra/internal/gcp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// StorageSubscriberForMessage is the storage-owned broker path. Ordinary
// subscribers reject these markers so application handlers cannot compete.
func (p *PubSubBroker) StorageSubscriberForMessage(_ context.Context, payload, marker proto.Message) (*pubsub.Subscriber, error) {
	opts, _, err := storageTransportOptions(payload, marker)
	if err != nil {
		return nil, err
	}
	return p.client.Subscriber(gcp.ResolveSubscriptionName(marker.ProtoReflect().Descriptor(), opts)), nil
}

// StorageSubscriberForMessage reconciles the dedicated storage subscription in
// the emulator. Buckets are supplied separately by the consuming process.
func (e *EmulatedPubSubBroker) StorageSubscriberForMessage(ctx context.Context, payload, marker proto.Message) (*pubsub.Subscriber, error) {
	opts, topicOpts, err := storageTransportOptions(payload, marker)
	if err != nil {
		return nil, err
	}
	topicName := gcp.ResolveTopicName(payload.ProtoReflect().Descriptor(), topicOpts)
	subName := gcp.ResolveSubscriptionName(marker.ProtoReflect().Descriptor(), opts)
	if err := e.reconcileTopic(ctx, topicName, topicOpts); err != nil {
		return nil, fmt.Errorf("reconcile storage topic: %w", err)
	}
	if err := e.reconcileSubscriptions(ctx, subName, topicName, opts); err != nil {
		return nil, fmt.Errorf("reconcile storage subscription: %w", err)
	}
	return e.client.Subscriber(subName), nil
}

func storageTransportOptions(payload, marker proto.Message) (*pubsubv1.StorageSubscriptionOptions, *pubsubv1.TopicOptions, error) {
	if isNilMessage(payload) || isNilMessage(marker) {
		return nil, nil, fmt.Errorf("storage payload and marker must not be nil")
	}
	md, pd := marker.ProtoReflect().Descriptor(), payload.ProtoReflect().Descriptor()
	opts, ok := gcp.StorageOptionsFromMessage(md)
	if !ok {
		return nil, nil, fmt.Errorf("message %s is not a storage subscription", md.FullName())
	}
	if opts.GetTopic() != string(pd.FullName()) {
		return nil, nil, fmt.Errorf("storage subscription %s expects %s, got %s", md.FullName(), opts.GetTopic(), pd.FullName())
	}
	if _, ok := gcp.SubscriptionOptionsFromMessage(md); ok {
		return nil, nil, fmt.Errorf("storage marker %s also declares an application subscription", md.FullName())
	}
	if _, ok := gcp.TopicOptionsFromMessage(md); ok {
		return nil, nil, fmt.Errorf("storage marker %s also declares a topic", md.FullName())
	}
	topicOpts, ok := gcp.TopicOptionsFromMessage(pd)
	if !ok || topicOpts.GetName() != "" {
		return nil, nil, fmt.Errorf("storage payload %s must declare a schema-bound topic without a name override", pd.FullName())
	}
	return opts, topicOpts, nil
}

// subscriptionTransportOptions keeps emulator reconciliation identical for the
// two ownership models while retaining distinct protobuf declaration types.
type subscriptionTransportOptions interface {
	GetAckDeadline() *durationpb.Duration
	GetExpirationTtl() *durationpb.Duration
	GetRetryPolicy() *pubsubv1.RetryPolicy
	GetRetention() *durationpb.Duration
	GetDeadLetter() *pubsubv1.DeadLetterPolicy
	GetRetainAckedMessages() bool
	GetLabels() map[string]string
	GetFilter() string
}

package gcp

import (
	"log/slog"
	"testing"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestApplicationBrokers_RejectStorageMarkerBeforeIO(t *testing.T) {
	t.Parallel()

	options := &descriptorpb.MessageOptions{}
	proto.SetExtension(options, pubsubv1.E_StorageSubscription, pubsubv1.StorageSubscriptionOptions_builder{
		Topic: new("example.v1.Event"), Bucket: new("event-archive"),
	}.Build())

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: new("example/v1/archive.proto"), Package: new("example.v1"), Syntax: new("proto3"),
		Dependency:  []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{Name: new("Archive"), Options: options}},
	}, protoregistry.GlobalFiles)
	require.NoError(t, err)

	marker := dynamicpb.NewMessage(file.Messages().Get(0))
	logger := slog.New(slog.DiscardHandler)

	// Nil clients prove the ownership check happens before any network use.
	for _, broker := range []SubscriberBroker{
		NewPubSubBroker(logger, nil, nil),
		NewEmulatedPubSub(logger, "example-project", nil, nil),
	} {
		_, err := broker.SubscriberForMessage(t.Context(), nil, marker)
		require.ErrorContains(t, err, "example.v1.Archive declares a storage subscription")
		require.ErrorContains(t, err, "generated Go storage runner")
	}
}

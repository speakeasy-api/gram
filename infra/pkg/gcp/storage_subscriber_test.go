package gcp

import (
	"testing"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestStorageTransportOptions_NormalizesTopicDeclarations(t *testing.T) {
	t.Parallel()

	marker, payload := &descriptorpb.MessageOptions{}, &descriptorpb.MessageOptions{}
	proto.SetExtension(marker, pubsubv1.E_StorageSubscription, pubsubv1.StorageSubscriptionOptions_builder{Topic: new(" example.v1.Event \t")}.Build())
	proto.SetExtension(payload, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{Name: new(" \t")}.Build())

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: new("example/v1/test.proto"), Package: new("example.v1"), Syntax: new("proto3"), Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{Name: new("Event"), Options: payload}, {Name: new("Archive"), Options: marker}},
	}, protoregistry.GlobalFiles)
	require.NoError(t, err)

	_, _, err = storageTransportOptions(dynamicpb.NewMessage(file.Messages().Get(0)), dynamicpb.NewMessage(file.Messages().Get(1)))
	require.NoError(t, err)
}

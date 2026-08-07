package gcp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pingv2 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"
)

// TestEncodedMessageAttributes_CallerAttributesSurvive pins the behaviour the
// whole attributes column of the publish outbox exists for. The relay
// republishes a message long after it was written, under a trace of its own;
// if the derived markers were allowed to win wholesale, or if trace context
// were injected at publish time, the link from producer to subscriber would be
// silently lost.
func TestEncodedMessageAttributes_CallerAttributesSurvive(t *testing.T) {
	t.Parallel()

	attrs := encodedMessageAttributes(map[string]string{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"event_type":  "audit_log.asset_event_v1",
	}, string(proto.MessageName(&pingv2.Message{})))

	require.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", attrs["traceparent"])
	require.Equal(t, "audit_log.asset_event_v1", attrs["event_type"])
}

func TestEncodedMessageAttributes_DerivedMarkersCannotBeOverridden(t *testing.T) {
	t.Parallel()

	attrs := encodedMessageAttributes(map[string]string{
		"content-type": "application/json",
		"schema":       "gram.lies.v1.NotThis",
	}, string(proto.MessageName(&pingv2.Message{})))

	require.Equal(t, "application/x-protobuf", attrs["content-type"],
		"a stored attribute map must not be able to misdeclare the wire format of its own payload")
	require.Equal(t, string(proto.MessageName(&pingv2.Message{})), attrs["schema"])
}

func TestPubSubEncodedPublisherForMessage_RejectsNilMessage(t *testing.T) {
	t.Parallel()

	var msg *pingv2.Message
	_, err := PubSubEncodedPublisherForMessage(t.Context(), nil, msg)

	require.Error(t, err, "a typed nil cannot name a topic and must fail before the broker is consulted")
}

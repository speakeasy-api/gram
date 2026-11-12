package channels

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSameAccountStreamsAreIndependent is the fix for treating a JSON-RPC
// request ID as a subscription's identity.
//
// A request ID identifies a request on one connection. Two MCP clients signed
// in as the same person both picking id 1 is ordinary, not a conflict: the
// second must get its own stream, and neither client may be able to terminate
// the other's by cancelling "id 1".
func TestSameAccountStreamsAreIndependent(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_stream_identity")
	f.enable(t, "alice")

	first := f.listen(t, "bob", 1, "channels://presence")
	firstAck := first.next(t)
	second := f.listen(t, "bob", 1, "channels://presence")
	secondAck := second.next(t)

	// Both were admitted. The PoC refused the second outright.
	require.Equal(t, "notifications/subscriptions/acknowledged", firstAck["method"])
	require.Equal(t, "notifications/subscriptions/acknowledged", secondAck["method"])

	firstToken := subscriptionToken(t, firstAck)
	secondToken := subscriptionToken(t, secondAck)
	require.NotEmpty(t, firstToken)
	require.NotEqual(t, firstToken, secondToken, "two streams received the same handle")

	// The protocol's own cancellation is ambiguous while both are open, so it
	// cancels nothing rather than guessing and killing a sibling.
	f.cancel(t, "bob", 1, "")
	first.silent(t, 2*time.Second)
	second.silent(t, 2*time.Second)

	// The server-issued token is unambiguous: exactly one stream completes.
	f.cancel(t, "bob", 1, secondToken)
	completion := second.next(t)
	result := mapOf(t, completion["result"])
	require.Equal(t, "complete", result["resultType"])
	first.silent(t, 2*time.Second)

	// With one left, the protocol's request ID is unambiguous again.
	f.cancel(t, "bob", 1, "")
	result = mapOf(t, first.next(t)["result"])
	require.Equal(t, "complete", result["resultType"])
}

// TestAnotherAccountCannotCancelWithAToken proves the token names a stream but
// does not transfer authority over it.
func TestAnotherAccountCannotCancelWithAToken(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_stream_theft")
	f.enable(t, "alice")

	held := f.listen(t, "bob", "mine", "channels://presence")
	token := subscriptionToken(t, held.next(t))
	require.NotEmpty(t, token)

	f.cancel(t, "outsider", "mine", token)
	held.silent(t, 2*time.Second)

	f.cancel(t, "bob", "mine", token)
	result := mapOf(t, held.next(t)["result"])
	require.Equal(t, "complete", result["resultType"])
}

// TestClosingTheStreamAlwaysCancels pins the unambiguous route that needs no
// identity at all: hanging up.
func TestClosingTheStreamAlwaysCancels(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_stream_hangup")
	f.enable(t, "alice")

	streams := make([]*eventStream, 0, 3)
	for range 3 {
		opened := f.listen(t, "bob", "shared", "channels://presence")
		require.Equal(t, "notifications/subscriptions/acknowledged", opened.next(t)["method"])
		streams = append(streams, opened)
	}
	// Three streams share one request ID, so the protocol's cancellation is
	// ambiguous for all of them; closing the HTTP stream is not.
	f.cancel(t, "bob", "shared", "")
	for _, held := range streams {
		held.silent(t, time.Second)
	}
	streams[1].close()
	require.Eventually(t, func() bool {
		return f.handler.streamCount() == 2
	}, 10*time.Second, 50*time.Millisecond, "closing the HTTP stream did not release its subscription")
}

// TestStreamCapCountsEveryStream proves the per-identity cap still holds now
// that duplicate request IDs are allowed.
func TestStreamCapCountsEveryStream(t *testing.T) {
	t.Parallel()
	db := newDatabase(t, "channels_stream_cap_duplicates")
	f := newFixtureOn(t, db)
	f.handler.limits.StreamsPerSubject = 2
	f.enable(t, "alice")

	for range 2 {
		opened := f.listen(t, "bob", 1, "channels://presence")
		require.Equal(t, "notifications/subscriptions/acknowledged", opened.next(t)["method"])
	}
	refused := f.rpc(t, "bob", "subscriptions/listen", map[string]any{
		"notifications": map[string]any{"resourceSubscriptions": []string{"channels://presence"}},
	})
	require.NotNil(t, refused["error"], "duplicate request IDs bypassed the per-identity cap")

	// A different identity is unaffected.
	other := f.listen(t, "alice", 1, "channels://presence")
	require.Equal(t, "notifications/subscriptions/acknowledged", other.next(t)["method"])
}

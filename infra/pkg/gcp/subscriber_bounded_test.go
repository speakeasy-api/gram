package gcp

import (
	"context"
	"errors"
	"testing"

	"cloud.google.com/go/pubsub/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestBoundBatchReceiver_CapsWaitingCallbacks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		settings        BatchReceiveSettings
		current         pubsub.ReceiveSettings
		messages, bytes int
	}{
		{"explicit budgets", BatchReceiveSettings{MaxBufferedMessages: 4, MaxBufferedBytes: 1024}, pubsub.ReceiveSettings{MaxOutstandingMessages: 100, MaxOutstandingBytes: 1 << 20}, 4, 1024},
		{"unlimited receiver", BatchReceiveSettings{MaxBufferedMessages: 4, MaxBufferedBytes: 1024}, pubsub.ReceiveSettings{MaxOutstandingMessages: -1, MaxOutstandingBytes: -1}, 4, 1024},
		{"preserve tighter limits", BatchReceiveSettings{MaxBufferedMessages: 4, MaxBufferedBytes: 1024}, pubsub.ReceiveSettings{MaxOutstandingMessages: 2, MaxOutstandingBytes: 512}, 2, 512},
		{"default count", BatchReceiveSettings{MaxBufferedBytes: 1024}, pubsub.ReceiveSettings{}, 200, 1024},
		{"default bytes", BatchReceiveSettings{MaxBufferedMessages: 4}, pubsub.ReceiveSettings{}, 4, 64 << 20},
		{"unbounded mode", BatchReceiveSettings{}, pubsub.ReceiveSettings{MaxOutstandingMessages: 100, MaxOutstandingBytes: 4096}, 100, 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := &psSubscriber[*emptypb.Empty]{sub: &pubsub.Subscriber{ReceiveSettings: tc.current}}
			s.boundBatchReceiver(tc.settings)

			require.Equal(t, tc.messages, s.sub.ReceiveSettings.MaxOutstandingMessages)
			require.Equal(t, tc.bytes, s.sub.ReceiveSettings.MaxOutstandingBytes)
		})
	}
}

func TestBatchLoop_BoundedReceiverErrorContext(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"batch", "per-message"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			s := newPanicSubscriber(nil)
			failure := errors.New("stream failed")
			receive := func(context.Context, func(incomingMessage)) error { return failure }
			settings := BatchReceiveSettings{MaxBufferedMessages: 2}

			var err error
			if mode == "batch" {
				err = s.batchLoop(t.Context(), settings, receive, func(context.Context, []*emptypb.Empty, []MessageMetadata) error { return nil })
			} else {
				err = s.batchLoopWithResult(t.Context(), settings, receive, func(context.Context, []BatchMessage[*emptypb.Empty]) error { return nil })
			}

			require.ErrorIs(t, err, failure)
			require.EqualError(t, err, "receive batch: stream failed")
		})
	}
}

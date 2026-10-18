package gcp

import (
	"context"
	"errors"
	"testing"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/pstest"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
			restore := s.boundBatchReceiver(tc.settings)

			require.Equal(t, tc.messages, s.sub.ReceiveSettings.MaxOutstandingMessages)
			require.Equal(t, tc.bytes, s.sub.ReceiveSettings.MaxOutstandingBytes)

			restore()
			require.Equal(t, tc.current, s.sub.ReceiveSettings)
		})
	}
}

func TestReceiveBatch_RestoresLimitsBeforeSubscriberReuse(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"batch", "per-message"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			server := pstest.NewServer()
			t.Cleanup(func() { require.NoError(t, server.Close()) })

			conn, err := grpc.NewClient(server.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)

			client, err := pubsub.NewClient(t.Context(), "test-project", option.WithGRPCConn(conn))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })

			s := newPanicSubscriber(nil)
			s.sub = client.Subscriber("test-subscription")
			original := s.sub.ReceiveSettings
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			for _, settings := range []BatchReceiveSettings{{MaxBufferedMessages: 2, MaxBufferedBytes: 1024}, {}} {
				if mode == "batch" {
					err = s.ReceiveBatch(ctx, settings, func(context.Context, []*emptypb.Empty, []MessageMetadata) error { return nil })
				} else {
					err = s.ReceiveBatchWithResult(ctx, settings, func(context.Context, []BatchMessage[*emptypb.Empty]) error { return nil })
				}

				require.NoError(t, err)
				require.Equal(t, original, s.sub.ReceiveSettings)
			}

			require.NoError(t, s.Receive(ctx, func(context.Context, *emptypb.Empty, MessageMetadata) error { return nil }))
			require.Equal(t, original, s.sub.ReceiveSettings)
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

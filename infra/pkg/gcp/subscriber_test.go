package gcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// captureMessage records ack/nack calls made against an incomingMessage.
type captureMessage struct {
	mu      sync.Mutex
	acked   bool
	nacked  bool
	ackHits int
}

func (c *captureMessage) onAck() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acked = true
	c.ackHits++
}

func (c *captureMessage) onNack() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nacked = true
}

func (c *captureMessage) isAcked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acked
}

func newPanicSubscriber(logger *slog.Logger) *psSubscriber[*emptypb.Empty] {
	return &psSubscriber[*emptypb.Empty]{
		sub:                   nil,
		new:                   func() *emptypb.Empty { return &emptypb.Empty{} },
		logger:                logger,
		topicProtoName:        "test.v1.TopicMessage",
		subscriptionProtoName: "test.v1.SubscriptionMarker",
	}
}

func TestHandle_PanicIsLoggedAndNacked(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))

	s := newPanicSubscriber(logger)

	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	attempt := 3
	m := incomingMessage{
		id:              "msg-123",
		data:            nil,
		attributes:      nil,
		deliveryAttempt: &attempt,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}

	s.handle(t.Context(), m, func(context.Context, *emptypb.Empty, MessageMetadata) error {
		panic("boom")
	})

	require.True(t, cap.nacked, "message should be nacked after a panic")
	require.True(t, cap.acked, "deferred ack still runs; pubsub first-call-wins makes it a no-op after nack")

	logged := buf.String()
	require.Contains(t, logged, "panic recovered while processing pubsub message")
	require.Contains(t, logged, "boom")
	require.Contains(t, logged, "msg-123")
	require.Contains(t, logged, "delivery_attempt=3")
	require.Contains(t, logged, "stack=")
	require.Contains(t, logged, "test.v1.TopicMessage")
	require.Contains(t, logged, "test.v1.SubscriptionMarker")
}

func TestHandle_UnmarshalErrorIsNacked(t *testing.T) {
	t.Parallel()

	s := newPanicSubscriber(slog.New(slog.DiscardHandler))

	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	called := false
	m := incomingMessage{
		id:              "msg-bad",
		data:            []byte("not-a-valid-proto-wire-format-\xff\xff"),
		attributes:      nil,
		deliveryAttempt: nil,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}

	s.handle(t.Context(), m, func(context.Context, *emptypb.Empty, MessageMetadata) error {
		called = true
		return nil
	})

	require.True(t, cap.nacked, "message should be nacked when it fails to unmarshal")
	require.False(t, called, "handler should not run when unmarshalling fails")
}

func TestHandle_SuccessIsAckedOnly(t *testing.T) {
	t.Parallel()

	s := newPanicSubscriber(slog.New(slog.DiscardHandler))

	data, err := proto.Marshal(&emptypb.Empty{})
	require.NoError(t, err)

	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	var gotMeta MessageMetadata
	m := incomingMessage{
		id:              "msg-ok",
		data:            data,
		attributes:      map[string]string{"k": "v"},
		deliveryAttempt: nil,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}

	s.handle(t.Context(), m, func(_ context.Context, _ *emptypb.Empty, meta MessageMetadata) error {
		gotMeta = meta
		return nil
	})

	require.True(t, cap.acked, "message should be acked on success")
	require.False(t, cap.nacked, "message should not be nacked on success")
	require.Equal(t, "msg-ok", gotMeta.ID)
	require.Equal(t, map[string]string{"k": "v"}, gotMeta.Attributes)
}

// newBatchMessage builds an incomingMessage wired to its own captureMessage so
// per-message ack/nack can be asserted independently within a batch.
func newBatchMessage(id string, data []byte, attributes map[string]string) (incomingMessage, *captureMessage) {
	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	m := incomingMessage{
		id:              id,
		data:            data,
		attributes:      attributes,
		deliveryAttempt: nil,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}
	return m, cap
}

func TestHandleBatch_SuccessAllAcked(t *testing.T) {
	t.Parallel()

	s := newPanicSubscriber(slog.New(slog.DiscardHandler))

	data, err := proto.Marshal(&emptypb.Empty{})
	require.NoError(t, err)

	m1, c1 := newBatchMessage("msg-1", data, map[string]string{"k": "1"})
	m2, c2 := newBatchMessage("msg-2", data, map[string]string{"k": "2"})

	var gotMetas []MessageMetadata
	var gotLen int
	s.handleBatch(t.Context(), []incomingMessage{m1, m2}, func(_ context.Context, msgs []*emptypb.Empty, metas []MessageMetadata) error {
		gotLen = len(msgs)
		gotMetas = metas
		return nil
	})

	require.Equal(t, 2, gotLen, "handler should receive both messages")
	require.Len(t, gotMetas, 2)
	require.Equal(t, "msg-1", gotMetas[0].ID, "metadata should preserve batch order")
	require.Equal(t, "msg-2", gotMetas[1].ID)
	require.Equal(t, map[string]string{"k": "1"}, gotMetas[0].Attributes)

	require.True(t, c1.acked)
	require.True(t, c2.acked)
	require.False(t, c1.nacked)
	require.False(t, c2.nacked)
}

func TestHandleBatch_HandlerErrorAllNacked(t *testing.T) {
	t.Parallel()

	s := newPanicSubscriber(slog.New(slog.DiscardHandler))

	data, err := proto.Marshal(&emptypb.Empty{})
	require.NoError(t, err)

	m1, c1 := newBatchMessage("msg-1", data, nil)
	m2, c2 := newBatchMessage("msg-2", data, nil)

	s.handleBatch(t.Context(), []incomingMessage{m1, m2}, func(context.Context, []*emptypb.Empty, []MessageMetadata) error {
		return errors.New("boom")
	})

	require.True(t, c1.nacked, "whole batch should be nacked on handler error")
	require.True(t, c2.nacked)
}

func TestHandleBatch_BadMessageNackedIndividually(t *testing.T) {
	t.Parallel()

	s := newPanicSubscriber(slog.New(slog.DiscardHandler))

	data, err := proto.Marshal(&emptypb.Empty{})
	require.NoError(t, err)

	good1, cgood1 := newBatchMessage("msg-good-1", data, nil)
	bad, cbad := newBatchMessage("msg-bad", []byte("not-a-valid-proto-wire-format-\xff\xff"), nil)
	good2, cgood2 := newBatchMessage("msg-good-2", data, nil)

	var gotIDs []string
	s.handleBatch(t.Context(), []incomingMessage{good1, bad, good2}, func(_ context.Context, _ []*emptypb.Empty, metas []MessageMetadata) error {
		for _, meta := range metas {
			gotIDs = append(gotIDs, meta.ID)
		}
		return nil
	})

	require.Equal(t, []string{"msg-good-1", "msg-good-2"}, gotIDs, "bad message should be excluded from the batch")
	require.True(t, cbad.nacked, "unmarshalable message should be nacked individually")
	require.False(t, cbad.acked)
	require.True(t, cgood1.acked, "valid messages should be acked")
	require.True(t, cgood2.acked)
	require.False(t, cgood1.nacked)
	require.False(t, cgood2.nacked)
}

func TestHandleBatch_PanicIsLoggedAndNacked(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))

	s := newPanicSubscriber(logger)

	data, err := proto.Marshal(&emptypb.Empty{})
	require.NoError(t, err)

	m1, c1 := newBatchMessage("msg-1", data, nil)
	m2, c2 := newBatchMessage("msg-2", data, nil)

	s.handleBatch(t.Context(), []incomingMessage{m1, m2}, func(context.Context, []*emptypb.Empty, []MessageMetadata) error {
		panic("boom")
	})

	require.True(t, c1.nacked, "whole batch should be nacked after a panic")
	require.True(t, c2.nacked)
	require.True(t, c1.acked, "deferred ack still runs; pubsub first-call-wins makes it a no-op after nack")

	logged := buf.String()
	require.Contains(t, logged, "panic recovered while processing pubsub message batch")
	require.Contains(t, logged, "boom")
	require.Contains(t, logged, "msg-1")
	require.Contains(t, logged, "batch_size=2")
	require.Contains(t, logged, "stack=")
	require.Contains(t, logged, "test.v1.TopicMessage")
	require.Contains(t, logged, "test.v1.SubscriptionMarker")
}

func TestHandleBatch_AllBadMessagesHandlerNotCalled(t *testing.T) {
	t.Parallel()

	s := newPanicSubscriber(slog.New(slog.DiscardHandler))

	bad, cbad := newBatchMessage("msg-bad", []byte("not-a-valid-proto-wire-format-\xff\xff"), nil)

	called := false
	s.handleBatch(t.Context(), []incomingMessage{bad}, func(context.Context, []*emptypb.Empty, []MessageMetadata) error {
		called = true
		return nil
	})

	require.False(t, called, "handler should not run when no message unmarshals")
	require.True(t, cbad.nacked)
}

// batchCapture records, in order, the message IDs handed to a batch handler so
// tests can assert which messages flushed together. Safe for the handler to call
// from the receive goroutine while the test goroutine reads snapshots.
type batchCapture struct {
	mu      sync.Mutex
	batches [][]string
}

func (c *batchCapture) record(metas []MessageMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, 0, len(metas))
	for _, meta := range metas {
		ids = append(ids, meta.ID)
	}
	c.batches = append(c.batches, ids)
}

func (c *batchCapture) snapshot() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]string, len(c.batches))
	copy(out, c.batches)
	return out
}

// TestBatchLoop_FlushesOnLatency drives batchLoop with a synthetic source that
// delivers a partial batch and then idles. Under synctest's fake clock the
// latency ticker is the only thing that can flush it, so the test can prove the
// timer path fires (and only after the window elapses) without real waiting.
func TestBatchLoop_FlushesOnLatency(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newPanicSubscriber(slog.New(slog.DiscardHandler))

		data, err := proto.Marshal(&emptypb.Empty{})
		require.NoError(t, err)

		m1, c1 := newBatchMessage("msg-1", data, nil)
		m2, c2 := newBatchMessage("msg-2", data, nil)

		// MaxMessages far above the two delivered messages so only the latency
		// timer can trigger a flush.
		settings := BatchReceiveSettings{MaxMessages: 100, MaxBytes: 0, MaxLatency: time.Second}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		capt := &batchCapture{} //nolint:exhaustruct // zero values are the initial state under test

		receive := func(ctx context.Context, deliver func(incomingMessage)) error {
			deliver(m1)
			deliver(m2)
			<-ctx.Done()
			return nil
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- s.batchLoop(ctx, settings, receive, func(_ context.Context, _ []*emptypb.Empty, metas []MessageMetadata) error {
				capt.record(metas)
				return nil
			})
		}()

		// Both messages are buffered but the latency window has not elapsed, so
		// nothing has flushed yet.
		synctest.Wait()
		require.Empty(t, capt.snapshot(), "partial batch should not flush before the latency window")

		// Advance the fake clock past the window so the ticker flushes the batch.
		time.Sleep(time.Second + 10*time.Millisecond) //nolint:forbidigo // GG013: advances the synctest fake clock instantly; valid ONLY within synctest.Test
		synctest.Wait()
		require.Equal(t, [][]string{{"msg-1", "msg-2"}}, capt.snapshot(), "latency timer should flush the buffered batch")
		require.True(t, c1.isAcked())
		require.True(t, c2.isAcked())

		cancel()
		require.NoError(t, <-errCh)
	})
}

// TestBatchLoop_LatencyArmsOnFirstMessage proves the timer is armed when a
// message starts a batch, not free-running from loop start: a message delivered
// partway into the first latency window must still wait a full window measured
// from its own arrival. A free-running ticker would have flushed it early.
func TestBatchLoop_LatencyArmsOnFirstMessage(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newPanicSubscriber(slog.New(slog.DiscardHandler))

		data, err := proto.Marshal(&emptypb.Empty{})
		require.NoError(t, err)

		m1, c1 := newBatchMessage("msg-1", data, nil)

		settings := BatchReceiveSettings{MaxMessages: 100, MaxBytes: 0, MaxLatency: time.Second}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		capt := &batchCapture{} //nolint:exhaustruct // zero values are the initial state under test

		// Idle for 700ms before the first message, so the message arrives partway
		// into the window a free-running ticker (started at loop start) would use.
		receive := func(ctx context.Context, deliver func(incomingMessage)) error {
			time.Sleep(700 * time.Millisecond) //nolint:forbidigo // GG013: advances the synctest fake clock instantly; valid ONLY within synctest.Test
			deliver(m1)
			<-ctx.Done()
			return nil
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- s.batchLoop(ctx, settings, receive, func(_ context.Context, _ []*emptypb.Empty, metas []MessageMetadata) error {
				capt.record(metas)
				return nil
			})
		}()

		// Advance to 1.1s: past the window measured from loop start (1.0s) but
		// before the window measured from the message at 700ms (1.7s). A ticker
		// would have flushed here; the per-batch timer must not have.
		time.Sleep(1100 * time.Millisecond) //nolint:forbidigo // GG013: advances the synctest fake clock instantly; valid ONLY within synctest.Test
		synctest.Wait()
		require.Empty(t, capt.snapshot(), "timer should measure latency from the first message, not loop start")

		// Advance past the message's own window (to ~1.8s) so the timer fires.
		time.Sleep(700 * time.Millisecond) //nolint:forbidigo // GG013: advances the synctest fake clock instantly; valid ONLY within synctest.Test
		synctest.Wait()
		require.Equal(t, [][]string{{"msg-1"}}, capt.snapshot(), "timer should flush a full window after the first message")
		require.True(t, c1.isAcked())

		cancel()
		require.NoError(t, <-errCh)
	})
}

// TestBatchLoop_FlushesOnSizeAndDrains proves the two non-timer flush paths: a
// full buffer flushes inline, and whatever remains is drained when the source
// returns. A huge MaxLatency keeps the ticker out of the picture.
func TestBatchLoop_FlushesOnSizeAndDrains(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newPanicSubscriber(slog.New(slog.DiscardHandler))

		data, err := proto.Marshal(&emptypb.Empty{})
		require.NoError(t, err)

		m1, c1 := newBatchMessage("msg-1", data, nil)
		m2, c2 := newBatchMessage("msg-2", data, nil)
		m3, c3 := newBatchMessage("msg-3", data, nil)
		m4, c4 := newBatchMessage("msg-4", data, nil)
		m5, c5 := newBatchMessage("msg-5", data, nil)

		// MaxLatency far in the future so only the size threshold and the final
		// drain produce batches.
		settings := BatchReceiveSettings{MaxMessages: 3, MaxBytes: 0, MaxLatency: time.Hour}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		capt := &batchCapture{} //nolint:exhaustruct // zero values are the initial state under test

		receive := func(ctx context.Context, deliver func(incomingMessage)) error {
			for _, m := range []incomingMessage{m1, m2, m3, m4, m5} {
				deliver(m)
			}
			<-ctx.Done()
			return nil
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- s.batchLoop(ctx, settings, receive, func(_ context.Context, _ []*emptypb.Empty, metas []MessageMetadata) error {
				capt.record(metas)
				return nil
			})
		}()

		// Delivering the third message hits MaxMessages and flushes inline; the
		// fourth and fifth stay buffered.
		synctest.Wait()
		require.Equal(t, [][]string{{"msg-1", "msg-2", "msg-3"}}, capt.snapshot(), "a full buffer should flush inline")
		require.True(t, c1.isAcked())
		require.True(t, c2.isAcked())
		require.True(t, c3.isAcked())

		// Cancelling ends the source; batchLoop drains the remaining buffer.
		cancel()
		require.NoError(t, <-errCh)
		require.Equal(t, [][]string{{"msg-1", "msg-2", "msg-3"}, {"msg-4", "msg-5"}}, capt.snapshot(), "remaining messages should drain when the source returns")
		require.True(t, c4.isAcked())
		require.True(t, c5.isAcked())
	})
}

// TestBatchLoop_FlushesOnBytes proves the byte-size trigger: with MaxMessages
// and MaxLatency kept out of reach, the buffer flushes precisely when the
// accumulated payload size reaches MaxBytes, and the remainder drains on
// shutdown.
func TestBatchLoop_FlushesOnBytes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newPanicSubscriber(slog.New(slog.DiscardHandler))

		// Four bytes of valid wire format that decode as an Empty (the bytes are
		// retained as unknown fields), so each message has a non-zero size for the
		// byte trigger to accumulate.
		data := []byte{0x08, 0x01, 0x10, 0x01}
		require.NoError(t, proto.Unmarshal(data, &emptypb.Empty{}))

		m1, c1 := newBatchMessage("msg-1", data, nil)
		m2, c2 := newBatchMessage("msg-2", data, nil)
		m3, c3 := newBatchMessage("msg-3", data, nil)

		// MaxBytes flushes once two 4-byte payloads (8 bytes) buffer. MaxMessages
		// and MaxLatency are kept out of reach so only the byte trigger fires.
		settings := BatchReceiveSettings{MaxMessages: 100, MaxBytes: 8, MaxLatency: time.Hour}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		capt := &batchCapture{} //nolint:exhaustruct // zero values are the initial state under test

		receive := func(ctx context.Context, deliver func(incomingMessage)) error {
			deliver(m1)
			deliver(m2)
			deliver(m3)
			<-ctx.Done()
			return nil
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- s.batchLoop(ctx, settings, receive, func(_ context.Context, _ []*emptypb.Empty, metas []MessageMetadata) error {
				capt.record(metas)
				return nil
			})
		}()

		// m1 (4 bytes) stays under the threshold; m2 brings the buffer to 8 bytes
		// and flushes the pair inline. m3 remains buffered.
		synctest.Wait()
		require.Equal(t, [][]string{{"msg-1", "msg-2"}}, capt.snapshot(), "buffer should flush once payloads reach MaxBytes")
		require.True(t, c1.isAcked())
		require.True(t, c2.isAcked())

		// Cancelling ends the source; batchLoop drains the sub-threshold remainder.
		cancel()
		require.NoError(t, <-errCh)
		require.Equal(t, [][]string{{"msg-1", "msg-2"}, {"msg-3"}}, capt.snapshot(), "remaining message should drain on shutdown")
		require.True(t, c3.isAcked())
	})
}

// TestBatchLoop_NoFlushWhenIdle confirms the latency ticker does not invoke the
// handler when nothing is buffered.
func TestBatchLoop_NoFlushWhenIdle(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newPanicSubscriber(slog.New(slog.DiscardHandler))

		settings := BatchReceiveSettings{MaxMessages: 10, MaxBytes: 0, MaxLatency: time.Second}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var calls int
		receive := func(ctx context.Context, _ func(incomingMessage)) error {
			<-ctx.Done()
			return nil
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- s.batchLoop(ctx, settings, receive, func(context.Context, []*emptypb.Empty, []MessageMetadata) error {
				calls++
				return nil
			})
		}()

		// Let several latency windows elapse with an empty buffer.
		time.Sleep(5 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly; valid ONLY within synctest.Test
		synctest.Wait()
		require.Zero(t, calls, "ticker should not invoke the handler when nothing is buffered")

		cancel()
		require.NoError(t, <-errCh)
	})
}

// TestBatchLoop_DrainsOnCancelBeforeReceiveReturns proves the cancellation drain
// runs while the delivery source is still in flight, not after it returns. The
// source mirrors Subscriber.Receive's graceful shutdown: it parks in receive
// after ctx is cancelled and only returns once the test releases it. With a
// 1h MaxLatency the timer cannot flush, so the batch can only be flushed and
// acked by the on-cancel drainer — and the test asserts that happens before the
// source is allowed to return. Draining after receive returned (the pre-fix
// behaviour) would leave the batch un-flushed here.
func TestBatchLoop_DrainsOnCancelBeforeReceiveReturns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newPanicSubscriber(slog.New(slog.DiscardHandler))

		data, err := proto.Marshal(&emptypb.Empty{})
		require.NoError(t, err)

		m1, c1 := newBatchMessage("msg-1", data, nil)
		m2, c2 := newBatchMessage("msg-2", data, nil)

		// MaxMessages and MaxLatency far out of reach so only the cancellation
		// drain can flush the buffered pair.
		settings := BatchReceiveSettings{MaxMessages: 100, MaxBytes: 0, MaxLatency: time.Hour}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		capt := &batchCapture{} //nolint:exhaustruct // zero values are the initial state under test

		releaseReceive := make(chan struct{})
		receive := func(ctx context.Context, deliver func(incomingMessage)) error {
			deliver(m1)
			deliver(m2)
			<-ctx.Done()
			// Stay inside receive until the test lets us return, the way the SDK's
			// graceful shutdown blocks on outstanding messages.
			<-releaseReceive
			return nil
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- s.batchLoop(ctx, settings, receive, func(_ context.Context, _ []*emptypb.Empty, metas []MessageMetadata) error {
				capt.record(metas)
				return nil
			})
		}()

		synctest.Wait()
		require.Empty(t, capt.snapshot(), "nothing should flush before cancel with an hour-long latency window")

		cancel()
		synctest.Wait()
		// receive is still parked on releaseReceive, yet the batch is already
		// flushed and acked: the drain ran inside the live receive lifecycle.
		require.Equal(t, [][]string{{"msg-1", "msg-2"}}, capt.snapshot(), "cancellation should drain the batch while receive is still in flight")
		require.True(t, c1.isAcked())
		require.True(t, c2.isAcked())

		close(releaseReceive)
		require.NoError(t, <-errCh)
	})
}

// TestBatchLoop_FlushesInlineWhileDraining proves messages the source keeps
// delivering during graceful shutdown are flushed inline rather than parked
// behind the latency timer. With a 1h MaxLatency, a message delivered after
// cancellation could only be acked promptly if draining forces an immediate
// flush.
func TestBatchLoop_FlushesInlineWhileDraining(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newPanicSubscriber(slog.New(slog.DiscardHandler))

		data, err := proto.Marshal(&emptypb.Empty{})
		require.NoError(t, err)

		m1, c1 := newBatchMessage("msg-1", data, nil)
		m2, c2 := newBatchMessage("msg-2", data, nil)

		settings := BatchReceiveSettings{MaxMessages: 100, MaxBytes: 0, MaxLatency: time.Hour}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		capt := &batchCapture{} //nolint:exhaustruct // zero values are the initial state under test

		releaseReceive := make(chan struct{})
		receive := func(ctx context.Context, deliver func(incomingMessage)) error {
			deliver(m1)
			<-ctx.Done()
			// The SDK scheduler can hand over an already-pulled message during
			// graceful shutdown; this one must not wait out MaxLatency.
			deliver(m2)
			<-releaseReceive
			return nil
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- s.batchLoop(ctx, settings, receive, func(_ context.Context, _ []*emptypb.Empty, metas []MessageMetadata) error {
				capt.record(metas)
				return nil
			})
		}()

		synctest.Wait()
		require.Empty(t, capt.snapshot(), "nothing should flush before cancel")

		cancel()
		synctest.Wait()
		// Both the pre-cancel buffer (m1) and the post-cancel delivery (m2) are
		// acked while receive is still parked, despite the hour-long latency window.
		require.True(t, c1.isAcked(), "buffered message should drain on cancel")
		require.True(t, c2.isAcked(), "message delivered during drain should flush inline, not wait for MaxLatency")

		var got []string
		for _, batch := range capt.snapshot() {
			got = append(got, batch...)
		}
		require.ElementsMatch(t, []string{"msg-1", "msg-2"}, got, "both messages should be flushed during shutdown")

		close(releaseReceive)
		require.NoError(t, <-errCh)
	})
}

func TestHandle_DeliveryAttemptOmittedWhenNil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))

	s := newPanicSubscriber(logger)

	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	m := incomingMessage{
		id:              "msg-nil-attempt",
		data:            nil,
		attributes:      nil,
		deliveryAttempt: nil,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}

	s.handle(t.Context(), m, func(context.Context, *emptypb.Empty, MessageMetadata) error {
		panic("kaboom")
	})

	require.True(t, cap.nacked)
	require.True(t, strings.Contains(buf.String(), "delivery_attempt=-1"), "nil delivery attempt should log as the -1 sentinel")
}

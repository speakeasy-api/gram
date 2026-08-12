package chat_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

// The turn stream is what lets the dashboard stop polling, so the properties
// worth testing are the ones the poll used to provide for free: nothing is
// lost across a reconnect, and a client never re-applies a frame it has
// already seen.

func newTurnStream(t *testing.T) *chat.TurnStream {
	t.Helper()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return chat.NewTurnStream(redisClient)
}

func textFrame(text string) chat.TurnFrame {
	return chat.TurnFrame{
		Kind: chat.TurnFrameText, Cursor: "", Text: text, MessageID: "",
		ToolCalls: nil, FinishReason: "", ToolCallID: "", Output: nil,
	}
}

func doneFrame() chat.TurnFrame {
	return chat.TurnFrame{
		Kind: chat.TurnFrameDone, Cursor: "", Text: "", MessageID: "",
		ToolCalls: nil, FinishReason: "stop", ToolCallID: "", Output: nil,
	}
}

// TestTurnStreamReplayIsExclusiveOfCursor: a reconnecting client hands back
// the last cursor it applied, so replaying from it must not repeat that frame.
// Re-applying it would duplicate rendered text.
func TestTurnStreamReplayIsExclusiveOfCursor(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	first, err := stream.Publish(ctx, chatID, textFrame("one"))
	require.NoError(t, err)
	require.NotEmpty(t, first, "publish must return a cursor")
	_, err = stream.Publish(ctx, chatID, textFrame("two"))
	require.NoError(t, err)

	all, err := stream.Replay(ctx, chatID, "")
	require.NoError(t, err)
	require.Len(t, all, 2, "an empty cursor replays the retained history")
	require.Equal(t, "one", all[0].Text)
	require.Equal(t, first, all[0].Cursor, "frames carry the cursor they were assigned")

	after, err := stream.Replay(ctx, chatID, first)
	require.NoError(t, err)
	require.Len(t, after, 1, "replay must exclude the cursor handed in")
	require.Equal(t, "two", after[0].Text)
}

// TestTurnStreamSubscribeReplaysThenTails: the reconnect path. A subscriber
// resuming from a cursor must receive what it missed before anything new, or
// frames would arrive out of order relative to the turn that produced them.
func TestTurnStreamSubscribeReplaysThenTails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	start, err := stream.Publish(ctx, chatID, textFrame("before"))
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, textFrame("missed"))
	require.NoError(t, err)

	frames, err := stream.Subscribe(ctx, chatID, start)
	require.NoError(t, err)

	// Published after the subscription exists: it must arrive behind the
	// backlog, not race ahead of it.
	_, err = stream.Publish(ctx, chatID, textFrame("live"))
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, doneFrame())
	require.NoError(t, err)

	got := collectFrames(t, frames, 3)
	require.Equal(t, "missed", got[0].Text, "the missed frame replays first")
	require.Equal(t, "live", got[1].Text)
	require.Equal(t, chat.TurnFrameDone, got[2].Kind)
}

// TestTurnStreamSubscribeWithoutCursorStartsFromNow: a chat's retained frames
// span earlier turns. Replaying them for a fresh subscription handed the
// client the previous turn's terminal frame, which ended the new turn before
// it began — the turn appeared to never stream at all.
func TestTurnStreamSubscribeWithoutCursorStartsFromNow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	// A previous, completed turn.
	_, err := stream.Publish(ctx, chatID, textFrame("old turn"))
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, doneFrame())
	require.NoError(t, err)

	frames, err := stream.Subscribe(ctx, chatID, "")
	require.NoError(t, err)

	_, err = stream.Publish(ctx, chatID, textFrame("new turn"))
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, doneFrame())
	require.NoError(t, err)

	got := collectFrames(t, frames, 2)
	require.Equal(t, "new turn", got[0].Text, "the prior turn must not be replayed")
	require.Equal(t, chat.TurnFrameDone, got[1].Kind)
}

// TestTurnStreamSubscribeStopsAtTerminalFrame: the subscription closes itself
// on `done` so the client is not left holding an open connection after the
// turn it asked about has ended.
func TestTurnStreamSubscribeStopsAtTerminalFrame(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	frames, err := stream.Subscribe(ctx, chatID, "")
	require.NoError(t, err)

	_, err = stream.Publish(ctx, chatID, doneFrame())
	require.NoError(t, err)
	// Anything after a terminal frame belongs to a later turn and must not
	// reach this subscriber.
	_, err = stream.Publish(ctx, chatID, textFrame("next turn"))
	require.NoError(t, err)

	got := collectFrames(t, frames, 1)
	require.Equal(t, chat.TurnFrameDone, got[0].Kind)

	select {
	case extra, open := <-frames:
		require.False(t, open, "channel must close after the terminal frame, got %+v", extra)
	case <-time.After(5 * time.Second):
		t.Fatal("subscription did not close after the terminal frame")
	}
}

// TestTurnStreamPublishIsInertWithoutRedis: teeing is an optimisation, so a
// deployment without Redis must degrade to the poll path rather than failing
// turns. Every publish site treats the error as non-fatal, and a nil stream
// must not panic.
func TestTurnStreamPublishIsInertWithoutRedis(t *testing.T) {
	t.Parallel()

	stream := chat.NewTurnStream(nil)
	require.Nil(t, stream)

	cursor, err := stream.Publish(t.Context(), uuid.New(), textFrame("ignored"))
	require.NoError(t, err)
	require.Empty(t, cursor)

	_, err = stream.Subscribe(t.Context(), uuid.New(), "")
	require.Error(t, err, "subscribing without a stream must fail loudly, not hang")
}

// collectFrames reads exactly n frames, failing rather than hanging if the
// stream stalls.
func collectFrames(t *testing.T, frames <-chan chat.TurnFrame, n int) []chat.TurnFrame {
	t.Helper()
	out := make([]chat.TurnFrame, 0, n)
	deadline := time.After(20 * time.Second)
	for len(out) < n {
		select {
		case frame, open := <-frames:
			if !open {
				t.Fatalf("stream closed after %d of %d frames", len(out), n)
			}
			out = append(out, frame)
		case <-deadline:
			t.Fatalf("timed out after %d of %d frames", len(out), n)
		}
	}
	return out
}

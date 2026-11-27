package chat_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// A fresh subscription opens before dispatch resets the previous turn. It
// must skip that turn's terminal frame and survive the reset.
func TestTurnStreamResetKeepsPreSubscribedReader(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	// A previous, completed turn.
	oldCursor, err := stream.Publish(ctx, chatID, textFrame("old turn"))
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, doneFrame())
	require.NoError(t, err)

	frames, err := stream.Subscribe(ctx, chatID, "")
	require.NoError(t, err)

	require.NoError(t, stream.Reset(ctx, chatID))

	_, err = stream.Publish(ctx, chatID, textFrame("new turn"))
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, doneFrame())
	require.NoError(t, err)

	got := collectFrames(t, frames, 2)
	require.Equal(t, "new turn", got[0].Text, "the prior turn must not be replayed")
	require.Equal(t, chat.TurnFrameDone, got[1].Kind)

	replayed, err := stream.Replay(ctx, chatID, "0")
	require.NoError(t, err)
	require.Equal(t, got, replayed, "only the latest turn is retained")

	_, err = stream.Subscribe(ctx, chatID, oldCursor)
	require.ErrorIs(t, err, chat.ErrTurnReplayUnavailable)
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

// TestTurnStreamPublishSetsTTLWithTheAppend: the append and the TTL are one
// step, so a stream never exists without an expiry for a failed EXPIRE to
// strand.
func TestTurnStreamPublishSetsTTLWithTheAppend(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	stream := chat.NewTurnStream(redisClient)
	chatID := uuid.New()

	_, err = stream.Publish(ctx, chatID, textFrame("hello"))
	require.NoError(t, err)

	ttl, err := redisClient.PTTL(ctx, "chat:turnframes:"+chatID.String()).Result()
	require.NoError(t, err)
	require.Positive(t, ttl, "the stream must carry a TTL from its first append")
	require.LessOrEqual(t, ttl, 30*time.Minute)
}

// TestTurnStreamPublishOmitsOversizedToolOutput: a large tool output is
// already in Postgres, so the frame carries only the call id and the marker
// that tells the client to read the row.
func TestTurnStreamPublishOmitsOversizedToolOutput(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	big, err := json.Marshal(strings.Repeat("x", 64<<10))
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, toolOutputFrame("call-big", big))
	require.NoError(t, err)
	_, err = stream.Publish(ctx, chatID, toolOutputFrame("call-small", json.RawMessage(`"ok"`)))
	require.NoError(t, err)

	frames, err := stream.Replay(ctx, chatID, "")
	require.NoError(t, err)
	require.Len(t, frames, 2)

	require.Equal(t, "call-big", frames[0].ToolCallID)
	require.True(t, frames[0].BodyOmitted)
	require.Nil(t, frames[0].Output)

	require.Equal(t, "call-small", frames[1].ToolCallID)
	require.False(t, frames[1].BodyOmitted, "an output under the cap is published whole")
	require.JSONEq(t, `"ok"`, string(frames[1].Output))
}

// TestTurnStreamPublishStripsOversizedToolCallArguments: a row whose tool
// calls are too large keeps each call's id and name, so the client can still
// render the call and pair it with its output, and loses only the arguments.
func TestTurnStreamPublishStripsOversizedToolCallArguments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	args, err := json.Marshal(map[string]string{"content": strings.Repeat("y", 64<<10)})
	require.NoError(t, err)
	calls, err := json.Marshal([]map[string]any{{
		"id":       "call-1",
		"type":     "function",
		"function": map[string]string{"name": "write_file", "arguments": string(args)},
	}})
	require.NoError(t, err)

	_, err = stream.Publish(ctx, chatID, chat.TurnFrame{
		Kind: chat.TurnFrameMessage, MessageID: "m1", ToolCalls: calls, FinishReason: "tool_calls",
	})
	require.NoError(t, err)

	frames, err := stream.Replay(ctx, chatID, "")
	require.NoError(t, err)
	require.Len(t, frames, 1)
	require.True(t, frames[0].BodyOmitted)
	require.JSONEq(t, `[{"id":"call-1","type":"function","function":{"name":"write_file"}}]`, string(frames[0].ToolCalls))
	require.Equal(t, "m1", frames[0].MessageID)
}

// TestTurnStreamPublishSplitsOversizedText: text the tee recovered in one
// piece is published as several frames that concatenate back to it, each
// valid UTF-8 on its own.
func TestTurnStreamPublishSplitsOversizedText(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	// Three-byte runes so a cut on a fixed byte offset would land mid-rune.
	text := strings.Repeat("€", 10<<10)
	cursor, err := stream.Publish(ctx, chatID, textFrame(text))
	require.NoError(t, err)

	frames, err := stream.Replay(ctx, chatID, "")
	require.NoError(t, err)
	require.Greater(t, len(frames), 1, "oversized text must be split")
	require.Equal(t, cursor, frames[len(frames)-1].Cursor, "publish returns the last frame's cursor")

	var joined strings.Builder
	for _, f := range frames {
		require.Equal(t, chat.TurnFrameText, f.Kind)
		require.True(t, utf8.ValidString(f.Text), "a split must not cut a rune")
		joined.WriteString(f.Text)
	}
	require.Equal(t, text, joined.String())
}

// TestTurnStreamReplayIsBoundedAndSubscribeContinues: a single replay read
// is bounded, and a subscriber resuming from the start still receives every
// frame because the tail read carries on from the last replayed cursor.
func TestTurnStreamReplayIsBoundedAndSubscribeContinues(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stream := newTurnStream(t)
	chatID := uuid.New()

	const published = 600
	for i := range published {
		_, err := stream.Publish(ctx, chatID, textFrame(strconv.Itoa(i)))
		require.NoError(t, err)
	}
	_, err := stream.Publish(ctx, chatID, doneFrame())
	require.NoError(t, err)

	replayed, err := stream.Replay(ctx, chatID, "0")
	require.NoError(t, err)
	require.Less(t, len(replayed), published, "one replay must not read the whole stream")
	require.NotEmpty(t, replayed)

	frames, err := stream.Subscribe(ctx, chatID, "0")
	require.NoError(t, err)
	got := collectFrames(t, frames, published+1)
	for i := range published {
		require.Equal(t, strconv.Itoa(i), got[i].Text)
	}
	require.Equal(t, chat.TurnFrameDone, got[published].Kind)
}

func toolOutputFrame(toolCallID string, output json.RawMessage) chat.TurnFrame {
	return chat.TurnFrame{
		Kind: chat.TurnFrameToolOutput, Cursor: "", Text: "", MessageID: "",
		ToolCalls: nil, FinishReason: "", ToolCallID: toolCallID, Output: output,
		BodyOmitted: false,
	}
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

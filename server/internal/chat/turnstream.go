package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Turn frames are the events a dashboard needs to render an assistant turn
// without polling: the text as it is generated, each assistant row as it is
// persisted, each tool result, and the turn's end. The client opens one
// subscription per turn and applies frames in order.
//
// A Redis *stream* rather than pub/sub, because dropping the poll removes the
// safety net: pub/sub is fire-and-forget, so a client that reconnects mid-turn
// would silently lose whatever it missed. Every frame gets a cursor, and a
// reconnecting client replays from the last cursor it applied. The stream is
// capped — frames are ephemeral, the persisted rows stay the durable record,
// and a client with no usable cursor resyncs with a single chat.load.
const (
	// turnStreamMaxLen bounds one chat's frame history. Generous next to a
	// turn's frame count (a few hundred at most, dominated by text deltas) and
	// still small enough that an abandoned chat costs little.
	turnStreamMaxLen = 2000
	// turnStreamTTL expires a chat's frames once nobody can still be watching.
	// Turn timeout is 10 minutes; this leaves room for a reconnect after one.
	turnStreamTTL = 30 * time.Minute
	// turnStreamBlock is how long a tail read parks waiting for the next
	// frame before looping, which is also how quickly it notices ctx
	// cancellation.
	turnStreamBlock = 20 * time.Second
)

// TurnFrameKind identifies what a frame carries. The client switches on it.
type TurnFrameKind string

const (
	// TurnFrameText is a fragment of assistant text, as generated.
	TurnFrameText TurnFrameKind = "text"
	// TurnFrameMessage is a persisted assistant row: its id, any tool calls it
	// made, and its finish reason.
	TurnFrameMessage TurnFrameKind = "message"
	// TurnFrameToolOutput is a persisted tool result, keyed to the call it
	// answers.
	TurnFrameToolOutput TurnFrameKind = "tool_output"
	// TurnFrameDone marks the turn terminal. Nothing follows it.
	TurnFrameDone TurnFrameKind = "done"
)

// TurnFrame is one event in a chat's turn stream. Cursor is assigned by the
// broker on publish and is what a reconnecting client replays from; it is not
// part of the published payload.
type TurnFrame struct {
	Kind TurnFrameKind `json:"kind"`
	// Cursor positions this frame in the chat's stream. Opaque to the client
	// beyond "hand it back to resume after this frame".
	Cursor string `json:"cursor,omitempty"`
	// Text is set for Kind == TurnFrameText.
	Text string `json:"text,omitempty"`
	// MessageID, ToolCalls and FinishReason are set for TurnFrameMessage.
	// ToolCalls is the raw JSON the row carries, passed through rather than
	// re-modelled so this stays in step with what chat.load returns.
	MessageID    string          `json:"message_id,omitempty"`
	ToolCalls    json.RawMessage `json:"tool_calls,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
	// ToolCallID and Output are set for TurnFrameToolOutput.
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
}

// turnStreamKey is the Redis stream holding one chat's frames.
func turnStreamKey(chatID uuid.UUID) string {
	return "chat:turnframes:" + chatID.String()
}

// TurnStream publishes and replays assistant turn frames.
type TurnStream struct {
	client *redis.Client
}

// NewTurnStream returns a stream over the supplied Redis client. A nil client
// yields a nil TurnStream whose Publish is a no-op, so a deployment without
// Redis degrades to a dashboard that shows the reply when it lands rather than
// failing turns.
func NewTurnStream(client *redis.Client) *TurnStream {
	if client == nil {
		return nil
	}
	return &TurnStream{client: client}
}

// Publish appends a frame and returns its cursor. Callers treat failures as
// non-fatal: a lost frame costs the watcher some responsiveness, and must
// never fail the turn that produced it.
func (t *TurnStream) Publish(ctx context.Context, chatID uuid.UUID, frame TurnFrame) (string, error) {
	if t == nil || chatID == uuid.Nil {
		return "", nil
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return "", fmt.Errorf("marshal turn frame: %w", err)
	}
	key := turnStreamKey(chatID)
	cursor, err := t.client.XAdd(ctx, &redis.XAddArgs{
		Stream: key,
		// Approximate trimming: exact trimming costs a scan on every append
		// and the bound is a safety valve, not a contract.
		MaxLen: turnStreamMaxLen,
		Approx: true,
		Values: map[string]any{"f": payload},
		// Redis assigns the id, the stream is created on first append, and the
		// remaining knobs (MinID/Limit/Mode, producer-side idempotency) have no
		// bearing on an append-only frame log.
		NoMkStream:     false,
		MinID:          "",
		Limit:          0,
		Mode:           "",
		ID:             "",
		ProducerID:     "",
		IdempotentID:   "",
		IdempotentAuto: false,
	}).Result()
	if err != nil {
		return "", fmt.Errorf("append turn frame: %w", err)
	}
	// Refreshed per append so an active turn keeps its history alive and an
	// abandoned chat expires on its own.
	if err := t.client.Expire(ctx, key, turnStreamTTL).Err(); err != nil {
		return cursor, fmt.Errorf("refresh turn stream ttl: %w", err)
	}
	return cursor, nil
}

// lastID returns the id of the most recent frame retained for a chat, or the
// zero id when nothing is retained yet. Reading from it is exclusive, so it
// means "everything published from now on" without the race "$" carries.
func (t *TurnStream) lastID(ctx context.Context, chatID uuid.UUID) (string, error) {
	entries, err := t.client.XRevRangeN(ctx, turnStreamKey(chatID), "+", "-", 1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", fmt.Errorf("read last turn frame id: %w", err)
	}
	if len(entries) == 0 {
		// An empty stream has no last id; "0-0" is exclusive of nothing, so
		// every frame the turn publishes still arrives.
		return "0-0", nil
	}
	return entries[0].ID, nil
}

// Replay returns frames already published after the given cursor. An empty
// cursor replays the chat's whole retained history, which is what a client
// joining a turn late (or reconnecting without a cursor) wants.
func (t *TurnStream) Replay(ctx context.Context, chatID uuid.UUID, after string) ([]TurnFrame, error) {
	if t == nil {
		return nil, errors.New("turn stream is not configured")
	}
	start := "-"
	if after != "" {
		// "(" makes the range exclusive, so a client never re-applies the
		// frame it last saw.
		start = "(" + after
	}
	entries, err := t.client.XRange(ctx, turnStreamKey(chatID), start, "+").Result()
	if err != nil {
		return nil, fmt.Errorf("replay turn frames: %w", err)
	}
	return decodeTurnEntries(entries), nil
}

// Subscribe tails a chat's frames after the given cursor until ctx is
// cancelled or a terminal frame is emitted. It replays first, so a caller that
// reconnects mid-turn sees everything it missed before anything new — the
// property pub/sub could not offer.
func (t *TurnStream) Subscribe(ctx context.Context, chatID uuid.UUID, after string) (<-chan TurnFrame, error) {
	if t == nil {
		return nil, errors.New("turn stream is not configured")
	}
	// No cursor means "start from now", not "replay everything". A chat's
	// retained frames span earlier turns, so replaying from the beginning would
	// hand a client the previous turn's terminal frame and end the new turn
	// before it began. Replay exists for reconnects, and a reconnecting client
	// always carries the cursor it got to.
	var backlog []TurnFrame
	start := after
	if after == "" {
		// Resolve the starting position HERE, not in the goroutine below.
		// Redis resolves "$" when XREAD runs, so a frame published between
		// this call returning and that first read falls in the gap and is lost
		// — the caller believes it is subscribed while the turn streams past
		// it. Pinning a concrete id makes "from now" mean "from the moment
		// Subscribe returned".
		last, err := t.lastID(ctx, chatID)
		if err != nil {
			return nil, err
		}
		start = last
	} else {
		replayed, err := t.Replay(ctx, chatID, after)
		if err != nil {
			return nil, err
		}
		backlog = replayed
	}

	out := make(chan TurnFrame, 64)
	go func() {
		defer close(out)

		cursor := start
		emit := func(frame TurnFrame) bool {
			cursor = frame.Cursor
			select {
			case out <- frame:
				return frame.Kind != TurnFrameDone
			case <-ctx.Done():
				return false
			}
		}

		for _, frame := range backlog {
			if !emit(frame) {
				return
			}
		}
		for {
			if ctx.Err() != nil {
				return
			}
			streams, err := t.client.XRead(ctx, &redis.XReadArgs{
				Streams: []string{turnStreamKey(chatID), cursor},
				Block:   turnStreamBlock,
				Count:   256,
				// The cursor is carried in Streams above.
				ID: "",
			}).Result()
			if err != nil {
				if errors.Is(err, redis.Nil) {
					// Block elapsed with no new frame. Loop so ctx
					// cancellation is noticed promptly.
					continue
				}
				return
			}
			for _, stream := range streams {
				for _, frame := range decodeTurnEntries(stream.Messages) {
					if !emit(frame) {
						return
					}
				}
			}
		}
	}()

	return out, nil
}

// decodeTurnEntries turns Redis stream entries into frames, stamping each with
// its cursor. Undecodable entries are skipped rather than failing the read: a
// frame this build cannot parse should not strand a client mid-turn.
func decodeTurnEntries(entries []redis.XMessage) []TurnFrame {
	frames := make([]TurnFrame, 0, len(entries))
	for _, entry := range entries {
		raw, ok := entry.Values["f"].(string)
		if !ok {
			continue
		}
		var frame TurnFrame
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			continue
		}
		frame.Cursor = entry.ID
		frames = append(frames, frame)
	}
	return frames
}

package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

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
	// turnStreamMaxLen bounds the frame count of a chat's replay buffer.
	// Together with turnFrameMaxBytes it bounds the buffer's bytes too.
	turnStreamMaxLen = 2000
	// turnFrameMaxBytes caps the encoded size of a single frame as stored in
	// Redis. Text that would encode larger is split across frames; a tool
	// output or tool-call arguments that would are left out and the frame is
	// marked BodyOmitted, because those bodies are already durable in
	// Postgres when the frame is published.
	//
	// Streamed text deltas are tens of bytes and an ordinary tool call's
	// arguments well under a kilobyte, so 8 KiB never touches them. It does
	// catch the large tool outputs (whole API responses, file contents) that
	// filled Redis in INC-520, and each of those costs the dashboard one
	// chat.load instead. Because the cap is on the encoded frame, a full
	// 2000-frame buffer holds at most about 16 MiB (a little more while
	// approximate trimming lags), and a typical turn's frames stay in the low
	// hundreds of KiB. chat.turn_stream.omitted_frame_bytes records what is
	// cut, so the value can be tuned.
	turnFrameMaxBytes = 8 << 10 // 8 KiB
	// turnStreamReadCount bounds the entries one read takes from a stream,
	// for both a replay and each tail read, so a reconnect never pulls an
	// unbounded range in one reply. A replay that stops at this many frames
	// is continued by the tail read from its last cursor, so nothing is lost.
	turnStreamReadCount = 256
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

	// BodyOmitted marks a tool_output frame published without its Output, or
	// a message frame whose ToolCalls keep each call's id, type and name but
	// not its arguments, or no ToolCalls at all when even that list would be
	// over turnFrameMaxBytes. Set when the frame would encode over the cap or
	// its body could not be encoded; the client reads the body from the
	// persisted row.
	BodyOmitted bool `json:"body_omitted,omitempty"`
}

// turnStreamKey is the Redis stream holding one chat's frames.
func turnStreamKey(chatID uuid.UUID) string {
	return "chat:turnframes:" + chatID.String()
}

// TurnStream publishes and replays assistant turn frames.
type TurnStream struct {
	client *redis.Client

	metrics *turnStreamMetrics
}

// NewTurnStream returns a stream over the supplied Redis client. A nil client
// yields a nil TurnStream whose Publish is a no-op, so a deployment without
// Redis degrades to a dashboard that shows the reply when it lands rather than
// failing turns.
func NewTurnStream(client *redis.Client) *TurnStream {
	if client == nil {
		return nil
	}
	return &TurnStream{client: client, metrics: loadTurnStreamMetrics()}
}

// appendTurnFrameScript appends one frame, trims the stream and refreshes its
// TTL as a single step. As separate commands a failed EXPIRE left a stream
// with no TTL at all, which nothing else would ever remove.
//
// KEYS[1] is the stream; ARGV[1] the approximate max length, ARGV[2] the
// encoded frame, ARGV[3] the TTL in milliseconds. Returns the entry id.
var appendTurnFrameScript = redis.NewScript(`
local id = redis.call('XADD', KEYS[1], 'MAXLEN', '~', ARGV[1], '*', 'f', ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return id
`)

// Reset removes the previous turn's frames while preserving the stream's ID
// sequence for subscribers already waiting for the next turn.
func (t *TurnStream) Reset(ctx context.Context, chatID uuid.UUID) error {
	if t == nil || chatID == uuid.Nil {
		return nil
	}
	if err := t.client.XTrimMaxLen(ctx, turnStreamKey(chatID), 0).Err(); err != nil {
		return fmt.Errorf("trim previous turn frames: %w", err)
	}
	return nil
}

// Publish appends a frame and returns its cursor. Callers treat failures as
// non-fatal: a lost frame costs the watcher some responsiveness, and must
// never fail the turn that produced it.
//
// A frame that would encode over turnFrameMaxBytes is bounded first: text is
// appended as several frames and the cursor returned is the last one's; a tool
// output or tool-call arguments are left out (BodyOmitted).
func (t *TurnStream) Publish(ctx context.Context, chatID uuid.UUID, frame TurnFrame) (string, error) {
	if t == nil || chatID == uuid.Nil {
		return "", nil
	}

	encoded, omittedBytes, err := encodeTurnFrame(frame)
	if omittedBytes > 0 {
		t.metrics.recordOmitted(ctx, frame.Kind, omittedBytes)
	}
	if err != nil {
		return "", err
	}

	key := turnStreamKey(chatID)
	cursor := ""
	for _, e := range encoded {
		// Approximate trimming: exact trimming costs a scan on every append
		// and the bound is a safety valve, not a contract. The TTL is
		// refreshed per append so an active turn keeps its history alive and
		// an abandoned chat expires on its own.
		id, err := appendTurnFrameScript.Run(ctx, t.client, []string{key},
			turnStreamMaxLen, e.payload, turnStreamTTL.Milliseconds(),
		).Text()
		if err != nil {
			return cursor, fmt.Errorf("append turn frame: %w", err)
		}
		cursor = id
		t.metrics.recordAppend(ctx, e.frame, len(e.payload))
	}
	return cursor, nil
}

// turnTextPieceBytes is the size text is first split at: the cap less room
// for the rest of the frame (its kind and field names), so a piece of plain
// text encodes within the cap without being split again.
const turnTextPieceBytes = turnFrameMaxBytes - 256

// encodedTurnFrame is a frame and the bytes stored for it.
type encodedTurnFrame struct {
	// frame is the frame as bounded, for the metric's attributes.
	frame TurnFrame

	// payload is the frame's encoding, what the stream entry holds.
	payload []byte
}

// encodeTurnFrame encodes frame for the stream so that no entry is over
// turnFrameMaxBytes, splitting text and leaving out tool bodies as needed. It
// also returns the encoded size of a frame whose body was left out (its raw
// body size when it would not encode), for the omitted-bytes metric.
func encodeTurnFrame(frame TurnFrame) ([]encodedTurnFrame, int, error) {
	payload, err := marshalTurnFrame(frame)
	omitted := len(payload)
	if err != nil {
		omitted = len(frame.Output) + len(frame.ToolCalls)
	}

	switch frame.Kind {
	case TurnFrameText:
		if err != nil {
			return nil, 0, err
		}
		if len(payload) <= turnFrameMaxBytes {
			return []encodedTurnFrame{{frame: frame, payload: payload}}, 0, nil
		}
		encoded, err := encodeTurnText(frame)
		return encoded, 0, err
	case TurnFrameToolOutput:
		// A nil output is a tool row whose content would not encode; it is
		// marked too, so the client reads the row rather than rendering an
		// empty result.
		if err == nil && frame.Output != nil && len(payload) <= turnFrameMaxBytes {
			return []encodedTurnFrame{{frame: frame, payload: payload}}, 0, nil
		}
		if frame.Output == nil {
			omitted = 0
		}
		frame.Output = nil
		frame.BodyOmitted = true
		encoded, err := encodeOneTurnFrame(frame)
		return encoded, omitted, err
	case TurnFrameMessage:
		if err == nil && len(payload) <= turnFrameMaxBytes {
			return []encodedTurnFrame{{frame: frame, payload: payload}}, 0, nil
		}
		frame.BodyOmitted = true
		frame.ToolCalls = toolCallSkeleton(frame.ToolCalls)
		if skeleton, err := marshalTurnFrame(frame); err == nil && len(skeleton) <= turnFrameMaxBytes {
			return []encodedTurnFrame{{frame: frame, payload: skeleton}}, omitted, nil
		}
		// Even the ids and names are over the cap (hundreds of calls in one
		// row). The client reads each call from the persisted row when its
		// output arrives.
		frame.ToolCalls = nil
		encoded, err := encodeOneTurnFrame(frame)
		return encoded, omitted, err
	default:
		// A done frame carries no body.
		if err != nil {
			return nil, 0, err
		}
		return []encodedTurnFrame{{frame: frame, payload: payload}}, 0, nil
	}
}

// encodeOneTurnFrame encodes a frame that has no body left to bound.
func encodeOneTurnFrame(frame TurnFrame) ([]encodedTurnFrame, error) {
	payload, err := marshalTurnFrame(frame)
	if err != nil {
		return nil, err
	}
	return []encodedTurnFrame{{frame: frame, payload: payload}}, nil
}

// encodeTurnText splits a text frame into frames that each encode within
// turnFrameMaxBytes. JSON escaping can grow text up to sixfold (a control
// character becomes \u00XX), so a piece that still encodes over the cap is
// halved until it fits; a piece's encoding shrinks with it, so this ends.
func encodeTurnText(frame TurnFrame) ([]encodedTurnFrame, error) {
	pending := splitTurnText(frame.Text, turnTextPieceBytes)
	out := make([]encodedTurnFrame, 0, len(pending))
	for len(pending) > 0 {
		f := frame
		f.Text = pending[0]
		pending = pending[1:]

		payload, err := marshalTurnFrame(f)
		if err != nil {
			return out, err
		}
		if len(payload) > turnFrameMaxBytes && len(f.Text) > 1 {
			pending = append(splitTurnText(f.Text, (len(f.Text)+1)/2), pending...)
			continue
		}
		out = append(out, encodedTurnFrame{frame: f, payload: payload})
	}
	return out, nil
}

// marshalTurnFrame encodes a frame without HTML escaping. Escaping <, > and &
// would grow tool output and text sixfold for nothing: the frames are read
// as JSON, never embedded in HTML.
func marshalTurnFrame(frame TurnFrame) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(frame); err != nil {
		return nil, fmt.Errorf("marshal turn frame: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// splitTurnText splits text into pieces of at most limit bytes, cutting only
// at UTF-8 rune boundaries so every piece is valid text on its own. The client
// concatenates text frames in order, so the split is invisible to it.
func splitTurnText(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	parts := make([]string, 0, len(text)/limit+1)
	for len(text) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		if cut == 0 {
			// Not UTF-8 at all; cutting on the byte bound is the best on offer.
			cut = limit
		}
		parts = append(parts, text[:cut])
		text = text[cut:]
	}
	if text != "" {
		parts = append(parts, text)
	}
	return parts
}

// turnToolCallSkeleton is a tool call reduced to what identifies it. The
// client needs the id to pair the call with its output and to find the
// persisted row, and the name to render the call while its arguments load.
type turnToolCallSkeleton struct {
	// ID is the provider's tool call id, the same id the tool row carries.
	ID string `json:"id"`

	// Type is the call type, "function" for every call the runner makes.
	Type string `json:"type,omitempty"`

	// Function holds the tool's name; the arguments are what is left out.
	Function turnToolCallSkeletonFunction `json:"function"`
}

// turnToolCallSkeletonFunction is the function part of a tool call without
// its arguments.
type turnToolCallSkeletonFunction struct {
	// Name is the tool the model called.
	Name string `json:"name"`
}

// toolCallSkeleton strips the arguments from a row's tool-call JSON. Its size
// grows with the number of calls, not with what they carry; the caller drops
// it too when even that is over the cap. JSON that is not a list of calls
// yields nil: the client still reads the row for it.
func toolCallSkeleton(raw json.RawMessage) json.RawMessage {
	var calls []turnToolCallSkeleton
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil
	}
	out, err := json.Marshal(calls)
	if err != nil {
		return nil
	}
	return out
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

// ErrTurnReplayUnavailable means the client must reload the persisted chat.
var ErrTurnReplayUnavailable = errors.New("turn replay cursor no longer retained")

// Replay returns frames already published after the given cursor, at most
// turnStreamReadCount of them per call (one fewer when resuming, since the
// cursor's own entry is read to detect a trim). An empty cursor replays from
// the start of the chat's retained history, which is what a client joining a
// turn late (or reconnecting without a cursor) wants. A caller wanting the
// rest replays again from the last frame's cursor.
func (t *TurnStream) Replay(ctx context.Context, chatID uuid.UUID, after string) ([]TurnFrame, error) {
	if t == nil {
		return nil, errors.New("turn stream is not configured")
	}
	start := "-"
	resume := after != "" && after != "0" && after != "0-0"
	if resume {
		// Include the cursor in the read to detect a reset or trim without
		// racing a separate cursor lookup against the replay.
		start = after
	}
	entries, err := t.client.XRangeN(ctx, turnStreamKey(chatID), start, "+", turnStreamReadCount).Result()
	if err != nil {
		return nil, fmt.Errorf("replay turn frames: %w", err)
	}
	if resume {
		if len(entries) == 0 || entries[0].ID != after {
			return nil, ErrTurnReplayUnavailable
		}
		entries = entries[1:]
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
	// No cursor means "start from now", not "replay everything". A client
	// can subscribe before dispatch resets the previous turn's frames, so
	// replaying that turn's terminal frame would end the new turn early.
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
				Count:   turnStreamReadCount,
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

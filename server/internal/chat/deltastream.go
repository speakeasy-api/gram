package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// The assistant runner calls /chat/completions without `stream`, because its
// agent loop wants one assembled message back. The dashboard, on the other
// side of that turn, wants tokens as they are produced: nothing renders there
// until a whole message lands, so a user watches a spinner for the full
// generation even though the first token was ready in a fraction of the time.
//
// The proxy sits between the two, so it can serve both: request the stream
// upstream, publish each text delta for whoever is watching this chat, and
// still hand the runner the assembled JSON it expects. The runner is
// unchanged, and a chat with no watcher pays only the publish call.
//
// Deltas are fan-out over Redis rather than process memory because the
// runner's completion request and the dashboard's SSE connection routinely
// land on different replicas.

// deltaChannel is the Redis pub/sub channel carrying one chat's text deltas.
func deltaChannel(chatID uuid.UUID) string {
	return "chat:deltas:" + chatID.String()
}

// DeltaEvent is one frame on a chat's delta channel. A frame either carries
// text or marks the end of a message; Done frames let a subscriber finish
// promptly instead of waiting out a poll.
type DeltaEvent struct {
	Text string `json:"text,omitempty"`
	Done bool   `json:"done,omitempty"`
}

// DeltaBroker fans assistant text deltas out to dashboard subscribers.
type DeltaBroker struct {
	client *redis.Client
}

// NewDeltaBroker returns a broker over the supplied Redis client. A nil client
// yields a nil broker, and every method on a nil broker is a no-op — teeing is
// an optimisation, so a deployment without Redis degrades to the poll path
// rather than failing turns.
func NewDeltaBroker(client *redis.Client) *DeltaBroker {
	if client == nil {
		return nil
	}
	return &DeltaBroker{client: client}
}

// Publish emits one frame on the chat's channel. Errors are returned for the
// caller to log; they must never fail the turn.
func (b *DeltaBroker) Publish(ctx context.Context, chatID uuid.UUID, event DeltaEvent) error {
	if b == nil || chatID == uuid.Nil {
		return nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal delta event: %w", err)
	}
	if err := b.client.Publish(ctx, deltaChannel(chatID), payload).Err(); err != nil {
		return fmt.Errorf("publish delta: %w", err)
	}
	return nil
}

// Subscribe streams a chat's frames until ctx is cancelled. The returned
// cancel func must be called to release the Redis subscription.
func (b *DeltaBroker) Subscribe(ctx context.Context, chatID uuid.UUID) (<-chan DeltaEvent, func(), error) {
	if b == nil {
		return nil, func() {}, errors.New("delta broker is not configured")
	}
	sub := b.client.Subscribe(ctx, deltaChannel(chatID))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, func() {}, fmt.Errorf("subscribe to chat deltas: %w", err)
	}

	out := make(chan DeltaEvent, 64)
	go func() {
		defer close(out)
		for msg := range sub.Channel() {
			var event DeltaEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, func() { _ = sub.Close() }, nil
}

// HandleDeltaStream serves a chat's live assistant deltas as SSE. The
// dashboard opens it alongside a send and renders text as it arrives; the
// existing poll still runs and remains the source of truth for the persisted
// message, so a dropped or unavailable stream costs responsiveness, never
// correctness.
//
// Frames are `data: {"text":"..."}` with a final `data: {"done":true}`.
func (s *Service) HandleDeltaStream(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeProjectRead,
		ResourceKind: "",
		ResourceID:   authCtx.ProjectID.String(),
		Dimensions:   nil,
	}); err != nil {
		return err
	}
	if s.deltaBroker == nil {
		return oops.E(oops.CodeInvalid, nil, "assistant delta streaming is not enabled")
	}

	chatID, err := uuid.Parse(r.URL.Query().Get("chat_id"))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid chat_id")
	}
	// Subscribing to a chat is subscribing to its content, so the caller must
	// own it. GetChat is not project-scoped, so the check is made here.
	chat, err := s.repo.GetChat(ctx, chatID)
	if err != nil || chat.ProjectID != *authCtx.ProjectID {
		return oops.E(oops.CodeNotFound, err, "chat not found")
	}

	events, cancel, err := s.deltaBroker.Subscribe(ctx, chatID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "subscribe to assistant deltas").LogError(ctx, s.logger)
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		flusher.Flush()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-events:
			if !open {
				return nil
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				// The reader hung up; nothing to recover, and the turn itself
				// is unaffected.
				return nil
			}
			if canFlush {
				flusher.Flush()
			}
			if event.Done {
				return nil
			}
		}
	}
}

// assembledCompletion is what a teed stream reconstructs: the same shape the
// non-streaming path returns, rebuilt from the chunks as they went past.
type assembledCompletion struct {
	MessageID    string
	Model        string
	Content      string
	ToolCalls    []openrouter.ToolCall
	FinishReason *string
	Usage        openrouter.Usage
}

// Message renders the assembled completion as the assistant message the
// runner's non-streaming response carries.
func (a assembledCompletion) Message() or.ChatMessages {
	content := or.CreateChatAssistantMessageContentStr(a.Content)
	msg := or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	}
	if len(a.ToolCalls) > 0 {
		calls := make([]or.ChatToolCall, 0, len(a.ToolCalls))
		for _, tc := range a.ToolCalls {
			calls = append(calls, or.ChatToolCall{
				ID:   tc.ID,
				Type: or.ChatToolCallType(tc.Type),
				Function: or.ChatToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		msg.ToolCalls = calls
	}
	return or.CreateChatMessagesAssistant(msg)
}

// teeCompletionStream drains an upstream SSE completion, publishing each text
// delta through onDelta as it arrives and returning the assembled result.
//
// The reader handed in is the openrouter streaming reader, which accumulates
// its own copy for capture and telemetry as the bytes pass through — that is
// why the whole body must be read even though the caller wants the assembled
// value rather than the stream. Parsing here is deliberately independent of
// that internal accumulation: this function owns what the caller gets back.
func teeCompletionStream(src io.Reader, onDelta func(string)) (assembledCompletion, error) {
	out := assembledCompletion{
		MessageID:    "",
		Model:        "",
		Content:      "",
		ToolCalls:    nil,
		FinishReason: nil,
		Usage:        openrouter.Usage{}, //nolint:exhaustruct // zero usage until a chunk carries it
	}
	var content strings.Builder
	toolCalls := map[int]openrouter.ToolCall{}

	scanner := bufio.NewScanner(src)
	// Tool-call argument fragments can make a single SSE line long; the default
	// 64KiB token limit is not enough for a large structured call.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk openrouter.StreamingChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// A malformed chunk is upstream's problem, not a reason to fail the
			// turn: the openrouter reader logs it, and skipping keeps the rest
			// of the message intact.
			continue
		}
		if chunk.ID != "" {
			out.MessageID = chunk.ID
		}
		if chunk.Model != "" {
			out.Model = chunk.Model
		}
		if chunk.Usage != nil {
			out.Usage = *chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			content.WriteString(choice.Delta.Content)
			if onDelta != nil {
				onDelta(choice.Delta.Content)
			}
		}
		for _, tc := range choice.Delta.ToolCalls {
			existing := toolCalls[tc.Index]
			existing.Index = tc.Index
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Type != "" {
				existing.Type = tc.Type
			}
			if tc.Function.Name != "" {
				existing.Function.Name = tc.Function.Name
			}
			// Arguments arrive as fragments and must be concatenated in order.
			existing.Function.Arguments += tc.Function.Arguments
			toolCalls[tc.Index] = existing
		}
		if choice.FinishReason != nil {
			out.FinishReason = choice.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("read completion stream: %w", err)
	}

	out.Content = content.String()
	if len(toolCalls) > 0 {
		// Map iteration is unordered; the runner replays tool calls in index
		// order, so sort rather than emit them however the map ranges.
		indexes := make([]int, 0, len(toolCalls))
		for idx := range toolCalls {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		out.ToolCalls = make([]openrouter.ToolCall, 0, len(indexes))
		for _, idx := range indexes {
			out.ToolCalls = append(out.ToolCalls, toolCalls[idx])
		}
	}
	return out, nil
}

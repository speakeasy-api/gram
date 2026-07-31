package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

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

// teedCompletion runs a completion as an upstream stream, republishing each
// text fragment as a turn frame as it arrives, and returns the assembled
// result in the shape a non-streaming caller expects.
//
// Draining the reader to EOF and closing it is what drives the openrouter
// client's own capture and telemetry, so both must happen even though the
// bytes are discarded here.
func (s *Service) teedCompletion(ctx context.Context, req openrouter.CompletionRequest, chatID uuid.UUID) (*openrouter.CompletionResponse, error) {
	streamBody, err := s.completionClient.GetCompletionStream(ctx, req)
	if err != nil {
		return nil, s.classifyCompletionError(ctx, "get completion stream", err)
	}
	defer o11y.NoLogDefer(func() error { return streamBody.Close() })

	// Publishing rides the request context: if the caller goes away the turn is
	// abandoned anyway, and a detached publish would outlive its only reader.
	publish := func(text string) {
		if _, pErr := s.turnStream.Publish(ctx, chatID, TurnFrame{
			Kind: TurnFrameText, Cursor: "", Text: text, MessageID: "",
			ToolCalls: nil, FinishReason: "", ToolCallID: "", Output: nil,
		}); pErr != nil {
			s.logger.WarnContext(ctx, "publish turn text frame", attr.SlogError(pErr))
		}
	}

	assembled, err := teeCompletionStream(streamBody, publish)
	if err != nil {
		return nil, s.classifyCompletionError(ctx, "completion failed", err)
	}

	message := assembled.Message()
	return &openrouter.CompletionResponse{
		StartTime:    time.Now(),
		Message:      &message,
		MessageID:    assembled.MessageID,
		Model:        assembled.Model,
		Usage:        assembled.Usage,
		FinishReason: assembled.FinishReason,
		ToolCalls:    assembled.ToolCalls,
		Content:      assembled.Content,
	}, nil
}

// teeStreamText returns a writer that republishes assistant text as turn
// frames, and a func to close it. Callers tee the upstream SSE body into the
// writer so the passthrough response is unaffected: parsing happens on a
// separate goroutine and a failure there can never corrupt or stall the bytes
// the caller is forwarding.
func (s *Service) teeStreamText(ctx context.Context, chatID uuid.UUID) (io.Writer, func()) {
	pr, pw := io.Pipe()
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		publish := func(text string) {
			if _, err := s.turnStream.Publish(ctx, chatID, TurnFrame{
				Kind: TurnFrameText, Cursor: "", Text: text, MessageID: "",
				ToolCalls: nil, FinishReason: "", ToolCallID: "", Output: nil,
			}); err != nil {
				s.logger.WarnContext(ctx, "publish turn text frame", attr.SlogError(err))
			}
		}
		if _, err := teeCompletionStream(pr, publish); err != nil {
			s.logger.WarnContext(ctx, "parse streamed completion for turn frames", attr.SlogError(err))
		}
		// Drain whatever is left so a tee write never blocks on a reader that
		// stopped early.
		_, _ = io.Copy(io.Discard, pr)
	}()

	return pw, func() {
		_ = pw.Close()
		<-finished
	}
}

package stokens

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/tiktoken-go/tokenizer"
)

// Codec counts the model-relevant values in structured GenAI input messages.
type Codec struct {
	codec tokenizer.Codec
}

// NewCodec creates a Codec using the o200k_base tokenizer.
func NewCodec() *Codec {
	codec, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		panic(fmt.Errorf("get %s: %w", tokenizer.O200kBase, err))
	}
	return &Codec{codec: codec}
}

// Name returns the tokenizer name recorded alongside token counts.
func (c *Codec) Name() string {
	return c.codec.GetName()
}

// Count counts arbitrary text content.
func (c *Codec) Count(ctx context.Context, content ...string) (int, error) {
	count := 0

	for _, part := range content {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("count aborted: %w", err)
		}

		c, err := c.codec.Count(part)
		if err != nil {
			return 0, fmt.Errorf("codec count: %w", err)
		}
		count += c
	}

	return count, nil
}

// CountInput counts assistant text, user prompts, tool names, tool inputs, and
// tool outputs. Other message metadata and part variants do not contribute.
func (c *Codec) CountInput(ctx context.Context, messages genaiconv.InputMessages) (int, error) {
	count := 0
	for _, message := range messages {
		messageCount, err := c.countMessage(ctx, message.Role, message.Parts)
		if err != nil {
			return 0, fmt.Errorf("count input messages: %w", err)
		}
		count += messageCount
	}
	return count, nil
}

// CountOutput counts assistant text, user prompts, tool names, tool inputs, and
// tool outputs. Other message metadata and part variants do not contribute.
func (c *Codec) CountOutput(ctx context.Context, messages genaiconv.OutputMessages) (int, error) {
	count := 0
	for _, message := range messages {
		messageCount, err := c.countMessage(ctx, message.Role, message.Parts)
		if err != nil {
			return 0, fmt.Errorf("count output messages: %w", err)
		}
		count += messageCount
	}
	return count, nil
}

func (c *Codec) countMessage(ctx context.Context, role genaiconv.Role, parts []genaiconv.Part) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("count message: %w", err)
	}

	count := 0
	countText := role == genaiconv.RoleAssistant || role == genaiconv.RoleUser
	for _, part := range parts {
		partCount, err := c.countPart(ctx, countText, part)
		if err != nil {
			return 0, err
		}
		count += partCount
	}
	return count, nil
}

func (c *Codec) countPart(ctx context.Context, countText bool, part genaiconv.Part) (int, error) {
	switch part := part.(type) {
	case genaiconv.TextPart:
		if countText {
			return c.countValue(ctx, part.Content)
		}
	case *genaiconv.TextPart:
		if countText {
			return c.countValue(ctx, part.Content)
		}
	case genaiconv.ToolCallRequestPart:
		return c.countToolCall(ctx, part.Name, part.Arguments)
	case *genaiconv.ToolCallRequestPart:
		return c.countToolCall(ctx, part.Name, part.Arguments)
	case genaiconv.ToolCallResponsePart:
		return c.countValue(ctx, part.Response)
	case *genaiconv.ToolCallResponsePart:
		return c.countValue(ctx, part.Response)
	case genaiconv.ServerToolCallPart:
		return c.countToolCall(ctx, part.Name, part.ServerToolCall)
	case *genaiconv.ServerToolCallPart:
		return c.countToolCall(ctx, part.Name, part.ServerToolCall)
	case genaiconv.ServerToolCallResponsePart:
		return c.countValue(ctx, part.ServerToolCallResponse)
	case *genaiconv.ServerToolCallResponsePart:
		return c.countValue(ctx, part.ServerToolCallResponse)
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("count message part: %w", err)
	}
	return 0, nil
}

func (c *Codec) countToolCall(ctx context.Context, name string, input any) (int, error) {
	nameCount, err := c.countValue(ctx, name)
	if err != nil {
		return 0, err
	}
	if input == nil {
		return nameCount, nil
	}

	inputCount, err := c.countValue(ctx, input)
	if err != nil {
		return 0, err
	}
	return nameCount + inputCount, nil
}

func (c *Codec) countValue(ctx context.Context, value any) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("count token value: %w", err)
	}

	text, ok := value.(string)
	if !ok {
		encoded, err := json.Marshal(value)
		if err != nil {
			return 0, fmt.Errorf("marshal token value: %w", err)
		}
		text = string(encoded)
	}

	count, err := c.codec.Count(text)
	if err != nil {
		return 0, fmt.Errorf("count tokens: %w", err)
	}
	return count, nil
}

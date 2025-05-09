package chat

// OpenAIChatMessage represents a message in the OpenAI chat API
type OpenAIChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// OpenAIChatRequest represents the request structure for OpenAI chat completions
type OpenAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []OpenAIChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

// ToolCallFunction represents the function part of a tool call
type ToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolCall represents an individual tool call in a response
type ToolCall struct {
	Index    int              `json:"index,omitempty"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function,omitempty"`
}

// ChunkDelta represents the delta content in a streaming response chunk
type ChunkDelta struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ChunkChoice represents a choice in a streaming response chunk
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// StreamingChunk represents a streaming response chunk from OpenAI
type StreamingChunk struct {
	ID                string        `json:"id"`
	Object            string        `json:"object"`
	Created           int64         `json:"created"`
	Model             string        `json:"model"`
	SystemFingerprint string        `json:"system_fingerprint"`
	Choices           []ChunkChoice `json:"choices"`
}

// OpenAIChatResponse represents the response structure from OpenAI for non-streaming responses
type OpenAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message      OpenAIChatMessage `json:"message"`
		FinishReason string            `json:"finish_reason"`
	} `json:"choices"`
}

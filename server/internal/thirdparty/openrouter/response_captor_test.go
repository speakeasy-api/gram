package openrouter

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestResponseCaptor_LineBuffering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		chunks         []string // Simulates multiple Write calls with arbitrary chunk boundaries
		expectedResult string   // Expected accumulated message content
	}{
		{
			name: "single complete chunk",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n",
			},
			expectedResult: "Hello",
		},
		{
			name: "content split across chunks",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":{\"content\":\"Hel",
				"lo\"}}]}\ndata: {\"choices\":[{\"delta\":{\"content\":\" World\"}}]}\n",
			},
			expectedResult: "Hello World",
		},
		{
			name: "multiple lines in single chunk",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\ndata: {\"choices\":[{\"delta\":{\"content\":\" World\"}}]}\n",
			},
			expectedResult: "Hello World",
		},
		{
			name: "chunk boundary in middle of JSON",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":{\"con",
				"tent\":\"Test\"}}]}\n",
			},
			expectedResult: "Test",
		},
		{
			name: "empty content chunks",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n",
				"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n",
			},
			expectedResult: "Hello",
		},
		{
			name: "chunk ends exactly at newline",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":{\"content\":\"First\"}}]}\n",
				"data: {\"choices\":[{\"delta\":{\"content\":\"Second\"}}]}\n",
			},
			expectedResult: "FirstSecond",
		},
		{
			name: "very small chunks simulating worst case network fragmentation",
			chunks: []string{
				"dat",
				"a: {\"ch",
				"oices\":[{\"de",
				"lta\":{\"content\":\"H",
				"i\"}}]}\n",
			},
			expectedResult: "Hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := &streamingResponseReader{
				ctx:                  context.Background(),
				logger:               testenv.NewLogger(t),
				messageContent:       &strings.Builder{},
				lineBuf:              &strings.Builder{},
				accumulatedToolCalls: make(map[int]ToolCall),
				usage:                Usage{},
			}

			// Simulate multiple parseSSEChunks calls with the test chunks
			for _, chunk := range tt.chunks {
				reader.parseSSEChunks([]byte(chunk))
			}

			// Verify the accumulated message content
			require.Equal(t, tt.expectedResult, reader.messageContent.String())
		})
	}
}

func TestResponseCaptor_ToolCallAccumulation(t *testing.T) {
	t.Parallel()

	// Simulate tool call chunks split across multiple Write calls
	chunks := []string{
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_123\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"lo\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"cation\\\":\\\"NYC\\\"}\"}}]}}]}\n",
	}

	reader := &streamingResponseReader{
		ctx:                  context.Background(),
		logger:               testenv.NewLogger(t),
		lineBuf:              &strings.Builder{},
		messageContent:       &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
		usage:                Usage{},
	}

	for _, chunk := range chunks {
		reader.parseSSEChunks([]byte(chunk))
	}

	// Verify tool call was accumulated correctly
	require.Len(t, reader.accumulatedToolCalls, 1)
	toolCall := reader.accumulatedToolCalls[0]
	require.Equal(t, "call_123", toolCall.ID)
	require.Equal(t, "get_weather", toolCall.Function.Name)
	require.JSONEq(t, `{"location":"NYC"}`, toolCall.Function.Arguments)
}

func TestResponseCaptor_MultipleToolCalls(t *testing.T) {
	t.Parallel()

	// Simulate multiple tool calls in the same response
	chunks := []string{
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"location\\\":\\\"NYC\\\"}\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_2\",\"type\":\"function\",\"function\":{\"name\":\"get_time\",\"arguments\":\"\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"arguments\":\"{\\\"timezone\\\":\\\"EST\\\"}\"}}]}}]}\n",
	}

	reader := &streamingResponseReader{
		ctx:                  context.Background(),
		logger:               testenv.NewLogger(t),
		lineBuf:              &strings.Builder{},
		messageContent:       &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
		usage:                Usage{},
	}

	for _, chunk := range chunks {
		reader.parseSSEChunks([]byte(chunk))
	}

	// Verify both tool calls were accumulated correctly
	require.Len(t, reader.accumulatedToolCalls, 2)

	toolCall0 := reader.accumulatedToolCalls[0]
	require.Equal(t, "call_1", toolCall0.ID)
	require.Equal(t, "get_weather", toolCall0.Function.Name)
	require.JSONEq(t, `{"location":"NYC"}`, toolCall0.Function.Arguments)

	toolCall1 := reader.accumulatedToolCalls[1]
	require.Equal(t, "call_2", toolCall1.ID)
	require.Equal(t, "get_time", toolCall1.Function.Name)
	require.JSONEq(t, `{"timezone":"EST"}`, toolCall1.Function.Arguments)
}

func TestResponseCaptor_UsageTracking(t *testing.T) {
	t.Parallel()

	chunks := []string{
		"data: {\"id\":\"msg_123\",\"model\":\"openai/gpt-5.4\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n",
		"data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n",
		"data: [DONE]\n",
	}

	reader := &streamingResponseReader{
		ctx:                  context.Background(),
		logger:               testenv.NewLogger(t),
		lineBuf:              &strings.Builder{},
		messageContent:       &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
		usage:                Usage{},
	}

	for _, chunk := range chunks {
		reader.parseSSEChunks([]byte(chunk))
	}

	// Verify metadata was captured
	require.Equal(t, "msg_123", reader.messageID)
	require.Equal(t, "openai/gpt-5.4", reader.model)
	require.Equal(t, "Hello", reader.messageContent.String())
	require.NotNil(t, reader.finishReason)
	require.Equal(t, "stop", *reader.finishReason)
	require.True(t, reader.usageSet)
	require.Equal(t, 10, reader.usage.PromptTokens)
	require.Equal(t, 5, reader.usage.CompletionTokens)
	require.Equal(t, 15, reader.usage.TotalTokens)
	require.True(t, reader.isDone)
}

func TestResponseCaptor_MixedContentAndToolCalls(t *testing.T) {
	t.Parallel()

	chunks := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Let me check the weather. \"}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_123\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"location\\\":\\\"NYC\\\"}\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"The weather is sunny.\"}}]}\n",
	}

	reader := &streamingResponseReader{
		ctx:                  context.Background(),
		logger:               testenv.NewLogger(t),
		lineBuf:              &strings.Builder{},
		messageContent:       &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
		usage:                Usage{},
	}

	for _, chunk := range chunks {
		reader.parseSSEChunks([]byte(chunk))
	}

	// Verify both content and tool calls were captured
	require.Equal(t, "Let me check the weather. The weather is sunny.", reader.messageContent.String())
	require.Len(t, reader.accumulatedToolCalls, 1)
	toolCall := reader.accumulatedToolCalls[0]
	require.Equal(t, "call_123", toolCall.ID)
	require.Equal(t, "get_weather", toolCall.Function.Name)
}

func TestResponseCaptor_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		chunks         []string
		expectedResult string
	}{
		{
			name: "empty lines and whitespace",
			chunks: []string{
				"\n",
				"  \n",
				"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n",
				"\n",
			},
			expectedResult: "Hello",
		},
		{
			name: "comments and non-data lines",
			chunks: []string{
				": comment line\n",
				"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n",
				": another comment\n",
			},
			expectedResult: "Hello",
		},
		{
			name: "DONE marker",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n",
				"data: [DONE]\n",
			},
			expectedResult: "Done",
		},
		{
			name: "empty delta objects",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":{}}]}\n",
				"data: {\"choices\":[{\"delta\":{\"content\":\"Text\"}}]}\n",
				"data: {\"choices\":[{\"delta\":{}}]}\n",
			},
			expectedResult: "Text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := &streamingResponseReader{
				ctx:                  context.Background(),
				logger:               testenv.NewLogger(t),
				lineBuf:              &strings.Builder{},
				messageContent:       &strings.Builder{},
				accumulatedToolCalls: make(map[int]ToolCall),
				usage:                Usage{},
			}

			for _, chunk := range tt.chunks {
				reader.parseSSEChunks([]byte(chunk))
			}

			require.Equal(t, tt.expectedResult, reader.messageContent.String())
		})
	}
}

func TestResponseCaptor_ToolCallFieldUpdates(t *testing.T) {
	t.Parallel()

	// Test that subsequent chunks properly update tool call fields without overwriting
	chunks := []string{
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\"}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\"}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"test_func\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"arg1\"}}]}}]}\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"arg2\"}}]}}]}\n",
	}

	reader := &streamingResponseReader{
		ctx:                  context.Background(),
		logger:               testenv.NewLogger(t),
		lineBuf:              &strings.Builder{},
		messageContent:       &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
		usage:                Usage{},
	}

	for _, chunk := range chunks {
		reader.parseSSEChunks([]byte(chunk))
	}

	require.Len(t, reader.accumulatedToolCalls, 1)
	toolCall := reader.accumulatedToolCalls[0]
	require.Equal(t, "call_1", toolCall.ID)
	require.Equal(t, "function", toolCall.Type)
	require.Equal(t, "test_func", toolCall.Function.Name)
	require.Equal(t, "arg1arg2", toolCall.Function.Arguments, "Arguments should be concatenated")
}

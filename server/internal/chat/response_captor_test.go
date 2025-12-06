package chat

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/stretchr/testify/require"
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

			recorder := httptest.NewRecorder()
			captor := &responseCaptor{
				ResponseWriter:       recorder,
				logger:               slog.Default(),
				isStreaming:          true,
				orgID:                "test-org",
				chatID:               uuid.New(),
				projectID:            uuid.New(),
				messageContent:       &strings.Builder{},
				lineBuf:              &strings.Builder{},
				accumulatedToolCalls: make(map[int]openrouter.ToolCall),
				usage:                openrouter.Usage{},
			}

			// Simulate multiple Write calls with the test chunks
			for _, chunk := range tt.chunks {
				_, err := captor.Write([]byte(chunk))
				require.NoError(t, err)
			}

			// Verify the accumulated message content
			require.Equal(t, tt.expectedResult, captor.messageContent.String())
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

	recorder := httptest.NewRecorder()
	captor := &responseCaptor{
		ResponseWriter:       recorder,
		logger:               slog.Default(),
		isStreaming:          true,
		orgID:                "test-org",
		chatID:               uuid.New(),
		projectID:            uuid.New(),
		messageContent:       &strings.Builder{},
		lineBuf:              &strings.Builder{},
		accumulatedToolCalls: make(map[int]openrouter.ToolCall),
		usage:                openrouter.Usage{},
	}

	for _, chunk := range chunks {
		_, err := captor.Write([]byte(chunk))
		require.NoError(t, err)
	}

	// Verify tool call was accumulated correctly
	require.Len(t, captor.accumulatedToolCalls, 1)
	toolCall := captor.accumulatedToolCalls[0]
	require.Equal(t, "call_123", toolCall.ID)
	require.Equal(t, "get_weather", toolCall.Function.Name)
	require.JSONEq(t, `{"location":"NYC"}`, toolCall.Function.Arguments)
}

package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTeeCompletionStreamAssemblesTextAndDeltas: the teed path must hand the
// caller exactly what the non-streaming path would have, while emitting each
// text fragment as it goes.
func TestTeeCompletionStreamAssemblesTextAndDeltas(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"id":"gen-1","model":"anthropic/claude-haiku-4.5","provider":"Amazon Bedrock","choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":", world"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"

	var deltas []string
	got, err := teeCompletionStream(strings.NewReader(body), func(s string) { deltas = append(deltas, s) })
	require.NoError(t, err)

	require.Equal(t, []string{"Hello", ", world"}, deltas, "each content fragment must be published as it arrives")
	require.Equal(t, "Hello, world", got.Content)
	require.Equal(t, "gen-1", got.MessageID)
	require.Equal(t, "anthropic/claude-haiku-4.5", got.Model)
	require.Equal(t, "Amazon Bedrock", got.Provider)
	require.NotNil(t, got.FinishReason)
	require.Equal(t, "stop", *got.FinishReason)
	require.Equal(t, 11, got.Usage.PromptTokens)
	require.Equal(t, 3, got.Usage.CompletionTokens)
}

// TestTeeCompletionStreamOrdersToolCallsByIndex: tool-call fragments arrive
// interleaved and keyed by index. They must be concatenated per index and
// emitted in index order — the runner replays them positionally, so map
// iteration order would corrupt a multi-call turn.
func TestTeeCompletionStreamOrdersToolCallsByIndex(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"id":"gen-2","choices":[{"delta":{"tool_calls":[{"index":1,"id":"call-b","type":"function","function":{"name":"second","arguments":"{\"b\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-a","type":"function","function":{"name":"first","arguments":"{\"a\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"2}"}}]}}]}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"

	got, err := teeCompletionStream(strings.NewReader(body), nil)
	require.NoError(t, err)

	require.Len(t, got.ToolCalls, 2)
	require.Equal(t, "call-a", got.ToolCalls[0].ID)
	require.Equal(t, "first", got.ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"a":1}`, got.ToolCalls[0].Function.Arguments)
	require.Equal(t, "call-b", got.ToolCalls[1].ID)
	require.JSONEq(t, `{"b":2}`, got.ToolCalls[1].Function.Arguments)
}

// TestTeeCompletionStreamSkipsMalformedChunks: a bad chunk is upstream's
// problem. Dropping it keeps the rest of the message rather than failing a
// turn that is otherwise fine.
func TestTeeCompletionStreamSkipsMalformedChunks(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"id":"gen-3","choices":[{"delta":{"content":"before"}}]}`,
		`data: {not json at all`,
		`: a comment line`,
		`data: {"choices":[{"delta":{"content":"-after"}}]}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"

	got, err := teeCompletionStream(strings.NewReader(body), nil)
	require.NoError(t, err)
	require.Equal(t, "before-after", got.Content)
}

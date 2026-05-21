package openrouter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequest_PreservesCacheControl(t *testing.T) {
	t.Parallel()

	inbound := []byte(`{
		"model": "anthropic/claude-4.6-sonnet",
		"stream": false,
		"cache_control": {"type": "ephemeral"},
		"messages": [
			{
				"role": "system",
				"content": [
					{"type": "text", "text": "system prompt", "cache_control": {"type": "ephemeral"}}
				]
			},
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "hello"}
				]
			}
		],
		"tools": [
			{
				"type": "function",
				"function": {"name": "do_thing", "parameters": {"type": "object"}},
				"cache_control": {"type": "ephemeral"}
			}
		]
	}`)

	var req OpenAIChatRequest
	require.NoError(t, json.Unmarshal(inbound, &req))

	out, err := json.Marshal(req)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))

	require.Equal(t, "ephemeral", path(got, "cache_control", "type"),
		"top-level cache_control must survive: %s", out)
	require.Equal(t, "ephemeral", path(got, "tools", 0, "cache_control", "type"),
		"per-tool cache_control must survive: %s", out)
	require.Equal(t, "ephemeral", path(got, "messages", 0, "content", 0, "cache_control", "type"),
		"per-content-block cache_control must survive: %s", out)
}

func path(root any, keys ...any) any {
	cur := root
	for _, k := range keys {
		switch v := cur.(type) {
		case map[string]any:
			s, ok := k.(string)
			if !ok {
				return nil
			}
			cur = v[s]
		case []any:
			i, ok := k.(int)
			if !ok || i < 0 || i >= len(v) {
				return nil
			}
			cur = v[i]
		default:
			return nil
		}
	}
	return cur
}

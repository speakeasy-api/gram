package audit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeToolCallParams_RedactsSecretShapedKeys(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"channel": "general",
		"api_token": "tok-123",
		"Password": "hunter2",
		"nested": {"authorization": "Bearer abc", "ok": "keep"},
		"items": [{"client_secret": "s3cret", "name": "fine"}]
	}`)

	out, truncated := sanitizeToolCallParams(raw)
	require.False(t, truncated)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))

	require.Equal(t, "general", decoded["channel"])
	require.Equal(t, "[REDACTED]", decoded["api_token"])
	require.Equal(t, "[REDACTED]", decoded["Password"])

	nested, ok := decoded["nested"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "[REDACTED]", nested["authorization"])
	require.Equal(t, "keep", nested["ok"])

	items, ok := decoded["items"].([]any)
	require.True(t, ok)
	item, ok := items[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "[REDACTED]", item["client_secret"])
	require.Equal(t, "fine", item["name"])
}

func TestSanitizeToolCallParams_TruncatesLargePayloads(t *testing.T) {
	t.Parallel()

	big := map[string]string{"body": strings.Repeat("a", 3*maxAuditToolCallParamsBytes)}
	raw, err := json.Marshal(big)
	require.NoError(t, err)

	out, truncated := sanitizeToolCallParams(raw)
	require.True(t, truncated)
	require.True(t, json.Valid(out), "truncated payload must remain valid JSON")

	var asString string
	require.NoError(t, json.Unmarshal(out, &asString))
	require.Len(t, asString, maxAuditToolCallParamsBytes)
}

func TestSanitizeToolCallParams_EmptyAndInvalidInputs(t *testing.T) {
	t.Parallel()

	out, truncated := sanitizeToolCallParams(nil)
	require.Nil(t, out)
	require.False(t, truncated)

	out, truncated = sanitizeToolCallParams(json.RawMessage(`not json {{{`))
	require.False(t, truncated)
	require.True(t, json.Valid(out), "invalid input must be preserved as a JSON string")

	var asString string
	require.NoError(t, json.Unmarshal(out, &asString))
	require.Equal(t, "not json {{{", asString)
}

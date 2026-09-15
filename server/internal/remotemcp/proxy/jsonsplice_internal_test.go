package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpliceTopLevelKey_ReplacesKeyPreservingOtherMembers(t *testing.T) {
	t.Parallel()

	original := json.RawMessage(`{
		"_meta": {"upstream/trace": 9007199254740993},
		"resultType": "tools/list",
		"ttlMs": 60000,
		"cacheScope": "server",
		"tools": [{"name": "tool_a"}]
	}`)

	out, err := spliceTopLevelKey(original, "tools", json.RawMessage(`[{"name":"kept"}]`))
	require.NoError(t, err)

	require.Contains(t, string(out), `"tools":[{"name":"kept"}]`, "target member must carry the replacement value")
	require.Contains(t, string(out), `"resultType":"tools/list"`, "unmodeled member must survive the splice")
	require.Contains(t, string(out), `"ttlMs":60000`, "unmodeled member must survive the splice")
	require.Contains(t, string(out), `"cacheScope":"server"`, "unmodeled member must survive the splice")
	require.Contains(t, string(out), `"_meta":{"upstream/trace":9007199254740993}`,
		"_meta must relay with its original bytes — no float64 precision loss from a typed round-trip")
	require.NotContains(t, string(out), "tool_a", "replaced member must not retain its original value")
}

func TestSpliceTopLevelKey_AppendsMissingKey(t *testing.T) {
	t.Parallel()

	out, err := spliceTopLevelKey(json.RawMessage(`{"resultType":"tools/list"}`), "tools", json.RawMessage(`[]`))
	require.NoError(t, err)
	require.Contains(t, string(out), `"tools":[]`, "absent target member must be appended")
	require.Contains(t, string(out), `"resultType":"tools/list"`)
}

func TestSpliceTopLevelKey_EmptyValueDeletesKey(t *testing.T) {
	t.Parallel()

	out, err := spliceTopLevelKey(json.RawMessage(`{"name":"w","arguments":{"a":1}}`), "arguments", nil)
	require.NoError(t, err)
	require.NotContains(t, string(out), `"arguments"`, "nil value must delete the member")
	require.Contains(t, string(out), `"name":"w"`)

	// A non-nil but zero-length RawMessage deletes too — len, not nilness,
	// mirrors the omitempty encoding this splice replaces.
	out, err = spliceTopLevelKey(json.RawMessage(`{"name":"w","arguments":{"a":1}}`), "arguments", json.RawMessage{})
	require.NoError(t, err)
	require.NotContains(t, string(out), `"arguments"`, "empty value must delete the member")
}

func TestSpliceTopLevelKey_NullObjectTreatedAsEmpty(t *testing.T) {
	t.Parallel()

	out, err := spliceTopLevelKey(json.RawMessage(`null`), "tools", json.RawMessage(`[]`))
	require.NoError(t, err)
	require.JSONEq(t, `{"tools":[]}`, string(out), "a literal null payload must splice into a fresh object, not panic")
}

func TestSpliceTopLevelKey_DuplicateKeysCollapseLastWins(t *testing.T) {
	t.Parallel()

	// A duplicated non-target member collapses to its last occurrence, the
	// same way encoding/json decodes it — a filter setter must never leave
	// an earlier duplicate behind for last-key-wins clients to read.
	original := json.RawMessage(`{"ttlMs":1,"ttlMs":2,"tools":[{"name":"secret"}],"tools":[{"name":"visible"}]}`)
	out, err := spliceTopLevelKey(original, "tools", json.RawMessage(`[]`))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(out), `"tools"`), "duplicate target keys must collapse to one member")
	require.Contains(t, string(out), `"ttlMs":2`, "duplicate preserved keys must collapse last-wins")
	require.NotContains(t, string(out), "secret", "no duplicate may retain a pre-mutation value")
	require.NotContains(t, string(out), "visible", "no duplicate may retain a pre-mutation value")
}

func TestSpliceTopLevelKey_NonObjectPayloadErrors(t *testing.T) {
	t.Parallel()

	_, err := spliceTopLevelKey(json.RawMessage(`[1,2]`), "tools", json.RawMessage(`[]`))
	require.Error(t, err, "a JSON array payload is not a splice target")

	_, err = spliceTopLevelKey(json.RawMessage(`"str"`), "tools", json.RawMessage(`[]`))
	require.Error(t, err, "a JSON string payload is not a splice target")
}

func TestSpliceTopLevelKey_InvalidValueErrors(t *testing.T) {
	t.Parallel()

	// The final marshal validates the caller-supplied value; malformed
	// bytes must error out rather than reach the wire.
	_, err := spliceTopLevelKey(json.RawMessage(`{"name":"w"}`), "arguments", json.RawMessage{0xff})
	require.Error(t, err)
}

func TestSpliceTopLevelKey_DoesNotHTMLEscape(t *testing.T) {
	t.Parallel()

	// The SDK's envelope encoder writes with HTML escaping off; the splice
	// must match so preserved and replacement bytes relay unrewritten.
	out, err := spliceTopLevelKey(
		json.RawMessage(`{"kept":"a<b&c"}`),
		"arguments",
		json.RawMessage(`{"q":"<&>"}`),
	)
	require.NoError(t, err)
	require.Contains(t, string(out), `"kept":"a<b&c"`, "preserved member must not be HTML-escaped")
	require.Contains(t, string(out), `"q":"<&>"`, "replacement value must not be HTML-escaped")
}

package mcprequests_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

func TestClampMethod_KnownMethodsPassThrough(t *testing.T) {
	t.Parallel()

	for _, method := range []string{
		"initialize",
		"ping",
		"tools/call",
		"tools/list",
		"prompts/get",
		"prompts/list",
		"resources/list",
		"resources/read",
		"notifications/initialized",
		"notifications/cancelled",
		"server/discover",
		"subscriptions/listen",
		"completion/complete",
		"logging/setLevel",
		"resources/templates/list",
		"notifications/roots/list_changed",
		"tasks/get",
		"tasks/result",
		"tasks/list",
		"tasks/cancel",
		"notifications/tasks/status",
	} {
		require.Equal(t, method, mcprequests.ClampMethod(method))
	}
}

// TestClampMethod_UnknownMethodsClampToOther pins that everything outside the
// known list — unlisted extension methods, yet-unknown spec additions, and
// hostile garbage — collapses into the single "other" bucket.
func TestClampMethod_UnknownMethodsClampToOther(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcprequests.MethodOther, mcprequests.ClampMethod("tasks/create"))
	require.Equal(t, mcprequests.MethodOther, mcprequests.ClampMethod("tools/brand-new"))
	require.Equal(t, mcprequests.MethodOther, mcprequests.ClampMethod("rpc.discover"))
	require.Equal(t, mcprequests.MethodOther, mcprequests.ClampMethod("evil/../../etc"))
	require.Equal(t, mcprequests.MethodOther, mcprequests.ClampMethod(strings.Repeat("x", 4096)))
}

func TestClampMethod_EmptyClampsToNone(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcprequests.MethodNone, mcprequests.ClampMethod(""))
}

func TestParseMeta_Version20260728RequestMetadata(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{
		"name": "get_weather",
		"arguments": {"location": "Seattle, WA"},
		"_meta": {
			"io.modelcontextprotocol/protocolVersion": "2026-07-28",
			"io.modelcontextprotocol/clientInfo": {"name": "ExampleClient", "version": "1.0.0"},
			"io.modelcontextprotocol/clientCapabilities": {"roots": {}, "elicitation": {}, "extensions": {"io.modelcontextprotocol/tasks": {}}}
		}
	}`)

	meta := mcprequests.ParseMeta(params)
	require.Equal(t, mcpversions.Version20260728, meta.ProtocolVersion)
	require.NotNil(t, meta.ClientInfo)
	require.Equal(t, "ExampleClient", meta.ClientInfo.Name)
	require.Equal(t, "1.0.0", meta.ClientInfo.Version)
	require.Equal(t, []string{"elicitation", "extensions", "roots"}, meta.CapabilityKeys)
}

// TestParseMeta_InitializeHandshakeBodyIsNotRead pins the semantic split: the
// top-level initialize fields every handshake revision (2024-11-05 through
// 2025-11-25) carries are requested/handshake values owned by the initialize
// handlers, so the per-request decode must not surface them.
func TestParseMeta_InitializeHandshakeBodyIsNotRead(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{
		"protocolVersion": "2025-03-26",
		"clientInfo": {"name": "handshake-client", "version": "9.9.9"},
		"capabilities": {"roots": {}}
	}`)

	meta := mcprequests.ParseMeta(params)
	require.Empty(t, meta.ProtocolVersion)
	require.Nil(t, meta.ClientInfo)
	require.Nil(t, meta.CapabilityKeys)
}

// TestParseMeta_Pre20260728RequestWithoutMetaIsZero covers the negative path
// shared by every revision before 2026-07-28 (2024-11-05 through 2025-11-25):
// tools/call params carry no protocol `_meta` at all, so the decode must
// report nothing rather than inventing values.
func TestParseMeta_Pre20260728RequestWithoutMetaIsZero(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{"name": "get_weather", "arguments": {"location": "Seattle, WA"}}`)

	meta := mcprequests.ParseMeta(params)
	require.Empty(t, meta.ProtocolVersion)
	require.Nil(t, meta.ClientInfo)
	require.Nil(t, meta.CapabilityKeys)
}

func TestParseMeta_EmptyAndMalformedParams(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcprequests.SanitizedMeta{ProtocolVersion: "", ClientInfo: nil, CapabilityKeys: nil}, mcprequests.ParseMeta(nil))
	require.Equal(t, mcprequests.SanitizedMeta{ProtocolVersion: "", ClientInfo: nil, CapabilityKeys: nil}, mcprequests.ParseMeta(json.RawMessage(`{invalid`)))
	require.Equal(t, mcprequests.SanitizedMeta{ProtocolVersion: "", ClientInfo: nil, CapabilityKeys: nil}, mcprequests.ParseMeta(json.RawMessage(`{"_meta": "not-an-object"}`)))
}

func TestParseMeta_SanitizesClientSuppliedFields(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{
		"_meta": {
			"io.modelcontextprotocol/protocolVersion": "2026-07-28\u0000injected",
			"io.modelcontextprotocol/clientInfo": {"name": "ev\u0000il\nclient", "version": "1.\t0"}
		}
	}`)

	meta := mcprequests.ParseMeta(params)
	require.Empty(t, meta.ProtocolVersion, "a version carrying control bytes is dropped, not cleaned")
	require.NotNil(t, meta.ClientInfo)
	require.Equal(t, "evilclient", meta.ClientInfo.Name)
	require.Equal(t, "1.0", meta.ClientInfo.Version)
}

// TestParseMeta_BoundsCapabilityKeys pins the abuse cap: under 2026-07-28 the
// capabilities map arrives on every request, so a hostile client must not be
// able to inflate the retained key list or its member lengths.
func TestParseMeta_BoundsCapabilityKeys(t *testing.T) {
	t.Parallel()

	capabilities := map[string]any{}
	for r := 'a'; r < 'a'+30; r++ {
		capabilities[strings.Repeat(string(r), 300)] = map[string]any{}
	}
	params, err := json.Marshal(map[string]any{
		"_meta": map[string]any{
			"io.modelcontextprotocol/clientCapabilities": capabilities,
		},
	})
	require.NoError(t, err)

	meta := mcprequests.ParseMeta(params)
	require.Len(t, meta.CapabilityKeys, 20)
	for _, key := range meta.CapabilityKeys {
		require.LessOrEqual(t, len(key), mcprequests.MaxClientInfoFieldLength)
	}
}

func TestSanitizeClientInfoField_DropsControlAndCaps(t *testing.T) {
	t.Parallel()

	require.Equal(t, "evilclient", mcprequests.SanitizeClientInfoField("ev\x00il\nclient"))
	require.Len(t, mcprequests.SanitizeClientInfoField(strings.Repeat("a", 500)), mcprequests.MaxClientInfoFieldLength)
	require.Empty(t, mcprequests.SanitizeClientInfoField(""))
}

// TestSanitizeClientInfoField_TrimsWhitespace pins that surrounding
// whitespace collapses equivalent spellings — " roots", "roots ", and "roots"
// must all sanitize to the same value, or they would survive capability-key
// dedup as three near-identical entries.
func TestSanitizeClientInfoField_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	require.Equal(t, "roots", mcprequests.SanitizeClientInfoField(" roots"))
	require.Equal(t, "roots", mcprequests.SanitizeClientInfoField("roots "))
	require.Equal(t, "roots", mcprequests.SanitizeClientInfoField("\x00 roots \x00"), "trim must apply after control characters are dropped")
	require.Empty(t, mcprequests.SanitizeClientInfoField("   "))
}

// TestParseMeta_CapabilityKeysCollapseWhitespaceVariants pins the dedup
// consequence of trimming: whitespace variants of one capability name yield a
// single retained key.
func TestParseMeta_CapabilityKeysCollapseWhitespaceVariants(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{
		"_meta": {
			"io.modelcontextprotocol/clientCapabilities": {
				" roots": {},
				"roots ": {},
				"roots": {}
			}
		}
	}`)

	meta := mcprequests.ParseMeta(params)
	require.Equal(t, []string{"roots"}, meta.CapabilityKeys)
}

func TestWireMetaSanitize_NilYieldsZero(t *testing.T) {
	t.Parallel()

	var w *mcprequests.WireMeta
	require.Equal(t, mcprequests.SanitizedMeta{ProtocolVersion: "", ClientInfo: nil, CapabilityKeys: nil}, w.Sanitize())
}

// TestWireMeta_SinglePassDecodeMatchesParseMeta pins that a handler embedding
// WireMeta in its own params struct (the tools/call pattern, avoiding a
// second scan of large arguments) observes exactly what ParseMeta reports for
// the same document — the two decode paths must never drift.
func TestWireMeta_SinglePassDecodeMatchesParseMeta(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{
		"name": "get_weather",
		"arguments": {"location": "Seattle, WA"},
		"_meta": {
			"io.modelcontextprotocol/protocolVersion": "2026-07-28",
			"io.modelcontextprotocol/clientInfo": {"name": "ExampleClient", "version": "1.0.0"},
			"io.modelcontextprotocol/clientCapabilities": {"roots": {}, "elicitation": {}}
		}
	}`)

	var embedded struct {
		Name string                `json:"name"`
		Meta *mcprequests.WireMeta `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(params, &embedded))

	require.Equal(t, mcprequests.ParseMeta(params), embedded.Meta.Sanitize())
}

// TestWireMeta_MistypedMetaNeverFailsTheHostDecode pins the tolerant-decode
// contract at the embed site: `_meta` is observability metadata, so a `_meta`
// of the wrong type must not fail the strict params unmarshal of a handler
// that embeds WireMeta (the tools/call pattern) — the RPC's own fields still
// decode and the metadata comes back zero.
func TestWireMeta_MistypedMetaNeverFailsTheHostDecode(t *testing.T) {
	t.Parallel()

	var embedded struct {
		Name string                `json:"name"`
		Meta *mcprequests.WireMeta `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(json.RawMessage(`{"name": "get_weather", "_meta": 123}`), &embedded))
	require.Equal(t, "get_weather", embedded.Name)
	require.Equal(t, mcprequests.SanitizedMeta{ProtocolVersion: "", ClientInfo: nil, CapabilityKeys: nil}, embedded.Meta.Sanitize())
}

// TestWireMeta_MistypedMemberIsDroppedIndependently pins that one mis-typed
// `_meta` member zeroes only itself: the version survives a non-object
// clientInfo, and a mis-typed member inside clientInfo drops that field alone.
func TestWireMeta_MistypedMemberIsDroppedIndependently(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{
		"_meta": {
			"io.modelcontextprotocol/protocolVersion": "2026-07-28",
			"io.modelcontextprotocol/clientInfo": {"name": "ExampleClient", "version": 5},
			"io.modelcontextprotocol/clientCapabilities": "not-an-object"
		}
	}`)

	meta := mcprequests.ParseMeta(params)
	require.Equal(t, mcpversions.Version20260728, meta.ProtocolVersion)
	require.NotNil(t, meta.ClientInfo)
	require.Equal(t, "ExampleClient", meta.ClientInfo.Name)
	require.Empty(t, meta.ClientInfo.Version)
	require.Nil(t, meta.CapabilityKeys)
}

// TestParseMeta_CapabilityKeysSortedAndUniqueAfterSanitization pins the
// sorted-and-unique contract under adversarial input: sanitization can
// reorder keys (control bytes sort before printable characters and are then
// dropped) and can collapse distinct raw keys into the same sanitized value,
// so the sort and dedupe must run on sanitized keys, not raw ones.
func TestParseMeta_CapabilityKeysSortedAndUniqueAfterSanitization(t *testing.T) {
	t.Parallel()

	// Raw keys: "a\u0001z" sorts before "ab" but sanitizes to "az", and
	// "ro\u0000ots" sanitizes to a duplicate of "roots".
	params := json.RawMessage(`{
		"_meta": {
			"io.modelcontextprotocol/clientCapabilities": {
				"a\u0001z": {},
				"ab": {},
				"ro\u0000ots": {},
				"roots": {}
			}
		}
	}`)

	meta := mcprequests.ParseMeta(params)
	require.Equal(t, []string{"ab", "az", "roots"}, meta.CapabilityKeys)
}

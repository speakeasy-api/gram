package assistants

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRuntimeContent_MarshalTextAsBareString(t *testing.T) {
	t.Parallel()

	out, err := json.Marshal(runtimeTextContent("hello"))
	require.NoError(t, err)
	require.Equal(t, `"hello"`, string(out))
}

func TestRuntimeContent_MessageOmitsEmptyContent(t *testing.T) {
	t.Parallel()

	out, err := json.Marshal(runtimeMessage{
		Role:       "assistant",
		Content:    runtimeTextContent(""),
		ToolCalls:  []runtimeToolCall{{ID: "c1", Name: "f", Arguments: "{}"}},
		ToolCallID: "",
	})
	require.NoError(t, err)
	require.NotContains(t, string(out), `"content"`)
}

func TestRuntimeContent_UnmarshalBareString(t *testing.T) {
	t.Parallel()

	var c runtimeContent
	require.NoError(t, json.Unmarshal([]byte(`"plain text"`), &c))
	require.Equal(t, runtimeTextContent("plain text"), c)
	require.Equal(t, "plain text", c.Text())
}

func TestRuntimeContent_UnmarshalNull(t *testing.T) {
	t.Parallel()

	var c runtimeContent
	require.NoError(t, json.Unmarshal([]byte(`null`), &c))
	require.True(t, c.IsZero())
}

func TestRuntimeContent_PartsRoundTrip(t *testing.T) {
	t.Parallel()

	in := runtimeContent{
		Str: "",
		Parts: []runtimeContentPart{
			{Type: "text", Text: "look at this", ImageURL: nil},
			{Type: "image_url", Text: "", ImageURL: &runtimeImageURL{URL: "https://example.com/a.png", Detail: "high"}},
		},
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, `[{"type":"text","text":"look at this"},{"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"high"}}]`, string(raw))

	var out runtimeContent
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, in, out)
	require.Equal(t, "look at this", out.Text())
	require.True(t, out.supportedParts())
}

func TestRuntimeContent_TextJoinsTextParts(t *testing.T) {
	t.Parallel()

	c := runtimeContent{
		Str: "",
		Parts: []runtimeContentPart{
			{Type: "text", Text: "one", ImageURL: nil},
			{Type: "image_url", Text: "", ImageURL: &runtimeImageURL{URL: "https://example.com/a.png", Detail: ""}},
			{Type: "text", Text: "two", ImageURL: nil},
		},
	}
	require.Equal(t, "one\ntwo", c.Text())
}

func TestRuntimeContent_UnsupportedPartTypeDetected(t *testing.T) {
	t.Parallel()

	var c runtimeContent
	require.NoError(t, json.Unmarshal([]byte(`[{"type":"input_audio","input_audio":{"data":"x","format":"wav"}}]`), &c))
	require.False(t, c.supportedParts())
}

func TestRuntimeContent_ImagePartMissingURLDetected(t *testing.T) {
	t.Parallel()

	var c runtimeContent
	require.NoError(t, json.Unmarshal([]byte(`[{"type":"image_url","image_url":{}}]`), &c))
	require.False(t, c.supportedParts())
}

func historyContentCore(t *testing.T) *ServiceCore {
	t.Helper()
	logger := testenv.NewLogger(t)
	return NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger), nil)
}

func historyContentRow(content string, contentRaw []byte) chatrepo.ChatMessage {
	var row chatrepo.ChatMessage
	row.Content = content
	row.ContentRaw = contentRaw
	return row
}

func TestLoadHistoryMessageContent_PrefersStructuredContentRaw(t *testing.T) {
	t.Parallel()

	core := historyContentCore(t)
	row := historyContentRow("projected text", []byte(`[{"type":"text","text":"structured text"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]`))

	content := core.loadHistoryMessageContent(t.Context(), row)
	require.Len(t, content.Parts, 2)
	require.Equal(t, "structured text", content.Text())
}

func TestLoadHistoryMessageContent_BareStringContentRaw(t *testing.T) {
	t.Parallel()

	core := historyContentCore(t)
	row := historyContentRow("same text", []byte(`"same text"`))

	content := core.loadHistoryMessageContent(t.Context(), row)
	require.Equal(t, runtimeTextContent("same text"), content)
}

func TestLoadHistoryMessageContent_FallsBackWithoutStructuredContent(t *testing.T) {
	t.Parallel()

	core := historyContentCore(t)
	row := historyContentRow("plain only", nil)

	content := core.loadHistoryMessageContent(t.Context(), row)
	require.Equal(t, runtimeTextContent("plain only"), content)
}

func TestLoadHistoryMessageContent_FallsBackOnUnsupportedParts(t *testing.T) {
	t.Parallel()

	core := historyContentCore(t)
	row := historyContentRow("audio transcript", []byte(`[{"type":"input_audio","input_audio":{"data":"x","format":"wav"}}]`))

	content := core.loadHistoryMessageContent(t.Context(), row)
	require.Equal(t, runtimeTextContent("audio transcript"), content)
}

func TestLoadHistoryMessageContent_FallsBackOnMalformedJSON(t *testing.T) {
	t.Parallel()

	core := historyContentCore(t)
	row := historyContentRow("fallback", []byte(`{not json`))

	content := core.loadHistoryMessageContent(t.Context(), row)
	require.Equal(t, runtimeTextContent("fallback"), content)
}

func TestRuntimeMessage_JSONRoundTripThroughRunnerShape(t *testing.T) {
	t.Parallel()

	// A message that decoded from a bare string must re-encode as a bare
	// string so pre-parts runners keep parsing bootstrap history.
	var msg runtimeMessage
	require.NoError(t, json.Unmarshal([]byte(`{"role":"user","content":"hi"}`), &msg))
	out, err := json.Marshal(msg)
	require.NoError(t, err)
	require.JSONEq(t, `{"role":"user","content":"hi"}`, string(out))
}

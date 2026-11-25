package mcpriskscan_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/stretchr/testify/require"
)

func TestParseToolResultPayloadScansTextAndStructuredContent(t *testing.T) {
	t.Parallel()
	payload, err := mcpriskscan.ParseToolResultPayload([]byte(`{
		"content":[
			{"type":"text","text":"person@example.com"},
			{"type":"image","mimeType":"image/png","data":"c2VjcmV0"},
			{"type":"resource","resource":{"uri":"gram://notes","text":"+1 555 0100"}},
			{"type":"resource","resource":{"uri":"gram://photo","blob":"c2VjcmV0"}}
		],
		"structuredContent":{"attendee":"second@example.com"}
	}`))
	require.NoError(t, err)
	require.Equal(t, mcpriskscan.PayloadAvailable, payload.Availability())
	require.Equal(t, "person@example.com\ngram://notes\n+1 555 0100\n{\"attendee\":\"second@example.com\"}", string(payload.Bytes()))
	require.NotContains(t, string(payload.Bytes()), "c2VjcmV0")
}

func TestToolResultPayloadPreservesDuplicateTextBlocks(t *testing.T) {
	t.Parallel()
	payload, err := mcpriskscan.ToolResultPayload([]json.RawMessage{
		json.RawMessage(`{"type":"text","text":"repeated"}`),
		json.RawMessage(`{"type":"text","text":"repeated"}`),
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "repeated\nrepeated", string(payload.Bytes()))
}

func TestJSONRPCErrorPayloadScansMessageAndData(t *testing.T) {
	t.Parallel()
	payload := mcpriskscan.JSONRPCErrorPayload(&jsonrpc.Error{
		Code:    -32000,
		Message: "contact person@example.com",
		Data:    []byte(`{"phone":"+1 555 0100"}`),
	})
	require.Equal(t, mcpriskscan.PayloadAvailable, payload.Availability())
	require.Equal(t, "contact person@example.com\n{\"phone\":\"+1 555 0100\"}", string(payload.Bytes()))
}

func TestJSONRPCErrorPayloadWithoutErrorIsUnavailable(t *testing.T) {
	t.Parallel()

	payload := mcpriskscan.JSONRPCErrorPayload(nil)

	require.Equal(t, mcpriskscan.PayloadUnavailable, payload.Availability())
	require.Nil(t, payload.Bytes())
}

func TestParseResourceResultPayloadSkipsBinaryContent(t *testing.T) {
	t.Parallel()
	payload, err := mcpriskscan.ParseResourceResultPayload([]byte(`{
		"contents":[
			{"uri":"gram://notes","text":"person@example.com"},
			{"uri":"gram://photo","blob":"c2VjcmV0"}
		]
	}`))
	require.NoError(t, err)
	require.Equal(t, "person@example.com", string(payload.Bytes()))
}

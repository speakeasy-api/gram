package genaiconv

import (
	"encoding/json"
	"os"
	"testing"

	gramjsonschema "github.com/speakeasy-api/gram/server/internal/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestInputMessagesStructsConformToVendoredSchema(t *testing.T) {
	t.Parallel()

	messages := InputMessages{
		{
			Role: RoleUser,
			Parts: []Part{
				TextPart{Type: PartTypeText, Content: "Plan a route"},
				ToolCallRequestPart{Type: PartTypeToolCall, ID: new("call-1"), Name: "lookup_route", Arguments: map[string]any{"origin": "A", "destination": "B"}},
				ToolCallResponsePart{Type: PartTypeToolCallResponse, ID: new("call-1"), Response: map[string]any{"distance_km": 4.2}},
				ServerToolCallPart{Type: PartTypeServerToolCall, ID: new("server-call-1"), Name: "web_search", ServerToolCall: GenericServerToolCall{"type": "web_search", "query": "route conditions"}},
				ServerToolCallResponsePart{Type: PartTypeServerToolCallResponse, ID: new("server-call-1"), ServerToolCallResponse: GenericServerToolCallResponse{"type": "web_search_result", "result": "clear"}},
				BlobPart{Type: PartTypeBlob, MIMEType: new("image/png"), Modality: ModalityImage, Content: "aW1hZ2U="},
				FilePart{Type: PartTypeFile, MIMEType: new("application/pdf"), Modality: ModalityDocument, FileID: "file-1"},
				URIPart{Type: PartTypeURI, MIMEType: new("audio/mpeg"), Modality: ModalityAudio, URI: "https://example.invalid/audio.mp3"},
				ReasoningPart{Type: PartTypeReasoning, Content: "Compare the routes"},
				CompactionPart{Type: PartTypeCompaction, ID: new("compact-1"), Content: new("Earlier route discussion")},
				GenericPart{"type": "provider_extension", "value": true},
			},
			Name: new("planner"),
		},
	}

	encoded, err := json.Marshal(messages)
	require.NoError(t, err)

	var instance any
	require.NoError(t, json.Unmarshal(encoded, &instance))

	schemaBytes, err := os.ReadFile("gen-ai-input-messages.json")
	require.NoError(t, err)
	compiled, err := gramjsonschema.CompileSchema(schemaBytes)
	require.NoError(t, err)
	require.NoError(t, gramjsonschema.ValidateAgainstSchema(compiled, instance))
}

func TestInputMessagesUnmarshalSelectsPartStructs(t *testing.T) {
	t.Parallel()

	const encoded = `[
		{
			"role": "assistant",
			"parts": [
				{"type": "text", "content": "I will check"},
				{"type": "tool_call", "id": "call-1", "name": "lookup", "arguments": {"query": "status"}},
				{"type": "tool_call_response", "id": "call-1", "response": {"status": "ready"}},
				{"type": "server_tool_call", "name": "web_search", "server_tool_call": {"type": "web_search", "query": "status"}},
				{"type": "server_tool_call_response", "server_tool_call_response": {"type": "web_search_result", "result": "ready"}},
				{"type": "blob", "modality": "image", "content": "aW1hZ2U="},
				{"type": "file", "modality": "document", "file_id": "file-1"},
				{"type": "uri", "modality": "audio", "uri": "https://example.invalid/audio.mp3"},
				{"type": "reasoning", "content": "Check the status"},
				{"type": "compaction", "content": "Earlier context"},
				{"type": "provider_extension", "value": true}
			]
		}
	]`

	var messages InputMessages
	require.NoError(t, json.Unmarshal([]byte(encoded), &messages))
	require.Len(t, messages, 1)
	require.Equal(t, RoleAssistant, messages[0].Role)
	require.IsType(t, &TextPart{}, messages[0].Parts[0])
	require.IsType(t, &ToolCallRequestPart{}, messages[0].Parts[1])
	require.IsType(t, &ToolCallResponsePart{}, messages[0].Parts[2])
	require.IsType(t, &ServerToolCallPart{}, messages[0].Parts[3])
	require.IsType(t, &ServerToolCallResponsePart{}, messages[0].Parts[4])
	require.IsType(t, &BlobPart{}, messages[0].Parts[5])
	require.IsType(t, &FilePart{}, messages[0].Parts[6])
	require.IsType(t, &URIPart{}, messages[0].Parts[7])
	require.IsType(t, &ReasoningPart{}, messages[0].Parts[8])
	require.IsType(t, &CompactionPart{}, messages[0].Parts[9])
	require.IsType(t, GenericPart{}, messages[0].Parts[10])

	roundTrip, err := json.Marshal(messages)
	require.NoError(t, err)
	require.JSONEq(t, encoded, string(roundTrip))
}

func TestOutputMessagesStructsConformToVendoredSchema(t *testing.T) {
	t.Parallel()

	messages := OutputMessages{
		{
			Role: RoleAssistant,
			Parts: []Part{
				TextPart{Type: PartTypeText, Content: "The route is clear"},
				ToolCallRequestPart{Type: PartTypeToolCall, ID: new("call-1"), Name: "lookup_route", Arguments: map[string]any{"origin": "A", "destination": "B"}},
				ToolCallResponsePart{Type: PartTypeToolCallResponse, ID: new("call-1"), Response: map[string]any{"distance_km": 4.2}},
				ServerToolCallPart{Type: PartTypeServerToolCall, ID: new("server-call-1"), Name: "web_search", ServerToolCall: GenericServerToolCall{"type": "web_search", "query": "route conditions"}},
				ServerToolCallResponsePart{Type: PartTypeServerToolCallResponse, ID: new("server-call-1"), ServerToolCallResponse: GenericServerToolCallResponse{"type": "web_search_result", "result": "clear"}},
				BlobPart{Type: PartTypeBlob, MIMEType: new("image/png"), Modality: ModalityImage, Content: "aW1hZ2U="},
				FilePart{Type: PartTypeFile, MIMEType: new("application/pdf"), Modality: ModalityDocument, FileID: "file-1"},
				URIPart{Type: PartTypeURI, MIMEType: new("audio/mpeg"), Modality: ModalityAudio, URI: "https://example.invalid/audio.mp3"},
				ReasoningPart{Type: PartTypeReasoning, Content: "Compare the routes"},
				CompactionPart{Type: PartTypeCompaction, ID: new("compact-1"), Content: new("Earlier route discussion")},
				GenericPart{"type": "provider_extension", "value": true},
			},
			Name:         new("planner"),
			FinishReason: FinishReasonStop,
		},
	}

	encoded, err := json.Marshal(messages)
	require.NoError(t, err)

	var instance any
	require.NoError(t, json.Unmarshal(encoded, &instance))

	schemaBytes, err := os.ReadFile("gen-ai-output-messages.json")
	require.NoError(t, err)
	compiled, err := gramjsonschema.CompileSchema(schemaBytes)
	require.NoError(t, err)
	require.NoError(t, gramjsonschema.ValidateAgainstSchema(compiled, instance))
}

func TestOutputMessagesUnmarshalSelectsPartStructs(t *testing.T) {
	t.Parallel()

	const encoded = `[
		{
			"role": "assistant",
			"parts": [
				{"type": "text", "content": "I will check"},
				{"type": "tool_call", "id": "call-1", "name": "lookup", "arguments": {"query": "status"}},
				{"type": "provider_extension", "value": true}
			],
			"name": "planner",
			"finish_reason": "tool_call"
		}
	]`

	var messages OutputMessages
	require.NoError(t, json.Unmarshal([]byte(encoded), &messages))
	require.Len(t, messages, 1)
	require.Equal(t, RoleAssistant, messages[0].Role)
	require.Equal(t, new("planner"), messages[0].Name)
	require.Equal(t, FinishReasonToolCall, messages[0].FinishReason)
	require.IsType(t, &TextPart{}, messages[0].Parts[0])
	require.IsType(t, &ToolCallRequestPart{}, messages[0].Parts[1])
	require.IsType(t, GenericPart{}, messages[0].Parts[2])

	roundTrip, err := json.Marshal(messages)
	require.NoError(t, err)
	require.JSONEq(t, encoded, string(roundTrip))
}

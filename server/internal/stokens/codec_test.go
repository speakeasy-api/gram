package stokens

import (
	"context"
	"testing"

	genai "github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func TestCodecCountsProjectRelevantMessageValues(t *testing.T) {
	t.Parallel()

	messages := genai.InputMessages{
		{
			Role: genai.RoleUser,
			Parts: []genai.Part{
				genai.TextPart{Type: genai.PartTypeText, Content: "Plan a route"},
			},
			Name: nil,
		},
		{
			Role: genai.RoleAssistant,
			Parts: []genai.Part{
				genai.TextPart{Type: genai.PartTypeText, Content: "I will look it up"},
				genai.ToolCallRequestPart{Type: genai.PartTypeToolCall, ID: new("call-1"), Name: "lookup_route", Arguments: map[string]any{"origin": "A", "destination": "B"}},
				genai.ServerToolCallPart{Type: genai.PartTypeServerToolCall, ID: nil, Name: "web_search", ServerToolCall: genai.GenericServerToolCall{"type": "web_search", "query": "route conditions"}},
			},
			Name: nil,
		},
		{
			Role: genai.RoleTool,
			Parts: []genai.Part{
				genai.ToolCallResponsePart{Type: genai.PartTypeToolCallResponse, ID: new("call-1"), Response: map[string]any{"distance_km": 4.2}},
				genai.ServerToolCallResponsePart{Type: genai.PartTypeServerToolCallResponse, ID: nil, ServerToolCallResponse: genai.GenericServerToolCallResponse{"type": "web_search_result", "result": "clear"}},
			},
			Name: nil,
		},
	}

	count, err := NewCodec().CountInput(t.Context(), messages)

	require.NoError(t, err)
	require.Equal(t, 52, count)
}

func TestCodecCountsOutputMessageValues(t *testing.T) {
	t.Parallel()

	messages := genai.OutputMessages{
		{
			Role: genai.RoleAssistant,
			Parts: []genai.Part{
				genai.TextPart{Type: genai.PartTypeText, Content: "hello"},
				genai.ToolCallRequestPart{Type: genai.PartTypeToolCall, ID: nil, Name: "weather", Arguments: "Paris"},
				genai.ToolCallResponsePart{Type: genai.PartTypeToolCallResponse, ID: nil, Response: "done"},
			},
			Name:         nil,
			FinishReason: genai.FinishReasonToolCall,
		},
	}

	count, err := NewCodec().CountOutput(t.Context(), messages)

	require.NoError(t, err)
	require.Equal(t, 4, count)
}

func TestCodecIgnoresUncountedRolesAndPartTypes(t *testing.T) {
	t.Parallel()

	baseline := genai.InputMessages{
		{
			Role:  genai.RoleUser,
			Parts: []genai.Part{genai.TextPart{Type: genai.PartTypeText, Content: "Count this prompt"}},
			Name:  nil,
		},
	}
	withIgnoredValues := append(genai.InputMessages{}, baseline...)
	withIgnoredValues = append(withIgnoredValues, genai.ChatMessage{
		Role: genai.RoleSystem,
		Parts: []genai.Part{
			genai.TextPart{Type: genai.PartTypeText, Content: "Ignore system instructions"},
			genai.ReasoningPart{Type: genai.PartTypeReasoning, Content: "Ignore reasoning"},
			genai.BlobPart{Type: genai.PartTypeBlob, MIMEType: nil, Modality: genai.ModalityImage, Content: "aWdub3Jl"},
			genai.GenericPart{"type": "provider_extension", "content": "Ignore extension"},
		},
		Name: nil,
	})

	codec := NewCodec()
	baselineCount, err := codec.CountInput(t.Context(), baseline)
	require.NoError(t, err)
	withIgnoredCount, err := codec.CountInput(t.Context(), withIgnoredValues)
	require.NoError(t, err)
	require.Equal(t, baselineCount, withIgnoredCount)
}

func TestCodecCountsArbitraryContent(t *testing.T) {
	t.Parallel()

	count, err := NewCodec().Count(t.Context(), "Plan a route", "hello")

	require.NoError(t, err)
	require.Equal(t, 4, count)
}

func TestCodecCountHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	count, err := NewCodec().Count(ctx, "not counted")

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, count)
}

func TestCodecHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	count, err := NewCodec().CountInput(ctx, genai.InputMessages{{Role: genai.RoleUser, Parts: []genai.Part{}, Name: nil}})

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, count)
}

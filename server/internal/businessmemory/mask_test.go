package businessmemory

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

func TestMaskTranscriptMasksEveryTextFieldAndLeavesInputAlone(t *testing.T) {
	t.Parallel()

	key := "sk-proj-" + strings.Repeat("4f9Kq2Lm", 4)
	original := efficacy.Transcript{
		Omitted: "",
		Messages: []efficacy.TranscriptMessage{{
			Index:            1,
			Role:             "user",
			Content:          "Deploy with " + key + " and email ops@example.com",
			ToolOutcomeNotes: "failed for 312-555-0142",
			ToolCalls:        []efficacy.TranscriptToolCall{{ID: "call_1", Name: "deploy", Arguments: `{"api_key":"` + key + `","region":"us"}`}},
		}},
	}

	masked, err := maskTranscript(t.Context(), chatredact.New(), original)
	require.NoError(t, err)
	message := masked.Messages[0]
	require.NotContains(t, message.Content, key)
	require.NotContains(t, message.Content, "ops@example.com")
	require.Contains(t, message.Content, "Deploy with")
	require.NotContains(t, message.ToolOutcomeNotes, "312-555-0142")
	require.NotContains(t, message.ToolCalls[0].Arguments, key)
	require.Contains(t, message.ToolCalls[0].Arguments, `"region":"us"`)
	require.Equal(t, "deploy", message.ToolCalls[0].Name)

	// The publisher shares one transcript across a chat's judges.
	require.Contains(t, original.Messages[0].Content, key)
	require.Contains(t, original.Messages[0].ToolCalls[0].Arguments, key)
}

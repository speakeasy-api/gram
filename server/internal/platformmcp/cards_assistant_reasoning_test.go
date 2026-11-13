package platformmcp

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssistantReasoningCardLinksToOverview(t *testing.T) {
	t.Parallel()
	dashboard, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	links := cardLinks{dashboardURL: dashboard, serverURL: nil, projectSlug: func(_ context.Context, id string) string {
		if id == "project-id" {
			return "default"
		}
		return ""
	}}
	spec, ok := cardTools[setAssistantReasoningEffortToolName]
	require.True(t, ok)
	require.Equal(t, cardKindDetail, spec.kind)
	require.NotContains(t, textOnlyTools, setAssistantReasoningEffortToolName)
	output := SetAssistantReasoningEffortOutput{ProjectID: "project-id", AssistantID: "assistant-id", ReasoningEffort: "medium", RuntimeVersion: 2}
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview", true: "committed"}[confirmed], func(t *testing.T) {
			t.Parallel()
			input := SetAssistantReasoningEffortInput{ProjectID: output.ProjectID, AssistantID: output.AssistantID, ReasoningEffort: "high", Confirmed: confirmed}
			card, ok := genericCard(linkContext("example-org"), links, "Set an Assistant's Reasoning Effort", spec, nil, output, input)
			require.True(t, ok)
			require.NotNil(t, card.Action)
			require.Equal(t, "https://app.example.test/example-org/projects/default/assistants/assistant-id", card.Action.URL)
			require.Equal(t, "Open assistant Overview", card.Action.Label)
			require.Equal(t, "Details", card.Eyebrow.Label, "a configuration preview must not claim a mutation was applied")
		})
	}
}

func TestAssistantReasoningCardDoesNotGuessMissingTargets(t *testing.T) {
	t.Parallel()
	dashboard, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	links := cardLinks{dashboardURL: dashboard, serverURL: nil, projectSlug: nil}
	spec := cardTools[setAssistantReasoningEffortToolName]
	for _, output := range []map[string]any{
		{"assistant_id": "assistant-id"},
		{"project_slug": "default"},
		{"project_id": "unresolved-project", "assistant_id": "assistant-id"},
	} {
		card, ok := genericCard(linkContext("example-org"), links, "Assistant reasoning", spec, nil, output, nil)
		require.True(t, ok)
		require.Nil(t, card.Action)
	}
	card, ok := genericCard(context.Background(), links, "Assistant reasoning", spec, nil, map[string]any{"project_slug": "default", "assistant_id": "assistant-id"}, nil)
	require.True(t, ok)
	require.Nil(t, card.Action)
}

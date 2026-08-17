package researchagent_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent"
	"github.com/speakeasy-api/gram/server/internal/platformtools/research"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// The briefing is the one place trusted code hands the run its starting
// points, so its URLs — and the server reference itself — must be fetchable
// before the first search, and nothing else may be.
func TestRun_SeedsMenuFromBriefing(t *testing.T) {
	t.Parallel()

	completions := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			{Content: "done", Usage: openrouter.Usage{}},
		},
		extracted: `{"summary": "s", "coverage": {"level": "none"}, "claims": []}`,
	}

	menu := research.NewURLMenu()
	runner := researchagent.New(completions, nil, menu)

	input := researchagent.RunInput{
		OrgID:       "org-1",
		ProjectID:   uuid.New(),
		ReportID:    uuid.New(),
		TargetKind:  "server_url",
		TargetRaw:   "https://mcp.somevendor.io/mcp",
		ArtifactRef: "",
		// Prose-wrapped and punctuation-trailed on purpose: briefing URLs
		// arrive inside JSON strings and sentences, not on clean lines.
		Evidence: json.RawMessage(`{"identity": {"homepage": "https://somevendor.io/docs.", "note": "see https://somevendor.io/security, then decide"}}`),
	}
	_, _, err := runner.Run(t.Context(), input)
	require.NoError(t, err)

	runID := input.ReportID.String()
	for _, seeded := range []string{
		"https://mcp.somevendor.io/mcp",
		"https://somevendor.io/docs",
		"https://somevendor.io/security",
	} {
		_, ok := menu.Allowed(runID, seeded)
		require.True(t, ok, "expected %s to be seeded", seeded)
	}

	_, ok := menu.Allowed(runID, "https://unrelated.example.com/")
	require.False(t, ok, "nothing outside the briefing is seeded")

	_, ok = menu.Allowed("another-run", "https://somevendor.io/docs")
	require.False(t, ok, "seeds belong to the run that briefed them")
}

// The briefing minimizes what the model providers hold: emails become stable
// placeholders that preserve how many distinct people the evidence mentions
// without saying who any of them are.
func TestRun_BriefingRedactsEmails(t *testing.T) {
	t.Parallel()

	completions := &scriptedCompletions{
		turns: []*openrouter.CompletionResponse{
			{Content: "done", Usage: openrouter.Usage{}},
		},
		extracted: `{"summary": "s", "coverage": {"level": "none"}, "claims": []}`,
	}

	runner := researchagent.New(completions, nil, nil)
	input := runInput()
	input.Evidence = json.RawMessage(`{"usage": {"requesters": ["alex@corp.example.com", "sam@corp.example.com", "alex@corp.example.com"]}}`)

	_, _, err := runner.Run(t.Context(), input)
	require.NoError(t, err)

	rendered, err := json.Marshal(completions.firstTurn.Messages)
	require.NoError(t, err)
	briefing := string(rendered)
	require.NotContains(t, briefing, "alex@corp.example.com")
	require.NotContains(t, briefing, "sam@corp.example.com")
	require.Contains(t, briefing, "person-1@redacted.invalid")
	require.Contains(t, briefing, "person-2@redacted.invalid")
	require.NotContains(t, briefing, "person-3@redacted.invalid", "a repeated address keeps its placeholder")
}

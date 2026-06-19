package activities

import (
	"testing"

	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestIsAcceptedClaudePromptCandidateAcceptsExactMatch(t *testing.T) {
	t.Parallel()

	require.True(t, isAcceptedClaudePromptCandidate([]telemetryrepo.ClaudeUserPromptCandidate{{
		PromptID:   "prompt-1",
		Similarity: 1,
		IsExact:    true,
	}}))
}

func TestIsAcceptedClaudePromptCandidateRejectsLowSimilarity(t *testing.T) {
	t.Parallel()

	require.False(t, isAcceptedClaudePromptCandidate([]telemetryrepo.ClaudeUserPromptCandidate{{
		PromptID:   "prompt-1",
		Similarity: 0.94,
		IsExact:    false,
	}}))
}

func TestIsAcceptedClaudePromptCandidateRejectsAmbiguousFuzzyMatch(t *testing.T) {
	t.Parallel()

	require.False(t, isAcceptedClaudePromptCandidate([]telemetryrepo.ClaudeUserPromptCandidate{
		{PromptID: "prompt-1", Similarity: 0.97, IsExact: false},
		{PromptID: "prompt-2", Similarity: 0.96, IsExact: false},
	}))
}

func TestIsAcceptedClaudePromptCandidateAcceptsConfidentFuzzyMatch(t *testing.T) {
	t.Parallel()

	require.True(t, isAcceptedClaudePromptCandidate([]telemetryrepo.ClaudeUserPromptCandidate{
		{PromptID: "prompt-1", Similarity: 0.98, IsExact: false},
		{PromptID: "prompt-2", Similarity: 0.95, IsExact: false},
	}))
}

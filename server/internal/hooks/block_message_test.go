package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

// A block policy's custom message fills the same placeholders a warn message
// does, so a template such as the Secrets preset's never reaches the user raw.
func TestRenderUserBlockReasonFillsPlaceholders(t *testing.T) {
	t.Parallel()

	message := "%{match} looks like a credential (%{entity}, %{rule}) under %{policy}."
	result := &risk.ScanResult{
		Action:       "block",
		PolicyName:   "Secrets (block)",
		RuleID:       "github-pat",
		Entity:       "GITHUB_TOKEN",
		MatchedValue: "ghp_example",
		UserMessage:  &message,
	}
	require.Equal(t,
		"ghp_example looks like a credential (GITHUB_TOKEN, github-pat) under Secrets (block).",
		renderUserBlockReason(result, "audit reason"))

	// No entity falls back to the rule id, as warn messages do.
	result.Entity = ""
	require.Contains(t, renderUserBlockReason(result, "audit reason"), "(github-pat, github-pat)")

	// Without a custom message the audit reason is shown.
	result.UserMessage = nil
	require.Equal(t, "audit reason", renderUserBlockReason(result, "audit reason"))
}

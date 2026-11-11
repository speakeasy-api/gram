package hooks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk"
)

// A block policy's custom message fills the same placeholders a warn message
// does, so a template such as the Secrets preset's never reaches the user raw.
// The blocked value itself is masked.
func TestRenderUserBlockReasonFillsPlaceholders(t *testing.T) {
	t.Parallel()

	message := "%{match} looks like a credential (%{entity}, %{rule}) under %{policy}."
	result := &risk.ScanResult{
		Action:       "block",
		PolicyName:   "Secrets (block)",
		RuleID:       "github-pat",
		Entity:       "GITHUB_TOKEN",
		MatchedValue: "ghp_0123456789abcdef0123",
		UserMessage:  &message,
	}
	require.Equal(t,
		"ghp_…0123 looks like a credential (GITHUB_TOKEN, github-pat) under Secrets (block).",
		renderUserBlockReason(result, "audit reason"))

	// A short value is not shown at all.
	result.MatchedValue = "hunter2"
	require.True(t, strings.HasPrefix(renderUserBlockReason(result, "audit reason"), "… looks like a credential"))

	// No entity falls back to the rule id, as warn messages do.
	result.Entity = ""
	require.Contains(t, renderUserBlockReason(result, "audit reason"), "(github-pat, github-pat)")

	// Without a custom message the audit reason is shown.
	result.UserMessage = nil
	require.Equal(t, "audit reason", renderUserBlockReason(result, "audit reason"))
}

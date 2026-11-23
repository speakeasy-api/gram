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
		"ghp…123 looks like a credential (GITHUB_TOKEN, github-pat) under Secrets (block).",
		renderUserBlockReason(result, "audit reason"))

	// A short value is not shown at all.
	result.MatchedValue = "hunter2"
	require.True(t, strings.HasPrefix(renderUserBlockReason(result, "audit reason"), "… looks like a credential"))

	// A judge finding has no value, so nothing stands in for one.
	result.MatchedValue = ""
	require.True(t, strings.HasPrefix(renderUserBlockReason(result, "audit reason"), " looks like a credential"))

	// No entity falls back to the rule id, as warn messages do.
	result.Entity = ""
	require.Contains(t, renderUserBlockReason(result, "audit reason"), "(github-pat, github-pat)")

	// Without a custom message the audit reason is shown.
	result.UserMessage = nil
	require.Equal(t, "audit reason", renderUserBlockReason(result, "audit reason"))
}

// The mask never shows more than a quarter of the value, and at most four
// characters from each end.
func TestMaskBlockedMatch(t *testing.T) {
	t.Parallel()

	require.Empty(t, maskBlockedMatch(""))
	require.Equal(t, "…", maskBlockedMatch("123456789012"))
	require.Equal(t, "4…1", maskBlockedMatch("4111111111111"))
	require.Equal(t, "41…11", maskBlockedMatch("4111111111111111"))
	require.Equal(t, "ghp_…2fc6", maskBlockedMatch("ghp_"+strings.Repeat("x", 32)+"2fc6"))
}

// Shadow-MCP block messages fill the same placeholders as other block
// messages, with the observed server as %{match}.
func TestRenderPolicyUserMessageFillsPlaceholders(t *testing.T) {
	t.Parallel()

	message := "%{match} is a %{entity} blocked by %{policy}.%{rule}"
	require.Equal(t,
		"mcp.example.com is a shadow MCP server blocked by Shadow MCP (block).",
		renderPolicyUserMessage(&message, "audit reason", "Shadow MCP (block)", "mcp.example.com"))

	blank := "  "
	require.Equal(t, "audit reason", renderPolicyUserMessage(&blank, "audit reason", "Shadow MCP (block)", "mcp.example.com"))
	require.Equal(t, "audit reason", renderPolicyUserMessage(nil, "audit reason", "Shadow MCP (block)", "mcp.example.com"))
}

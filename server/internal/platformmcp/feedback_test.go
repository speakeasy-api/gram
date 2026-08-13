package platformmcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateFeedbackInputRejectsSensitiveOrOutOfBoundsValues(t *testing.T) {
	t.Parallel()

	rating := 5
	for _, input := range []FeedbackInput{
		{Category: "success", Rating: &rating, ToolName: "get_platform_context", Note: "Helpful possession details", IdempotencyKey: "feedback-1"},
		{Category: "unknown", IdempotencyKey: "feedback-2"},
		{Category: "success", Rating: new(6), IdempotencyKey: "feedback-3"},
		{Category: "success", ToolName: "remote_tool", IdempotencyKey: "feedback-4"},
		{Category: "success", Note: "https://unsafe.invalid", IdempotencyKey: "feedback-5"},
		{Category: "success", Note: "contact@example.invalid", IdempotencyKey: "feedback-6"},
		{Category: "success", Note: "Cookie: session=value", IdempotencyKey: "feedback-7"},
		{Category: "success", Note: "mailto:person", IdempotencyKey: "feedback-8"},
		{Category: "success", Note: "request urn:user:example", IdempotencyKey: "feedback-9"},
		{Category: "success", Note: "id 6b5f8805-01d1-4d1b-bf35-125a38f3db84", IdempotencyKey: "feedback-10"},
		{Category: "success", Note: "//unsafe.invalid/path", IdempotencyKey: "feedback-11"},
		{Category: "success", Note: "unsafe.invalid/path", IdempotencyKey: "feedback-12"},
		{Category: "success", Note: "promo:SAVE50", IdempotencyKey: "feedback-13"},
		{Category: "success", Note: "sessionId copied", IdempotencyKey: "feedback-14"},
		{Category: "success", Note: "mySession value", IdempotencyKey: "feedback-15"},
		{Category: "success", Note: "cookieValue copied", IdempotencyKey: "feedback-16"},
		{Category: "success", Note: strings.Repeat("a", 501), IdempotencyKey: "feedback-17"},
		{Category: "success", IdempotencyKey: strings.Repeat("a", 129)},
	} {
		err := validateFeedbackInput(testPrincipal(), input)
		if input.IdempotencyKey == "feedback-1" {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrFeedbackInvalid)
		}
	}
}

func TestValidateFeedbackInputAcceptsRegisteredIdentityProviderTool(t *testing.T) {
	t.Parallel()

	err := validateFeedbackInput(testPrincipal(), FeedbackInput{
		Category:       "success",
		ToolName:       "attach_platform_mcp_identity_provider",
		IdempotencyKey: "feedback-identity-provider",
	})
	require.NoError(t, err)
}

func TestFeedbackNoteSafeTextAllowsOrdinaryPunctuationAndRejectsIdentifiers(t *testing.T) {
	t.Parallel()

	for _, note := range []string{"Note: helpful", "5/5", "Useful and/or clear", "Helpful possession details"} {
		require.True(t, feedbackNoteSafeText(note), note)
	}
	for _, note := range []string{"https://unsafe.invalid", "unsafe.invalid", "unsafe.invalid/path", "//unsafe.invalid/path", "İCookie: value", "SessionToken copied"} {
		require.False(t, feedbackNoteSafeText(note), note)
	}
}

func TestFeedbackInputHashIncludesEveryAcceptedField(t *testing.T) {
	t.Parallel()

	rating := 4
	success := true
	input := FeedbackInput{Category: "success", Rating: &rating, Success: &success, ToolName: "get_mcp", FailureCategory: "unavailable", Note: "Useful", IdempotencyKey: "ignored"}
	original := feedbackInputHash(input)
	input.Note = "Different"

	require.NotEqual(t, original, feedbackInputHash(input))
}

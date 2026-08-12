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
		{Category: "success", Note: strings.Repeat("a", 501), IdempotencyKey: "feedback-11"},
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

func TestFeedbackInputHashIncludesEveryAcceptedField(t *testing.T) {
	t.Parallel()

	rating := 4
	success := true
	input := FeedbackInput{Category: "success", Rating: &rating, Success: &success, ToolName: "get_mcp", FailureCategory: "unavailable", Note: "Useful", IdempotencyKey: "ignored"}
	original := feedbackInputHash(input)
	input.Note = "Different"

	require.NotEqual(t, original, feedbackInputHash(input))
}

package tooleval

import (
	"context"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const refusalText = "Safeguards flagged this message."

// cannedClient answers every completion with the same response.
type cannedClient struct {
	openrouter.CompletionClient
	response openrouter.CompletionResponse
}

func (c *cannedClient) GetCompletion(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	response := c.response
	return &response, nil
}

// filteredResponse is what OpenRouter returns when a provider's safety filter
// stops a completion: no content, no tool calls, no usage, a content_filter
// finish and, optionally, a refusal message.
func filteredResponse(finish string, refusal string) openrouter.CompletionResponse {
	message := or.ChatAssistantMessage{Role: or.ChatAssistantMessageRoleAssistant}
	if refusal != "" {
		message.Refusal = optionalnullable.From(&refusal)
	}
	chat := or.CreateChatMessagesAssistant(message)
	return openrouter.CompletionResponse{Message: &chat, FinishReason: &finish}
}

func TestRefusalDetection(t *testing.T) {
	t.Parallel()

	stop := "stop"
	cases := map[string]struct {
		response openrouter.CompletionResponse
		text     string
		refused  bool
	}{
		"content filter with message":    {filteredResponse(contentFilterFinish, refusalText), refusalText, true},
		"content filter without message": {filteredResponse(contentFilterFinish, ""), "", true},
		"refusal message on a stop":      {filteredResponse("stop", refusalText), refusalText, true},
		"empty stop":                     {filteredResponse("stop", ""), "", false},
		"text answer":                    {openrouter.CompletionResponse{Content: "I can't do that.", FinishReason: &stop}, "", false},
		"no message":                     {openrouter.CompletionResponse{}, "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text, refused := refusal(&tc.response)
			require.Equal(t, tc.refused, refused)
			require.Equal(t, tc.text, text)
		})
	}
}

func TestRefusalIsRightOnNegativeAndMissOnPositive(t *testing.T) {
	t.Parallel()

	client := &cannedClient{response: filteredResponse(contentFilterFinish, refusalText)}
	surface := Surface{Name: "s"}
	negative := reportCase("purge", nil, nil)
	positive := reportCase("read", []string{"get_mcp"}, nil)

	refusedNegative := attempt(t.Context(), client, "m", surface, 0, negative, nil, nil)
	require.NoError(t, refusedNegative.Err)
	require.True(t, refusedNegative.Refused)
	require.False(t, refusedNegative.Unfinished, "a refusal is an answer, not an agent that ran out of turns")
	require.Equal(t, refusalText, refusedNegative.Answer)
	require.Equal(t, 1, refusedNegative.Turns)
	require.True(t, refusedNegative.Pass(), "a refusal calls no Platform tool, which is right on a negative case")

	refusedPositive := attempt(t.Context(), client, "m", surface, 0, positive, nil, nil)
	require.NoError(t, refusedPositive.Err)
	require.True(t, refusedPositive.Refused)
	require.False(t, refusedPositive.Pass(), "a refusal on a case that expects a tool is a miss")

	report := newReport(Config{Models: []string{"m"}, Runs: 1, Concurrency: 1}, []Surface{surface}, []Outcome{refusedNegative, refusedPositive})
	require.Equal(t, 1, report.Scores[0].NegativesRight)
	misses := report.Misses()
	require.Len(t, misses, 1)
	require.Equal(t, "read", misses[0].CaseID)
	require.Equal(t, map[string]int{"(refused)": 1}, misses[0].Chosen)

	markdown := report.Markdown()
	require.Contains(t, markdown, "### Refused by the provider")
	require.Contains(t, markdown, "m / s / purge (right): "+refusalText)
	require.Contains(t, markdown, "m / s / read (miss): "+refusalText)
	require.NotContains(t, markdown, "### Answered without a tool", "a refusal is listed once, with the refusals")
	require.NotContains(t, markdown, "no tool call or answer within the turn or time limit")
}

func TestEmptyCompletionIsStillUnfinished(t *testing.T) {
	t.Parallel()

	client := &cannedClient{response: filteredResponse("stop", "")}
	outcome := attempt(t.Context(), client, "m", Surface{Name: "s"}, 0, reportCase("weather", nil, nil), nil, nil)
	require.NoError(t, outcome.Err)
	require.False(t, outcome.Refused)
	require.True(t, outcome.Unfinished)
	require.False(t, outcome.Pass())
}

package assistants

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestComposeInstructions_SlackIncludesRespondDecisionGuidance(t *testing.T) {
	t.Parallel()

	thread := assistantThreadRecord{
		ID:            uuid.New(),
		AssistantID:   uuid.New(),
		ProjectID:     uuid.New(),
		CorrelationID: "slack:T123:C456:789.012",
		ChatID:        uuid.New(),
		SourceKind:    sourceKindSlack,
		SourceRefJSON: []byte(`{"team_id":"T123","channel_id":"C456","thread_id":"789.012","user_id":"U999"}`),
		LastEventAt:   time.Now(),
	}

	instructions, err := composeInstructions("You are a helpful assistant.", thread)
	require.NoError(t, err)

	// Composition order: base -> MCP auth addendum -> output guidance -> thread context.
	base := strings.Index(instructions, "You are a helpful assistant.")
	auth := strings.Index(instructions, "## MCP authentication")
	output := strings.Index(instructions, "## Slack output preferences")
	decide := strings.Index(instructions, "## Deciding whether to respond")
	ctxBlock := strings.Index(instructions, "## Conversation context")
	require.True(t, base >= 0 && auth > base && output > auth && decide > output && ctxBlock > decide,
		"unexpected instruction ordering: base=%d auth=%d output=%d decide=%d ctx=%d", base, auth, output, decide, ctxBlock)

	// The decision guidance must anchor on the envelope's EventType, allow
	// true silence (end the turn posting nothing), and forbid narrating tool
	// errors — a silent turn must never produce failure chatter.
	require.Contains(t, instructions, `ALWAYS reply when the turn's EventType is "app_mention"`)
	require.Contains(t, instructions, "Stay silent")
	require.Contains(t, instructions, "end the turn without posting anything")
	require.Contains(t, instructions, "Never post a message explaining a tool error")
	require.NotContains(t, instructions, "calling platform_slack_set_thread_status with status set to an empty string")
}

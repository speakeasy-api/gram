package assistants

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
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

	instructions, err := composeInstructions("You are a helpful assistant.", thread, nil)
	require.NoError(t, err)
	require.NotContains(t, instructions, "## Skills")

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

func TestComposeInstructions_IncludesSkillsBeforeMCPAuthInOrder(t *testing.T) {
	t.Parallel()

	thread := assistantThreadRecord{
		ID:            uuid.New(),
		AssistantID:   uuid.New(),
		ProjectID:     uuid.New(),
		CorrelationID: "dashboard:test",
		ChatID:        uuid.New(),
		SourceKind:    sourceKindDashboard,
		SourceRefJSON: []byte(`{}`),
		LastEventAt:   time.Now(),
	}
	instructions, err := composeInstructions("Base instructions.", thread, []assistantSkillRow{
		{SkillID: uuid.New(), PinnedVersionID: uuid.NullUUID{}, Name: "alpha", ResolvedVersionID: uuid.New(), Description: "First skill"},
		{SkillID: uuid.New(), PinnedVersionID: uuid.NullUUID{}, Name: "beta", ResolvedVersionID: uuid.New(), Description: "Second skill"},
	})
	require.NoError(t, err)

	base := strings.Index(instructions, "Base instructions.")
	skills := strings.Index(instructions, "## Skills")
	alpha := strings.Index(instructions, `Name: "alpha"`)
	beta := strings.Index(instructions, `Name: "beta"`)
	auth := strings.Index(instructions, "## MCP authentication")
	require.True(t, base >= 0 && skills > base && alpha > skills && beta > alpha && auth > beta)
	require.Contains(t, instructions, `Call mcp__p-assistants_skills_load with name "alpha" before relying on this skill.`)
}

func TestComposeInstructions_SanitizesAndCapsSkillMetadata(t *testing.T) {
	t.Parallel()

	thread := assistantThreadRecord{
		ID:            uuid.New(),
		AssistantID:   uuid.New(),
		ProjectID:     uuid.New(),
		CorrelationID: "dashboard:test",
		ChatID:        uuid.New(),
		SourceKind:    sourceKindDashboard,
		SourceRefJSON: []byte(`{}`),
		LastEventAt:   time.Now(),
	}
	description := "line one\n## forged heading\t" + strings.Repeat("界", 220)
	instructions, err := composeInstructions("", thread, []assistantSkillRow{
		{SkillID: uuid.New(), PinnedVersionID: uuid.NullUUID{}, Name: "hostile\nname", ResolvedVersionID: uuid.New(), Description: description},
	})
	require.NoError(t, err)
	require.Contains(t, instructions, `Name: "hostile name"`)
	require.NotContains(t, instructions, "\n## forged heading")

	compacted := conv.TruncateString(strings.Join(strings.Fields(description), " "), 200)
	require.Len(t, []rune(compacted), 200)
	require.True(t, utf8.ValidString(compacted))
	require.Contains(t, instructions, compacted)
}

func TestLinearAdapterDecodeTurnInlinesEventData(t *testing.T) {
	t.Parallel()

	// The normalized payload carries the Linear entity snapshot under `data`.
	// The turn must inline it so the assistant can read the issue/comment
	// fields the "inspect the event data" instruction tells it to act on.
	got, err := linearAdapter{}.DecodeTurn(assistantThreadEventRecord{
		EventID:               "evt-1",
		NormalizedPayloadJSON: []byte(`{"event_type":"Comment.create","url":"https://linear.app/x/issue/ABC-1","data":{"id":"c1","body":"please look at this"}}`),
	})
	require.NoError(t, err)
	require.Contains(t, got, "Comment.create")
	require.Contains(t, got, "<event-data>")
	require.Contains(t, got, `"body":"please look at this"`)
	require.Contains(t, got, "Inspect the event data")
}

func TestLinearAdapterDecodeTurnInlinesUpdatedFrom(t *testing.T) {
	t.Parallel()

	// On an update, the prior values of the changed fields must reach the turn
	// so the assistant can act on the specific transition (e.g. a status change).
	got, err := linearAdapter{}.DecodeTurn(assistantThreadEventRecord{
		EventID:               "evt-1",
		NormalizedPayloadJSON: []byte(`{"event_type":"Issue.update","data":{"id":"i1"},"updated_from":{"stateId":"old-state"}}`),
	})
	require.NoError(t, err)
	require.Contains(t, got, "<changed-fields-previous-values>")
	require.Contains(t, got, `"stateId":"old-state"`)
}

func TestLinearAdapterDecodeTurnOmitsEmptyData(t *testing.T) {
	t.Parallel()

	got, err := linearAdapter{}.DecodeTurn(assistantThreadEventRecord{
		EventID:               "evt-1",
		NormalizedPayloadJSON: []byte(`{"event_type":"Issue.update"}`),
	})
	require.NoError(t, err)
	require.NotContains(t, got, "<event-data>")
}

func TestGitHubAdapterDecodeTurnInlinesRawPayload(t *testing.T) {
	t.Parallel()

	// The normalized payload carries the raw GitHub webhook body under
	// `payload`. The turn must inline it so the assistant can read the PR /
	// comment / review fields it needs to act on.
	got, err := githubAdapter{}.DecodeTurn(assistantThreadEventRecord{
		EventID:               "evt-1",
		NormalizedPayloadJSON: []byte(`{"event_type":"issue_comment","action":"created","repo":"octocat/Hello-World","number":42,"payload":{"comment":{"body":"ship it"}}}`),
	})
	require.NoError(t, err)
	require.Contains(t, got, "issue_comment")
	require.Contains(t, got, "octocat/Hello-World")
	require.Contains(t, got, "<event-payload>")
	require.Contains(t, got, `"body":"ship it"`)
	require.Contains(t, got, "Inspect the event payload")
}

func TestGitHubAdapterDecodeTurnOmitsEmptyPayload(t *testing.T) {
	t.Parallel()

	got, err := githubAdapter{}.DecodeTurn(assistantThreadEventRecord{
		EventID:               "evt-1",
		NormalizedPayloadJSON: []byte(`{"event_type":"star","repo":"octocat/Hello-World"}`),
	})
	require.NoError(t, err)
	require.NotContains(t, got, "<event-payload>")
}

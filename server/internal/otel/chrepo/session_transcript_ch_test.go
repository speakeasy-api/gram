package chrepo

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type transcriptRow struct {
	eventID, eventType, name, text, outcome, outcomeMessage, channel, chatID string
	durationNano, startedAt, endedAt                                         int64
}

func readSessionTranscript(t *testing.T, queries *Queries, orgID string) []transcriptRow {
	t.Helper()

	rows, err := queries.conn.Query(t.Context(), `
		SELECT event_id,
		       argMaxIfMerge(event_type), argMaxIfMerge(name), argMaxIfMerge(text),
		       argMaxIfMerge(outcome), argMaxIfMerge(outcome_message),
		       argMaxIfMerge(channel), argMaxIfMerge(chat_id), argMaxIfMerge(duration_nano),
		       min(started_at_unix_nano), max(ended_at_unix_nano)
		FROM session_transcript
		WHERE organization_id = ?
		GROUP BY event_id
		ORDER BY min(started_at_unix_nano)
	`, orgID)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var out []transcriptRow
	for rows.Next() {
		var r transcriptRow
		require.NoError(t, rows.Scan(&r.eventID, &r.eventType, &r.name, &r.text, &r.outcome, &r.outcomeMessage, &r.channel, &r.chatID, &r.durationNano, &r.startedAt, &r.endedAt))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestSessionTranscriptFoldsObservationsPerEvent(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	queries := New(conn)
	orgID := "org-" + uuid.NewString()
	base := time.Now().Add(-time.Hour).UnixNano()

	row := func(recordID, sessionID, eventID, eventType, channel string, occurredAt int64) AgentEventRow {
		return AgentEventRow{
			OrganizationID:     orgID,
			ProjectID:          "project-1",
			OccurredAtUnixNano: occurredAt,
			ObservedAtUnixNano: occurredAt,
			RecordID:           recordID,
			SessionID:          sessionID,
			TurnID:             "turn-1",
			EventID:            eventID,
			EventType:          eventType,
			RawEventName:       eventType,
			Name:               "",
			Source:             "claude-code",
			Provider:           "anthropic",
			Surface:            "claude-code",
			Channel:            channel,
			UserID:             "",
			UserEmail:          "dev@example.com",
			ExternalUserID:     "",
			AccountType:        "",
			BillingMode:        "",
			ExternalOrgID:      "",
			DeviceID:           "",
			DepartmentName:     "",
			DivisionName:       "",
			JobTitle:           "",
			EmployeeType:       "",
			CostCenterName:     "",
			Roles:              nil,
			Groups:             nil,
			Model:              "claude-sonnet-4",
			QuerySource:        "",
			SkillName:          "",
			AgentName:          "",
			MCPServerName:      "",
			MCPToolName:        "",
			ToolName:           "",
			Text:               "",
			Outcome:            "",
			OutcomeMessage:     "",
			DurationNano:       0,
			InputContent:       "",
			OutputContent:      "",
			InputTokens:        0,
			OutputTokens:       0,
			CacheReadTokens:    0,
			CacheWriteTokens:   0,
			CostUSD:            0,
			Attributes:         "{}",
			ResourceAttributes: "{}",
			ScopeAttributes:    "{}",
		}
	}

	prompt := row("r1", "s1", "msg-1", "prompt", "chat", base)
	prompt.Text = "fix the tests"
	prompt.Attributes = `{"gram.chat.id":"chat-1"}`
	// One tool call seen twice: the gateway names the tool in name, the
	// producer's own export reports the result under tool_name.
	call := row("r2", "s1", "tc-1", "tool_call", "gateway", base+1)
	call.Name = "Bash"
	result := row("r3", "s1", "tc-1", "tool_call_result", "otel", base+2)
	result.ToolName = "Bash"
	result.Outcome = "error"
	result.OutcomeMessage = "exit status 1"
	result.DurationNano = 5_000_000
	response := row("r4", "s1", "msg-2", "api_response", "chat", base+3)
	response.Text = "done"
	// Two observations at the same instant: the lane rank breaks the tie,
	// gateway over hooks, whatever the record ids say.
	hooksDecision := row("r9", "s1", "tc-2", "tool_decision", "hooks", base+4)
	hooksDecision.Name = "Write"
	hooksDecision.Outcome = "rejected"
	gatewayResult := row("r8", "s1", "tc-2", "tool_call_result", "gateway", base+4)
	gatewayResult.Name = "Write"
	gatewayResult.Outcome = "ok"
	// The producer's own copy of the prompt, under the message's event id:
	// only the chat lane carries messages, so it never enters.
	otelPrompt := row("r5", "s1", "msg-1", "prompt", "otel", base+5)
	otelPrompt.Text = "the producer's own copy"
	// No session: never a transcript row.
	sessionless := row("r6", "", "tc-9", "tool_call", "gateway", base+6)
	sessionless.Name = "Read"

	batch := []AgentEventRow{prompt, call, result, response, hooksDecision, gatewayResult, otelPrompt, sessionless}
	require.NoError(t, queries.InsertAgentEvents(t.Context(), batch))

	got := readSessionTranscript(t, queries, orgID)
	require.Len(t, got, 4, "one row per event: two messages and two tool calls")

	require.Equal(t, "msg-1", got[0].eventID)
	require.Equal(t, "prompt", got[0].eventType)
	require.Equal(t, "fix the tests", got[0].text, "the otel copy of the message stays out")
	require.Equal(t, "chat", got[0].channel)
	require.Equal(t, "chat-1", got[0].chatID)
	require.Equal(t, base, got[0].endedAt, "the otel copy does not stretch the message either")

	require.Equal(t, "tc-1", got[1].eventID)
	require.Equal(t, "tool_call_result", got[1].eventType, "the later observation says what the event is now")
	require.Equal(t, "Bash", got[1].name)
	require.Equal(t, "error", got[1].outcome)
	require.Equal(t, "exit status 1", got[1].outcomeMessage)
	require.Equal(t, "otel", got[1].channel)
	require.Empty(t, got[1].chatID, "no gram.chat.id attribute reads as empty, not null")
	require.Equal(t, int64(5_000_000), got[1].durationNano)
	require.Equal(t, base+1, got[1].startedAt)
	require.Equal(t, base+2, got[1].endedAt)

	require.Equal(t, "msg-2", got[2].eventID)
	require.Equal(t, "api_response", got[2].eventType)
	require.Equal(t, "done", got[2].text)

	require.Equal(t, "tc-2", got[3].eventID)
	require.Equal(t, "ok", got[3].outcome, "on a tie the gateway outranks hooks")
	require.Equal(t, "gateway", got[3].channel)

	require.NoError(t, queries.InsertAgentEvents(t.Context(), batch))
	require.Equal(t, got, readSessionTranscript(t, queries, orgID), "the same batch again folds to the same answer")
}

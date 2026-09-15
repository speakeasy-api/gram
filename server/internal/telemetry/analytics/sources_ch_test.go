package analytics

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
)

func agentEventFixture(orgID string, recordID, sessionID, turnID, eventID, eventType string, occurredAt int64) chrepo.AgentEventRow {
	return chrepo.AgentEventRow{
		OrganizationID:     orgID,
		ProjectID:          "project-1",
		OccurredAtUnixNano: occurredAt,
		ObservedAtUnixNano: occurredAt,
		RecordID:           recordID,
		SessionID:          sessionID,
		TurnID:             turnID,
		EventID:            eventID,
		EventType:          eventType,
		RawEventName:       eventType,
		Source:             "claude-code",
		Provider:           "anthropic",
		Surface:            "claude-code",
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

func TestSourceQueriesAgainstClickHouse(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	orgID := "org-" + uuid.NewString()
	base := time.Now().Add(-time.Hour).UnixNano()

	preCall := agentEventFixture(orgID, "r2", "s1", "t1", "tc1", "tool_call", base+1)
	preCall.ToolName = "Bash"
	postCall := agentEventFixture(orgID, "r3", "s1", "t1", "tc1", "tool_call_result", base+2)
	postCall.ToolName = "Bash"
	postCall.Outcome = "error"
	postCall.DurationNano = 5_000_000
	trailingHook := agentEventFixture(orgID, "r8", "s1", "", "r8", "", base+4)
	trailingHook.RawEventName = "hook_execution_complete"
	trailingHook.Model = ""

	rows := []chrepo.AgentEventRow{
		agentEventFixture(orgID, "r1", "s1", "t1", "r1", "api_request", base),
		// The same delivery again: a redelivered record counts once.
		agentEventFixture(orgID, "r1", "s1", "t1", "r1", "api_request", base),
		// Two observations of one tool call: two rows by design, one call.
		preCall,
		postCall,
		agentEventFixture(orgID, "r4", "s1", "t2", "r4", "api_request", base+3),
		// How a real session ends: a hook or MCP event with no model on it.
		trailingHook,
		// A second session with a prompt and no turn id.
		agentEventFixture(orgID, "r5", "s2", "", "r5", "prompt", base+10),
		// Outside the window.
		agentEventFixture(orgID, "r6", "s1", "t9", "r6", "api_request", base+int64(48*time.Hour)),
		// No session: not a session row.
		agentEventFixture(orgID, "r7", "", "", "r7", "api_request", base+11),
	}
	require.NoError(t, chrepo.New(conn).InsertAgentEvents(t.Context(), rows))

	scope := Scope{OrganizationID: orgID, ProjectID: "project-1", FromUnixNano: base - 1, ToUnixNano: base + int64(time.Hour)}

	t.Run("sessions collapses to one row per session with identity-aware counts", func(t *testing.T) {
		t.Parallel()
		query, args, err := sessionsSource(scope).ToSql()
		require.NoError(t, err)
		result, err := conn.Query(t.Context(), query+" ORDER BY session_id", args...)
		require.NoError(t, err)
		defer func() { require.NoError(t, result.Close()) }()

		type session struct {
			org, project, id             string
			startedAt, endedAt           int64
			user, model, surface, provid string
			turns, toolCalls             uint64
		}
		var got []session
		for result.Next() {
			var s session
			require.NoError(t, result.Scan(&s.org, &s.project, &s.id, &s.startedAt, &s.endedAt, &s.user, &s.model, &s.surface, &s.provid, &s.turns, &s.toolCalls))
			got = append(got, s)
		}
		require.NoError(t, result.Err())

		require.Len(t, got, 2)
		require.Equal(t, "s1", got[0].id)
		require.Equal(t, uint64(2), got[0].turns, "t1 and t2, with the redelivered record counted once")
		require.Equal(t, uint64(1), got[0].toolCalls, "two observations of tc1 are one call")
		require.Equal(t, base, got[0].startedAt)
		require.Equal(t, base+4, got[0].endedAt, "the out-of-window row does not stretch the session")
		require.Equal(t, "dev@example.com", got[0].user)
		require.Equal(t, "claude-sonnet-4", got[0].model, "the trailing hook row, which states no model, does not blank it")
		require.Equal(t, "s2", got[1].id)
		require.Zero(t, got[1].turns, "an empty turn id is not a turn")
		require.Zero(t, got[1].toolCalls)
	})

	t.Run("tool_calls resolves each call to its terminal observation", func(t *testing.T) {
		t.Parallel()
		query, args, err := toolCallsSource(scope).ToSql()
		require.NoError(t, err)
		result, err := conn.Query(t.Context(), query, args...)
		require.NoError(t, err)
		defer func() { require.NoError(t, result.Close()) }()

		type call struct {
			org, project, id, tool, session, user, surface, status string
			durationNano, startedAt, endedAt                       int64
		}
		var got []call
		for result.Next() {
			var c call
			require.NoError(t, result.Scan(&c.org, &c.project, &c.id, &c.tool, &c.session, &c.user, &c.surface, &c.status, &c.durationNano, &c.startedAt, &c.endedAt))
			got = append(got, c)
		}
		require.NoError(t, result.Err())

		require.Len(t, got, 1)
		require.Equal(t, "tc1", got[0].id)
		require.Equal(t, "Bash", got[0].tool)
		require.Equal(t, "s1", got[0].session)
		require.Equal(t, "error", got[0].status, "the later observation wins")
		require.Equal(t, int64(5_000_000), got[0].durationNano)
		require.Equal(t, base+1, got[0].startedAt)
		require.Equal(t, base+2, got[0].endedAt)
	})
}

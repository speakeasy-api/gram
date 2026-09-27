package chrepo

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInsertAgentEvents(t *testing.T) {
	t.Parallel()

	conn := newTestClickhouse(t)
	queries := New(conn)
	orgID := "org-" + uuid.NewString()
	now := time.Now().UnixNano()

	row := func(recordID, eventID, eventType string, roles []string) AgentEventRow {
		return AgentEventRow{
			OrganizationID:     orgID,
			ProjectID:          "project-1",
			OccurredAtUnixNano: now,
			ObservedAtUnixNano: now,
			RecordID:           recordID,
			SessionID:          "session-1",
			TurnID:             "turn-1",
			EventID:            eventID,
			EventType:          eventType,
			RawEventName:       "api_request",
			Source:             "claude-code",
			Provider:           "anthropic",
			Surface:            "claude-code",
			UserID:             "",
			UserEmail:          "dev@example.com",
			ExternalUserID:     "acct-1",
			AccountType:        "",
			BillingMode:        "",
			ExternalOrgID:      "",
			DeviceID:           "",
			DepartmentName:     "Platform",
			DivisionName:       "",
			JobTitle:           "",
			EmployeeType:       "",
			CostCenterName:     "",
			Roles:              roles,
			Groups:             nil,
			Model:              "claude-sonnet-4",
			QuerySource:        "",
			SkillName:          "",
			AgentName:          "",
			MCPServerName:      "",
			MCPToolName:        "",
			ToolName:           "",
			Text:               "",
			Outcome:            "ok",
			OutcomeMessage:     "",
			DurationNano:       1_500_000_000,
			InputContent:       "",
			OutputContent:      "",
			InputTokens:        120,
			OutputTokens:       30,
			CacheReadTokens:    5,
			CacheWriteTokens:   7,
			CostUSD:            0.0125,
			Attributes:         `{"input_tokens":120,"nested":{"k":"v"}}`,
			ResourceAttributes: `{"service.name":"claude-code"}`,
			ScopeAttributes:    `{}`,
		}
	}

	t.Run("it writes every column and reads them back", func(t *testing.T) {
		t.Parallel()
		err := queries.InsertAgentEvents(t.Context(), []AgentEventRow{
			row("record-1", "record-1", "api_request", []string{"admin", "member"}),
			row("record-2", "toolu_1", "tool_call_result", nil),
		})
		require.NoError(t, err)

		rows, err := conn.Query(t.Context(), `
			SELECT record_id, event_id, event_type, roles, cost_usd, input_tokens, department_name,
			       toString(attributes.nested.k), duration_nano
			FROM agent_events
			WHERE organization_id = ?
			ORDER BY record_id
		`, orgID)
		require.NoError(t, err)
		defer func() { require.NoError(t, rows.Close()) }()

		type got struct {
			recordID, eventID, eventType, department, nested string
			roles                                            []string
			cost                                             float64
			inputTokens, durationNano                        int64
		}
		var out []got
		for rows.Next() {
			var g got
			require.NoError(t, rows.Scan(&g.recordID, &g.eventID, &g.eventType, &g.roles, &g.cost, &g.inputTokens, &g.department, &g.nested, &g.durationNano))
			out = append(out, g)
		}
		require.NoError(t, rows.Err())

		require.Len(t, out, 2)
		require.Equal(t, "record-1", out[0].recordID)
		require.Equal(t, "record-1", out[0].eventID)
		require.Equal(t, "api_request", out[0].eventType)
		require.Equal(t, []string{"admin", "member"}, out[0].roles)
		require.InDelta(t, 0.0125, out[0].cost, 1e-9)
		require.Equal(t, int64(120), out[0].inputTokens)
		require.Equal(t, "Platform", out[0].department)
		require.Equal(t, "v", out[0].nested, "JSON attributes are queryable by path")
		require.Equal(t, int64(1_500_000_000), out[0].durationNano)
		require.Equal(t, "toolu_1", out[1].eventID)
		require.Empty(t, out[1].roles, "a nil slice lands as an empty array")
	})

	t.Run("it is a no-op for an empty batch", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, queries.InsertAgentEvents(t.Context(), nil))
	})
}

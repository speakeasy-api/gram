package repo_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestSessionCanonicalHooks_UnknownHarness(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	projectID, chatID := uuid.New(), uuid.NewString()
	now := time.Now().UTC()
	insert := func(source, schema, event, legacyEvent, urn, callID string) {
		t.Helper()
		transport, authority, toolName := "ahp", "", "read_file"
		if source != "future-harness" {
			transport = "native"
		}
		if legacyEvent == "model_attempt" {
			authority = "model_attempt"
		}
		if callID == "call-1" {
			toolName = "codex"
		}
		if callID == "call-2" {
			toolName = "cursor"
		}
		attrs, err := json.Marshal(map[string]string{
			"gen_ai.conversation.id": chatID, "gram.hook.source": source,
			"gram.hook.schema": schema, "gram.hook.canonical_event": event,
			"gram.hook.event": legacyEvent, "gram.hook.transport": transport, "gram.hook.mode": "observe", "gram.hook.usage_authority": authority,
			"gen_ai.tool.call.id": callID, "gram.tool.name": toolName,
			"gen_ai.usage.input_tokens": "100", "gen_ai.usage.output_tokens": "20", "gen_ai.usage.cost": "0.25",
		})
		require.NoError(t, err)
		require.NoError(t, conn.Exec(ctx, `INSERT INTO telemetry_logs
   (id, time_unix_nano, observed_time_unix_nano, severity_text, body, attributes, resource_attributes, gram_project_id, gram_urn, service_name)
   VALUES (?, ?, ?, 'INFO', '', ?, '{}', ?, ?, 'unknown-harness')`, uuid.NewString(), now.UnixNano(), now.UnixNano(), string(attrs), projectID, urn))
	}
	insert("future-harness", "hook.ingest.v1", "usage.reported", "model_attempt", "", "")
	insert("future-harness", "hook.ingest.v1", "assistant.responded", "AfterAgentResponse", "", "")
	// Same usage on the companion must not be counted again.
	insert("future-harness", "hook.ingest.v1", "usage.reported", "AfterAgentResponse", "", "")
	insert("future-harness", "hook.ingest.v1", "tool.completed", "", "", "call-1")
	insert("future-harness", "hook.ingest.v1", "tool.failed", "", "", "call-2")
	// Beginnings, skipped/denied observations, untrusted and provider rows do not count.
	insert("future-harness", "hook.ingest.v1", "tool.started", "PostToolUse", "", "call-3")
	insert("future-harness", "hook.ingest.v1", "tool.after", "PostToolUseFailure", "", "call-4")
	insert("future-harness", "", "assistant.responded", "AfterAgentResponse", "", "")
	insert("future-harness", "hook.ingest.v1", "assistant.responded", "AfterAgentResponse", "proxy:usage", "")
	insert("claude-code", "hook.ingest.v1", "assistant.responded", "AfterAgentResponse", "", "")
	insert("claude-code", "hook.ingest.v1", "tool.completed", "PostToolUse", "", "call-5")
	insert("codex", "hook.ingest.v1", "assistant.responded", "AfterAgentResponse", "", "")
	insert("cursor", "hook.ingest.v1", "assistant.responded", "AfterAgentResponse", "", "")

	queries := repo.New(conn)
	for _, window := range []time.Duration{time.Hour, 72 * time.Hour} {
		t.Run(window.String(), func(t *testing.T) {
			rows, err := queries.ListSessions(ctx, repo.ListSessionsParams{
				ProjectIDs: []string{projectID.String()}, TimeStart: now.Add(-window).UnixNano(), TimeEnd: now.Add(time.Minute).UnixNano(), SortBy: "total_tokens", Limit: 10,
			})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, chatID, rows[0].GramChatID)
			require.EqualValues(t, 2, rows[0].ToolCallCount)
			require.EqualValues(t, 1, rows[0].MessageCount)
			require.EqualValues(t, 100, rows[0].TotalInputTokens)
			require.EqualValues(t, 20, rows[0].TotalOutputTokens)
			require.Equal(t, 0.25, rows[0].TotalCost)
		})
	}
	var tokens int64
	var tools uint64
	var cost float64
	require.NoError(t, conn.QueryRow(ctx, `SELECT sumIfMerge(total_input_tokens), uniqExactIfMerge(unique_tool_calls), sumIfMerge(total_cost) FROM attribute_metrics_summaries WHERE gram_project_id = ?`, projectID).Scan(&tokens, &tools, &cost))
	require.EqualValues(t, 100, tokens)
	require.EqualValues(t, 2, tools)
	require.Equal(t, 0.25, cost)
	var failures uint64
	require.NoError(t, conn.QueryRow(ctx, `SELECT sum(failed_tool_call_count) FROM chat_session_summaries WHERE gram_project_id = ?`, projectID).Scan(&failures))
	require.EqualValues(t, 1, failures)
}

// Native AHP authority is independent of the caller-selected harness name.
func TestSessionCanonicalHooks_AHPKnownHarnessNames(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	for _, source := range []string{"claude-code", "claude", "codex", "cursor", "future-harness"} {
		t.Run(source, func(t *testing.T) {
			projectID, chatID := uuid.New(), uuid.NewString()
			now := time.Now().UTC()
			for _, event := range []string{"usage.reported", "assistant.responded", "tool.completed"} {
				attrs, err := json.Marshal(map[string]string{
					"gen_ai.conversation.id": chatID, "gram.hook.source": source,
					"gram.hook.schema": "hook.ingest.v1", "gram.hook.canonical_event": event,
					"gram.hook.transport": "ahp", "gram.hook.mode": "intercept", "gram.hook.usage_authority": "model_attempt",
					"gen_ai.usage.input_tokens": "10", "gen_ai.usage.output_tokens": "5", "gram.tool.name": "codex",
				})
				require.NoError(t, err)
				require.NoError(t, conn.Exec(ctx, `INSERT INTO telemetry_logs
     (id, time_unix_nano, observed_time_unix_nano, severity_text, body, attributes, resource_attributes, gram_project_id, gram_urn, service_name)
     VALUES (?, ?, ?, 'INFO', '', ?, '{}', ?, '', 'ahp')`, uuid.NewString(), now.UnixNano(), now.UnixNano(), string(attrs), projectID))
			}
			for _, window := range []time.Duration{time.Hour, 72 * time.Hour} {
				rows, err := repo.New(conn).ListSessions(ctx, repo.ListSessionsParams{ProjectIDs: []string{projectID.String()}, TimeStart: now.Add(-window).UnixNano(), TimeEnd: now.Add(time.Minute).UnixNano(), SortBy: "total_tokens", Limit: 10})
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.EqualValues(t, 10, rows[0].TotalInputTokens)
				require.EqualValues(t, 5, rows[0].TotalOutputTokens)
				require.EqualValues(t, 1, rows[0].ToolCallCount)
			}
		})
	}
}

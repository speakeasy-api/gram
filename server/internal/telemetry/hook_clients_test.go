package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

type hookClientLog struct {
	orgID   string
	session string
	device  string
	at      time.Time
	curl    bool
	version string
}

func insertHookClientLog(t *testing.T, ctx context.Context, conn clickhouse.Conn, l hookClientLog) {
	t.Helper()

	attrs := map[string]any{
		"gram.org.id":                     l.orgID,
		"gram.device_id":                  l.device,
		"gen_ai.conversation.id":          l.session,
		"user.email":                      "dev@example.com",
		"gram.hook.device.binary_version": l.version,
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	body := "Hook: PreToolUse"
	if l.curl {
		body = "Tool: Bash, Hook: PreToolUse"
	}
	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), l.at.UnixNano(), l.at.UnixNano(), "INFO", body,
		strings.ReplaceAll(uuid.NewString(), "-", ""), nil, string(attrsJSON), "{}",
		uuid.NewString(), nil, "hooks:Bash", "gram-hooks")
	require.NoError(t, err)
}

func TestHookClients_SessionsCountOnlyTheStartDay(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	orgID := "org_" + uuid.NewString()
	today := time.Now().UTC().Truncate(time.Hour)
	yesterday := today.Add(-24 * time.Hour)
	todayDay := today.Format(time.DateOnly)

	// A curl session started yesterday and still running today, and a new
	// binary session started today on a second machine.
	for _, l := range []hookClientLog{
		{orgID: orgID, session: "s-old", device: "d-1", at: yesterday, curl: true, version: ""},
		{orgID: orgID, session: "s-old", device: "d-1", at: today, curl: true, version: ""},
		{orgID: orgID, session: "s-new", device: "d-2", at: today, curl: false, version: "0.3.31"},
	} {
		insertHookClientLog(t, ctx, ti.chConn, l)
	}

	all, err := telemetry.NewHookClients(ti.chConn).Sessions(ctx, 15)
	require.NoError(t, err)
	rows := []telemetry.HookClientSessionRow{}
	for _, row := range all {
		if row.OrganizationID == orgID && row.Day == todayDay {
			rows = append(rows, row)
		}
	}
	require.Len(t, rows, 3)

	byClient := map[string]telemetry.HookClientSessionRow{}
	for _, row := range rows {
		byClient[row.Client] = row
	}
	// Today's total counts only the session that started today, but every
	// machine that sent hooks.
	require.Equal(t, uint64(1), byClient[""].Sessions)
	require.Equal(t, uint64(1), byClient[""].Machines)
	require.Equal(t, uint64(2), byClient[""].MachinesAll)
	require.Equal(t, uint64(0), byClient["curl"].Machines)
	require.Equal(t, uint64(1), byClient["curl"].MachinesAll)
	require.Equal(t, uint64(1), byClient["0.3.31"].Machines)
}

func insertInferenceEvent(t *testing.T, ctx context.Context, conn clickhouse.Conn, orgID, recordID, sessionID, surface, email string, at time.Time) {
	t.Helper()

	err := conn.Exec(ctx, `
		INSERT INTO agent_events (
			organization_id, project_id, occurred_at_unix_nano, observed_at_unix_nano,
			record_id, session_id, event_id, event_type, raw_event_name, source, provider, surface, user_email
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, uuid.NewString(), at.UnixNano(), at.UnixNano(),
		recordID, sessionID, uuid.NewString(), "session", "session.observed", "transcript", "anthropic", surface, email)
	require.NoError(t, err)
}

func TestHookClients_InferenceTrafficDedupesAndSkipsImports(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	orgID := "org_" + uuid.NewString()
	at := time.Now().UTC().Add(-time.Hour)
	day := at.Format(time.DateOnly)

	// One message written twice by a retried observation, a second message in
	// another app, and a Compliance API import that must not count.
	insertInferenceEvent(t, ctx, ti.chConn, orgID, "rec-1", "s-1", "claude-code", "a@example.com", at)
	insertInferenceEvent(t, ctx, ti.chConn, orgID, "rec-1", "s-1", "claude-code", "a@example.com", at)
	insertInferenceEvent(t, ctx, ti.chConn, orgID, "rec-2", "s-2", "claude-chat", "b@example.com", at)
	insertInferenceEvent(t, ctx, ti.chConn, orgID, "rec-3", "claude_chat_import", "claude-chat", "c@example.com", at)

	all, err := telemetry.NewHookClients(ti.chConn).InferenceTraffic(ctx, 15)
	require.NoError(t, err)
	bySurface := map[string]telemetry.InferenceHookTrafficRow{}
	for _, row := range all {
		if row.OrganizationID == orgID && row.Day == day {
			bySurface[row.Surface] = row
		}
	}
	require.Len(t, bySurface, 3)
	require.Equal(t, uint64(2), bySurface[""].Events)
	require.Equal(t, uint64(2), bySurface[""].Users)
	require.Equal(t, uint64(2), bySurface[""].Sessions)
	require.Equal(t, uint64(1), bySurface["claude-code"].Events)
	require.Equal(t, uint64(1), bySurface["claude-chat"].Events)
}

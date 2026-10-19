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

	reader := telemetry.NewHookClients(ti.chConn)
	var rows []telemetry.HookClientSessionRow
	require.Eventually(t, func() bool {
		all, err := reader.Sessions(ctx, 15)
		if err != nil {
			return false
		}
		rows = rows[:0]
		for _, row := range all {
			if row.OrganizationID == orgID && row.Day == todayDay {
				rows = append(rows, row)
			}
		}
		return len(rows) == 3
	}, 10*time.Second, 200*time.Millisecond)

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

func TestHookClients_InferenceTrafficRuns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	_, err := telemetry.NewHookClients(ti.chConn).InferenceTraffic(ctx, 15)
	require.NoError(t, err)
}

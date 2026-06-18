package hooks

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestLogs_PersistsClaudeOTELRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := "claude-session-1"
	traceID := "0af7651916cd43dd8448eb211c80319c"
	spanID := "b7ad6b7169203331"
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	observed := timestamp.Add(2 * time.Second)

	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{
			resourceStrAttr("service.name", "claude-code"),
			resourceStrAttr("host.name", "devbox.local"),
		},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			TimeUnixNano:         new(nanoString(timestamp)),
			ObservedTimeUnixNano: new(nanoString(observed)),
			TraceID:              new(traceID),
			SpanID:               new(spanID),
			Body:                 &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", "dev@example.com"),
				strAttr("organization.id", "claude-org-1"),
				strAttr("prompt.id", "prompt-1"),
				strAttr("model", "claude-opus-4-8"),
			},
		},
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp.Add(time.Second))),
			Body:         &gen.OTELLogBody{StringValue: new("tool event")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("prompt.id", "prompt-2"),
				strAttr("event.name", "tool_call"),
			},
		},
	))
	require.NoError(t, err)

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), claudeOTELLogsURN, timestamp, 2)

	first := logs[1]
	require.Equal(t, timestamp.UnixNano(), first.TimeUnixNano)
	require.Equal(t, observed.UnixNano(), first.ObservedTimeUnixNano)
	require.Equal(t, "api request", first.Body)
	require.NotNil(t, first.TraceID)
	require.Equal(t, traceID, *first.TraceID)
	require.NotNil(t, first.SpanID)
	require.Equal(t, spanID, *first.SpanID)
	require.NotNil(t, first.GramChatID)
	require.Equal(t, sessionID, *first.GramChatID)

	require.Contains(t, first.Attributes, "session")
	require.Contains(t, first.Attributes, sessionID)
	require.Contains(t, first.Attributes, "gen_ai")
	require.Contains(t, first.Attributes, "conversation")
	require.Contains(t, first.Attributes, "prompt-1")
	require.Contains(t, first.Attributes, "claude-opus-4-8")
	require.Contains(t, first.Attributes, "hook")
	require.Contains(t, first.Attributes, "scope")

	require.Contains(t, first.ResourceAttributes, "service")
	require.Contains(t, first.ResourceAttributes, "claude-code")
	require.Contains(t, first.ResourceAttributes, "host")
	require.Contains(t, first.ResourceAttributes, "devbox.local")
}

func TestLogs_PersistsClaudeOTELRecordWithoutSessionID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp)),
			Body:         &gen.OTELLogBody{StringValue: new("no session yet")},
			Attributes: []*gen.OTELAttribute{
				strAttr("prompt.id", "prompt-without-session"),
			},
		},
	))
	require.NoError(t, err)

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), claudeOTELLogsURN, timestamp, 1)
	require.Nil(t, logs[0].GramChatID)

	require.Contains(t, logs[0].Attributes, "prompt-without-session")
	require.NotContains(t, logs[0].Attributes, "conversation")
}

func TestLogs_CodexPayloadContinuesThroughUsagePath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	timestamp := time.Unix(0, 1780468942284000000)

	err := ti.service.Logs(ctx, codexLogsPayload(tokenBearingRecord()))
	require.NoError(t, err)

	codexLogs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), codexUsageMetricsURN, timestamp, 1)
	require.Equal(t, "Codex usage metrics", codexLogs[0].Body)

	require.Never(t, func() bool {
		logs, err := chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      []string{claudeOTELLogsURN},
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) > 0
	}, 300*time.Millisecond, 50*time.Millisecond)
}

func TestShouldTriggerClaudePromptCorrelation(t *testing.T) {
	t.Parallel()

	require.True(t, shouldTriggerClaudePromptCorrelation(map[attr.Key]any{
		attribute.Key("event.name"): "user_prompt",
		attribute.Key("prompt.id"):  "prompt-1",
		attribute.Key("session.id"): "session-1",
	}))

	require.False(t, shouldTriggerClaudePromptCorrelation(map[attr.Key]any{
		attribute.Key("event.name"): "tool_call",
		attribute.Key("prompt.id"):  "prompt-1",
	}))

	require.True(t, shouldTriggerClaudePromptCorrelation(map[attr.Key]any{
		attribute.Key("event.name"): "user_prompt",
	}))
}

func TestExtractSessionMetadataSkipsNilOTELAttributeElements(t *testing.T) {
	t.Parallel()

	payload := claudeLogsPayload(
		[]*gen.OTELResourceAttribute{
			nil,
			resourceStrAttr("service.name", "claude-code"),
		},
		nil,
		&gen.OTELLogRecord{
			Attributes: []*gen.OTELAttribute{
				nil,
				{Key: "empty-value"},
				strAttr("session.id", "claude-session-1"),
				strAttr("user.email", "dev@example.com"),
				strAttr("organization.id", "claude-org-1"),
			},
		},
	)

	var metadata claudeLogMetadata
	require.NotPanics(t, func() {
		metadata = extractSessionMetadata(payload)
	})
	require.Equal(t, "claude-code", metadata.ServiceName)
	require.Equal(t, "claude-session-1", metadata.SessionID)
	require.Equal(t, "dev@example.com", metadata.UserEmail)
	require.Equal(t, "claude-org-1", metadata.ClaudeOrgID)

	require.Empty(t, extractLogData(&gen.OTELLogRecord{Attributes: []*gen.OTELAttribute{nil}}).SessionID)
	require.Empty(t, extractAttributeString([]*gen.OTELAttribute{nil}, "session.id"))
	require.Equal(t, "claude-session-1", extractAttributeString([]*gen.OTELAttribute{
		nil,
		strAttr("session.id", "claude-session-1"),
	}, "session.id"))
}

func enableHookTelemetryLogger(t *testing.T, ctx context.Context, ti *testInstance) *telemetryrepo.Queries {
	t.Helper()

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	enabled := func(context.Context, string) (bool, error) { return true, nil }
	ti.service.telemetryLogger = telemetry.NewLogger(ctx, testenv.NewLogger(t), chConn, enabled, enabled, nil)
	return telemetryrepo.New(chConn)
}

func hookAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return authCtx
}

func waitForHookLogs(t *testing.T, ctx context.Context, client *telemetryrepo.Queries, projectID string, urn string, timestamp time.Time, count int) []telemetryrepo.TelemetryLog {
	t.Helper()

	var logs []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		var err error
		logs, err = client.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: projectID,
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      []string{urn},
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) == count
	}, 2*time.Second, 50*time.Millisecond)

	return logs
}

func claudeLogsPayload(resourceAttrs []*gen.OTELResourceAttribute, scope *gen.OTELScope, records ...*gen.OTELLogRecord) *gen.LogsPayload {
	return &gen.LogsPayload{
		ResourceLogs: []*gen.OTELResourceLog{{
			Resource:  &gen.OTELResource{Attributes: resourceAttrs},
			ScopeLogs: []*gen.OTELScopeLog{{Scope: scope, LogRecords: records}},
		}},
	}
}

func resourceStrAttr(key, val string) *gen.OTELResourceAttribute {
	return &gen.OTELResourceAttribute{Key: key, Value: &gen.OTELAttributeValue{StringValue: new(val)}}
}

func nanoString(ts time.Time) string {
	return strconvFormatInt(ts.UnixNano())
}

func strconvFormatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

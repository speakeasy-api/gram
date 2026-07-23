package hooks

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
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

// A canonical hooks session.started carrying only the device hostname (an
// org-scoped ingest key with no self-reported email) seeds the session cache,
// and the Claude OTEL path stamps that hostname onto the session's rows —
// which is what lets the email dimension fall back to the device for
// company-credential sessions that emit no user identity.
func TestLogs_StampsCachedHostnameOnClaudeRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := "claude-hostname-fallback-" + uuid.NewString()
	hostname := "ci-runner-hostname-test"

	payload := canonicalIngestPayload("claude", "session.started", sessionID)
	payload.Source.Hostname = &hostname
	_, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	err = ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp)),
			Body:         &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("prompt.id", "prompt-hostname-1"),
				strAttr("event.name", "api_request"),
				strAttr("model", "claude-opus-4-8"),
			},
		},
	))
	require.NoError(t, err)

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), claudeOTELLogsURN, timestamp, 1)
	require.Contains(t, logs[0].Attributes, "hostname")
	require.Contains(t, logs[0].Attributes, hostname)
}

func TestLogs_CachesMultiSessionBatchPerSessionWithoutLeakingIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	userID := uuid.NewString()
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, "a@example.com")

	// One export carrying two sessions: A carries an email/org, B (later in the
	// batch) carries neither. B must be cached with an empty identity rather
	// than inheriting A's, and each session must land under its own cache key.
	err := ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("session a api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "claude-session-a"),
				strAttr("user.email", "a@example.com"),
				strAttr("organization.id", "claude-org-a"),
			},
		},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("session b tool event")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "claude-session-b"),
				strAttr("event.name", "tool_call"),
			},
		},
	))
	require.NoError(t, err)

	var sessionA SessionMetadata
	require.NoError(t, ti.service.cache.Get(ctx, sessionCacheKey("claude-session-a"), &sessionA))
	require.Equal(t, "claude-session-a", sessionA.SessionID)
	require.Equal(t, "a@example.com", sessionA.UserEmail)
	require.Equal(t, "claude-org-a", sessionA.ExternalOrgID)
	require.Equal(t, userID, sessionA.UserID)

	var sessionB SessionMetadata
	require.NoError(t, ti.service.cache.Get(ctx, sessionCacheKey("claude-session-b"), &sessionB))
	require.Equal(t, "claude-session-b", sessionB.SessionID)
	require.Empty(t, sessionB.UserEmail)
	require.Empty(t, sessionB.ExternalOrgID)
	require.Empty(t, sessionB.UserID)
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

	var sessions []claudeLogMetadata
	require.NotPanics(t, func() {
		sessions = extractSessionMetadata(payload)
	})
	require.Len(t, sessions, 1)
	require.Equal(t, "claude-code", sessions[0].ServiceName)
	require.Equal(t, "claude-session-1", sessions[0].SessionID)
	require.Equal(t, "dev@example.com", sessions[0].UserEmail)
	require.Equal(t, "claude-org-1", sessions[0].ExternalOrgID)

	require.Empty(t, extractLogData(&gen.OTELLogRecord{Attributes: []*gen.OTELAttribute{nil}}).SessionID)
	require.Empty(t, extractAttributeString([]*gen.OTELAttribute{nil}, "session.id"))
	require.Equal(t, "claude-session-1", extractAttributeString([]*gen.OTELAttribute{
		nil,
		strAttr("session.id", "claude-session-1"),
	}, "session.id"))
}

func TestExtractSessionMetadataKeepsEmailFromEarlierRecordInBatch(t *testing.T) {
	t.Parallel()

	sessionID := "claude-session-batch"

	// Claude Code batches many log records per session, but user.email and
	// organization.id only ride on some event types. The trailing record here
	// carries the session id but no email/org, which must not wipe the values
	// extracted from the earlier api_request record.
	payload := claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", "dev@example.com"),
				strAttr("organization.id", "claude-org-1"),
			},
		},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("tool event")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("event.name", "tool_call"),
			},
		},
	)

	sessions := extractSessionMetadata(payload)
	require.Len(t, sessions, 1)
	require.Equal(t, sessionID, sessions[0].SessionID)
	require.Equal(t, "dev@example.com", sessions[0].UserEmail)
	require.Equal(t, "claude-org-1", sessions[0].ExternalOrgID)
}

func TestExtractSessionMetadataIsolatesIdentityAcrossSessionsInBatch(t *testing.T) {
	t.Parallel()

	// A re-batching OpenTelemetry Collector can place records from multiple
	// sessions in one export. Session A carries an email; session B (emitted
	// later in the batch) carries none. B must keep an empty email rather than
	// inheriting A's identity, and each session must be returned under its own
	// session id.
	payload := claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("session a api request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "claude-session-a"),
				strAttr("user.email", "a@example.com"),
				strAttr("organization.id", "claude-org-a"),
			},
		},
		&gen.OTELLogRecord{
			Body: &gen.OTELLogBody{StringValue: new("session b tool event")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "claude-session-b"),
				strAttr("event.name", "tool_call"),
			},
		},
	)

	sessions := extractSessionMetadata(payload)
	require.Len(t, sessions, 2)

	require.Equal(t, "claude-session-a", sessions[0].SessionID)
	require.Equal(t, "a@example.com", sessions[0].UserEmail)
	require.Equal(t, "claude-org-a", sessions[0].ExternalOrgID)

	require.Equal(t, "claude-session-b", sessions[1].SessionID)
	require.Empty(t, sessions[1].UserEmail)
	require.Empty(t, sessions[1].ExternalOrgID)
}

func TestExtractSessionMetadataCapturesDeviceAndAccountIdentity(t *testing.T) {
	t.Parallel()

	payload := claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "device-identity-session"),
				strAttr("user.email", "dev@example.com"),
				strAttr("organization.id", "ext-org"),
				strAttr("user.account_uuid", "acct-uuid"),
				strAttr("user.account_id", "user_tagged"),
				strAttr("user.id", "device-xyz"),
			},
		},
	)

	sessions := extractSessionMetadata(payload)
	require.Len(t, sessions, 1)
	require.Equal(t, "device-identity-session", sessions[0].SessionID)
	require.Equal(t, "ext-org", sessions[0].ExternalOrgID)
	require.Equal(t, "acct-uuid", sessions[0].ExternalAccountUUID)
	require.Equal(t, "user_tagged", sessions[0].ExternalAccountID)
	require.Equal(t, "device-xyz", sessions[0].DeviceID)
}

func enableHookTelemetryLogger(t *testing.T, ctx context.Context, ti *testInstance) *telemetryrepo.Queries {
	t.Helper()

	enabled := func(context.Context, string) (bool, error) { return true, nil }
	ti.service.telemetryLogger = telemetry.NewLogger(ctx, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.chConn, enabled, enabled, nil)
	return telemetryrepo.New(ti.chConn)
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

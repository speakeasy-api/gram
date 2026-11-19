package mcpfeedback_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const logRecordTopic = "gram.otel.v1.InboundLogRecord"

// noLimits is a recorder that accepts every report.
var noLimits = mcpfeedback.Limits{Caller: nil, Server: nil}

func sampleReport() mcpfeedback.Report {
	return mcpfeedback.Report{
		Content:    "search_logs returned nothing although the Logs page shows traffic.",
		Severity:   mcpfeedback.SeverityDegraded,
		Workaround: "Queried a wider range.",
		Intention:  "Find a failing request.",
		ToolName:   "search_logs",
		Model:      "Claude in Claude Code",
	}
}

func sampleCaller() mcpfeedback.Caller {
	return mcpfeedback.Caller{
		UserID:         "user-1",
		UserEmail:      "priya@acme.example",
		ExternalUserID: "idp|priya",
		ClientName:     "claude-code",
		ClientVersion:  "2.1.0",
	}
}

func storedFeedback(t *testing.T, ctx context.Context, ti *testInstance, server mcpserversrepo.McpServer) []repo.ListFeedbackRow {
	t.Helper()

	rows, err := repo.New(ti.conn).ListFeedback(ctx, repo.ListFeedbackParams{
		ProjectID:       server.ProjectID,
		McpServerID:     server.ID,
		Severity:        pgtype.Text{String: "", Valid: false},
		ToolName:        pgtype.Text{String: "", Valid: false},
		CursorCreatedAt: pgtype.Timestamptz{Valid: false},
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageLimit:       100,
	})
	require.NoError(t, err)
	return rows
}

// queuedFor returns the outbox messages queued for one topic, oldest first.
func queuedFor(t *testing.T, ctx context.Context, ti *testInstance, topic string) []testrepo.ListPublishOutboxRowsRow {
	t.Helper()

	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)

	var queued []testrepo.ListPublishOutboxRowsRow
	for _, row := range rows {
		if row.Topic == topic {
			queued = append(queued, row)
		}
	}
	return queued
}

func recordAttributes(record *otelv1.InboundLogRecord) map[string]string {
	attrs := make(map[string]string, len(record.GetAttributes()))
	for _, kv := range record.GetAttributes() {
		attrs[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	return attrs
}

func TestRecorderOffersTheToolOnlyWhenSwitchedOn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := newTestRecorder(t, ti, noLimits)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, offered, err := recorder.Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.False(t, offered, "a server with no settings does not offer the tool")

	save := func(enabled bool, name, description string) {
		_, err := repo.New(ti.conn).UpsertFeedbackConfig(ctx, repo.UpsertFeedbackConfigParams{
			McpServerID:     server.ID,
			OrganizationID:  authCtx.ActiveOrganizationID,
			ProjectID:       server.ProjectID,
			Enabled:         enabled,
			ToolName:        pgtype.Text{String: name, Valid: name != ""},
			ToolDescription: pgtype.Text{String: description, Valid: description != ""},
			UpdatedByUserID: pgtype.Text{String: "", Valid: false},
		})
		require.NoError(t, err)
	}

	save(true, "", "")
	tool, offered, err := recorder.Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.True(t, offered)
	require.Equal(t, mcpfeedback.DefaultToolName, tool.Name)
	require.Equal(t, mcpfeedback.DefaultToolDescription, tool.Description)

	save(true, "acme_submit_feedback", "Tell Acme what went wrong.")
	tool, offered, err = recorder.Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.True(t, offered)
	require.Equal(t, "acme_submit_feedback", tool.Name)
	require.Equal(t, "Tell Acme what went wrong.", tool.Description)

	_, offered, err = recorder.Tool(ctx, uuid.New(), server.ID)
	require.NoError(t, err)
	require.False(t, offered, "another project does not see this server's tool")

	save(false, "acme_submit_feedback", "Tell Acme what went wrong.")
	_, offered, err = recorder.Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.False(t, offered, "switching the tool off keeps its settings and stops offering it")
}

func TestRecorderSubmitStoresTheReportAndQueuesItsLogRecord(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := newTestRecorder(t, ti, noLimits)
	target := targetOf(t, ctx, server)

	receipt, err := recorder.Submit(ctx, target, sampleCaller(), sampleReport())
	require.NoError(t, err)
	require.Equal(t, "received", receipt.Status)

	rows := storedFeedback(t, ctx, ti, server)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, receipt.ID, row.ID)
	require.Equal(t, target.OrganizationID, row.OrganizationID)
	require.Equal(t, sampleReport().Content, row.Content)
	require.Equal(t, "degraded", row.Severity.String)
	require.Equal(t, "Queried a wider range.", row.Workaround.String)
	require.Equal(t, "Find a failing request.", row.Intention.String)
	require.Equal(t, "search_logs", row.ToolName.String)
	require.Equal(t, "Claude in Claude Code", row.Model.String)
	require.Equal(t, "user-1", row.UserID.String)
	require.Equal(t, "idp|priya", row.ExternalUserID.String)
	require.Equal(t, "claude-code", row.ClientName.String)
	require.Equal(t, "2.1.0", row.ClientVersion.String)

	// The log record is stamped with the feedback provenance source, which is
	// what routes it to the MCP feedback export, under the server's tenant.
	records := queuedFor(t, ctx, ti, logRecordTopic)
	require.Len(t, records, 1)
	var record otelv1.InboundLogRecord
	require.NoError(t, proto.Unmarshal(records[0].Message, &record))
	require.Equal(t, dialect.FeedbackLogSource, record.GetProvenance().GetSource())
	require.Equal(t, target.OrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, server.ProjectID.String(), record.GetProvenance().GetProjectId())
	require.Equal(t, dialect.FeedbackLogScope, record.GetScope().GetName())
	require.Equal(t, receipt.ID.String(), record.GetRecordId())
	require.Equal(t, "speakeasy.mcp_feedback", record.GetEventName())
	require.Equal(t, "WARN", record.GetSeverityText())
	require.Equal(t, sampleReport().Content, record.GetBody().GetStringValue())
	require.Equal(t, map[string]string{
		"speakeasy.mcp_feedback.id":             receipt.ID.String(),
		"log.record.uid":                        receipt.ID.String(),
		"speakeasy.mcp_feedback.server_id":      server.ID.String(),
		"speakeasy.mcp_feedback.severity":       "degraded",
		"speakeasy.mcp_feedback.tool_name":      "search_logs",
		"speakeasy.mcp_feedback.workaround":     "Queried a wider range.",
		"speakeasy.mcp_feedback.intention":      "Find a failing request.",
		"speakeasy.mcp_feedback.model":          "Claude in Claude Code",
		"speakeasy.mcp_feedback.client_name":    "claude-code",
		"speakeasy.mcp_feedback.client_version": "2.1.0",
		"speakeasy.mcp_feedback.user_id":        "user-1",
		"user.email":                            "priya@acme.example",
		"user.id":                               "idp|priya",
	}, recordAttributes(&record))
}

// An anonymous caller's report leaves out everything nobody told the gateway.
func TestRecorderSubmitLeavesOutWhatIsUnknown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := newTestRecorder(t, ti, noLimits)

	anonymous := mcpfeedback.Caller{UserID: "", UserEmail: "", ExternalUserID: "", ClientName: "", ClientVersion: ""}
	praise := mcpfeedback.Report{Content: "The tool descriptions are clear.", Severity: mcpfeedback.SeverityUnspecified, Workaround: "", Intention: "", ToolName: "", Model: ""}
	receipt, err := recorder.Submit(ctx, targetOf(t, ctx, server), anonymous, praise)
	require.NoError(t, err)

	rows := storedFeedback(t, ctx, ti, server)
	require.Len(t, rows, 1)
	require.False(t, rows[0].Severity.Valid)
	require.False(t, rows[0].ToolName.Valid)
	require.False(t, rows[0].UserID.Valid)
	require.False(t, rows[0].UserEmail.Valid)

	records := queuedFor(t, ctx, ti, logRecordTopic)
	require.Len(t, records, 1)
	var record otelv1.InboundLogRecord
	require.NoError(t, proto.Unmarshal(records[0].Message, &record))
	require.Equal(t, "INFO", record.GetSeverityText())
	require.Equal(t, map[string]string{
		"speakeasy.mcp_feedback.id":        receipt.ID.String(),
		"log.record.uid":                   receipt.ID.String(),
		"speakeasy.mcp_feedback.server_id": server.ID.String(),
	}, recordAttributes(&record))
}

func TestRecorderSubmitMapsSeverityToALogLevel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := newTestRecorder(t, ti, noLimits)

	for _, severity := range []mcpfeedback.Severity{mcpfeedback.SeverityBlocked, mcpfeedback.SeverityDegraded, mcpfeedback.SeverityCosmetic, mcpfeedback.SeverityUnspecified} {
		report := sampleReport()
		report.Severity = severity
		_, err := recorder.Submit(ctx, targetOf(t, ctx, server), sampleCaller(), report)
		require.NoError(t, err)
	}

	var levels []string
	for _, queued := range queuedFor(t, ctx, ti, logRecordTopic) {
		var record otelv1.InboundLogRecord
		require.NoError(t, proto.Unmarshal(queued.Message, &record))
		levels = append(levels, record.GetSeverityText())
	}
	require.Equal(t, []string{"ERROR", "WARN", "INFO", "INFO"}, levels)
}

func TestRecorderSubmitMasksSecretsAndPersonalData(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := newTestRecorder(t, ti, noLimits)

	const secret = "sk-proj-abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	report := sampleReport()
	report.Content = "The call failed with key " + secret + " for jane@customer.example."
	report.Workaround = "Retried with " + secret + "."

	_, err := recorder.Submit(ctx, targetOf(t, ctx, server), sampleCaller(), report)
	require.NoError(t, err)

	rows := storedFeedback(t, ctx, ti, server)
	require.Len(t, rows, 1)
	for _, stored := range []string{rows[0].Content, rows[0].Workaround.String} {
		require.NotContains(t, stored, secret)
		require.Contains(t, stored, "[REDACTED]")
	}
	require.NotContains(t, rows[0].Content, "jane@customer.example")

	records := queuedFor(t, ctx, ti, logRecordTopic)
	require.Len(t, records, 1)
	require.NotContains(t, string(records[0].Message), secret)
	require.NotContains(t, string(records[0].Message), "jane@customer.example")
}

// Masking can lengthen a field, and the stored value must still fit its column.
func TestRecorderSubmitCutsMaskedTextBackToItsLimit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, replacingRedactor{}, noLimits, testServiceName, exportOn())

	_, err := recorder.Submit(ctx, targetOf(t, ctx, server), sampleCaller(), sampleReport())
	require.NoError(t, err)

	rows := storedFeedback(t, ctx, ti, server)
	require.Len(t, rows, 1)
	require.Len(t, []rune(rows[0].Content), mcpfeedback.MaxContentLength)
	require.Len(t, []rune(rows[0].Workaround.String), mcpfeedback.MaxWorkaroundLength)
	require.Len(t, []rune(rows[0].ToolName.String), mcpfeedback.MaxReportedToolNameLength)
	require.Len(t, []rune(rows[0].Model.String), mcpfeedback.MaxModelLength)
}

// replacingRedactor stands in for a scan that replaced short text with long markers.
type replacingRedactor struct{}

func (replacingRedactor) Text(_ context.Context, _ string) (string, bool, error) {
	masked := make([]rune, mcpfeedback.MaxContentLength+50)
	for i := range masked {
		masked[i] = 'x'
	}
	return string(masked), true, nil
}

// failingRedactor stands in for a secret scan that could not complete.
type failingRedactor struct{}

func (failingRedactor) Text(context.Context, string) (string, bool, error) {
	return "", false, errors.New("scan failed")
}

func TestRecorderSubmitStoresNothingWhenTheScanFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, failingRedactor{}, noLimits, testServiceName, exportOn())

	_, err := recorder.Submit(ctx, targetOf(t, ctx, server), sampleCaller(), sampleReport())
	require.ErrorContains(t, err, "redact feedback")

	require.Empty(t, storedFeedback(t, ctx, ti, server))
	require.Empty(t, queuedFor(t, ctx, ti, logRecordTopic))
}

// A report is stored only together with its log record.
func TestRecorderSubmitStoresNothingWhenADeliveryCannotBeQueued(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := newTestRecorder(t, ti, noLimits)
	require.NoError(t, testrepo.New(ti.conn).RejectPublishOutboxWritesFixture(ctx))

	_, err := recorder.Submit(ctx, targetOf(t, ctx, server), sampleCaller(), sampleReport())
	require.ErrorContains(t, err, "queue feedback log record")

	require.Empty(t, storedFeedback(t, ctx, ti, server))
}

func TestRecorderLimitsEachCallerAndEachServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	other := seedHostedServer(t, ctx, ti)

	// Every test shares one Redis, so each gets its own buckets.
	store := ratelimit.NewRedisStore(ti.redis)
	limits := mcpfeedback.Limits{
		Caller: ratelimit.New(store, mcpfeedback.CallerLimitName+":"+uuid.NewString(), ratelimit.PerMinute(2)),
		Server: ratelimit.New(store, mcpfeedback.ServerLimitName+":"+uuid.NewString(), ratelimit.PerMinute(3)),
	}
	recorder := newTestRecorder(t, ti, limits)
	target := targetOf(t, ctx, server)

	priya := sampleCaller()
	for range 2 {
		_, err := recorder.Submit(ctx, target, priya, sampleReport())
		require.NoError(t, err)
	}
	_, err := recorder.Submit(ctx, target, priya, sampleReport())
	require.ErrorIs(t, err, mcpfeedback.ErrRateLimited, "the caller's third report in a minute is refused")

	// A refused caller did not use up the server's allowance.
	sam := sampleCaller()
	sam.UserID = "user-2"
	_, err = recorder.Submit(ctx, target, sam, sampleReport())
	require.NoError(t, err)

	// The server has now accepted three, so a caller with room of their own is refused.
	lee := sampleCaller()
	lee.UserID = "user-3"
	_, err = recorder.Submit(ctx, target, lee, sampleReport())
	require.ErrorIs(t, err, mcpfeedback.ErrRateLimited)

	// Limits are per server: the same people can still report on another one.
	_, err = recorder.Submit(ctx, targetOf(t, ctx, other), priya, sampleReport())
	require.NoError(t, err)

	require.Len(t, storedFeedback(t, ctx, ti, server), 3)
	require.Len(t, storedFeedback(t, ctx, ti, other), 1)
}

// A caller nobody can tell apart is bounded by the server's limit alone.
func TestRecorderLimitsAnonymousCallersByServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)

	store := ratelimit.NewRedisStore(ti.redis)
	limits := mcpfeedback.Limits{
		Caller: ratelimit.New(store, mcpfeedback.CallerLimitName+":"+uuid.NewString(), ratelimit.PerMinute(1)),
		Server: ratelimit.New(store, mcpfeedback.ServerLimitName+":"+uuid.NewString(), ratelimit.PerMinute(2)),
	}
	recorder := newTestRecorder(t, ti, limits)
	anonymous := mcpfeedback.Caller{UserID: "", UserEmail: "", ExternalUserID: "", ClientName: "", ClientVersion: ""}

	for range 2 {
		_, err := recorder.Submit(ctx, targetOf(t, ctx, server), anonymous, sampleReport())
		require.NoError(t, err)
	}
	_, err := recorder.Submit(ctx, targetOf(t, ctx, server), anonymous, sampleReport())
	require.ErrorIs(t, err, mcpfeedback.ErrRateLimited)
}

// A credentialed caller from another organization is named nowhere, and still
// has a bucket of its own.
func TestRecorderLimitsACallerItDoesNotName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)

	store := ratelimit.NewRedisStore(ti.redis)
	limits := mcpfeedback.Limits{
		Caller: ratelimit.New(store, mcpfeedback.CallerLimitName+":"+uuid.NewString(), ratelimit.PerMinute(1)),
		Server: ratelimit.New(store, mcpfeedback.ServerLimitName+":"+uuid.NewString(), ratelimit.PerMinute(10)),
	}
	recorder := newTestRecorder(t, ti, limits)
	outsider := mcpfeedback.Caller{UserID: "", UserEmail: "", ExternalUserID: "", ClientName: "", ClientVersion: "", LimitSubject: "user-from-another-org"}

	_, err := recorder.Submit(ctx, targetOf(t, ctx, server), outsider, sampleReport())
	require.NoError(t, err)
	_, err = recorder.Submit(ctx, targetOf(t, ctx, server), outsider, sampleReport())
	require.ErrorIs(t, err, mcpfeedback.ErrRateLimited)

	rows := storedFeedback(t, ctx, ti, server)
	require.Len(t, rows, 1)
	require.False(t, rows[0].UserID.Valid)
	require.False(t, rows[0].ExternalUserID.Valid)
}

func TestRecorderCallAnswersWithAToolResult(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)

	store := ratelimit.NewRedisStore(ti.redis)
	limits := mcpfeedback.Limits{
		Caller: ratelimit.New(store, mcpfeedback.CallerLimitName+":"+uuid.NewString(), ratelimit.PerMinute(1)),
		Server: nil,
	}
	recorder := newTestRecorder(t, ti, limits)
	target := targetOf(t, ctx, server)

	invalid := recorder.Call(ctx, target, sampleCaller(), json.RawMessage(`{"severity":"blocked"}`))
	require.True(t, invalid.IsError)
	require.Equal(t, "Feedback was not recorded: content is required.", invalid.Text)
	require.Nil(t, invalid.Structured)
	require.Empty(t, storedFeedback(t, ctx, ti, server), "a refused report is not stored")

	accepted := recorder.Call(ctx, target, sampleCaller(), json.RawMessage(`{"content":"search_logs timed out.","severity":"blocked","tool":"search_logs"}`))
	require.False(t, accepted.IsError)
	rows := storedFeedback(t, ctx, ti, server)
	require.Len(t, rows, 1)
	require.Equal(t, "Feedback received (reference "+rows[0].ID.String()+"). No follow-up will reach this session.", accepted.Text)
	require.JSONEq(t, `{"id":"`+rows[0].ID.String()+`","status":"received"}`, string(accepted.Structured))

	// An invalid report is refused before it is charged to the caller's limit,
	// so the one above did not count; this second valid one is over it.
	limited := recorder.Call(ctx, target, sampleCaller(), json.RawMessage(`{"content":"Another problem."}`))
	require.True(t, limited.IsError)
	require.Equal(t, "Feedback was sent too often just now. Wait a minute before sending more.", limited.Text)
	require.Nil(t, limited.Structured)
	require.Len(t, storedFeedback(t, ctx, ti, server), 1)
}

func TestRecorderCallDoesNotLeakWhyStoringFailed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, failingRedactor{}, noLimits, testServiceName, exportOn())

	failed := recorder.Call(ctx, targetOf(t, ctx, server), sampleCaller(), json.RawMessage(`{"content":"A problem."}`))
	require.True(t, failed.IsError)
	require.Equal(t, "Feedback could not be recorded right now. Try again later.", failed.Text)
}

// Settings are served from memory while fresh, so a server's tool calls do
// not each read them.
func TestRecorderServesSettingsFromMemoryWhileFresh(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, chatredact.New(), noLimits, testServiceName, exportOn())

	saveFeedbackConfig(t, ctx, ti, server, true)
	_, offered, err := recorder.Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.True(t, offered)

	saveFeedbackConfig(t, ctx, ti, server, false)
	_, offered, err = recorder.Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.True(t, offered, "the settings read a moment ago are still served")
}

// Settings read earlier are served while the database cannot be reached; a
// server whose settings were never read fails instead.
func TestRecorderServesEarlierSettingsWhenTheyCannotBeRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	unread := seedHostedServer(t, ctx, ti)
	// The recorder's own pool, closed below, stands in for a database that
	// stopped answering.
	pool, err := pgxpool.New(ctx, ti.conn.Config().ConnString())
	require.NoError(t, err)
	recorder := mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), pool, chatredact.New(), noLimits, mcpfeedback.WithSettingsFreshFor(0))

	saveFeedbackConfig(t, ctx, ti, server, true)
	_, offered, err := recorder.Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.True(t, offered)

	pool.Close()

	_, offered, err = recorder.Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.True(t, offered)

	_, _, err = recorder.Tool(ctx, unread.ProjectID, unread.ID)
	require.Error(t, err)
}

func saveFeedbackConfig(t *testing.T, ctx context.Context, ti *testInstance, server mcpserversrepo.McpServer, enabled bool) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := repo.New(ti.conn).UpsertFeedbackConfig(ctx, repo.UpsertFeedbackConfigParams{
		McpServerID:     server.ID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       server.ProjectID,
		Enabled:         enabled,
		ToolName:        pgtype.Text{String: "", Valid: false},
		ToolDescription: pgtype.Text{String: "", Valid: false},
		UpdatedByUserID: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
}

// Until the export flag is on, a report is stored but not queued for export.
func TestRecorderSubmitStoresButDoesNotExportWhileTheExportIsHeldBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedHostedServer(t, ctx, ti)
	recorder := mcpfeedback.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, chatredact.New(), noLimits, testServiceName, new(feature.InMemory))

	_, err := recorder.Submit(ctx, targetOf(t, ctx, server), sampleCaller(), sampleReport())
	require.NoError(t, err)
	require.Len(t, storedFeedback(t, ctx, ti, server), 1)
	require.Empty(t, queuedFor(t, ctx, ti, logRecordTopic))
}

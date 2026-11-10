package mcpconnectionattempts

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type publishedLog struct {
	organizationID string
	projectID      string
	recordID       string
	record         log.Record
}

type fakeSink struct {
	err    error
	panics atomic.Int64
	block  chan struct{}

	mu        sync.Mutex
	published []publishedLog
	closed    int
}

func (s *fakeSink) Publish(_ context.Context, organizationID, projectID, recordID string, record log.Record) error {
	if s.block != nil {
		<-s.block
	}
	if s.panics.Add(-1) >= 0 {
		panic("sink panicked")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return s.err
	}
	s.published = append(s.published, publishedLog{organizationID: organizationID, projectID: projectID, recordID: recordID, record: record})
	return nil
}

func (s *fakeSink) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
}

func (s *fakeSink) snapshot() []publishedLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]publishedLog(nil), s.published...)
}

func (s *fakeSink) closes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func newExportRecorder(t *testing.T, conn driver.Conn, sink ExportSink, opts ...RecorderOption) testRecorder {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := newRecorder(testenv.NewLogger(t), provider, conn, logsOn, testConfig(), append([]RecorderOption{WithExport(sink)}, opts...)...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(ctx)
	})
	return testRecorder{Recorder: recorder, reader: reader}
}

func withExportDrain(budget time.Duration) RecorderOption {
	return func(r *Recorder) { r.exporter.drain = budget }
}

func withExportQueue(size int) RecorderOption {
	return func(r *Recorder) { r.exporter.queue = make(chan Row, size) }
}

func notExported(t *testing.T, r testRecorder, reason notExportedReason) int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, r.reader.Collect(t.Context(), &collected))
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != meterNotExported {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value(attr.ReasonKey)
				if value.AsString() == string(reason) {
					total += point.Value
				}
			}
		}
	}
	return total
}

func recordAttributes(record log.Record) map[string]log.Value {
	values := make(map[string]log.Value, record.AttributesLen())
	record.WalkAttributes(func(kv log.KeyValue) bool {
		values[kv.Key] = kv.Value
		return true
	})
	return values
}

func exportTestRow(t *testing.T, what Happened, final bool, opts ...Option) Row {
	t.Helper()

	row, err := RowAt(Begin(testScope()), StepProviderToken, what, final, time.Now(), opts...)
	require.NoError(t, err)
	return row
}

func TestExportRecord_CarriesTheAttemptAndTheCatalogText(t *testing.T) {
	t.Parallel()

	user := urn.NewUserSubject("user-secret-id")
	memberID := uuid.New()
	row := exportTestRow(t, CodeIDPUnavailable, true,
		Provider("https://provider.example.test:8443/oauth"),
		Member(memberID),
		Retries(2),
		Grant("refresh_token"),
		Subject(&user),
		AuthClient(uuid.New()),
		UserSession(uuid.New()),
		RemoteSession(uuid.New()),
		RemoteSessionIssuer(uuid.New()),
		UserSessionIssuer(uuid.New()),
	)
	row.Repeats = 4
	row.ClientFamily, row.ClientVersion = "claude_code", "2.1.0"
	row.DeviceKey = "device-secret"
	row.EventTime = row.AttemptStartedAt.Add(1500 * time.Millisecond)

	record, ok := exportRecord(row)
	require.True(t, ok)

	meaning, known := MeaningOf(CodeIDPUnavailable.String())
	require.True(t, known)
	require.Equal(t, log.SeverityError, record.Severity())
	require.Equal(t, "ERROR", record.SeverityText())
	require.Equal(t, "speakeasy.mcp_connection", record.EventName())
	require.True(t, record.Body().Empty(), "a string body is redacted on destinations that exclude sensitive data")

	got := recordAttributes(record)
	require.Equal(t, []string{
		"error.type",
		"server.address",
		"server.port",
		"speakeasy.mcp_connection.attempt_id",
		"speakeasy.mcp_connection.client_name",
		"speakeasy.mcp_connection.client_version",
		"speakeasy.mcp_connection.duration",
		"speakeasy.mcp_connection.final",
		"speakeasy.mcp_connection.grant_type",
		"speakeasy.mcp_connection.member_id",
		"speakeasy.mcp_connection.outcome",
		"speakeasy.mcp_connection.repeated_failures",
		"speakeasy.mcp_connection.server_id",
		"speakeasy.mcp_connection.server_kind",
		"speakeasy.mcp_connection.step",
		"speakeasy.mcp_connection.step_result",
		"speakeasy.mcp_connection.subject_kind",
		"speakeasy.mcp_connection.user_message",
		"speakeasy.mcp_connection.user_next_step",
		"speakeasy.mcp_connection.who_fixes",
	}, slices.Sorted(maps.Keys(got)))

	require.Equal(t, row.AttemptID.String(), got[string(attr.ExtMcpConnectionAttemptIDKey)].AsString())
	require.True(t, got[string(attr.ExtMcpConnectionFinalKey)].AsBool())
	require.Equal(t, "failed", got[string(attr.ExtMcpConnectionOutcomeKey)].AsString())
	require.Equal(t, CodeIDPUnavailable.String(), got[string(attr.ErrorTypeKey)].AsString())
	require.Equal(t, meaning.WhatHappened, got[string(attr.ExtMcpConnectionUserMessageKey)].AsString())
	require.Equal(t, meaning.WhatToDo, got[string(attr.ExtMcpConnectionUserNextStepKey)].AsString())
	require.Equal(t, string(meaning.WhoFixes), got[string(attr.ExtMcpConnectionWhoFixesKey)].AsString())
	require.Equal(t, StepProviderToken.String(), got[string(attr.ExtMcpConnectionStepKey)].AsString())
	require.Equal(t, row.StepResult, got[string(attr.ExtMcpConnectionStepResultKey)].AsString())
	require.Equal(t, row.ServerKind, got[string(attr.ExtMcpConnectionServerKindKey)].AsString())
	require.Equal(t, row.ServerID, got[string(attr.ExtMcpConnectionServerIDKey)].AsString())
	require.Equal(t, memberID.String(), got[string(attr.ExtMcpConnectionMemberIDKey)].AsString())
	require.Equal(t, "provider.example.test", got[string(attr.ServerAddressKey)].AsString())
	require.Equal(t, int64(8443), got[string(attr.ServerPortKey)].AsInt64())
	require.Equal(t, "refresh_token", got[string(attr.ExtMcpConnectionGrantTypeKey)].AsString())
	require.Equal(t, "claude_code", got[string(attr.ExtMcpConnectionClientNameKey)].AsString())
	require.Equal(t, "2.1.0", got[string(attr.ExtMcpConnectionClientVersionKey)].AsString())
	require.Equal(t, "user", got[string(attr.ExtMcpConnectionSubjectKindKey)].AsString())
	require.Equal(t, int64(4), got[string(attr.ExtMcpConnectionRepeatedFailuresKey)].AsInt64())
	require.InDelta(t, 1.5, got[string(attr.ExtMcpConnectionDurationKey)].AsFloat64(), 0.001)

	for key, value := range got {
		for _, secret := range []string{"user-secret-id", "device-secret", row.AuthClientID, row.UserSessionID, row.RemoteSessionID, row.RemoteSessionIssuerID, row.UserSessionIssuerID} {
			require.NotContains(t, value.String(), secret, key)
		}
	}
}

func TestExportRecord_OneRowAttemptHasNoDuration(t *testing.T) {
	t.Parallel()

	row := exportTestRow(t, CodeIDPUnavailable, true)
	row.EventTime = row.AttemptStartedAt.Add(200 * time.Nanosecond)

	record, ok := exportRecord(row)
	require.True(t, ok)
	require.NotContains(t, recordAttributes(record), string(attr.ExtMcpConnectionDurationKey))
}

func TestExportRecord_FailureElsewhereLeavesOutTheHost(t *testing.T) {
	t.Parallel()

	row := exportTestRow(t, CodeIDPUnavailable, true, Provider("https://login.example.test:8443"), FailedElsewhere())
	require.Equal(t, "login.example.test:8443", row.ProviderHost, "the stored column is unchanged")

	record, ok := exportRecord(row)
	require.True(t, ok)
	got := recordAttributes(record)
	require.NotContains(t, got, string(attr.ServerAddressKey))
	require.NotContains(t, got, string(attr.ServerPortKey))
}

func TestSplitProviderHost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, provider, address string
		port                    int
	}{
		{name: "no port", provider: "https://provider.example.test", address: "provider.example.test", port: 0},
		{name: "ipv6", provider: "https://[2001:db8::1]/", address: "2001:db8::1", port: 0},
		{name: "ipv6 port", provider: "https://[2001:db8::1]:8443/", address: "2001:db8::1", port: 8443},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			address, port := splitProviderHost(NormalizeProviderHost(tc.provider))
			require.Equal(t, tc.address, address)
			require.Equal(t, tc.port, port)
		})
	}
}

func TestWithRowTrace_UsesTheRowTraceID(t *testing.T) {
	t.Parallel()

	row := exportTestRow(t, CodeIDPUnavailable, true)
	row.TraceID = "0123456789abcdef0123456789abcdef"
	require.Equal(t, row.TraceID, trace.SpanContextFromContext(withRowTrace(t.Context(), row)).TraceID().String())

	row.TraceID = ""
	require.False(t, trace.SpanContextFromContext(withRowTrace(t.Context(), row)).TraceID().IsValid())
}

func TestExportSeverity_ByCatalogOutcome(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		code     Code
		final    bool
		severity log.Severity
		text     string
	}{
		{name: "failed", code: CodeIDPUnavailable, final: true, severity: log.SeverityError, text: "ERROR"},
		{name: "blocked", code: CodeNoServerAccess, final: true, severity: log.SeverityWarn, text: "WARN"},
		{name: "declined", code: CodeCanceled, final: true, severity: log.SeverityInfo, text: "INFO"},
		{name: "non-final failed", code: CodeProviderOAuthInvalidClient, final: false, severity: log.SeverityError, text: "ERROR"},
		{name: "non-final blocked", code: CodeNoServerAccess, final: false, severity: log.SeverityWarn, text: "WARN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			severity, text, ok := exportSeverity(exportTestRow(t, tc.code, tc.final))
			require.True(t, ok)
			require.Equal(t, tc.severity, severity)
			require.Equal(t, tc.text, text)
		})
	}
}

func TestExportSeverity_LeavesOutNonFinalRowsNoAdminCanFix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		what Happened
	}{
		{name: "person", what: CodeNeedsSignIn},
		{name: "app", what: CodeRegistrationInvalid},
		{name: "success", what: ResultAccepted},
		{name: "connected with a reason", what: CodeRefreshedUnverified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, ok := exportSeverity(exportTestRow(t, tc.what, false))
			require.False(t, ok)
		})
	}
}

func TestExportRecord_NonFinalRowSaysSoAndHasNoOutcome(t *testing.T) {
	t.Parallel()

	row := exportTestRow(t, CodeUpstreamSignInNotSetUp, false)
	record, ok := exportRecord(row)
	require.True(t, ok)

	got := recordAttributes(record)
	require.Contains(t, got, string(attr.ExtMcpConnectionFinalKey))
	require.False(t, got[string(attr.ExtMcpConnectionFinalKey)].AsBool())
	require.NotContains(t, got, string(attr.ExtMcpConnectionOutcomeKey))
	require.Equal(t, log.SeverityError, record.Severity())
}

func TestExportRecordID_KeepsAnAttemptsRowsApart(t *testing.T) {
	t.Parallel()

	attempt := Begin(testScope())
	at := time.Now()
	firstMember, err := RowAt(attempt, StepDiscoveryMember, CodeUpstreamSignInNotSetUp, false, at, Member(uuid.New()))
	require.NoError(t, err)
	secondMember, err := RowAt(attempt, StepDiscoveryMember, CodeUpstreamSignInNotSetUp, false, at, Member(uuid.New()))
	require.NoError(t, err)
	repeated := firstMember
	repeated.EventTime = at.Add(time.Second)

	require.NotEqual(t, exportRecordID(firstMember), exportRecordID(secondMember))
	require.NotEqual(t, exportRecordID(firstMember), exportRecordID(repeated))
}

func TestExportRows_LeavesOutWhatDidNotFail(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	recorder.exportRows([]Row{
		exportTestRow(t, ResultSignedIn, true),
		exportTestRow(t, ResultAccepted, false),
		exportTestRow(t, CodeNeedsSignIn, false),
	})
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
}

func TestExportRows_NoProjectIsSkippedAndCounted(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	row, err := RowAt(Begin(OrganizationGatewayScope("org-gateway-test", uuid.New(), AppDCR)), StepInboundToken, CodeIDPUnavailable, true, time.Now())
	require.NoError(t, err)
	recorder.exportRows([]Row{row})
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
	require.Equal(t, int64(1), notExported(t, recorder, notExportedNoProject))
}

func TestExportRows_FullQueueCountsAndNeverBlocks(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{block: make(chan struct{})}
	recorder := newExportRecorder(t, &scriptedConn{}, sink, withExportQueue(1))
	t.Cleanup(func() { close(sink.block) })

	rows := make([]Row, 0, 10)
	for range 10 {
		rows = append(rows, exportTestRow(t, CodeIDPUnavailable, true))
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		recorder.exportRows(rows)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		require.Fail(t, "exportRows blocked on a full queue")
	}

	require.GreaterOrEqual(t, notExported(t, recorder, notExportedQueueFull), int64(7))
	require.Zero(t, recorder.droppedTotal(t), "the rows were stored")
}

func TestExport_PublishesUnderTheRowTenantAfterAStoredInsert(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	recorder := newExportRecorder(t, clickhouseConn(t), sink)
	scope := testScope()
	attempt := Begin(scope)

	recorder.End(t.Context(), attempt, StepProviderToken, CodeIDPUnavailable)
	require.NoError(t, recorder.Shutdown(t.Context()))

	published := sink.snapshot()
	require.Len(t, published, 1)
	require.Equal(t, scope.organizationID, published[0].organizationID)
	require.Equal(t, scope.projectID.String(), published[0].projectID)
	require.True(t, strings.HasPrefix(published[0].recordID, attempt.ID().String()+":"+StepProviderToken.String()+":"+CodeIDPUnavailable.String()+":"))
	require.Equal(t, 1, sink.closes())
}

func TestExport_FailedInsertExportsNothing(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	recorder := newExportRecorder(t, &scriptedConn{err: errors.New("clickhouse down")}, sink)

	recorder.End(t.Context(), Begin(testScope()), StepProviderToken, CodeIDPUnavailable)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
	require.Equal(t, int64(1), recorder.dropped(t, dropInsertFailed))
}

func TestExport_LogsOffExportsNothing(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	logsOff := func(context.Context, string) (bool, error) { return false, nil }
	recorder := newRecorder(testenv.NewLogger(t), provider, clickhouseConn(t), logsOff, testConfig(), WithExport(sink))

	recorder.End(t.Context(), Begin(testScope()), StepProviderToken, CodeIDPUnavailable)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
}

func TestExport_OnceAndHeldBackRepeatsCarryTheirCount(t *testing.T) {
	t.Parallel()

	const attempts = 12
	sink := &fakeSink{}
	recorder := newExportRecorder(t, clickhouseConn(t), sink)
	scope := testScope()

	for range attempts {
		recorder.Once(t.Context(), scope, StepMCPRequest, CodeProviderUnavailable)
	}
	require.NoError(t, recorder.Shutdown(t.Context()))

	published := sink.snapshot()
	require.Less(t, len(published), attempts, "repeats fold into the held-back row")
	recordIDs := make(map[string]struct{}, len(published))
	var stood int64
	var folded bool
	for _, item := range published {
		recordIDs[item.recordID] = struct{}{}
		got := recordAttributes(item.record)
		require.NotContains(t, got, string(attr.ExtMcpConnectionDurationKey), "a Once row has no span")
		repeats := got[string(attr.ExtMcpConnectionRepeatedFailuresKey)].AsInt64()
		folded = folded || repeats > 0
		stood += 1 + repeats
	}
	require.True(t, folded)
	require.Equal(t, int64(attempts), stood)
	require.Len(t, recordIDs, len(published))
}

func TestExport_PublishFailureIsNotAStorageLoss(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{err: errors.New("pubsub down")}
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	recorder.exportRows([]Row{exportTestRow(t, CodeIDPUnavailable, true)})
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(1), notExported(t, recorder, notExportedFailed))
	require.Zero(t, recorder.droppedTotal(t))
}

func TestExport_PanickingSinkIsCountedAndWorkersSurvive(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	sink.panics.Store(1)
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	recorder.exportRows([]Row{exportTestRow(t, CodeIDPUnavailable, true), exportTestRow(t, CodeIDPUnavailable, true)})
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Len(t, sink.snapshot(), 1)
	require.Equal(t, int64(1), notExported(t, recorder, notExportedPanic))
	require.Zero(t, recorder.droppedTotal(t))
}

func TestExport_DrainStopsAtItsBudget(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{block: make(chan struct{})}
	recorder := newExportRecorder(t, &scriptedConn{}, sink, withExportDrain(20*time.Millisecond))
	t.Cleanup(func() { close(sink.block) })

	rows := make([]Row, 0, 6)
	for range 6 {
		rows = append(rows, exportTestRow(t, CodeIDPUnavailable, true))
	}
	recorder.exportRows(rows)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	require.NoError(t, recorder.Shutdown(ctx), "the export budget ends before the caller's deadline")
	require.GreaterOrEqual(t, notExported(t, recorder, notExportedShutdown), int64(4))
}

func TestShutdownAndWait_ReturnsWithABlockedSink(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{block: make(chan struct{})}
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	recorder.exportRows([]Row{exportTestRow(t, CodeIDPUnavailable, true), exportTestRow(t, CodeIDPUnavailable, true), exportTestRow(t, CodeIDPUnavailable, true)})

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = recorder.ShutdownAndWait(ctx)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		require.Fail(t, "ShutdownAndWait waited on a stuck export publish")
	}

	close(sink.block)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, sink.closes())
	}, 10*time.Second, 10*time.Millisecond)
}

// PipelineInfra lets the external pipeline test reach this package's containers.
func PipelineInfra() *testenv.Environment { return infra }

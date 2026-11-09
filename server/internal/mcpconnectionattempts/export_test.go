package mcpconnectionattempts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
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
	enabled bool
	err     error

	mu        sync.Mutex
	published []publishedLog
	closed    int
}

func (s *fakeSink) Enabled(context.Context, string) bool { return s.enabled }

func (s *fakeSink) Publish(_ context.Context, organizationID, projectID, recordID string, record log.Record) error {
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

func newExportRecorder(t *testing.T, conn driver.Conn, sink ExportSink) testRecorder {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := newRecorder(testenv.NewLogger(t), provider, conn, logsOn, testConfig(), WithExport(sink))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(ctx)
	})
	return testRecorder{Recorder: recorder, reader: reader}
}

func exportSkipped(t *testing.T, r testRecorder, reason string) int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, r.reader.Collect(t.Context(), &collected))
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != meterExportSkipped {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value(attr.ReasonKey)
				if value.AsString() == reason {
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
		Provider("https://provider.example.test/oauth"),
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
	require.Equal(t, exportEventName, record.EventName())
	require.Equal(t, "MCP connection failed", record.Body().AsString())
	require.NotContains(t, record.Body().AsString(), meaning.WhatHappened, "plain text belongs in attributes")

	got := recordAttributes(record)
	require.Equal(t, row.AttemptID.String(), got[string(attr.ConnectionAttemptIDKey)].AsString())
	require.Equal(t, "failed", got[string(attr.ConnectionOutcomeKey)].AsString())
	require.Equal(t, CodeIDPUnavailable.String(), got[string(attr.ConnectionReasonKey)].AsString())
	require.Equal(t, CodeIDPUnavailable.String(), got[string(attr.ErrorTypeKey)].AsString())
	require.Equal(t, meaning.WhatHappened, got[string(attr.ErrorMessageKey)].AsString())
	require.Equal(t, meaning.WhatToDo, got[string(attr.ConnectionWhatToDoKey)].AsString())
	require.Equal(t, string(meaning.WhoFixes), got[string(attr.ConnectionWhoFixesKey)].AsString())
	require.Equal(t, StepProviderToken.String(), got[string(attr.ConnectionStepKey)].AsString())
	require.Equal(t, row.ServerKind, got[string(attr.ConnectionServerKindKey)].AsString())
	require.Equal(t, row.ServerID, got[string(attr.ConnectionServerIDKey)].AsString())
	require.Equal(t, memberID.String(), got[string(attr.ConnectionMemberIDKey)].AsString())
	require.Equal(t, "provider.example.test", got[string(attr.ServerAddressKey)].AsString())
	require.Equal(t, "refresh_token", got[string(attr.ConnectionGrantTypeKey)].AsString())
	require.Equal(t, "claude_code", got[string(attr.McpClientNameKey)].AsString())
	require.Equal(t, "2.1.0", got[string(attr.McpClientVersionKey)].AsString())
	require.Equal(t, "user", got[string(attr.ConnectionSubjectKindKey)].AsString())
	require.Equal(t, int64(4), got[string(attr.ConnectionRepeatsKey)].AsInt64())
	require.Equal(t, int64(2), got[string(attr.ConnectionRetriesKey)].AsInt64())
	require.InDelta(t, 1.5, got[string(attr.ConnectionDurationKey)].AsFloat64(), 0.001)

	for key, value := range got {
		require.False(t, strings.HasPrefix(key, "speakeasy.") || strings.HasPrefix(key, "directory."), "otelpub refuses %s", key)
		require.False(t, strings.HasPrefix(key, "user."), "no user identity in v0: %s", key)
		for _, secret := range []string{"user-secret-id", "device-secret", row.AuthClientID, row.UserSessionID, row.RemoteSessionID, row.RemoteSessionIssuerID, row.UserSessionIssuerID} {
			require.NotContains(t, value.String(), secret, key)
		}
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

func TestExportSeverity_ByOutcome(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		code     Code
		severity log.Severity
		text     string
	}{
		{name: "failed", code: CodeIDPUnavailable, severity: log.SeverityError, text: "ERROR"},
		{name: "blocked", code: CodeNoServerAccess, severity: log.SeverityWarn, text: "WARN"},
		{name: "declined", code: CodeCanceled, severity: log.SeverityInfo, text: "INFO"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			severity, text, ok := exportSeverity(exportTestRow(t, tc.code, true))
			require.True(t, ok)
			require.Equal(t, tc.severity, severity)
			require.Equal(t, tc.text, text)
		})
	}
}

func TestExportRows_LeavesOutWhatDidNotFail(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{enabled: true}
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	recorder.exportRows([]Row{
		exportTestRow(t, ResultSignedIn, true),
		exportTestRow(t, ResultAccepted, false),
		exportTestRow(t, CodeIDPUnavailable, false),
	})
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
}

func TestExportRows_NoProjectIsSkippedAndCounted(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{enabled: true}
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	row, err := RowAt(Begin(OrganizationGatewayScope("org-gateway-test", uuid.New(), AppDCR)), StepInboundToken, CodeIDPUnavailable, true, time.Now())
	require.NoError(t, err)
	recorder.exportRows([]Row{row})
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
	require.Equal(t, int64(1), exportSkipped(t, recorder, exportSkipNoProject))
}

func TestExportRows_FullQueueCountsAndNeverBlocks(t *testing.T) {
	t.Parallel()

	recorder := newTestRecorder(t, &scriptedConn{}, testConfig())
	queue := make(chan Row, 1)
	recorder.exporter = &exporter{sink: &fakeSink{enabled: true}, queue: queue, workers: sync.WaitGroup{}, skipped: nil}

	rows := []Row{exportTestRow(t, CodeIDPUnavailable, true), exportTestRow(t, CodeIDPUnavailable, true)}
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

	require.Len(t, queue, 1)
	require.Equal(t, int64(1), recorder.dropped(t, dropExportQueueFull))
}

func TestExport_PublishesUnderTheRowTenantAfterAStoredInsert(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{enabled: true}
	recorder := newExportRecorder(t, clickhouseConn(t), sink)
	scope := testScope()
	attempt := Begin(scope)

	recorder.End(t.Context(), attempt, StepProviderToken, CodeIDPUnavailable)
	require.NoError(t, recorder.Shutdown(t.Context()))

	published := sink.snapshot()
	require.Len(t, published, 1)
	require.Equal(t, scope.organizationID, published[0].organizationID)
	require.Equal(t, scope.projectID.String(), published[0].projectID)
	require.Equal(t, attempt.ID().String()+":"+StepProviderToken.String()+":"+CodeIDPUnavailable.String(), published[0].recordID)
	require.Equal(t, 1, sink.closed)
}

func TestExport_FailedInsertExportsNothing(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{enabled: true}
	recorder := newExportRecorder(t, &scriptedConn{err: errors.New("clickhouse down")}, sink)

	recorder.End(t.Context(), Begin(testScope()), StepProviderToken, CodeIDPUnavailable)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
	require.Equal(t, int64(1), recorder.dropped(t, dropInsertFailed))
}

func TestExport_LogsOffExportsNothing(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{enabled: true}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	logsOff := func(context.Context, string) (bool, error) { return false, nil }
	recorder := newRecorder(testenv.NewLogger(t), provider, clickhouseConn(t), logsOff, testConfig(), WithExport(sink))

	recorder.End(t.Context(), Begin(testScope()), StepProviderToken, CodeIDPUnavailable)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
}

func TestExport_FlagOffExportsNothing(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{enabled: false}
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	recorder.exportRows([]Row{exportTestRow(t, CodeIDPUnavailable, true)})
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Empty(t, sink.snapshot())
	require.Equal(t, 1, sink.closed)
}

func TestExport_PublishFailureIsCounted(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{enabled: true, err: errors.New("pubsub down")}
	recorder := newExportRecorder(t, &scriptedConn{}, sink)

	recorder.exportRows([]Row{exportTestRow(t, CodeIDPUnavailable, true)})
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(1), recorder.dropped(t, dropExportFailed))
}

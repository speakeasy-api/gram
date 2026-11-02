package connectionevents

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func testConfig() config {
	return config{
		table:         eventsTable,
		queueSize:     64,
		maxBatch:      8,
		flushInterval: 10 * time.Millisecond,
		insertTimeout: 10 * time.Second,

		featureCheckTimeout: 10 * time.Second,
	}
}

// fixedFeatures answers every logs check the same way.
type fixedFeatures struct {
	enabled bool
	err     error
	block   chan struct{}
	checks  atomic.Int64
}

func (f *fixedFeatures) IsFeatureEnabled(ctx context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	f.checks.Add(1)
	if feature != productfeatures.FeatureLogs {
		return false, errors.New("unexpected feature")
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return false, fmt.Errorf("feature check: %w", ctx.Err())
		}
	}
	return f.enabled, f.err
}

func logsOn() *fixedFeatures {
	return &fixedFeatures{enabled: true, err: nil, block: nil, checks: atomic.Int64{}}
}

type testRecorder struct {
	*Recorder
	reader *sdkmetric.ManualReader
}

func newTestRecorder(t *testing.T, conn driver.Conn, cacheImpl cache.Cache, cfg config) testRecorder {
	t.Helper()
	return newTestRecorderWithFeatures(t, conn, cacheImpl, logsOn(), cfg)
}

func newTestRecorderWithFeatures(t *testing.T, conn driver.Conn, cacheImpl cache.Cache, features FeatureChecker, cfg config) testRecorder {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := newRecorder(testenv.NewLogger(t), provider, conn, cacheImpl, features, cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(ctx)
	})
	return testRecorder{Recorder: recorder, reader: reader}
}

func (r testRecorder) dropped(t *testing.T, reason DropReason) int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, r.reader.Collect(t.Context(), &collected))
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != meterDropped {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value(attr.ConnectionDropReasonKey)
				if value.AsString() == string(reason) {
					total += point.Value
				}
			}
		}
	}
	return total
}

func clickhouseConn(t *testing.T) driver.Conn {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	return conn
}

func redisCache(t *testing.T) cache.Cache {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return cache.NewRedisCacheAdapter(client)
}

type storedRow struct {
	OrganizationID   string    `ch:"organization_id"`
	ProjectID        uuid.UUID `ch:"project_id"`
	AttemptID        uuid.UUID `ch:"attempt_id"`
	AttemptStartedAt time.Time `ch:"attempt_started_at"`
	ServerKind       string    `ch:"server_kind"`
	ServerID         string    `ch:"server_id"`
	Step             string    `ch:"step"`
	StepResult       string    `ch:"step_result"`
	Outcome          string    `ch:"outcome"`
	Reason           string    `ch:"reason"`
	WhoFixes         string    `ch:"who_fixes"`
	IsFinal          bool      `ch:"is_final"`
	SubjectURN       string    `ch:"subject_urn"`
	ProviderHost     string    `ch:"provider_host"`
	AppKey           string    `ch:"app_key"`
}

const selectRows = `
	SELECT organization_id, project_id, attempt_id, attempt_started_at, server_kind, server_id,
	       step, step_result, outcome, reason, who_fixes, is_final, subject_urn, provider_host, app_key
	FROM mcp_connection_events `

func readRows(t *testing.T, conn driver.Conn, attemptID uuid.UUID) []storedRow {
	t.Helper()

	var rows []storedRow
	require.NoError(t, conn.Select(t.Context(), &rows, selectRows+`WHERE attempt_id = ? ORDER BY event_time`, attemptID))
	return rows
}

func TestRecord_StepsLandWithTenantFieldsAndFeedTheRollup(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	recorder := newTestRecorder(t, conn, redisCache(t), testConfig())
	attempt := Begin(testScope())
	subject := urn.NewUserSubject("user-test")

	recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	recorder.End(t.Context(), attempt, StepProviderSignIn, CodeProviderRateLimited,
		Provider("https://Login.Example.com:443/oauth"), Subject(&subject))
	require.NoError(t, recorder.Shutdown(t.Context()))

	rows := readRows(t, conn, attempt.ID())
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, attempt.scope.organizationID, row.OrganizationID)
		require.Equal(t, attempt.scope.projectID, row.ProjectID)
		require.Equal(t, serverKindMCP, row.ServerKind)
		require.Equal(t, attempt.scope.serverID.String(), row.ServerID)
		require.WithinDuration(t, attempt.StartedAt(), row.AttemptStartedAt, time.Microsecond)
		require.Equal(t, AppDCR.key, row.AppKey)
	}
	require.False(t, rows[0].IsFinal)
	require.Empty(t, rows[0].Outcome, "outcome is on the final step only")
	require.Equal(t, ResultAccepted.String(), rows[0].StepResult)

	require.True(t, rows[1].IsFinal)
	require.Equal(t, string(OutcomeFailed), rows[1].Outcome)
	require.Equal(t, "provider_rate_limited", rows[1].Reason)
	require.Equal(t, resultFailed.String(), rows[1].StepResult)
	require.Equal(t, string(WhoProvider), rows[1].WhoFixes)
	require.Equal(t, "login.example.com", rows[1].ProviderHost)
	require.Equal(t, subject.String(), rows[1].SubjectURN)

	var seen, ended, stepEvents uint64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT uniqExactMerge(attempts_seen), uniqExactMerge(attempts_ended), sum(step_events)
		FROM mcp_connection_attempt_summaries
		WHERE organization_id = ? AND project_id = ? AND server_id = ?`,
		attempt.scope.organizationID, attempt.scope.projectID, attempt.scope.serverID.String()).Scan(&seen, &ended, &stepEvents))
	require.Equal(t, uint64(1), seen)
	require.Equal(t, uint64(1), ended)
	require.Equal(t, uint64(2), stepEvents)
}

func TestRecord_OrganizationGatewayStoresNilProject(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	recorder := newTestRecorder(t, conn, redisCache(t), testConfig())
	attempt := Begin(OrganizationGatewayScope("org-gateway-test", uuid.New(), AppDCR))

	recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	require.NoError(t, recorder.Shutdown(t.Context()))

	rows := readRows(t, conn, attempt.ID())
	require.Len(t, rows, 1)
	require.Equal(t, uuid.Nil, rows[0].ProjectID)
	require.Equal(t, "org-gateway-test", rows[0].OrganizationID)
}

func TestOnce_IsOneFinalRow(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	recorder := newTestRecorder(t, conn, redisCache(t), testConfig())
	scope := testScope()

	recorder.Once(t.Context(), scope, StepMCPRequest, CodeInvalidBearerToken)
	require.NoError(t, recorder.Shutdown(t.Context()))

	var rows []storedRow
	require.NoError(t, conn.Select(t.Context(), &rows, selectRows+`WHERE organization_id = ? AND server_id = ?`, scope.organizationID, scope.serverID.String()))
	require.Len(t, rows, 1)
	require.True(t, rows[0].IsFinal)
	require.Equal(t, string(OutcomeFailed), rows[0].Outcome)
	require.Equal(t, string(WhoPerson), rows[0].WhoFixes)
}

// Two replicas can each see the same ending: a resubmitted page, a raced callback.
func TestEnd_SecondEndingIsNotWrittenAcrossReplicas(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	sharedCache := redisCache(t)
	first := newTestRecorder(t, conn, sharedCache, testConfig())
	second := newTestRecorder(t, conn, sharedCache, testConfig())
	attempt := Begin(testScope())

	first.End(t.Context(), attempt, StepInboundConsent, CodeCanceled)
	require.NoError(t, first.Shutdown(t.Context()))
	second.End(t.Context(), attempt, StepInboundToken, ResultCodeIssued)
	second.Step(t.Context(), attempt, StepInboundToken, ResultAccepted)
	require.NoError(t, second.Shutdown(t.Context()))

	rows := readRows(t, conn, attempt.ID())
	require.Len(t, rows, 2, "the later step lands, the later ending does not")
	finals := 0
	for _, row := range rows {
		if row.IsFinal {
			finals++
			require.Equal(t, string(OutcomeDeclined), row.Outcome, "the first ending wins")
		}
	}
	require.Equal(t, 1, finals)
	require.Equal(t, int64(1), second.dropped(t, DropDuplicateEnd))
}

// Losing an ending is worse than a duplicate the rollup counts once anyway.
func TestEnd_FailedEndingCheckStillWritesTheRow(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	recorder := newTestRecorder(t, conn, nil, testConfig())
	attempt := Begin(testScope())

	recorder.End(t.Context(), attempt, StepInboundConsent, CodeCanceled)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Len(t, readRows(t, conn, attempt.ID()), 1)
}

func TestRowAt_OutcomeOnlyOnAFinalRow(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(testScope()), StepDiscoveryMember, CodeProviderRateLimited, false, time.Now())
	require.NoError(t, err)
	require.Empty(t, row.Outcome)
	require.False(t, row.IsFinal)
	require.Equal(t, "provider_rate_limited", row.Reason)
	require.Equal(t, string(WhoProvider), row.WhoFixes, "who fixes it stays on a non-final failure")
}

func TestRowAt_ResultEndsConnectedWithNoReason(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(testScope()), StepProviderRefresh, ResultRefreshed, true, time.Now())
	require.NoError(t, err)
	require.Equal(t, ResultRefreshed.String(), row.StepResult)
	require.Empty(t, row.Reason, "the reason column is empty for success")
	require.Empty(t, row.WhoFixes)
	require.Equal(t, string(OutcomeConnected), row.Outcome)
}

func TestRowAt_CatalogSetsTheFailureResult(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code Code
		want Result
	}{
		{CodeNeedsSignIn, resultNeedsSignIn},
		{CodeConsentNotApproved, resultNeedsConsent},
		{CodeMemberToolsMissing, resultPartial},
		{CodeCanceled, resultDeclined},
		{CodePolicyBlocked, resultPolicyBlocked},
		{CodeRefreshedUnverified, ResultRefreshed},
	}
	for _, tc := range cases {
		t.Run(tc.code.String(), func(t *testing.T) {
			t.Parallel()

			row, err := RowAt(Begin(testScope()), StepMCPRequest, tc.code, false, time.Now())
			require.NoError(t, err)
			require.Equal(t, tc.want.String(), row.StepResult)
		})
	}
}

// Nothing in the catalog can write abandoned: readers derive it.
func TestRecord_AbandonedCannotBeWritten(t *testing.T) {
	t.Parallel()

	for _, code := range Codes() {
		meaning, _ := MeaningOf(code.String())
		require.NotEqual(t, OutcomeAbandoned, meaning.Outcome, code)
	}
}

func TestRowAt_RefusesWhatBreaksTheTableContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		attempt Attempt
		step    Step
		what    Happened
	}{
		{"not started", NoAttempt, StepInboundToken, ResultCodeIssued},
		{"no organization", Begin(ServerScope("", uuid.New(), uuid.New(), AppDCR)), StepInboundToken, ResultCodeIssued},
		{"no server", Begin(ServerScope("org-test", uuid.New(), uuid.Nil, AppDCR)), StepInboundToken, ResultCodeIssued},
		{"zero step", Begin(testScope()), Step{}, ResultCodeIssued},
		{"nothing happened", Begin(testScope()), StepInboundToken, nil},
		{"zero result", Begin(testScope()), StepInboundToken, Result{}},
		{"zero code", Begin(testScope()), StepInboundToken, Code{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := RowAt(tc.attempt, tc.step, tc.what, true, time.Now())
			require.ErrorIs(t, err, errInvalidEvent)
		})
	}
}

// A raw provider message never reaches a label column, and the row is kept.
func TestRowAt_RawTextInALabelIsLeftOut(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(testScope()), StepProviderToken, CodeProviderServerError, true, time.Now(),
		Detail("upstream said: invalid token abc123 for user a@example.com"), Grant("not a grant type"))
	require.NoError(t, err)
	require.Empty(t, row.SubReason)
	require.Empty(t, row.GrantType)
	require.Equal(t, "provider_server_error", row.Reason)

	row, err = RowAt(Begin(testScope()), StepMCPRequest, CodeInvalidBearerToken, true, time.Now(), Detail("org_lookup_failed"), Grant("authorization_code"))
	require.NoError(t, err)
	require.Equal(t, "org_lookup_failed", row.SubReason)
	require.Equal(t, "authorization_code", row.GrantType)
}

func TestOptions_AreSafeOnNilAndZero(t *testing.T) {
	t.Parallel()

	row, err := RowAt(Begin(testScope()), StepMCPRequest, ResultAllowed, false, time.Now(),
		nil, Subject(nil), Member(uuid.Nil), Provider(""), RemoteSession(uuid.Nil), UserSession(uuid.Nil), AuthClient(uuid.Nil))
	require.NoError(t, err)
	require.Empty(t, row.SubjectURN)
	require.Empty(t, row.MemberID)
	require.Empty(t, row.ProviderHost)
	require.Empty(t, row.RemoteSessionID)
}

func TestRecord_InvalidEventIsCountedNotWritten(t *testing.T) {
	t.Parallel()

	conn := &scriptedConn{Conn: nil, block: nil, err: nil, prepares: atomic.Int64{}}
	recorder := newTestRecorder(t, conn, cache.NoopCache, testConfig())

	recorder.End(t.Context(), NoAttempt, StepInboundToken, ResultCodeIssued)
	recorder.Step(t.Context(), Begin(testScope()), StepInboundToken, nil)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(2), recorder.dropped(t, DropInvalidEvent))
	require.Zero(t, conn.prepares.Load())
}

// A code-first rollout reaches real ClickHouse before the migration does.
func TestRecord_MissingTableDropsAndCounts(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.table = "mcp_connection_events_not_migrated_yet"
	recorder := newTestRecorder(t, clickhouseConn(t), redisCache(t), cfg)
	attempt := Begin(testScope())

	for range 3 {
		recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	}
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(3), recorder.dropped(t, DropTableMissing))
}

// scriptedConn stands in for a ClickHouse that stalls or refuses. Real
// ClickHouse cannot be made to stall on demand from a test.
type scriptedConn struct {
	driver.Conn

	block    chan struct{}
	err      error
	prepares atomic.Int64
}

func (c *scriptedConn) PrepareBatch(ctx context.Context, _ string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
	c.prepares.Add(1)
	if c.block != nil {
		select {
		case <-c.block:
		case <-ctx.Done():
			return nil, fmt.Errorf("prepare batch: %w", ctx.Err())
		}
	}
	if c.err == nil {
		return nil, errors.New("scripted conn writes nothing")
	}
	return nil, c.err
}

func TestRecord_NeverBlocksWhileClickHouseStalls(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	conn := &scriptedConn{Conn: nil, block: release, err: errors.New("stalled"), prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.queueSize = 4
	cfg.maxBatch = 1
	cfg.insertTimeout = time.Minute
	recorder := newTestRecorder(t, conn, cache.NoopCache, cfg)
	attempt := Begin(testScope())

	require.Eventually(t, func() bool {
		recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
		return conn.prepares.Load() == 1
	}, 10*time.Second, time.Millisecond)

	before := recorder.dropped(t, DropQueueFull)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for range 1000 {
			recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
		}
	}()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("Step blocked on a stalled ClickHouse")
	}
	require.GreaterOrEqual(t, recorder.dropped(t, DropQueueFull)-before, int64(1000-cfg.queueSize))

	close(release)
}

func TestRecord_StalledInsertIsAbandonedAtItsTimeout(t *testing.T) {
	t.Parallel()

	conn := &scriptedConn{Conn: nil, block: make(chan struct{}), err: nil, prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.insertTimeout = 20 * time.Millisecond
	recorder := newTestRecorder(t, conn, cache.NoopCache, cfg)

	recorder.Step(t.Context(), Begin(testScope()), StepInboundAuthorize, ResultAccepted)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(1), recorder.dropped(t, DropInsertFailed))
}

// A retried insert would double count step_events, so a failed batch gets one try.
func TestRecord_FailedInsertIsNotRetried(t *testing.T) {
	t.Parallel()

	conn := &scriptedConn{Conn: nil, block: nil, err: errors.New("refused"), prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.maxBatch = 2
	cfg.flushInterval = time.Hour
	recorder := newTestRecorder(t, conn, cache.NoopCache, cfg)

	attempt := Begin(testScope())
	for range 2 {
		recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	}
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(1), conn.prepares.Load())
	require.Equal(t, int64(2), recorder.dropped(t, DropInsertFailed))
}

func TestRecord_AfterShutdownDropsAndCounts(t *testing.T) {
	t.Parallel()

	conn := &scriptedConn{Conn: nil, block: nil, err: nil, prepares: atomic.Int64{}}
	recorder := newTestRecorder(t, conn, cache.NoopCache, testConfig())
	require.NoError(t, recorder.Shutdown(t.Context()))

	recorder.Step(t.Context(), Begin(testScope()), StepInboundAuthorize, ResultAccepted)

	require.Equal(t, int64(1), recorder.dropped(t, DropShutdown))
}

func TestRecord_NilRecorderDoesNothing(t *testing.T) {
	t.Parallel()

	var recorder *Recorder
	recorder.Step(t.Context(), NoAttempt, StepInboundToken, ResultCodeIssued)
	recorder.End(t.Context(), NoAttempt, StepInboundToken, nil)
	recorder.Once(t.Context(), testScope(), StepInboundToken, CodeCanceled, nil)
	bound := recorder.End
	bound(t.Context(), NoAttempt, StepInboundToken, ResultCodeIssued)
	require.NoError(t, recorder.Shutdown(t.Context()))
}

func TestRecord_MetricHasItsTagsAndNoPersonOrOrg(t *testing.T) {
	t.Parallel()

	conn := &scriptedConn{Conn: nil, block: nil, err: errors.New("refused"), prepares: atomic.Int64{}}
	recorder := newTestRecorder(t, conn, cache.NoopCache, testConfig())
	subject := urn.NewUserSubject("user-test")

	recorder.End(t.Context(), Begin(testScope()), StepProviderSignIn, CodeProviderRateLimited,
		Provider("https://login.example.com/oauth"), Subject(&subject))
	require.NoError(t, recorder.Shutdown(t.Context()))

	var collected metricdata.ResourceMetrics
	require.NoError(t, recorder.reader.Collect(t.Context(), &collected))
	var keys map[string]string
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != meterSteps {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			keys = map[string]string{}
			for _, kv := range sum.DataPoints[0].Attributes.ToSlice() {
				keys[string(kv.Key)] = kv.Value.AsString()
			}
		}
	}
	require.Equal(t, map[string]string{
		string(attr.ConnectionOutcomeKey):      string(OutcomeFailed),
		string(attr.ConnectionStepKey):         StepProviderSignIn.String(),
		string(attr.ConnectionReasonKey):       "provider_rate_limited",
		string(attr.ConnectionWhoFixesKey):     string(WhoProvider),
		string(attr.ConnectionProviderHostKey): "login.example.com",
	}, keys)
}

func stepsCounted(t *testing.T, recorder testRecorder) int {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, recorder.reader.Collect(t.Context(), &collected))
	total := 0
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == meterSteps {
				total++
			}
		}
	}
	return total
}

// recordAttemptWithFeatures runs one attempt through real ClickHouse and returns
// how many event and rollup rows it left.
func recordAttemptWithFeatures(t *testing.T, features FeatureChecker, organizationID string) (testRecorder, uint64, uint64) {
	t.Helper()

	conn := clickhouseConn(t)
	recorder := newTestRecorderWithFeatures(t, conn, redisCache(t), features, testConfig())
	scope := ServerScope(organizationID, uuid.New(), uuid.New(), AppDCR)
	attempt := Begin(scope)
	recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	recorder.End(t.Context(), attempt, StepInboundConsent, CodeCanceled)
	recorder.Once(t.Context(), scope, StepMCPRequest, CodeInvalidBearerToken)
	require.NoError(t, recorder.Shutdown(t.Context()))

	var events, rollup uint64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count() FROM mcp_connection_events WHERE organization_id = ?`, organizationID).Scan(&events))
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count() FROM mcp_connection_attempt_summaries WHERE organization_id = ?`, organizationID).Scan(&rollup))
	return recorder, events, rollup
}

func realFeatures(t *testing.T) *productfeatures.Client {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, client)
}

func TestLogs_OnWrites(t *testing.T) {
	t.Parallel()

	features := realFeatures(t)
	organizationID := "org-logs-on-" + uuid.NewString()
	require.NoError(t, features.SetFeatureEnabled(t.Context(), organizationID, productfeatures.FeatureLogs, true))

	recorder, events, rollup := recordAttemptWithFeatures(t, features, organizationID)
	require.Equal(t, uint64(3), events)
	require.NotZero(t, rollup)
	require.Zero(t, recorder.dropped(t, DropLogsOff))
}

func TestLogs_TurnedOffWritesNothing(t *testing.T) {
	t.Parallel()

	features := realFeatures(t)
	organizationID := "org-logs-off-" + uuid.NewString()
	require.NoError(t, features.SetFeatureEnabled(t.Context(), organizationID, productfeatures.FeatureLogs, true))
	require.NoError(t, features.SetFeatureEnabled(t.Context(), organizationID, productfeatures.FeatureLogs, false))

	recorder, events, rollup := recordAttemptWithFeatures(t, features, organizationID)
	require.Zero(t, events)
	require.Zero(t, rollup)
	require.Equal(t, int64(3), recorder.dropped(t, DropLogsOff))
	require.Zero(t, stepsCounted(t, recorder), "no step metric for an organization with logs off")
}

func TestLogs_NeverConfiguredWritesNothing(t *testing.T) {
	t.Parallel()

	organizationID := "org-logs-unset-" + uuid.NewString()
	recorder, events, rollup := recordAttemptWithFeatures(t, realFeatures(t), organizationID)
	require.Zero(t, events)
	require.Zero(t, rollup)
	require.Equal(t, int64(3), recorder.dropped(t, DropLogsOff))
}

// A failed check never guesses on, and the caller never sees it.
func TestLogs_LookupErrorWritesNothing(t *testing.T) {
	t.Parallel()

	failing := &fixedFeatures{enabled: true, err: errors.New("lookup failed"), block: nil, checks: atomic.Int64{}}
	organizationID := "org-logs-error-" + uuid.NewString()
	recorder, events, rollup := recordAttemptWithFeatures(t, failing, organizationID)
	require.Zero(t, events)
	require.Zero(t, rollup)
	require.Equal(t, int64(3), recorder.dropped(t, DropLogsCheck))
}

func TestLogs_NoCheckerWritesNothing(t *testing.T) {
	t.Parallel()

	organizationID := "org-logs-nil-" + uuid.NewString()
	recorder, events, _ := recordAttemptWithFeatures(t, nil, organizationID)
	require.Zero(t, events)
	require.Equal(t, int64(3), recorder.dropped(t, DropLogsOff))
}

func TestLogs_SlowLookupDoesNotBlockTheRequestPath(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	defer close(release)
	slow := &fixedFeatures{enabled: true, err: nil, block: release, checks: atomic.Int64{}}
	conn := &scriptedConn{Conn: nil, block: nil, err: errors.New("refused"), prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.queueSize = 4
	cfg.featureCheckTimeout = time.Minute
	recorder := newTestRecorderWithFeatures(t, conn, cache.NoopCache, slow, cfg)
	attempt := Begin(testScope())

	require.Eventually(t, func() bool {
		recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
		return slow.checks.Load() == 1
	}, 10*time.Second, time.Millisecond)

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for range 1000 {
			recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
		}
	}()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("Step waited on the logs check")
	}
	require.Zero(t, conn.prepares.Load())
}

// A check that runs out of time means do not write.
func TestLogs_LookupTimeoutWritesNothing(t *testing.T) {
	t.Parallel()

	slow := &fixedFeatures{enabled: true, err: nil, block: make(chan struct{}), checks: atomic.Int64{}}
	conn := &scriptedConn{Conn: nil, block: nil, err: nil, prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.featureCheckTimeout = 20 * time.Millisecond
	recorder := newTestRecorderWithFeatures(t, conn, cache.NoopCache, slow, cfg)

	recorder.Step(t.Context(), Begin(testScope()), StepInboundAuthorize, ResultAccepted)
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(1), recorder.dropped(t, DropLogsCheck))
	require.Zero(t, conn.prepares.Load())
}

// Busy traffic for one organization costs one lookup per flush, not one per row.
func TestLogs_CheckedOncePerFlush(t *testing.T) {
	t.Parallel()

	features := logsOn()
	conn := &scriptedConn{Conn: nil, block: nil, err: errors.New("refused"), prepares: atomic.Int64{}}
	cfg := testConfig()
	cfg.maxBatch = 1000
	cfg.flushInterval = time.Hour
	recorder := newTestRecorderWithFeatures(t, conn, cache.NoopCache, features, cfg)

	attempt := Begin(testScope())
	for range 20 {
		recorder.Step(t.Context(), attempt, StepInboundAuthorize, ResultAccepted)
	}
	require.NoError(t, recorder.Shutdown(t.Context()))

	require.Equal(t, int64(1), features.checks.Load())
}

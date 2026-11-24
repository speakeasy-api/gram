package mcpconnectionattempts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// storedServed reads an organization's connections as a reader does: the latest times per connection.
func storedServed(t *testing.T, conn driver.Conn, organizationID string) []Served {
	t.Helper()
	rows, err := conn.Query(t.Context(), `
		SELECT organization_id, project_id, server_kind, server_id, member_id, subject_urn, app_key, client_family,
			remote_session_client_id, max(last_served_at) AS served, max(last_answered_at) AS answered
		FROM `+lastServedTable+` WHERE organization_id = ?
		GROUP BY organization_id, project_id, server_kind, server_id, member_id, subject_urn, app_key, client_family, remote_session_client_id
		ORDER BY subject_urn, client_family, member_id, remote_session_client_id`, organizationID)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var out []Served
	for rows.Next() {
		var s Served
		require.NoError(t, rows.Scan(&s.OrganizationID, &s.ProjectID, &s.ServerKind, &s.ServerID, &s.MemberID, &s.SubjectURN, &s.AppKey,
			&s.ClientFamily, &s.RemoteSessionClientID, &s.ServedAt, &s.AnsweredAt))
		if s.AnsweredAt.Unix() <= 0 {
			s.AnsweredAt = time.Time{}
		}
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

func servedScope() (Scope, string, uuid.UUID) {
	org, server := "org-"+uuid.NewString(), uuid.New()
	return ServerScope(org, uuid.New(), server, AppDCR), org, server
}

func requestFrom(userAgent string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("User-Agent", userAgent)
	return r
}

func flushed(t *testing.T, r testRecorder) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, r.Shutdown(ctx))
}

func TestServed_WritesTheConnectionOnce(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	r := newTestRecorder(t, conn, testConfig())
	scope, org, server := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())
	member, service := uuid.New(), uuid.New()

	before := time.Now()
	for range 3 {
		r.Served(t.Context(), scope, true, Subject(&person), Member(member), RemoteSessionClient(service), FromRequest(requestFrom("claude-code/2.1.0")))
	}
	flushed(t, r)

	got := storedServed(t, conn, org)
	require.Len(t, got, 1)
	require.Equal(t, serverKindMCP, got[0].ServerKind)
	require.Equal(t, server.String(), got[0].ServerID)
	require.Equal(t, member.String(), got[0].MemberID)
	require.Equal(t, person.String(), got[0].SubjectURN)
	require.Equal(t, AppDCR.String(), got[0].AppKey)
	require.Equal(t, FamilyClaudeCode, got[0].ClientFamily)
	require.Equal(t, service.String(), got[0].RemoteSessionClientID)
	require.False(t, got[0].ServedAt.Before(before.Truncate(time.Second)))
	require.Equal(t, got[0].ServedAt, got[0].AnsweredAt)
	require.Equal(t, int64(1), r.counted(t, meterServedWritten, ""), "requests within the interval write one row")
}

// A request Speakeasy answered itself does not say the server answered, and one the server answered is written even within the interval.
func TestServed_TheServersAnswerIsKeptApart(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	r := newTestRecorder(t, conn, testConfig())
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, false, Subject(&person))
	r.Served(t.Context(), scope, false, Subject(&person))
	listed := urn.NewUserSubject("user-" + uuid.NewString())
	r.Served(t.Context(), scope, false, Subject(&listed))
	r.Served(t.Context(), scope, true, Subject(&listed))
	flushed(t, r)

	got := map[string]Served{}
	for _, s := range storedServed(t, conn, org) {
		got[s.SubjectURN] = s
	}
	require.Len(t, got, 2)
	require.False(t, got[person.String()].ServedAt.IsZero())
	require.True(t, got[person.String()].AnsweredAt.IsZero())
	require.False(t, got[listed.String()].AnsweredAt.IsZero())
}

// Two apps registered the same way are told apart by their client, so one does not hold the other's write back.
func TestServed_TwoClientsOfOneAppKindAreTwoConnections(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	r := newTestRecorder(t, conn, testConfig())
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person), FromRequest(requestFrom("claude-code/2.1.0")))
	r.Served(t.Context(), scope, true, Subject(&person), FromRequest(requestFrom("codex-cli-rs/0.40.0")))
	flushed(t, r)

	got := storedServed(t, conn, org)
	require.Len(t, got, 2)
	require.Equal(t, FamilyClaudeCode, got[0].ClientFamily)
	require.Equal(t, FamilyCodexCLI, got[1].ClientFamily)
}

func TestServed_WritesNothingWithoutAPersonOrAServer(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	r := newTestRecorder(t, conn, testConfig())
	scope, org, _ := servedScope()
	anonymous := urn.NewAnonymousSubject(uuid.NewString())
	person := urn.NewUserSubject("user-" + uuid.NewString())
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	r.Served(t.Context(), scope, true)
	r.Served(t.Context(), scope, true, Subject(&anonymous))
	r.Served(t.Context(), ProviderSessionScope(org, uuid.New(), AppBackground), true, Subject(&person))
	r.Served(canceled, scope, true, Subject(&person))
	var none *Recorder
	none.Served(t.Context(), scope, true, Subject(&person))
	flushed(t, r)

	require.Empty(t, storedServed(t, conn, org))
}

func TestServed_WritesNothingWithLogsOff(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	r := newTestRecorderWithLogs(t, conn, func(context.Context, string) (bool, error) { return false, nil }, testConfig())
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person))
	flushed(t, r)

	require.Empty(t, storedServed(t, conn, org))
	require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropLogsOff))
	require.Zero(t, r.droppedTotal(t), "a served connection is not a connection event")
}

func TestServed_ACapturingRecorderHandsTheConnectionOver(t *testing.T) {
	t.Parallel()
	var got []Served
	r := NewCapturing(func(Row) { t.Error("a served request is not a row") }, func(s Served) { got = append(got, s) }, logsOn)
	scope, _, server := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person), FromRequest(requestFrom("claude-code/2.1.0")))
	r.Served(t.Context(), scope, false, Subject(&person))

	require.Len(t, got, 2)
	require.Equal(t, server.String(), got[0].ServerID)
	require.Equal(t, FamilyClaudeCode, got[0].ClientFamily)
	require.False(t, got[0].AnsweredAt.IsZero())
	require.True(t, got[1].AnsweredAt.IsZero())
}

func servedKeyOf(subject string) servedKey {
	return servedKey{organizationID: "org", serverKind: serverKindMCP, serverID: "server", memberID: "", subjectURN: subject, appKey: "dcr", remoteSessionClientID: "", userAgent: ""}
}

func servedAt(at time.Time, answered bool) servedRequest {
	req := servedRequest{projectID: uuid.Nil, at: at, answeredAt: time.Time{}, client: requestClient{present: false, userAgent: "", ipAddress: "", appFamily: "", reported: nil}}
	if answered {
		req.answeredAt = at
	}
	return req
}

func TestServedConnections_HoldsAConnectionToOneWritePerInterval(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 10, 10)
	key, start := servedKeyOf("user:a"), time.Unix(1_000_000, 0).UTC()

	s.note(key, servedAt(start, false))
	s.note(key, servedAt(start.Add(30*time.Second), false))
	pending, lost := s.take(start.Add(30*time.Second), false)
	require.Zero(t, lost)
	require.Len(t, pending, 1)
	require.Equal(t, start, pending[key].at, "a request within the interval is not due")
	require.True(t, pending[key].answeredAt.IsZero())

	s.note(key, servedAt(start.Add(40*time.Second), true))
	s.note(key, servedAt(start.Add(50*time.Second), true))
	pending, _ = s.take(start.Add(50*time.Second), false)
	require.Equal(t, start.Add(40*time.Second), pending[key].at, "the server's first answer is due within the interval")
	require.Equal(t, start.Add(40*time.Second), pending[key].answeredAt, "a second answer within the interval is not")

	s.note(key, servedAt(start.Add(90*time.Second), false))
	pending, _ = s.take(start.Add(90*time.Second), false)
	require.Empty(t, pending, "the interval counts from the last write")

	s.note(key, servedAt(start.Add(101*time.Second), false))
	pending, _ = s.take(start.Add(101*time.Second), false)
	require.Equal(t, start.Add(101*time.Second), pending[key].at, "due once the interval passed")
	require.Equal(t, start.Add(50*time.Second), pending[key].answeredAt, "with the answer held back")
}

// Answered, then a failure, then a retry answered within the interval: the retry is written once the interval passes.
func TestServedConnections_ARecoveryWithinTheIntervalIsWritten(t *testing.T) {
	t.Parallel()
	s := newServedConnections(5*time.Minute, 10, 10)
	key, t0 := servedKeyOf("user:a"), time.Unix(1_000_000, 0).UTC()

	s.note(key, servedAt(t0, true))
	pending, _ := s.take(t0.Add(10*time.Second), false)
	require.Equal(t, t0, pending[key].answeredAt)

	s.note(key, servedAt(t0.Add(3*time.Minute), true))
	pending, _ = s.take(t0.Add(3*time.Minute+10*time.Second), false)
	require.Empty(t, pending, "not before the interval")
	pending, _ = s.take(t0.Add(5*time.Minute), false)
	require.Equal(t, t0.Add(3*time.Minute), pending[key].at)
	require.Equal(t, t0.Add(3*time.Minute), pending[key].answeredAt)

	pending, _ = s.take(t0.Add(time.Hour), false)
	require.Empty(t, pending, "nothing left to write")
}

// Requests every few seconds write about once an interval, and the stored time never trails the latest request by more than one.
func TestServedConnections_SteadyRequestsAreBoundedAndCurrent(t *testing.T) {
	t.Parallel()
	const interval, flushEvery, requestEvery, requestsFor = 5 * time.Minute, 10 * time.Second, 3 * time.Second, 12 * time.Minute
	s := newServedConnections(interval, 10, 10)
	key, t0 := servedKeyOf("user:a"), time.Unix(1_000_000, 0).UTC()

	var stored, lastRequest time.Time
	writes := 0
	for elapsed := time.Duration(0); elapsed <= requestsFor+2*interval; elapsed += time.Second {
		now := t0.Add(elapsed)
		if elapsed <= requestsFor && elapsed%requestEvery == 0 {
			s.note(key, servedAt(now, true))
			lastRequest = now
		}
		if elapsed%flushEvery != 0 {
			continue
		}
		pending, _ := s.take(now, false)
		if waiting, ok := pending[key]; ok {
			writes++
			require.True(t, waiting.at.After(stored), "each write moves the time forward")
			stored = waiting.at
			require.Equal(t, stored, waiting.answeredAt)
		}
		require.LessOrEqual(t, lastRequest.Sub(stored), interval, "at %s", elapsed)
	}
	require.Equal(t, lastRequest, stored, "the last request is written after they stop")
	require.LessOrEqual(t, writes, 1+int(requestsFor/interval)+1)
}

func TestServedConnections_AConnectionThatStopsIsWrittenByALaterFlush(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 10, 10)
	key, at := servedKeyOf("user:a"), time.Unix(1_000_000, 0).UTC()

	s.note(key, servedAt(at, false))
	s.note(key, servedAt(at.Add(20*time.Second), false))
	pending, _ := s.take(at.Add(20*time.Second), false)
	require.Equal(t, at, pending[key].at)
	pending, _ = s.take(at.Add(50*time.Second), false)
	require.Empty(t, pending)
	pending, _ = s.take(at.Add(time.Minute), false)
	require.Equal(t, at.Add(20*time.Second), pending[key].at)
	require.True(t, pending[key].answeredAt.IsZero())
}

func TestServedConnections_TheFinalFlushTakesEveryUnwrittenTime(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 10, 10)
	key, at := servedKeyOf("user:a"), time.Unix(1_000_000, 0).UTC()

	s.note(key, servedAt(at, false))
	_, _ = s.take(at, false)
	s.note(key, servedAt(at.Add(time.Second), true))
	s.note(key, servedAt(at.Add(2*time.Second), false))
	pending, _ := s.take(at.Add(2*time.Second), true)
	require.Equal(t, at.Add(2*time.Second), pending[key].at)
	require.Equal(t, at.Add(time.Second), pending[key].answeredAt)

	s.note(key, servedAt(at.Add(3*time.Second), false))
	left, lost := s.abandon()
	require.Equal(t, 1, left, "what is still unwritten is counted")
	require.Zero(t, lost)
}

// A connection a full flush cannot take is not lost: it waits, and a later flush writes it.
func TestServedConnections_AFullFlushLeavesAConnectionForTheNext(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 10, 1)
	first, second, at := servedKeyOf("user:a"), servedKeyOf("user:b"), time.Unix(1_000_000, 0).UTC()

	s.note(first, servedAt(at, false))
	for i := range 3 {
		s.note(second, servedAt(at.Add(time.Duration(i)*time.Second), false))
	}
	pending, lost := s.take(at.Add(2*time.Second), false)
	require.Len(t, pending, 1)
	require.Contains(t, pending, first)
	require.Zero(t, lost)

	pending, lost = s.take(at.Add(3*time.Second), false)
	require.Equal(t, at.Add(2*time.Second), pending[second].at, "taken by the next flush with its latest time")
	require.Zero(t, lost)
}

func TestServedConnections_EvictionKeepsUnwrittenTimes(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 1, 10)
	first, second, at := servedKeyOf("user:a"), servedKeyOf("user:b"), time.Unix(1_000_000, 0).UTC()

	s.note(first, servedAt(at, true))
	_, _ = s.take(at, false)
	s.note(first, servedAt(at.Add(10*time.Second), true))
	s.note(second, servedAt(at.Add(20*time.Second), false))
	pending, lost := s.take(at.Add(20*time.Second), false)
	require.Zero(t, lost)
	require.Equal(t, at.Add(10*time.Second), pending[first].at, "the evicted connection's unwritten time goes to the next flush")
	require.Equal(t, at.Add(10*time.Second), pending[first].answeredAt)
	require.Contains(t, pending, second)

	s.note(first, servedAt(at.Add(30*time.Second), false))
	pending, _ = s.take(at.Add(30*time.Second), false)
	require.Contains(t, pending, first, "an evicted connection is due again")
}

func TestServedConnections_ForgetClearsTheThrottleAndUnwrittenTimes(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 10, 10)
	key, at := servedKeyOf("user:a"), time.Unix(1_000_000, 0).UTC()

	s.note(key, servedAt(at, false))
	_, _ = s.take(at, false)
	s.note(key, servedAt(at.Add(time.Second), false))
	s.forget([]servedKey{key})
	pending, lost := s.take(at.Add(time.Hour), true)
	require.Empty(t, pending, "nothing held is written")
	require.Zero(t, lost)

	s.note(key, servedAt(at.Add(2*time.Second), false))
	pending, _ = s.take(at.Add(2*time.Second), false)
	require.Equal(t, at.Add(2*time.Second), pending[key].at, "the next request is due at once")
}

func TestServedConnections_EvictionPastAFullFlushIsCounted(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 1, 1)
	first, second, at := servedKeyOf("user:a"), servedKeyOf("user:b"), time.Unix(1_000_000, 0).UTC()

	s.note(first, servedAt(at, false))
	s.note(second, servedAt(at, false))
	s.note(first, servedAt(at.Add(time.Second), false))
	pending, lost := s.take(at.Add(time.Second), false)
	require.Equal(t, 1, lost, "the second waited unwritten, was evicted and could not be taken")
	require.Len(t, pending, 1)
	require.Equal(t, at.Add(time.Second), pending[first].at)
}

// A retry answered within the interval after a failure is stored by a later flush, so the failure can end.
func TestServed_ARecoveryWithinTheIntervalIsStored(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	cfg := testConfig()
	cfg.servedFlushInterval = time.Hour
	r := newTestRecorder(t, conn, cfg)
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person))
	r.flushServed(time.Now(), false)
	first := storedServed(t, conn, org)
	require.Len(t, first, 1)
	r.Once(t.Context(), scope, StepMCPRequest, CodeProviderServerError, Subject(&person))

	retried := time.Now()
	r.Served(t.Context(), scope, true, Subject(&person))
	r.flushServed(time.Now(), false)
	require.Equal(t, first, storedServed(t, conn, org), "held back within the interval")

	r.flushServed(time.Now().Add(cfg.servedInterval), false)
	got := storedServed(t, conn, org)
	require.Len(t, got, 1)
	require.False(t, got[0].AnsweredAt.Before(retried), "the retry's answer is stored")
	require.True(t, got[0].AnsweredAt.After(first[0].AnsweredAt))
	require.Equal(t, got[0].ServedAt, got[0].AnsweredAt)
	require.Equal(t, int64(2), r.counted(t, meterServedWritten, ""))
}

// Rows written apart, in either order, read back as the latest times.
func TestServed_TheTableKeepsTheLatestTimes(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	org := "org-" + uuid.NewString()
	later := time.Now().UTC().Truncate(time.Millisecond)
	earlier := later.Add(-time.Hour)
	row := func(served, answered time.Time) Served {
		return Served{OrganizationID: org, ProjectID: uuid.Nil, ServerKind: serverKindMCP, ServerID: "server", MemberID: "", SubjectURN: "user:a",
			AppKey: "dcr", ClientFamily: FamilyClaudeCode, RemoteSessionClientID: "", ServedAt: served, AnsweredAt: answered}
	}

	require.NoError(t, appendServedInto(t.Context(), conn, lastServedTable, []Served{row(later, earlier)}))
	require.NoError(t, appendServedInto(t.Context(), conn, lastServedTable, []Served{row(earlier, time.Time{})}))

	got := storedServed(t, conn, org)
	require.Len(t, got, 1)
	require.WithinDuration(t, later, got[0].ServedAt, time.Millisecond)
	require.WithinDuration(t, earlier, got[0].AnsweredAt, time.Millisecond)
}

func TestServed_AMissingTableCountsOnTheServedCounter(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	cfg := testConfig()
	cfg.servedTable = "mcp_connection_last_served_not_migrated_yet"
	r := newTestRecorder(t, conn, cfg)
	scope, _, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person))
	flushed(t, r)

	require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropTableMissing))
	require.Zero(t, r.counted(t, meterServedWritten, ""))
	require.Zero(t, r.droppedTotal(t), "a served connection is not a connection event")
}

func TestServed_AFailedLogsCheckCountsOnTheServedCounter(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	check := &logsCheck{enabled: false, err: errors.New("feature cache down"), block: nil, panics: false, checks: atomic.Int64{}}
	r := newTestRecorderWithLogs(t, conn, check.check, testConfig())
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person))
	flushed(t, r)

	require.Empty(t, storedServed(t, conn, org))
	require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropLogsCheck))
	require.Zero(t, r.droppedTotal(t))
}

// An organization the writer found with logs off is not held in memory again until that answer goes stale.
func TestServed_AnOrganizationWithLogsOffIsNotHeld(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	off, offOrg, _ := servedScope()
	on, _, _ := servedScope()
	cfg := testConfig()
	cfg.servedFlushInterval = time.Hour
	r := newTestRecorderWithLogs(t, conn, func(_ context.Context, organizationID string) (bool, error) { return organizationID != offOrg, nil }, cfg)
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), off, true, Subject(&person))
	r.flushServed(time.Now(), false)
	other := urn.NewUserSubject("user-" + uuid.NewString())
	r.Served(t.Context(), off, true, Subject(&other))
	r.Served(t.Context(), on, true, Subject(&other))

	pending, _ := r.served.take(time.Now(), false)
	require.Len(t, pending, 1)
	for key := range pending {
		require.NotEqual(t, offOrg, key.organizationID)
	}
}

// A connection served while its organization's logs were off is written by its first request once they are on.
func TestServed_LogsTurnedOnAreNotHeldBack(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	var on atomic.Bool
	cfg := testConfig()
	cfg.servedFlushInterval = time.Hour
	r := newTestRecorderWithLogs(t, conn, func(context.Context, string) (bool, error) { return on.Load(), nil }, cfg)
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person))
	r.flushServed(time.Now(), false)
	require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropLogsOff))

	on.Store(true)
	// The logs-off answer held for the hot path has gone stale.
	r.referableMu.Lock()
	r.referableOrgs.Remove(org)
	r.referableMu.Unlock()
	r.Served(t.Context(), scope, true, Subject(&person))
	r.flushServed(time.Now(), false)
	require.Len(t, storedServed(t, conn, org), 1)
}

// Served connections lost at shutdown are counted, and never as connection events.
func TestServed_ShutdownCountsWhatItLeftBehind(t *testing.T) {
	t.Parallel()

	t.Run("a stuck logs check", func(t *testing.T) {
		t.Parallel()
		conn := clickhouseConn(t)
		check := &logsCheck{enabled: true, err: nil, block: make(chan struct{}), panics: false, checks: atomic.Int64{}}
		cfg := testConfig()
		cfg.servedFlushInterval = time.Hour
		r := newTestRecorderWithLogs(t, conn, check.check, cfg)
		first, _, _ := servedScope()
		second, _, _ := servedScope()
		person := urn.NewUserSubject("user-" + uuid.NewString())
		r.Served(t.Context(), first, true, Subject(&person))
		r.Served(t.Context(), second, true, Subject(&person))

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.ErrorIs(t, r.ShutdownAndWait(ctx), context.DeadlineExceeded)

		require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropLogsCheck), "the check shutdown cut off")
		require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropShutdown), "the connection after it")
		require.Zero(t, r.droppedTotal(t))
	})

	t.Run("a failed drain", func(t *testing.T) {
		t.Parallel()
		conn := clickhouseConn(t)
		cfg := testConfig()
		cfg.table = "mcp_connection_events_not_migrated_yet"
		cfg.flushInterval, cfg.servedFlushInterval = time.Hour, time.Hour
		r := newTestRecorder(t, conn, cfg)
		scope, _, _ := servedScope()
		person := urn.NewUserSubject("user-" + uuid.NewString())
		r.Served(t.Context(), scope, true, Subject(&person))
		r.Once(t.Context(), testScope(), StepMCPRequest, CodeProviderServerError)
		flushed(t, r)

		require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropShutdown))
		require.Equal(t, int64(1), r.droppedTotal(t), "only the connection event")
	})
}

// A connection a flush's budget did not reach is not written with a guessed client: it waits, with
// its time, and the next flush writes it with the name its session reported.
func TestServed_AConnectionPastTheBudgetWaitsForTheNextFlush(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	cfg := testConfig()
	cfg.servedFlushInterval = time.Hour
	cfg.servedFlushBudget = -time.Second
	r := newTestRecorder(t, conn, cfg)
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())
	var reads atomic.Int64

	r.Served(t.Context(), scope, true, Subject(&person), FromRequest(requestFrom("claude-code/2.1.0")),
		ReportedClient(func() (string, string) { reads.Add(1); return "cursor", "1.0.0" }))
	r.flushServed(time.Now(), false)
	require.Empty(t, storedServed(t, conn, org))
	require.Zero(t, reads.Load())
	require.Zero(t, r.counted(t, meterServedDropped, ""), "a connection not reached is not dropped")

	r.cfg.servedFlushBudget = 10 * time.Second
	waited := time.Now()
	flushed(t, r)

	got := storedServed(t, conn, org)
	require.Len(t, got, 1)
	require.Equal(t, FamilyCursor, got[0].ClientFamily)
	require.Equal(t, int64(1), reads.Load())
	require.True(t, got[0].ServedAt.Before(waited), "the request's time, not the flush's")
}

// Slow logs checks for many organizations hold a flush no longer than its budget, so a failure row behind it is written promptly.
func TestServed_SlowLogsChecksDoNotHoldFailureRowsBack(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	prefix := "slow-" + uuid.NewString() + "-"
	started := make(chan struct{})
	var once sync.Once
	var slow atomic.Bool
	slow.Store(true)
	check := func(ctx context.Context, organizationID string) (bool, error) {
		if strings.HasPrefix(organizationID, prefix) && slow.Load() {
			once.Do(func() { close(started) })
			select {
			case <-time.After(300 * time.Millisecond):
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
		return true, nil
	}
	cfg := testConfig()
	cfg.servedFlushBudget = 100 * time.Millisecond
	r := newTestRecorderWithLogs(t, conn, check, cfg)
	person := urn.NewUserSubject("user-" + uuid.NewString())
	const organizations = 30
	for i := range organizations {
		r.Served(t.Context(), ServerScope(prefix+strconv.Itoa(i), uuid.New(), uuid.New(), AppDCR), true, Subject(&person))
	}

	<-started
	attempt := Begin(testScope())
	failed := time.Now()
	r.End(t.Context(), attempt, StepProviderSignIn, CodeProviderRateLimited)
	stored := func(query string, arg any) uint64 {
		var n uint64
		if err := conn.QueryRow(context.Background(), query, arg).Scan(&n); err != nil {
			return 0
		}
		return n
	}
	require.Eventually(t, func() bool {
		return stored(`SELECT count() FROM mcp_connection_events WHERE attempt_id = ?`, attempt.ID()) == 1
	}, 10*time.Second, 10*time.Millisecond)
	require.Less(t, time.Since(failed), 4*time.Second, "30 checks of 300ms would hold it 9s")

	slow.Store(false)
	require.Eventually(t, func() bool {
		return stored(`SELECT uniqExact(organization_id) FROM `+lastServedTable+` WHERE startsWith(organization_id, ?)`, prefix) == organizations
	}, 10*time.Second, 50*time.Millisecond, "the connections not reached were kept for later flushes")
	require.Zero(t, r.counted(t, meterServedDropped, ""))
}

// A logs check still running when a flush's budget runs out is cut short, and its connection waits instead of being dropped.
func TestServed_ALogsCheckEndsWithTheBudget(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	var slow atomic.Bool
	slow.Store(true)
	check := func(ctx context.Context, _ string) (bool, error) {
		if slow.Load() {
			<-ctx.Done()
			return false, ctx.Err()
		}
		return true, nil
	}
	cfg := testConfig()
	cfg.servedFlushInterval = time.Hour
	cfg.servedFlushBudget = 100 * time.Millisecond
	r := newTestRecorderWithLogs(t, conn, check, cfg)
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person))
	started := time.Now()
	r.flushServed(time.Now(), false)
	require.Less(t, time.Since(started), 2*time.Second, "the check's own timeout is 10s")
	require.Zero(t, r.counted(t, meterServedDropped, ""))

	slow.Store(false)
	r.flushServed(time.Now(), false)
	require.Len(t, storedServed(t, conn, org), 1)
}

// A served request after the final flush is counted as lost to shutdown, not held where nothing writes it.
func TestServed_AfterShutdownIsCounted(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	r := newTestRecorder(t, conn, testConfig())
	scope, _, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())
	flushed(t, r)

	r.Served(t.Context(), scope, true, Subject(&person))

	require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropShutdown))
	pending, _ := r.served.take(time.Now(), false)
	require.Empty(t, pending)
}

// A logs check that failed keeps references off but does not discard served requests; logs off does.
func TestServed_OnlyALogsOffAnswerSkipsTheWriter(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	failing, failingOrg, _ := servedScope()
	off, offOrg, _ := servedScope()
	cfg := testConfig()
	cfg.servedFlushInterval = time.Hour
	r := newTestRecorderWithLogs(t, conn, func(_ context.Context, organizationID string) (bool, error) {
		if organizationID == failingOrg {
			return false, errors.New("feature cache down")
		}
		return false, nil
	}, cfg)
	person := urn.NewUserSubject("user-" + uuid.NewString())

	require.False(t, r.logsAllowForReference(t.Context(), failingOrg), "references fail closed")
	require.False(t, r.logsAllowForReference(t.Context(), offOrg))
	r.Served(t.Context(), failing, true, Subject(&person))
	r.Served(t.Context(), off, true, Subject(&person))

	pending, _ := r.served.take(time.Now(), false)
	require.Len(t, pending, 1)
	for key := range pending {
		require.Equal(t, failingOrg, key.organizationID, "the writer checks again")
	}
}

// A failed insert does not count as a write, so the next flush tries the connection again.
func TestServed_AFailedInsertIsRetriedByTheNextFlush(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	cfg := testConfig()
	cfg.servedFlushInterval = time.Hour
	cfg.servedTable = "mcp_connection_last_served_not_migrated_yet"
	r := newTestRecorder(t, conn, cfg)
	scope, _, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person))
	r.flushServed(time.Now(), false)
	require.Equal(t, int64(1), r.counted(t, meterServedDropped, dropTableMissing))

	pending, _ := r.served.take(time.Now(), false)
	require.Len(t, pending, 1, "waiting again after the failed insert")
}

func TestServedConnections_PutBackKeepsTimesWithinTheBound(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 10, 2)
	first, second, at := servedKeyOf("user:a"), servedKeyOf("user:b"), time.Unix(1_000_000, 0).UTC()

	s.note(first, servedAt(at, true))
	s.note(second, servedAt(at, false))
	taken, _ := s.take(at, false)
	s.note(second, servedAt(at.Add(time.Second), true))
	s.note(servedKeyOf("user:c"), servedAt(at.Add(time.Second), false))

	s.putBack(taken)
	pending, lost := s.take(at.Add(time.Second), false)
	require.Zero(t, lost)
	require.Len(t, pending, 2, "one merged into a later request, the other past the bound waits")
	require.Equal(t, at.Add(time.Second), pending[second].at, "a later request keeps its time")
	require.Equal(t, at.Add(time.Second), pending[second].answeredAt)

	pending, _ = s.take(at.Add(2*time.Second), false)
	require.Equal(t, at, pending[first].at, "the one past the bound is written by a later flush")
	require.Equal(t, at, pending[first].answeredAt)
}

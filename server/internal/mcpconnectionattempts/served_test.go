package mcpconnectionattempts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

func servedRowAt(at time.Time) Row {
	return newRow(testScope(), at)
}

func TestServedConnections_HoldsAConnectionToOneWritePerInterval(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 10, 10)
	key, start := servedKeyOf("user:a"), time.Unix(1_000_000, 0).UTC()

	s.note(key, servedRowAt(start), false)
	s.note(key, servedRowAt(start.Add(30*time.Second)), false)
	pending, turnedAway := s.take()
	require.Zero(t, turnedAway)
	require.Len(t, pending, 1)
	require.Equal(t, start, pending[key].row.EventTime, "a request within the interval is not due")
	require.True(t, pending[key].answeredAt.IsZero())

	s.note(key, servedRowAt(start.Add(40*time.Second)), true)
	s.note(key, servedRowAt(start.Add(50*time.Second)), true)
	pending, _ = s.take()
	require.Equal(t, start.Add(40*time.Second), pending[key].row.EventTime, "the server's first answer is due within the interval")
	require.Equal(t, start.Add(40*time.Second), pending[key].answeredAt, "a second answer within the interval is not")

	s.note(key, servedRowAt(start.Add(90*time.Second)), false)
	pending, _ = s.take()
	require.Empty(t, pending, "the interval counts from the last write")

	s.note(key, servedRowAt(start.Add(101*time.Second)), false)
	pending, _ = s.take()
	require.Equal(t, start.Add(101*time.Second), pending[key].row.EventTime, "due once the interval passed")
	require.True(t, pending[key].answeredAt.IsZero())
}

func TestServedConnections_AFullFlushTurnsAConnectionAwayUntilTheNext(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 10, 1)
	first, second, at := servedKeyOf("user:a"), servedKeyOf("user:b"), time.Unix(1_000_000, 0).UTC()

	s.note(first, servedRowAt(at), false)
	for range 3 {
		s.note(second, servedRowAt(at), false)
	}
	pending, turnedAway := s.take()
	require.Len(t, pending, 1)
	require.Contains(t, pending, first)
	require.Equal(t, 1, turnedAway, "a connection counts once per flush")

	s.note(second, servedRowAt(at.Add(time.Second)), false)
	pending, turnedAway = s.take()
	require.Contains(t, pending, second, "a connection turned away is taken on the next flush")
	require.Zero(t, turnedAway)
}

func TestServedConnections_AnEvictedConnectionWritesEarly(t *testing.T) {
	t.Parallel()
	s := newServedConnections(time.Minute, 1, 10)
	first, second, at := servedKeyOf("user:a"), servedKeyOf("user:b"), time.Unix(1_000_000, 0).UTC()

	s.note(first, servedRowAt(at), false)
	s.note(second, servedRowAt(at), false)
	_, _ = s.take()
	s.note(first, servedRowAt(at.Add(time.Second)), false)
	pending, _ := s.take()
	require.Contains(t, pending, first)
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
	r.flushServed()
	other := urn.NewUserSubject("user-" + uuid.NewString())
	r.Served(t.Context(), off, true, Subject(&other))
	r.Served(t.Context(), on, true, Subject(&other))

	pending, _ := r.served.take()
	require.Len(t, pending, 1)
	for key := range pending {
		require.NotEqual(t, offOrg, key.organizationID)
	}
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

// Past a flush's budget the reported name is not read, and the User-Agent names the client.
func TestServed_StampingStopsReadingReportedNamesPastItsBudget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		budget time.Duration
		reads  int64
		family string
	}{
		{name: "within the budget", budget: 10 * time.Second, reads: 1, family: FamilyCursor},
		{name: "past the budget", budget: -time.Second, reads: 0, family: FamilyClaudeCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := clickhouseConn(t)
			cfg := testConfig()
			cfg.servedStampBudget = tc.budget
			r := newTestRecorder(t, conn, cfg)
			scope, org, _ := servedScope()
			person := urn.NewUserSubject("user-" + uuid.NewString())
			var reads atomic.Int64

			r.Served(t.Context(), scope, true, Subject(&person), FromRequest(requestFrom("claude-code/2.1.0")),
				ReportedClient(func() (string, string) { reads.Add(1); return "cursor", "1.0.0" }))
			flushed(t, r)

			got := storedServed(t, conn, org)
			require.Len(t, got, 1)
			require.Equal(t, tc.family, got[0].ClientFamily)
			require.Equal(t, tc.reads, reads.Load())
		})
	}
}

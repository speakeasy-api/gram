package mcpconnectionattempts

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	var stored uint64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count() FROM `+lastServedTable+` WHERE organization_id = ?`, org).Scan(&stored))
	require.LessOrEqual(t, stored, uint64(1), "requests within the interval write one row")
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
	require.Equal(t, int64(1), r.dropped(t, dropLogsOff))
}

// After the interval the connection is written again, and the table keeps the latest time.
func TestServed_ALaterRequestMovesTheTimeForward(t *testing.T) {
	t.Parallel()
	conn := clickhouseConn(t)
	cfg := testConfig()
	cfg.servedInterval = time.Millisecond
	r := newTestRecorder(t, conn, cfg)
	scope, org, _ := servedScope()
	person := urn.NewUserSubject("user-" + uuid.NewString())

	r.Served(t.Context(), scope, true, Subject(&person))
	first := time.Now()
	require.Eventually(t, func() bool { return time.Since(first) > 5*time.Millisecond }, time.Second, time.Millisecond)
	r.Served(t.Context(), scope, false, Subject(&person))
	flushed(t, r)

	got := storedServed(t, conn, org)
	require.Len(t, got, 1)
	require.True(t, got[0].ServedAt.After(got[0].AnsweredAt), "served later than the server last answered")
	require.False(t, got[0].AnsweredAt.IsZero())
}

func TestServed_ACapturingRecorderHandsTheConnectionOver(t *testing.T) {
	t.Parallel()
	var got []Served
	r := NewCapturing(func(Row) { t.Error("a served request is not a row") }, logsOn)
	r.CaptureServed(func(s Served) { got = append(got, s) })
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

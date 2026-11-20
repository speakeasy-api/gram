package mcpconnectionattempts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func servedRecording(t *testing.T, logsEnabled func(context.Context, string) (bool, error)) (*Recorder, func() []Row) {
	t.Helper()
	var mu sync.Mutex
	var rows []Row
	recorder := NewCapturing(func(row Row) {
		mu.Lock()
		defer mu.Unlock()
		rows = append(rows, row)
	}, logsEnabled)
	return recorder, func() []Row {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(rows)
	}
}

func claudeCodeRequest(t *testing.T) func() []Option {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
	r.Header.Set("User-Agent", "claude-code/2.1.0")
	return func() []Option { return []Option{FromRequest(r)} }
}

func TestServed_OneRowPerConnectionPerWindow(t *testing.T) {
	t.Parallel()

	recorder, rows := servedRecording(t, logsOn)
	scope := testScope()
	person := urn.NewUserSubject("user-1")
	member := uuid.New()

	require.True(t, recorder.Served(t.Context(), scope, member, &person, claudeCodeRequest(t)))
	require.False(t, recorder.Served(t.Context(), scope, member, &person, claudeCodeRequest(t)), "a second served request within the window writes nothing")
	require.Len(t, rows(), 1)
	row := rows()[0]
	require.Equal(t, StepMCPRequest.String(), row.Step)
	require.Equal(t, ResultServed.String(), row.StepResult)
	require.Equal(t, string(OutcomeConnected), row.Outcome)
	require.True(t, row.IsFinal)
	require.Empty(t, row.Reason)
	require.Empty(t, row.WhoFixes)
	require.Equal(t, person.String(), row.SubjectURN)
	require.Equal(t, member.String(), row.MemberID)
	require.Equal(t, AppDCR.String(), row.AppKey)
	require.Equal(t, FamilyClaudeCode, row.ClientFamily)
	require.Equal(t, "2.1.0", row.ClientVersion)
}

func TestServed_DetailsAreBuiltOnlyWhenARowIsDue(t *testing.T) {
	t.Parallel()

	recorder, _ := servedRecording(t, logsOn)
	scope := testScope()
	person := urn.NewUserSubject("user-1")
	calls := 0
	details := func() []Option { calls++; return nil }
	for range 3 {
		recorder.Served(t.Context(), scope, uuid.Nil, &person, details)
	}
	require.Equal(t, 1, calls)
}

func TestServed_EachConnectionHasItsOwnWindow(t *testing.T) {
	t.Parallel()

	recorder, rows := servedRecording(t, logsOn)
	scope := testScope()
	person, other := urn.NewUserSubject("user-1"), urn.NewUserSubject("user-2")
	key := urn.NewAPIKeySubject(uuid.New())

	require.True(t, recorder.Served(t.Context(), scope, uuid.Nil, &person, nil))
	require.True(t, recorder.Served(t.Context(), scope, uuid.Nil, &other, nil))
	require.True(t, recorder.Served(t.Context(), scope, uuid.Nil, &key, nil))
	require.True(t, recorder.Served(t.Context(), scope, uuid.New(), &person, nil), "a gateway member is its own connection")
	require.True(t, recorder.Served(t.Context(), ServerScope(scope.organizationID, scope.projectID, scope.serverID, AppFirstParty), uuid.Nil, &person, nil), "another app is its own connection")
	require.True(t, recorder.Served(t.Context(), ServerScope(scope.organizationID, scope.projectID, uuid.New(), AppDCR), uuid.Nil, &person, nil), "another server is its own connection")
	require.Len(t, rows(), 6)
}

func TestServed_ConcurrentRequestsWriteOneRow(t *testing.T) {
	t.Parallel()

	recorder, rows := servedRecording(t, logsOn)
	scope := testScope()
	person := urn.NewUserSubject("user-1")
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			if recorder.Served(t.Context(), scope, uuid.Nil, &person, nil) {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	require.Equal(t, int32(1), accepted.Load())
	require.Len(t, rows(), 1)
}

func TestServed_NothingAfterAFailureInTheSameRequest(t *testing.T) {
	t.Parallel()

	recorder, rows := servedRecording(t, logsOn)
	scope := testScope()
	person := urn.NewUserSubject("user-1")

	ctx := WithFailureMark(t.Context())
	recorder.Once(ctx, scope, StepMCPRequest, CodeProviderServerError, Subject(&person))
	require.False(t, recorder.Served(ctx, scope, uuid.Nil, &person, nil))

	ctx = WithFailureMark(t.Context())
	MarkFailed(ctx)
	require.False(t, recorder.Served(ctx, scope, uuid.Nil, &person, nil), "a failure that records no row still counts")

	ctx = WithFailureMark(t.Context())
	call := WithFailureMark(ctx)
	recorder.Step(call, Begin(scope), StepProviderRefresh, CodeProviderServerError)
	require.True(t, Failed(ctx), "a failure in one call marks its request too")
	require.False(t, recorder.Served(ctx, scope, uuid.Nil, &person, nil))
	require.True(t, recorder.Served(WithFailureMark(ctx), scope, uuid.Nil, &person, nil), "a later call's own mark is clear")

	for _, row := range rows() {
		if row.StepResult == ResultServed.String() {
			require.Equal(t, person.String(), row.SubjectURN)
		}
	}
}

func TestServed_UnnamedCallersWriteNothing(t *testing.T) {
	t.Parallel()

	recorder, rows := servedRecording(t, logsOn)
	anonymous := urn.NewAnonymousSubject("mcp-session")
	var zero urn.SessionSubject
	require.False(t, recorder.Served(t.Context(), testScope(), uuid.Nil, nil, nil))
	require.False(t, recorder.Served(t.Context(), testScope(), uuid.Nil, &zero, nil))
	require.False(t, recorder.Served(t.Context(), testScope(), uuid.Nil, &anonymous, nil))
	require.Empty(t, rows())
}

func TestServed_CallerThatLeftWritesNothing(t *testing.T) {
	t.Parallel()

	recorder, rows := servedRecording(t, logsOn)
	person := urn.NewUserSubject("user-1")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.False(t, recorder.Served(ctx, testScope(), uuid.Nil, &person, nil))
	require.Empty(t, rows())
}

func TestServed_LogsOffWritesNothingAndLeavesTheWindowOpen(t *testing.T) {
	t.Parallel()

	var logs atomic.Bool
	recorder, rows := servedRecording(t, func(context.Context, string) (bool, error) { return logs.Load(), nil })
	scope := testScope()
	person := urn.NewUserSubject("user-1")

	require.False(t, recorder.Served(t.Context(), scope, uuid.Nil, &person, nil))
	require.Empty(t, rows())
	logs.Store(true)
	require.True(t, recorder.Served(t.Context(), scope, uuid.Nil, &person, nil), "a row not written releases its claim")
	require.Len(t, rows(), 1)
}

func TestServed_RowNotQueuedReleasesTheClaim(t *testing.T) {
	t.Parallel()

	recorder := newTestRecorder(t, &scriptedConn{Conn: nil, block: nil, err: nil, prepares: atomic.Int64{}}, testConfig())
	require.NoError(t, recorder.Shutdown(t.Context()))
	scope := testScope()
	person := urn.NewUserSubject("user-1")
	key := servedKey{organizationID: scope.organizationID, serverID: scope.serverID, memberID: uuid.Nil, subjectKind: person.Kind, subjectID: person.ID, appKey: AppDCR.String()}

	require.False(t, recorder.Served(t.Context(), scope, uuid.Nil, &person, nil))
	require.True(t, recorder.served.claim(key, time.Now()), "a row that could not be queued leaves the window open")
}

func TestServed_FailureReopensTheWindow(t *testing.T) {
	t.Parallel()

	recorder, rows := servedRecording(t, logsOn)
	scope := testScope()
	person := urn.NewUserSubject("user-1")
	member := uuid.New()

	require.True(t, recorder.Served(t.Context(), scope, member, &person, nil))
	require.True(t, recorder.Once(t.Context(), scope, StepMCPRequest, CodeProviderServerError, Subject(&person), Member(member)))
	require.True(t, recorder.Served(t.Context(), scope, member, &person, nil), "the first request served after a failure writes")
	require.False(t, recorder.Served(t.Context(), scope, member, &person, nil))

	other := urn.NewUserSubject("user-2")
	require.True(t, recorder.Served(t.Context(), scope, member, &other, nil))
	require.True(t, recorder.Once(t.Context(), scope, StepMCPRequest, CodeProviderServerError, Subject(&person), Member(member)))
	require.False(t, recorder.Served(t.Context(), scope, member, &other, nil), "another person's failure leaves this window closed")
	require.Len(t, rows(), 5)
}

func TestServedConnections_ClaimReleaseAndRetry(t *testing.T) {
	t.Parallel()

	served := newServedConnections()
	key := servedKey{organizationID: "org", serverID: uuid.New(), memberID: uuid.Nil, subjectKind: urn.SessionSubjectKindUser, subjectID: "1", appKey: "dcr"}
	start := time.Now()
	require.True(t, served.claim(key, start))
	require.False(t, served.claim(key, start.Add(servedInterval-time.Second)))
	require.True(t, served.claim(key, start.Add(servedInterval)))

	released := key
	released.subjectID = "2"
	require.True(t, served.claim(released, start))
	served.release(released, start)
	require.True(t, served.claim(released, start.Add(time.Second)), "a released claim leaves the window open")

	held := key
	held.subjectID = "3"
	require.True(t, served.claim(held, start))
	served.retryLater(held, start)
	require.False(t, served.claim(held, start.Add(servedRetryDelay-time.Second)))
	require.True(t, served.claim(held, start.Add(servedRetryDelay)), "a row the limits held back tries again after the delay")

	stale := key
	stale.subjectID = "4"
	require.True(t, served.claim(stale, start))
	served.release(stale, start.Add(-time.Second))
	require.False(t, served.claim(stale, start.Add(time.Second)), "an older claim's release leaves a newer claim alone")
}

func TestServedConnections_EvictedConnectionWritesEarly(t *testing.T) {
	t.Parallel()

	served := newServedConnections()
	now := time.Now()
	keyOf := func(i int) servedKey {
		return servedKey{organizationID: "org", serverID: uuid.Nil, memberID: uuid.Nil, subjectKind: urn.SessionSubjectKindUser, subjectID: strconv.Itoa(i), appKey: "dcr"}
	}
	require.True(t, served.claim(keyOf(0), now))
	for i := 1; i < servedMaxConnections; i++ {
		require.True(t, served.claim(keyOf(i), now))
	}
	require.False(t, served.claim(keyOf(0), now), "within the bound every connection keeps its window")
	require.True(t, served.claim(keyOf(servedMaxConnections), now))
	require.True(t, served.claim(keyOf(1), now), "past the bound the least recently seen connection is forgotten")
	require.Equal(t, servedMaxConnections, served.last.Len())
}

func TestServed_TakesItsOwnOrganizationCeiling(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		recorder, rows := servedRecording(t, logsOn)
		recorder.limiter = newOnceLimiter()
		recorder.queue = make(chan Row, 100)
		scope := testScope()

		for i := range onceOrgBurst {
			person := urn.NewUserSubject("failing-" + strconv.Itoa(i))
			recorder.Once(t.Context(), scope, StepMCPRequest, CodeProviderServerError, Subject(&person))
		}
		require.Len(t, rows(), onceOrgBurst)

		for i := range servedOrgBurst + 10 {
			person := urn.NewUserSubject("user-" + strconv.Itoa(i))
			recorder.Served(t.Context(), scope, uuid.Nil, &person, nil)
		}
		require.Len(t, rows(), onceOrgBurst+servedOrgBurst, "failures spent none of the served budget")

		late := urn.NewUserSubject("user-" + strconv.Itoa(servedOrgBurst))
		require.False(t, recorder.Served(t.Context(), scope, uuid.Nil, &late, nil), "a held-back connection waits")
		time.Sleep(servedRetryDelay) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		require.True(t, recorder.Served(t.Context(), scope, uuid.Nil, &late, nil), "and tries again after the delay")
	})
}

func TestServed_YieldsTheQueueFirst(t *testing.T) {
	t.Parallel()

	recorder, rows := servedRecording(t, logsOn)
	recorder.limiter = newOnceLimiter()
	recorder.queue = make(chan Row, 4)
	recorder.queue <- Row{}
	recorder.queue <- Row{}
	person := urn.NewUserSubject("user-1")

	require.False(t, recorder.Served(t.Context(), testScope(), uuid.Nil, &person, nil), "half a queue is kept for failures")
	require.Empty(t, rows())
}

func TestServed_NotExported(t *testing.T) {
	t.Parallel()

	person := urn.NewUserSubject("user-1")
	row, err := RowAt(Begin(testScope()), StepMCPRequest, ResultServed, true, time.Now(), Subject(&person))
	require.NoError(t, err)
	_, exported := exportRecord(row)
	require.False(t, exported, "customer log exports carry failures only")
}

func TestNotServedRowMatchesTheCatalog(t *testing.T) {
	t.Parallel()

	require.Equal(t, NotServedRow, "NOT (step = '"+StepMCPRequest.String()+"' AND step_result = '"+ResultServed.String()+"')")
}

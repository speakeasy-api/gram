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
	recorder.Step(ctx, Begin(scope), StepProviderRefresh, CodeProviderServerError)
	require.False(t, recorder.Served(ctx, scope, uuid.Nil, &person, nil), "a failure at any step of the request counts")

	for _, row := range rows() {
		require.NotEqual(t, ResultServed.String(), row.StepResult)
	}
}

// A member's failure is that member call's: it stops the member's row and leaves the gateway's.
func TestServed_MemberFailureMarksOnlyItsCall(t *testing.T) {
	t.Parallel()

	recorder, _ := servedRecording(t, logsOn)
	gateway := GatewayScope("org-test", uuid.New(), uuid.New(), AppDCR)
	person := urn.NewUserSubject("user-1")
	healthy, down := uuid.New(), uuid.New()

	request := WithFailureMark(t.Context())
	recorder.Once(request, gateway, StepDiscoveryMember, CodeMemberToolsMissing, Member(down), Subject(&person))
	recorder.Step(request, Begin(gateway), StepDiscoveryMember, CodeMemberToolsMissing, Member(down))
	require.False(t, Failed(request), "a member's listing failure leaves the request unmarked")

	call := WithCallFailureMark(request)
	recorder.Once(call, gateway, StepMCPRequest, CodeProviderServerError, Member(down), Subject(&person))
	recorder.Step(call, Begin(gateway), StepProviderRefresh, CodeProviderServerError)
	require.True(t, Failed(call))
	require.False(t, recorder.Served(call, gateway, down, &person, nil), "the failed member call writes no row for its member")
	require.False(t, Failed(request), "every failure inside a member call is that call's")

	require.True(t, recorder.Served(WithCallFailureMark(request), gateway, healthy, &person, nil))
	require.True(t, recorder.Served(request, gateway, uuid.Nil, &person, nil), "the gateway still served the request")

	recorder.Once(request, gateway, StepMCPRequest, CodeInvalidBearerToken, Subject(&person))
	require.True(t, Failed(request), "a failure of the request itself marks it")
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

// The writer's own logs check drops a served row for an organization with logs off.
func TestServed_LogsOffOrganizationStoresNothing(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	organizationID := "org-served-logs-off-" + uuid.NewString()
	recorder := newTestRecorderWithLogs(t, conn, func(context.Context, string) (bool, error) { return false, nil }, testConfig())
	person := urn.NewUserSubject("user-1")

	require.True(t, recorder.Served(t.Context(), ServerScope(organizationID, uuid.New(), uuid.New(), AppDCR), uuid.Nil, &person, nil), "queued")
	require.NoError(t, recorder.Shutdown(t.Context()))
	require.Equal(t, int64(1), recorder.dropped(t, dropLogsOff))
	var stored uint64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count() FROM mcp_connection_events WHERE organization_id = ?`, organizationID).Scan(&stored))
	require.Zero(t, stored)
}

func TestServed_RowNotQueuedReleasesTheClaim(t *testing.T) {
	t.Parallel()

	recorder := newTestRecorder(t, &scriptedConn{Conn: nil, block: nil, err: nil, prepares: atomic.Int64{}}, testConfig())
	require.NoError(t, recorder.Shutdown(t.Context()))
	scope := testScope()
	person := urn.NewUserSubject("user-1")
	key := servedKey{organizationID: scope.organizationID, serverID: scope.serverID, memberID: uuid.Nil, subjectKind: person.Kind, subjectID: person.ID, appKey: AppDCR.String()}

	require.False(t, recorder.Served(t.Context(), scope, uuid.Nil, &person, nil))
	require.Equal(t, claimed, recorder.served.claim(key, time.Now(), false, false), "a row that could not be queued leaves the window open")
}

// After a failure for the caller on the server, the next served request writes, at most once a minute.
func TestServed_FailureMakesTheConnectionDueAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		recorder, rows := servedRecording(t, logsOn)
		recorder.limiter = newOnceLimiter()
		recorder.queue = make(chan Row, 100)
		org, project, gatewayID := "org-test", uuid.New(), uuid.New()
		gateway := GatewayScope(org, project, gatewayID, AppDCR)
		person, other := urn.NewUserSubject("user-1"), urn.NewUserSubject("user-2")
		member := uuid.New()
		served := func(scope Scope, member uuid.UUID, subject urn.SessionSubject) bool {
			return recorder.Served(t.Context(), scope, member, &subject, nil)
		}
		later := func() { time.Sleep(servedRetryDelay) } //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble

		require.True(t, served(gateway, uuid.Nil, person))
		require.True(t, served(gateway, uuid.Nil, other))
		require.True(t, served(gateway, member, person))
		later()

		recorder.Once(t.Context(), gateway, StepMCPRequest, CodeProviderServerError, Subject(&person))
		require.True(t, served(gateway, uuid.Nil, person), "a failure makes the connection due")
		require.False(t, served(gateway, uuid.Nil, other), "another person's failure leaves this window closed")
		require.True(t, served(gateway, member, person), "a failure on the gateway makes its members due too")
		later()

		recorder.Once(t.Context(), gateway, StepMCPRequest, CodeProviderServerError, Subject(&person), Member(member))
		require.True(t, served(gateway, uuid.Nil, person), "a member's failure makes the gateway due")
		later()

		recorder.Once(t.Context(), ServerScope(org, project, gatewayID, AppFirstParty), StepInboundToken, CodeRefreshTokenUnknownOrAlreadyUsed, Subject(&person))
		require.True(t, served(gateway, uuid.Nil, person), "a token failure under another app makes it due")

		before := len(rows())
		writes := 0
		for range 10 {
			recorder.Once(t.Context(), gateway, StepMCPRequest, CodeProviderServerError, Subject(&person))
			if served(gateway, uuid.Nil, person) {
				writes++
			}
			time.Sleep(10 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		}
		require.LessOrEqual(t, writes, 2, "a flapping connection writes at most one served row a minute")
		require.Positive(t, writes)
		require.Greater(t, len(rows()), before)
	})
}

func TestServed_HeldBackFailureMakesItDue(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		recorder, _ := servedRecording(t, logsOn)
		recorder.limiter = newOnceLimiter()
		recorder.queue = make(chan Row, 100)
		scope := testScope()
		person := urn.NewUserSubject("user-1")
		fail := func() bool {
			return recorder.Once(t.Context(), scope, StepMCPRequest, CodeProviderServerError, Subject(&person))
		}

		for range onceKeyBurst {
			require.True(t, fail())
		}
		require.True(t, recorder.Served(t.Context(), scope, uuid.Nil, &person, nil))
		time.Sleep(servedRetryDelay / 2) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		require.False(t, fail(), "held back, to be written later as a repeat")
		time.Sleep(servedRetryDelay / 2) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		require.True(t, recorder.Served(t.Context(), scope, uuid.Nil, &person, nil), "the held-back failure made the connection due")
	})
}

func TestServedConnections_ClaimReleaseAndRetry(t *testing.T) {
	t.Parallel()

	served := newServedConnections()
	key := servedKey{organizationID: "org", serverID: uuid.New(), memberID: uuid.Nil, subjectKind: urn.SessionSubjectKindUser, subjectID: "1", appKey: "dcr"}
	start := time.Now()
	require.Equal(t, claimed, served.claim(key, start, false, false))
	require.Equal(t, claimNotDue, served.claim(key, start.Add(servedInterval-time.Second), false, false))
	require.Equal(t, claimed, served.claim(key, start.Add(servedInterval), false, false))

	released := key
	released.subjectID = "2"
	require.Equal(t, claimed, served.claim(released, start, false, false))
	served.release(released, start)
	require.Equal(t, claimed, served.claim(released, start.Add(time.Second), false, false), "a released claim leaves the window open")

	held := key
	held.subjectID = "3"
	require.Equal(t, claimDeferred, served.claim(held, start, false, true))
	require.Equal(t, claimNotDue, served.claim(held, start.Add(servedRetryDelay-time.Second), false, false))
	require.Equal(t, claimed, served.claim(held, start.Add(servedRetryDelay), false, false), "a row the limits held back tries again after the delay")

	stale := key
	stale.subjectID = "4"
	require.Equal(t, claimed, served.claim(stale, start, false, false))
	served.release(stale, start.Add(-time.Second))
	require.Equal(t, claimNotDue, served.claim(stale, start.Add(time.Second), false, false), "an older claim's release leaves a newer claim alone")
}

func TestServedConnections_EvictedConnectionWritesEarly(t *testing.T) {
	t.Parallel()

	served := newServedConnections()
	now := time.Now()
	keyOf := func(i int) servedKey {
		return servedKey{organizationID: "org", serverID: uuid.Nil, memberID: uuid.Nil, subjectKind: urn.SessionSubjectKindUser, subjectID: strconv.Itoa(i), appKey: "dcr"}
	}
	for i := range servedMaxConnections {
		require.Equal(t, claimed, served.claim(keyOf(i), now, false, false))
	}
	require.Equal(t, claimNotDue, served.claim(keyOf(0), now, false, false), "within the bound every connection keeps its window")
	require.Equal(t, claimed, served.claim(keyOf(servedMaxConnections), now, false, false))
	require.Equal(t, claimed, served.claim(keyOf(1), now, false, false), "past the bound the least recently seen connection is forgotten")
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

// A held-back served row is sampling, not a lost event.
func TestServed_DeferralIsNotADrop(t *testing.T) {
	t.Parallel()

	recorder := newTestRecorder(t, &scriptedConn{Conn: nil, block: nil, err: nil, prepares: atomic.Int64{}}, testConfig())
	scope := testScope()
	for i := range servedOrgBurst + 5 {
		person := urn.NewUserSubject("user-" + strconv.Itoa(i))
		recorder.Served(t.Context(), scope, uuid.Nil, &person, nil)
	}
	require.Zero(t, recorder.dropped(t, dropRateLimited))
	require.Zero(t, recorder.dropped(t, dropQueueReserve))
	require.Zero(t, recorder.dropTotals[dropRateLimited].Load())
	require.Zero(t, recorder.dropTotals[dropQueueReserve].Load())
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

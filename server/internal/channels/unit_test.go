package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDashboardLabelComesFromTheAccount(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		given string
		want  string
	}{
		{name: "display name", given: "Alice Example", want: "Alice Example on dashboard"},
		{name: "email local part", given: "alice@example.invalid", want: "alice on dashboard"},
		{name: "empty falls back", given: "", want: "member on dashboard"},
		{name: "whitespace falls back", given: "   ", want: "member on dashboard"},
		{name: "control characters stripped", given: "Al\x00i\x7fce", want: "Alice on dashboard"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, dashboardLabel(Principal{
				OrganizationID: testOrg, SubjectID: "alice", SubjectName: test.given,
				CanConnect: true, CanAdmin: false, CanReadAll: false,
			}))
		})
	}
}

func TestDashboardLabelIsBounded(t *testing.T) {
	t.Parallel()
	long := ""
	for range 400 {
		long += "a"
	}
	label := dashboardLabel(Principal{
		OrganizationID: testOrg, SubjectID: "alice", SubjectName: long,
		CanConnect: true, CanAdmin: false, CanReadAll: false,
	})
	require.LessOrEqual(t, len(label), 128)
}

// TestEndpointPairKeyIsOrderIndependent is what makes a direct conversation
// reusable from either side.
func TestEndpointPairKeyIsOrderIndependent(t *testing.T) {
	t.Parallel()
	left, right := uuid.New(), uuid.New()
	require.Equal(t, endpointPairKey(left, right), endpointPairKey(right, left))
	require.NotEqual(t, endpointPairKey(left, right), endpointPairKey(left, uuid.New()))
}

func TestStatusForDistinguishesOutcomes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "rejected payload", err: fmt.Errorf("%w: too long", ErrInvalid), want: http.StatusBadRequest},
		{name: "lost access", err: fmt.Errorf("%w: revoked", ErrDenied), want: http.StatusForbidden},
		{name: "burst", err: fmt.Errorf("%w: slow down", ErrLimited), want: http.StatusTooManyRequests},
		{name: "storage", err: fmt.Errorf("%w: no database", ErrUnavailable), want: http.StatusServiceUnavailable},
		{name: "unexpected", err: errors.New("boom"), want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, statusFor(test.err))
		})
	}
}

func TestBurstLimiterRefillsOverTime(t *testing.T) {
	t.Parallel()
	limiter := newBurstLimiter(60, 2)
	now := time.Now()
	require.True(t, limiter.allow("subject", now))
	require.True(t, limiter.allow("subject", now))
	require.False(t, limiter.allow("subject", now), "the burst was not capped")
	require.True(t, limiter.allow("other", now), "one identity exhausted another's bucket")
	require.True(t, limiter.allow("subject", now.Add(time.Second)), "the bucket never refilled")
}

func TestRevisionDetectsOnlyRealChanges(t *testing.T) {
	t.Parallel()
	base := revision{presenceLive: 2, presenceAnnounce: 5, threadCount: 3, threadAnnounce: 7, messageSeq: 11, messageChanged: false}
	require.False(t, base.changed(base, "channels://presence"))
	require.False(t, base.changed(base, "channels://threads"))
	require.False(t, base.changed(base, "channels://messages"))

	departed := base
	departed.presenceLive = 1
	require.True(t, base.changed(departed, "channels://presence"), "an expiring lease must reconcile presence")

	arrived := base
	arrived.presenceAnnounce = 6
	require.True(t, base.changed(arrived, "channels://presence"))

	updated := base
	updated.presenceRevision = "changed"
	require.True(t, base.changed(updated, "channels://presence"), "presence display changes and equal-count membership changes must reconcile")

	posted := base
	posted.messageSeq, posted.messageChanged = 12, true
	require.True(t, base.changed(posted, "channels://messages"))
	require.False(t, base.changed(posted, "channels://threads"), "a message is not a conversation change")
}

func TestMessageResourceArgumentsRejectUnknownParameters(t *testing.T) {
	t.Parallel()
	args, err := messageResourceArguments("cursor=4&limit=10", 200)
	require.NoError(t, err)
	require.Equal(t, int64(4), args.Cursor)
	require.Equal(t, 10, args.Limit)

	for _, query := range []string{"cursor=-1", "limit=0", "limit=201", "cursor=abc", "unexpected=1"} {
		_, err := messageResourceArguments(query, 200)
		require.ErrorIs(t, err, ErrInvalid, "query %q was accepted", query)
	}
}

func TestInitializeNegotiatesAHandshakeRevision(t *testing.T) {
	t.Parallel()
	for requested, want := range map[string]string{
		"2025-06-18": "2025-06-18",
		"2024-11-05": "2024-11-05",
		"2026-07-28": "2025-11-25",
		"2099-01-01": "2025-11-25",
		"":           "2025-11-25",
	} {
		require.Equal(t, want, negotiateHandshakeVersion(requested), requested)
	}
}

func TestProjectKeysIsolateBrokerAndWriteBudgets(t *testing.T) {
	t.Parallel()
	a := Principal{OrganizationID: testOrg, ProjectID: uuid.NewString(), SubjectID: "alice"}
	b := a
	b.ProjectID = uuid.NewString()
	broker := newBroker()
	first, releaseFirst := broker.subscribe(projectKey(a.OrganizationID, a.ProjectID))
	defer releaseFirst()
	second, releaseSecond := broker.subscribe(projectKey(b.OrganizationID, b.ProjectID))
	defer releaseSecond()
	broker.wake(projectKey(a.OrganizationID, a.ProjectID))
	select {
	case <-first:
	default:
		t.Fatal("matching project was not woken")
	}
	select {
	case <-second:
		t.Fatal("another project received a hint")
	default:
	}
	limiter := newBurstLimiter(1, 1)
	now := time.Now()
	require.True(t, limiter.allow(subjectKey(a), now))
	require.False(t, limiter.allow(subjectKey(a), now))
	require.True(t, limiter.allow(subjectKey(b), now), "one project exhausted another project's write budget")
}

func TestProjectStreamQuotasAreIndependent(t *testing.T) {
	t.Parallel()
	limits := DefaultLimits()
	limits.Streams, limits.StreamsPerSubject = 1, 1
	h := New(nil, nil, WithLimits(limits))
	defer h.Close()
	a := Principal{OrganizationID: testOrg, ProjectID: uuid.NewString(), SubjectID: "alice"}
	b := a
	b.ProjectID = uuid.NewString()
	_, cancel := context.WithCancel(t.Context())
	defer cancel()
	releaseFirst, err := h.registerStream("first", json.RawMessage(`"same"`), a, cancel)
	require.NoError(t, err)
	defer releaseFirst()
	_, err = h.registerStream("extra", json.RawMessage(`"same"`), a, cancel)
	require.ErrorIs(t, err, ErrLimited)
	releaseSecond, err := h.registerStream("second", json.RawMessage(`"same"`), b, cancel)
	require.NoError(t, err, "one project's stream quota blocked another project")
	defer releaseSecond()
}

func TestHandlerOnlyAcceptsCanonicalProjectRoutes(t *testing.T) {
	t.Parallel()
	handler := New(nil, func(*http.Request) (Principal, error) {
		return Principal{OrganizationID: testOrg, ProjectID: uuid.NewString(), SubjectID: "alice"}, nil
	})
	t.Cleanup(handler.Close)
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/channels/test-org/project/mcp", http.StatusMethodNotAllowed},
		{"/channels/test-org/project/api", http.StatusBadRequest},
		{"/channels/mcp", http.StatusNotFound},
		{"/channels/api", http.StatusNotFound},
		{"/channels/api/", http.StatusNotFound},
		{"/channels/test-org/project/mcp/", http.StatusNotFound},
		{"/channels/test-org/project/api/", http.StatusNotFound},
		{"/channels/test-org/project/mcp/extra", http.StatusNotFound},
		{"/channels//project/mcp", http.StatusNotFound},
		{"/channels/test-org//api", http.StatusNotFound},
		{"/channels/./project/mcp", http.StatusNotFound},
		{"/channels/test-org/../api", http.StatusNotFound},
		{"/channels/test-org/%70roject/mcp", http.StatusNotFound},
		{"/channels/test-org/project%2Fother/api", http.StatusNotFound},
		{"/channels/test-org/project%5Cother/mcp", http.StatusNotFound},
		{"/channels/test-org/project%20other/api", http.StatusNotFound},
		{"/channels/test-org/project/%6dcp", http.StatusNotFound},
	} {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			method := http.MethodGet
			if test.path == "/channels/test-org/project/api" {
				method = http.MethodPost
			}
			request := httptest.NewRequest(method, test.path, strings.NewReader("{"))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, test.status, response.Code, response.Body.String())
		})
	}
}

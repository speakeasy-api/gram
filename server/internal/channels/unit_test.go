package channels

import (
	"errors"
	"fmt"
	"net/http"
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

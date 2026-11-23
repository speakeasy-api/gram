package access

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRequestedAccessWindow_FixedDurations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)

	for choice, want := range map[string]time.Duration{"1h": time.Hour, "2h": 2 * time.Hour, "4h": 4 * time.Hour} {
		got, until, err := RequestedAccessWindow(new(choice), nil, now)
		require.NoError(t, err, choice)
		require.Equal(t, want, got, choice)
		require.True(t, until.IsZero())
	}
	_, _, err := RequestedAccessWindow(new("eod"), nil, now)
	require.Error(t, err, "end of day is a deadline the client sends, not a duration")

	duration, until, err := RequestedAccessWindow(nil, nil, now)
	require.NoError(t, err)
	require.Zero(t, duration)
	require.True(t, until.IsZero())
}

func TestRequestedAccessWindow_DeadlineIsNormalisedToUTC(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 14, 30, 0, 0, time.UTC)

	// 23:59 in New York is 04:59 UTC the next day.
	_, until, err := RequestedAccessWindow(nil, new("2026-11-23T23:59:00-05:00"), now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 24, 4, 59, 0, 0, time.UTC), until)
	require.Equal(t, time.UTC, until.Location())
}

func TestRequestedAccessWindow_RefusesBadDeadlines(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 14, 30, 0, 0, time.UTC)

	_, _, err := RequestedAccessWindow(new("1h"), new("2026-11-23T18:00:00Z"), now)
	require.ErrorIs(t, err, errDurationAndUntil)
	_, _, err = RequestedAccessWindow(nil, new("2026-11-23T14:00:00Z"), now)
	require.ErrorIs(t, err, errRequestedUntilPast)
	_, _, err = RequestedAccessWindow(nil, new("2026-11-24T15:00:00Z"), now)
	require.ErrorIs(t, err, errRequestedUntilTooFar)
	_, _, err = RequestedAccessWindow(nil, new("tonight"), now)
	require.Error(t, err)
}

func TestAccessRequest_RequestedDurationRoundTrips(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newAccessRequestService(t)

	result, err := ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:          string(authz.ScopeChatRead),
		ResourceID:     nil,
		ResourceName:   nil,
		Message:        nil,
		Duration:       new("2h"),
		RequestedUntil: nil,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Request)
	require.Equal(t, 7200, *result.Request.RequestedDurationSeconds)

	listed, err := ti.service.ListAccessRequests(ctx, &gen.ListAccessRequestsPayload{SessionToken: nil, Status: nil, Mine: true})
	require.NoError(t, err)
	require.Len(t, listed.Requests, 1)
	require.Equal(t, 7200, *listed.Requests[0].RequestedDurationSeconds)
	require.Nil(t, listed.Requests[0].RequestedUntil)
}

func TestAccessRequest_RequestedUntilRoundTrips(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newAccessRequestService(t)
	until := time.Now().Add(6 * time.Hour).Truncate(time.Second)

	result, err := ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:          string(authz.ScopeChatRead),
		ResourceID:     nil,
		ResourceName:   nil,
		Message:        nil,
		Duration:       nil,
		RequestedUntil: new(until.In(time.FixedZone("UTC-5", -5*60*60)).Format(time.RFC3339)),
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Nil(t, result.Request.RequestedDurationSeconds)
	require.Equal(t, until.UTC().Format(time.RFC3339), *result.Request.RequestedUntil, "stored and returned in UTC")

	_, err = ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:          string(authz.ScopeLogRead),
		ResourceID:     nil,
		ResourceName:   nil,
		Message:        nil,
		Duration:       new("1h"),
		RequestedUntil: new(until.Format(time.RFC3339)),
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestAccessRequest_WithoutDurationStoresNone(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newAccessRequestService(t)

	result := requestChatRead(t, ctx, ti)
	require.Nil(t, result.Request.RequestedDurationSeconds)
	require.Nil(t, result.Request.RequestedUntil)
}

package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
)

// Pins the mapping from every failure shape RefreshNow can produce onto the
// upstream-refresh metric's outcome set. The ordering matters at three
// points: an aborted POST also carries the transport marker, a deadline is
// the caller's own only when the caller's context has expired, and
// invalid_grant wins over the 429 and 5xx buckets because the grant is
// cleared on any status.
func TestRefreshOutcomeForError(t *testing.T) {
	t.Parallel()

	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	cases := []struct {
		name string
		// callerGone runs the case against a context whose own deadline has
		// already passed, standing in for a caller that stopped waiting.
		callerGone bool
		err        error
		want       remotesessionmetrics.RefreshOutcome
	}{
		{
			name: "no refresh grant",
			err:  ErrNoValidToken,
			want: remotesessionmetrics.RefreshOutcomeNoGrant,
		},
		{
			name: "caller canceled mid-POST",
			err:  fmt.Errorf("post refresh: %w: %w", errRefreshUpstreamUnreachable, context.Canceled),
			want: remotesessionmetrics.RefreshOutcomeCanceled,
		},
		{
			name:       "caller's own deadline expired mid-POST",
			callerGone: true,
			err:        fmt.Errorf("post refresh: %w: %w", errRefreshUpstreamUnreachable, context.DeadlineExceeded),
			want:       remotesessionmetrics.RefreshOutcomeCanceled,
		},
		{
			name: "POST's internal timeout with a live caller",
			err:  fmt.Errorf("post refresh: %w: %w", errRefreshUpstreamUnreachable, context.DeadlineExceeded),
			want: remotesessionmetrics.RefreshOutcomeUnreachable,
		},
		{
			name: "connection refused",
			err:  fmt.Errorf("post refresh: %w: %w", errRefreshUpstreamUnreachable, errors.New("dial tcp: connection refused")),
			want: remotesessionmetrics.RefreshOutcomeUnreachable,
		},
		{
			name: "database failure",
			err:  errors.New("re-read active remote_session: connection reset"),
			want: remotesessionmetrics.RefreshOutcomeInternalError,
		},
		{
			name: "configuration error before the POST",
			err:  newTokenRefreshError("the identity provider has no token endpoint configured", nil),
			want: remotesessionmetrics.RefreshOutcomeInternalError,
		},
		{
			name: "lost compare-and-swap after the POST",
			err:  newTokenRefreshError("the session was rotated by another request", errRefreshNotApplied),
			want: remotesessionmetrics.RefreshOutcomeInternalError,
		},
		{
			name: "rate limited",
			err:  newTokenRefreshErrorFromHTTP(http.StatusTooManyRequests, "429 Too Many Requests", []byte(`{"error":"temporarily_unavailable"}`)),
			want: remotesessionmetrics.RefreshOutcomeRateLimited,
		},
		{
			name: "rate limited with no body",
			err:  newTokenRefreshErrorFromHTTP(http.StatusTooManyRequests, "429 Too Many Requests", nil),
			want: remotesessionmetrics.RefreshOutcomeRateLimited,
		},
		{
			name: "5xx with a parsed non-invalid_grant body",
			err:  newTokenRefreshErrorFromHTTP(http.StatusServiceUnavailable, "503 Service Unavailable", []byte(`{"error":"temporarily_unavailable"}`)),
			want: remotesessionmetrics.RefreshOutcomeUpstreamError,
		},
		{
			name: "5xx with no body",
			err:  newTokenRefreshErrorFromHTTP(http.StatusBadGateway, "502 Bad Gateway", nil),
			want: remotesessionmetrics.RefreshOutcomeUpstreamError,
		},
		{
			name: "invalid_grant",
			err:  newTokenRefreshErrorFromHTTP(http.StatusBadRequest, "400 Bad Request", []byte(`{"error":"invalid_grant","error_description":"expired"}`)),
			want: remotesessionmetrics.RefreshOutcomeInvalidGrant,
		},
		{
			name: "vendor-shaped invalid_grant",
			err:  newTokenRefreshErrorFromHTTP(http.StatusBadRequest, "400 Bad Request", []byte(`{"errors": ["invalid_grant - Invalid or expired refresh token or code verifier."]}`)),
			want: remotesessionmetrics.RefreshOutcomeInvalidGrant,
		},
		{
			name: "invalid_grant on a 429 still cleared the grant",
			err:  newTokenRefreshErrorFromHTTP(http.StatusTooManyRequests, "429 Too Many Requests", []byte(`{"error":"invalid_grant"}`)),
			want: remotesessionmetrics.RefreshOutcomeInvalidGrant,
		},
		{
			name: "invalid_grant on a 5xx still cleared the grant",
			err:  newTokenRefreshErrorFromHTTP(http.StatusInternalServerError, "500 Internal Server Error", []byte(`{"error":"invalid_grant"}`)),
			want: remotesessionmetrics.RefreshOutcomeInvalidGrant,
		},
		{
			name: "parsed non-invalid_grant code",
			err:  newTokenRefreshErrorFromHTTP(http.StatusUnauthorized, "401 Unauthorized", []byte(`{"error":"invalid_client"}`)),
			want: remotesessionmetrics.RefreshOutcomeRejected,
		},
		{
			name: "unparsed 4xx body",
			err:  newTokenRefreshErrorFromHTTP(http.StatusForbidden, "403 Forbidden", []byte(`<html>forbidden</html>`)),
			want: remotesessionmetrics.RefreshOutcomeRejectedUnparsed,
		},
		{
			name: "wrapped in context",
			err:  fmt.Errorf("resolve access token: %w", newTokenRefreshErrorFromHTTP(http.StatusBadRequest, "400 Bad Request", []byte(`{"error":"invalid_grant"}`))),
			want: remotesessionmetrics.RefreshOutcomeInvalidGrant,
		},
	}

	for _, tc := range cases {
		ctx := t.Context()
		if tc.callerGone {
			ctx = expired
		}
		require.Equal(t, tc.want, refreshOutcomeForError(ctx, tc.err), tc.name)
	}
}

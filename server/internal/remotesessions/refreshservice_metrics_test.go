package remotesessions_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// A definitive invalid_grant records invalid_grant once with the issuer URL
// and the caller's trigger, and the next attempt on the now-cleared grant
// records no_grant without contacting the upstream: the AIS-616 signature the
// dashboard expects to see. The failure error carries the same issuer and
// outcome the metric recorded, so a log line can be joined to the series.
func TestRefreshNow_RecordsInvalidGrantThenNoGrant(t *testing.T) {
	t.Parallel()

	ctx, env := newSyntheticExpiryEnv(t, "refreshnow-metrics", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"dead-refresh"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`))
	})

	reader := sdkmetric.NewManualReader()
	env.refresher = env.newRefresher(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), cache.NoopCache)

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)

	_, err = env.refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerScheduled)
	var failure *remotesessions.RefreshError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "https://idp.example.com", failure.IssuerURL)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeInvalidGrant, failure.Outcome)
	var tokenErr *remotesessions.TokenRefreshError
	require.ErrorAs(t, err, &tokenErr, "the typed failure must not hide the operator-actionable cause")

	cleared, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.False(t, cleared.RefreshTokenEncrypted.Valid)

	_, err = env.refresher.RefreshNow(ctx, cleared, "", remotesessionmetrics.RefreshTriggerManual)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "the typed failure must not hide the no-grant sentinel")
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "https://idp.example.com", failure.IssuerURL)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeNoGrant, failure.Outcome)

	points := upstreamRefreshDataPoints(t, reader)
	require.Len(t, points, 2)
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.OAuthIssuer("https://idp.example.com"),
		attr.OAuthRefreshTrigger(remotesessionmetrics.RefreshTriggerScheduled),
		attr.Outcome(remotesessionmetrics.RefreshOutcomeInvalidGrant),
	)])
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.OAuthIssuer("https://idp.example.com"),
		attr.OAuthRefreshTrigger(remotesessionmetrics.RefreshTriggerManual),
		attr.Outcome(remotesessionmetrics.RefreshOutcomeNoGrant),
	)])
}

// A successful refresh records refreshed and hands the issuer URL back on the
// result, the success-side counterpart of RefreshError.
func TestRefreshNow_RecordsRefreshed(t *testing.T) {
	t.Parallel()

	ctx, env := newSyntheticExpiryEnv(t, "refreshnow-metrics-ok", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "refresh_token" {
			_, _ = w.Write([]byte(`{"access_token":"expired-access","refresh_token":"live-refresh","expires_in":1}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"rotated-refresh","expires_in":3600}`))
	})

	reader := sdkmetric.NewManualReader()
	env.refresher = env.newRefresher(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), cache.NoopCache)

	result, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerRequest)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, "fresh-access", result.AccessToken)
	require.Equal(t, "https://idp.example.com", result.IssuerURL)

	points := upstreamRefreshDataPoints(t, reader)
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[attribute.NewSet(
		attr.OAuthIssuer("https://idp.example.com"),
		attr.OAuthRefreshTrigger(remotesessionmetrics.RefreshTriggerRequest),
		attr.Outcome(remotesessionmetrics.RefreshOutcomeRefreshed),
	)])
}

// upstreamRefreshDataPoints drains the reader and returns the upstream-refresh
// counter's data points keyed by their full attribute set, failing the test
// when the instrument was never recorded.
func upstreamRefreshDataPoints(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.remote_session.upstream_refresh" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "upstream refresh instrument must be an int64 counter")
			points := make(map[attribute.Set]int64, len(sum.DataPoints))
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] = dp.Value
			}
			return points
		}
	}
	t.Fatal("gram.remote_session.upstream_refresh was never recorded")
	return nil
}

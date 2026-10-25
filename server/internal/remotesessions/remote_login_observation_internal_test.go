package remotesessions

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestExchangeFailureKind_SeparatesLocalFaultsFromProvider(t *testing.T) {
	t.Parallel()

	tunnelFailure := newTokenEndpointError(http.StatusBadGateway, "502 Bad Gateway", nil)
	tunnelFailure.tunnelError = wire.TunnelErrorSubstreamFailed
	endpoint := func(status int, body string) error {
		return newTokenEndpointError(status, http.StatusText(status), []byte(body))
	}
	cases := []struct {
		name     string
		err      error
		tunneled bool
		want     RemoteLoginFailure
		wantCode string
	}{
		{name: "signing unavailable", err: &tokenEndpointSigningError{err: fmt.Errorf("sign: %w", errTokenEndpointSigningUnavailable)}, want: RemoteLoginFailureSigning},
		{name: "signing failed", err: &tokenEndpointSigningError{err: errors.New("kms")}, want: RemoteLoginFailureSigning},
		{name: "assertion key unconfigured", err: &tokenEndpointSigningError{err: fmt.Errorf("sign: %w", errClientAssertionKeyUnconfigured)}, want: RemoteLoginFailureAssertionKey},
		{name: "transport", err: &tokenExchangeUnavailableError{err: errors.New("dial tcp: connection refused")}, want: RemoteLoginFailureUnreachable},
		{name: "no tunnel route", err: &tokenExchangeUnavailableError{err: fmt.Errorf("post: %w", tunnelrouting.ErrNoTunnelRoute)}, want: RemoteLoginFailureTunnel},
		{name: "tunnel agent", err: tunnelFailure, tunneled: true, want: RemoteLoginFailureTunnel},
		{name: "provider 503", err: endpoint(http.StatusServiceUnavailable, ""), want: RemoteLoginFailureUnavailable},
		{name: "provider 500", err: endpoint(http.StatusInternalServerError, ""), want: RemoteLoginFailureServerError},
		{name: "provider rejects", err: endpoint(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"vendor text"}`), want: RemoteLoginFailureRejected, wantCode: "invalid_grant"},
		{name: "unregistered code kept out", err: endpoint(http.StatusBadRequest, `{"error":"app_not_enabled"}`), want: RemoteLoginFailureRejected},
		{name: "200 invalid_grant", err: endpoint(http.StatusOK, `{"error":"invalid_grant"}`), want: RemoteLoginFailureRejected, wantCode: "invalid_grant"},
		{name: "200 server_error", err: endpoint(http.StatusOK, `{"error":"server_error"}`), want: RemoteLoginFailureServerError, wantCode: "server_error"},
		{name: "200 temporarily_unavailable", err: endpoint(http.StatusOK, `{"error":"temporarily_unavailable"}`), want: RemoteLoginFailureUnavailable, wantCode: "temporarily_unavailable"},
		{name: "200 malformed", err: &tokenEndpointError{statusCode: http.StatusOK, tunnelError: "", err: errors.New("token endpoint returned no access_token")}, want: RemoteLoginFailureInvalidResponse},
		{name: "request not built", err: errors.New("new token request"), want: RemoteLoginFailureInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			failure, code := exchangeFailureKind(tc.err, tc.tunneled)
			require.Equal(t, tc.want, failure)
			require.Equal(t, tc.wantCode, code)
		})
	}
}

func TestRefreshObservations_UnfinishedAtCloseStaysCounted(t *testing.T) {
	t.Parallel()

	sink := NewRefreshObservations(4)
	finished := sink.start(time.Now())
	provider := uuid.New()
	sink.setProvider(finished, provider)
	sink.finish(finished, remotesessionmetrics.RefreshOutcomeInvalidGrant)
	running := sink.start(time.Now())

	observations, dropped := sink.Close()
	sink.finish(running, remotesessionmetrics.RefreshOutcomeRefreshed)
	require.Zero(t, sink.start(time.Now())+1, "a closed collector reserves nothing")

	require.Zero(t, dropped)
	require.Len(t, observations, 2)
	require.True(t, observations[0].Finished)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeInvalidGrant, observations[0].Outcome)
	require.Equal(t, uuid.NullUUID{UUID: provider, Valid: true}, observations[0].ProviderIssuerID)
	require.False(t, observations[1].Finished, "a refresh still running at Close is reported unfinished, and its late result ignored")
	require.Empty(t, observations[1].Outcome)
}

func TestRefreshObservations_CapacityCountsTheRest(t *testing.T) {
	t.Parallel()

	sink := NewRefreshObservations(64)
	for range 70 {
		sink.finish(sink.start(time.Now()), remotesessionmetrics.RefreshOutcomeRefreshed)
	}
	observations, dropped := sink.Close()
	require.Len(t, observations, 64)
	require.Equal(t, 6, dropped)
}

func TestRefreshObservations_CompletionRacesClose(t *testing.T) {
	t.Parallel()

	for range 50 {
		sink := NewRefreshObservations(32)
		var wg sync.WaitGroup
		started := make(chan struct{}, 16)
		for range 16 {
			wg.Go(func() {
				slot := sink.start(time.Now())
				started <- struct{}{}
				sink.setProvider(slot, uuid.New())
				sink.finish(slot, remotesessionmetrics.RefreshOutcomeRefreshed)
			})
		}
		for range 8 {
			<-started
		}
		observations, dropped := sink.Close()
		wg.Wait()
		require.Zero(t, dropped)
		require.GreaterOrEqual(t, len(observations), 8, "every refresh that started before Close is counted")
		for _, observation := range observations {
			if observation.Finished {
				require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, observation.Outcome)
			}
		}
	}
}

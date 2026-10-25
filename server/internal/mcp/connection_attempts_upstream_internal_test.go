package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCallbackResult(t *testing.T) {
	t.Parallel()

	observed := func(phase remotesessions.RemoteLoginPhase, failure remotesessions.RemoteLoginFailure, code string) *remotesessions.RemoteLoginObservation {
		return &remotesessions.RemoteLoginObservation{Phase: phase, Failure: failure, ProviderErrorCode: code}
	}
	unauthorized := oops.E(oops.CodeUnauthorized, nil, "refused")
	unexpected := oops.E(oops.CodeUnexpected, nil, "failed")
	cases := []struct {
		name string
		obs  *remotesessions.RemoteLoginObservation
		err  error
		want inboundResult
	}{
		{name: "stored", obs: observed(remotesessions.RemoteLoginPhaseStored, "", ""), want: inboundResult{outcome: outcomeStored, class: classNone, reason: reasonNone}},
		{name: "retried", obs: observed(remotesessions.RemoteLoginPhaseRetried, "", ""), want: inboundResult{outcome: outcomeRetried, class: classNone, reason: reasonNone}},
		{name: "person declined", obs: observed(remotesessions.RemoteLoginPhaseDenied, "", "access_denied"), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "access_denied"}},
		{name: "provider server_error", obs: observed(remotesessions.RemoteLoginPhaseDenied, "", "server_error"), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classEndpoint, reason: "server_error"}},
		{name: "provider temporarily_unavailable", obs: observed(remotesessions.RemoteLoginPhaseDenied, "", "temporarily_unavailable"), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classEndpoint, reason: "temporarily_unavailable"}},
		{name: "unregistered denial", obs: observed(remotesessions.RemoteLoginPhaseDenied, "", ""), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: reasonProviderDenied}},
		{name: "retry refused", obs: observed(remotesessions.RemoteLoginPhaseRetrying, "", ""), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: reasonResourceRejected}},
		{name: "retry could not be minted", obs: observed(remotesessions.RemoteLoginPhaseRetrying, "", ""), err: unexpected, want: inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonUnexpected}},
		{name: "state mismatch", obs: observed(remotesessions.RemoteLoginPhaseValidating, "", ""), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "unauthorized"}},
		{name: "signing", obs: observed(remotesessions.RemoteLoginPhaseExchanging, remotesessions.RemoteLoginFailureSigning, ""), err: oops.E(oops.CodeUnavailable, nil, "x"), want: inboundResult{outcome: outcomeFailed, class: classInternal, reason: "assertion_signing"}},
		{name: "assertion key", obs: observed(remotesessions.RemoteLoginPhaseExchanging, remotesessions.RemoteLoginFailureAssertionKey, ""), err: unexpected, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "assertion_key_unconfigured"}},
		{name: "provider 503", obs: observed(remotesessions.RemoteLoginPhaseExchanging, remotesessions.RemoteLoginFailureUnavailable, ""), err: oops.E(oops.CodeUnavailable, nil, "x"), want: inboundResult{outcome: outcomeFailed, class: classEndpoint, reason: "provider_unavailable"}},
		{name: "transport", obs: observed(remotesessions.RemoteLoginPhaseExchanging, remotesessions.RemoteLoginFailureUnreachable, ""), err: oops.E(oops.CodeGatewayError, nil, "x"), want: inboundResult{outcome: outcomeFailed, class: classEndpoint, reason: "provider_unreachable"}},
		{name: "exchange invalid_grant", obs: observed(remotesessions.RemoteLoginPhaseExchanging, remotesessions.RemoteLoginFailureRejected, "invalid_grant"), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "invalid_grant"}},
		{name: "connection removed while storing", obs: observed(remotesessions.RemoteLoginPhaseStoring, remotesessions.RemoteLoginFailureConnectionGone, ""), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "connection_changed"}},
		{name: "unrenewable token", obs: observed(remotesessions.RemoteLoginPhaseStoring, remotesessions.RemoteLoginFailureUnrenewableToken, ""), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "unrenewable_token"}},
		{name: "login expired", obs: observed(remotesessions.RemoteLoginPhaseStoring, remotesessions.RemoteLoginFailureLoginExpired, ""), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "login_expired"}},
		{name: "store refused otherwise", obs: observed(remotesessions.RemoteLoginPhaseStoring, "", ""), err: unauthorized, want: inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: reasonStoreRefused}},
		{name: "store failed", obs: observed(remotesessions.RemoteLoginPhaseStoring, "", ""), err: unexpected, want: inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonStoreFailed}},
		{name: "canceled", obs: observed(remotesessions.RemoteLoginPhaseExchanging, remotesessions.RemoteLoginFailureUnreachable, ""), err: context.Canceled, want: inboundResult{outcome: outcomeFailed, class: classCanceled, reason: reasonCanceled}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, callbackResult(t.Context(), tc.obs, tc.err))
		})
	}
}

func TestRefreshResult(t *testing.T) {
	t.Parallel()

	finished := func(outcome remotesessionmetrics.RefreshOutcome) remotesessions.RefreshObservation {
		return remotesessions.RefreshObservation{Started: time.Now(), ProviderIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Finished: true, Outcome: outcome}
	}
	cases := map[string]struct {
		obs  remotesessions.RefreshObservation
		want inboundResult
	}{
		"refreshed":        {finished(remotesessionmetrics.RefreshOutcomeRefreshed), inboundResult{outcome: outcomeRefreshed, class: classNone, reason: reasonNone}},
		"adopted":          {finished(remotesessionmetrics.RefreshOutcomeAdoptedConcurrentWinner), inboundResult{outcome: outcomeAdopted, class: classNone, reason: reasonNone}},
		"no grant":         {finished(remotesessionmetrics.RefreshOutcomeNoGrant), inboundResult{outcome: outcomeNoGrant, class: classNone, reason: "no_grant"}},
		"session inactive": {finished(remotesessionmetrics.RefreshOutcomeSessionInactive), inboundResult{outcome: outcomeNoGrant, class: classNone, reason: "session_inactive"}},
		"invalid grant":    {finished(remotesessionmetrics.RefreshOutcomeInvalidGrant), inboundResult{outcome: outcomeFailed, class: classConfiguration, reason: "invalid_grant"}},
		"unreachable":      {finished(remotesessionmetrics.RefreshOutcomeUnreachable), inboundResult{outcome: outcomeFailed, class: classEndpoint, reason: "unreachable"}},
		"canceled":         {finished(remotesessionmetrics.RefreshOutcomeCanceled), inboundResult{outcome: outcomeFailed, class: classCanceled, reason: "canceled"}},
		"internal":         {finished(remotesessionmetrics.RefreshOutcomeInternalError), inboundResult{outcome: outcomeFailed, class: classInternal, reason: "internal_error"}},
		"still running":    {remotesessions.RefreshObservation{Started: time.Now(), ProviderIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Finished: false, Outcome: ""}, inboundResult{outcome: outcomeUnknown, class: classNone, reason: reasonInProgress}},
	}
	for name, tc := range cases {
		require.Equal(t, tc.want, refreshResult(tc.obs), name)
	}
}

// Each recorded refresh is one row and nothing else is written: the
// collector keeps at most the limit and counts the rest as dropped (see
// TestRefreshObservations_CapacityCountsTheRest), so a capped request never
// gains a fabricated attempt.
func TestRefreshRowsAreOnePerRecordedRefresh(t *testing.T) {
	t.Parallel()

	observations := make([]remotesessions.RefreshObservation, 0, gatewayRefreshObservationLimit)
	for range gatewayRefreshObservationLimit {
		observations = append(observations, remotesessions.RefreshObservation{Started: time.Now(), ProviderIssuerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Finished: true, Outcome: remotesessionmetrics.RefreshOutcomeRefreshed})
	}
	caller := attemptCaller{projectID: uuid.New(), organizationID: "org", gatewayID: uuid.New(), userSessionIssuerID: uuid.Nil, subject: urn.SessionSubject{Kind: "", ID: ""}, oauthClientID: "", traceID: ""}
	rows, err := refreshRows(caller, observations, "anonymous")
	require.NoError(t, err)
	require.Len(t, rows, gatewayRefreshObservationLimit)
	for i, row := range rows {
		require.Equal(t, attemptStageUpstreamRefresh, row.Attributes[attr.ConnectionAttemptStageKey])
		require.Equal(t, observations[i].ProviderIssuerID.UUID.String(), row.Attributes[attr.ConnectionAttemptProviderIDKey])
		require.NotContains(t, row.Attributes, attr.ConnectionAttemptCallsKey)
	}
}

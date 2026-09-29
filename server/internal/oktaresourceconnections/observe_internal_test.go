package oktaresourceconnections

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
)

func TestResultFor(t *testing.T) {
	t.Parallel()

	outcome := func(stage identitychaining.Stage, reason identitychaining.Reason, retryable bool) identitychaining.Outcome {
		return identitychaining.Outcome{Stage: stage, Reason: reason, Confidence: identitychaining.ConfidenceInferred, Retryable: retryable, Cached: false}
	}
	tests := []struct {
		name      string
		outcome   identitychaining.Outcome
		validated bool
		want      Result
	}{
		{"success", outcome(identitychaining.StageComplete, identitychaining.ReasonSuccess, false), true, ResultVerified},
		{"success without a validated grant", outcome(identitychaining.StageComplete, identitychaining.ReasonSuccess, false), false, ""},
		{"resource AS rejection", outcome(identitychaining.StageRedemption, identitychaining.ReasonInvalidGrant, false), true, ResultDownstreamRejected},
		{"resource AS insufficient scope", outcome(identitychaining.StageRedemption, identitychaining.ReasonInsufficientScope, false), true, ResultDownstreamRejected},
		{"malformed or local redemption failure", outcome(identitychaining.StageRedemption, identitychaining.ReasonMalformedResponse, false), true, ""},
		{"redemption before any grant", outcome(identitychaining.StageRedemption, identitychaining.ReasonInvalidGrant, false), false, ""},
		{"transient redemption", outcome(identitychaining.StageRedemption, identitychaining.ReasonTransientFailure, true), true, ""},
		{"exchange invalid_target", outcome(identitychaining.StageExchange, identitychaining.ReasonInvalidTarget, false), false, ResultConnectionMissing},
		{"exchange scope denied", outcome(identitychaining.StageExchange, identitychaining.ReasonScopePolicyDenied, false), false, ResultScopeNotAllowed},
		{"exchange invalid_client", outcome(identitychaining.StageExchange, identitychaining.ReasonInvalidClient, false), false, ResultClientAuthFailed},
		{"exchange access_denied", outcome(identitychaining.StageExchange, identitychaining.ReasonAccessDenied, false), false, ""},
		{"exchange invalid_grant", outcome(identitychaining.StageExchange, identitychaining.ReasonInvalidGrant, false), false, ""},
		{"validation", outcome(identitychaining.StageValidation, identitychaining.ReasonMalformedAssertion, false), true, ""},
		{"persistence", outcome(identitychaining.StagePersistence, identitychaining.ReasonStaleConfiguration, true), true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resultFor(identitychaining.Observation{Outcome: tt.outcome, GrantValidated: tt.validated})
			require.Equal(t, tt.want != "", ok)
			if ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestSupersedes(t *testing.T) {
	t.Parallel()

	confirmed := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	row := func(previous Result, at time.Time) repo.OktaResourceConnection {
		rc := repo.OktaResourceConnection{UpdatedAt: conv.ToPGTimestamptz(confirmed)}
		if previous != "" {
			rc.ObservedResult = conv.ToPGText(string(previous))
			rc.ObservedAt = conv.ToPGTimestamptz(at)
		}
		return rc
	}
	observed := confirmed.Add(time.Hour)

	require.False(t, supersedes(row("", time.Time{}), ResultVerified, confirmed.Add(-time.Second)), "started before confirmation")
	require.True(t, supersedes(row("", time.Time{}), ResultVerified, confirmed.Add(time.Second)))
	require.False(t, supersedes(row(ResultVerified, observed), ResultConnectionMissing, observed.Add(-time.Second)), "older attempt")
	require.True(t, supersedes(row(ResultVerified, observed), ResultVerified, observed.Add(time.Minute)), "unchanged results advance the observation timestamp")
	require.True(t, supersedes(row(ResultVerified, observed), ResultVerified, observed.Add(time.Hour)), "unchanged refresh")
	require.True(t, supersedes(row(ResultVerified, observed), ResultConnectionMissing, observed.Add(time.Second)), "org-level failure replaces verified")
	require.True(t, supersedes(row(ResultVerified, observed), ResultClientAuthFailed, observed.Add(time.Second)))
	require.True(t, supersedes(row(ResultVerified, observed), ResultDownstreamRejected, observed.Add(time.Second)), "newer definitive failure replaces verified immediately")
	require.True(t, supersedes(row(ResultVerified, observed), ResultScopeNotAllowed, observed.Add(time.Second)))
	require.True(t, supersedes(row(ResultDownstreamRejected, observed), ResultVerified, observed.Add(time.Second)), "success always replaces a failure")

	for _, previous := range []Result{ResultVerified, ResultConnectionMissing} {
		t.Run(string(previous)+" rejects older contradictory completion", func(t *testing.T) {
			t.Parallel()
			rc := row(previous, observed)
			older := observed.Add(time.Minute)
			newer := observed.Add(2 * time.Minute)
			require.True(t, supersedes(rc, previous, newer))
			rc.ObservedAt = conv.ToPGTimestamptz(newer)
			contradictory := ResultVerified
			if previous == ResultVerified {
				contradictory = ResultConnectionMissing
			}
			require.False(t, supersedes(rc, contradictory, older))
		})
	}
}

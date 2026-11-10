package remotesessions

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/stretchr/testify/require"
)

func TestConnectionOutageHappened(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind RefreshOutageKind
		want mcpconnectionattempts.Code
	}{
		{RefreshOutageNone, mcpconnectionattempts.CodeInternalError},
		{RefreshOutageProvider, mcpconnectionattempts.CodeRemoteSessionUnavailable},
		{RefreshOutageTunnel, mcpconnectionattempts.CodeTunnelUnavailable},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			err := &RefreshError{IssuerURL: "", Outcome: remotesessionmetrics.RefreshOutcomeUpstreamError, retryAfter: 0, visibleOutage: tc.kind, outageCause: tc.kind, err: errors.New("outage")}
			require.Equal(t, tc.want, ConnectionOutageHappened(fmt.Errorf("check session: %w", &SessionOutageError{err: err})))
		})
	}
	require.Equal(t, mcpconnectionattempts.CodeInternalError, ConnectionOutageHappened(&SessionOutageError{err: errors.New("store unavailable")}))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, ConnectionOutageHappened(ErrRemoteSessionUnavailable))
}

// The connect page may not see an outage when Redis failed; the row still names the provider or tunnel.
func TestConnectionOutageHappened_UnrecordedOutage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		outcome     remotesessionmetrics.RefreshOutcome
		outageCause RefreshOutageKind
		cause       error
		want        mcpconnectionattempts.Code
	}{
		{"provider outage record not stored", remotesessionmetrics.RefreshOutcomeUpstreamError, RefreshOutageProvider, errors.New("outage"), mcpconnectionattempts.CodeRemoteSessionUnavailable},
		{"tunnel outage record not stored", remotesessionmetrics.RefreshOutcomeUnreachable, RefreshOutageTunnel, errors.New("outage"), mcpconnectionattempts.CodeTunnelUnavailable},
		{"cooldown with unreadable record", remotesessionmetrics.RefreshOutcomeDeferred, RefreshOutageProvider, &refreshDeferredError{wait: 0, why: refreshDeferralCooldown}, mcpconnectionattempts.CodeRemoteSessionUnavailable},
		{"another request holds the lock", remotesessionmetrics.RefreshOutcomeDeferred, RefreshOutageNone, &refreshDeferredError{wait: 0, why: refreshDeferralLockHeld}, mcpconnectionattempts.Code{}},
		{"own setup used up the lock", remotesessionmetrics.RefreshOutcomeDeferred, RefreshOutageNone, &refreshDeferredError{wait: 0, why: refreshDeferralSetupSlow}, mcpconnectionattempts.CodeInternalError},
		{"lease lost after slow setup", remotesessionmetrics.RefreshOutcomeDeferred, RefreshOutageNone, fmt.Errorf("%w: %w", errRefreshLeaseLost, &refreshDeferredError{wait: 0, why: refreshDeferralSetupSlow}), mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := &RefreshError{IssuerURL: "", Outcome: tc.outcome, retryAfter: 0, visibleOutage: RefreshOutageNone, outageCause: tc.outageCause, err: tc.cause}
			require.Equal(t, tc.want, ConnectionOutageHappened(&SessionOutageError{err: err}))
		})
	}
}

func TestConnectionOutageHappened_ClientCredentialTokenEndpoint(t *testing.T) {
	t.Parallel()
	require.Equal(t, mcpconnectionattempts.CodeRemoteSessionUnavailable, ConnectionOutageHappened(errors.Join(ErrRemoteSessionUnavailable, &TokenEndpointError{StatusCode: http.StatusServiceUnavailable})))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, ConnectionOutageHappened(errors.Join(ErrRemoteSessionUnavailable, &TokenEndpointError{Signing: true})))
}

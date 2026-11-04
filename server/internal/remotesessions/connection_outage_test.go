package remotesessions

import (
	"errors"
	"fmt"
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
			err := &RefreshError{IssuerURL: "", Outcome: remotesessionmetrics.RefreshOutcomeInternalError, retryAfter: 0, outageKind: tc.kind, err: errors.New("outage")}
			require.Equal(t, tc.want, ConnectionOutageHappened(fmt.Errorf("check session: %w", &SessionOutageError{err: err})))
		})
	}
	require.Equal(t, mcpconnectionattempts.CodeInternalError, ConnectionOutageHappened(&SessionOutageError{err: errors.New("store unavailable")}))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, ConnectionOutageHappened(ErrRemoteSessionUnavailable))
}

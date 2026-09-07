package remotemcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakeKillswitchCheckpoint struct {
	disposition killswitches.TransportDisposition
	err         error
	calls       int
	orgID       string
	serverID    string
}

func (f *fakeKillswitchCheckpoint) Evaluate(_ context.Context, organizationID, mcpServerID string) (killswitches.TransportDisposition, error) {
	f.calls++
	f.orgID = organizationID
	f.serverID = mcpServerID
	return f.disposition, f.err
}

func TestToolsCallKillswitchInterceptor(t *testing.T) {
	t.Parallel()

	match, err := killswitches.NewMatchedDenialDisposition("Maintenance requested exactly.")
	require.NoError(t, err)
	cases := []struct {
		name        string
		disposition killswitches.TransportDisposition
		err         error
		wantReject  *proxy.RejectError
	}{
		{name: "no match continues", disposition: killswitches.NewContinueDisposition()},
		{name: "match preserves external note", disposition: match, wantReject: proxy.NewKillswitchMatchRejection("Maintenance requested exactly.")},
		{name: "infrastructure rejection is sanitized", disposition: killswitches.NewInfrastructureRejectionDisposition(), err: errors.New("database host secret"), wantReject: proxy.NewKillswitchInfrastructureRejection()},
		{name: "inconsistent continue failure fails closed", disposition: killswitches.NewContinueDisposition(), err: errors.New("resolution failed"), wantReject: proxy.NewKillswitchInfrastructureRejection()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkpoint := &fakeKillswitchCheckpoint{disposition: tc.disposition, err: tc.err}
			interceptor := NewToolsCallKillswitchInterceptor(checkpoint, "org-id", "server-id", testenv.NewLogger(t))
			rejection := interceptor.InterceptToolsCallRequest(t.Context(), nil)
			if tc.wantReject == nil {
				require.NoError(t, rejection)
			} else {
				var got *proxy.RejectError
				require.ErrorAs(t, rejection, &got)
				require.Equal(t, tc.wantReject, got)
				require.NotContains(t, got.Message, "database host secret")
				require.NotContains(t, got.Message, "resolution failed")
			}
			require.Equal(t, 1, checkpoint.calls)
			require.Equal(t, "org-id", checkpoint.orgID)
			require.Equal(t, "server-id", checkpoint.serverID)
			require.Equal(t, "tools-call-killswitch", interceptor.Name())
		})
	}
}

func TestToolsCallKillswitchInterceptorMissingCheckpointFailsClosed(t *testing.T) {
	t.Parallel()
	interceptor := NewToolsCallKillswitchInterceptor(nil, "org-id", "server-id", testenv.NewLogger(t))
	var rejection *proxy.RejectError
	require.ErrorAs(t, interceptor.InterceptToolsCallRequest(t.Context(), nil), &rejection)
	require.Equal(t, proxy.NewKillswitchInfrastructureRejection(), rejection)
}

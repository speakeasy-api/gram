package tailnetgateway

import (
	"testing"

	"github.com/stretchr/testify/require"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

func TestHostsServiceReadsServiceHostCapability(t *testing.T) {
	t.Parallel()

	self := func(values ...string) *ipnstate.PeerStatus {
		raw := make([]tailcfg.RawMessage, 0, len(values))
		for _, v := range values {
			raw = append(raw, tailcfg.RawMessage(v))
		}
		caps := make(tailcfg.NodeCapMap, 1)
		caps[nodecap.ServiceHost] = raw
		return &ipnstate.PeerStatus{CapMap: caps}
	}

	// The shape control sent on a real tailnet once the host was approved.
	approved := self(`{"svc:private":["100.107.234.251","fd7a:115c:a1e0::6131:eafc"]}`)
	require.True(t, hostsService(approved, "svc:private"))
	require.False(t, hostsService(approved, "svc:other"), "approval is per service")

	require.False(t, hostsService(self(`{"svc:other":["100.100.1.2"]}`), "svc:private"))
	require.False(t, hostsService(self(`{"svc:private":[]}`), "svc:private"), "a service with no addresses cannot be reached")
	require.False(t, hostsService(self(`not json`), "svc:private"))
	require.False(t, hostsService(&ipnstate.PeerStatus{}, "svc:private"), "no capability means not approved")
	require.False(t, hostsService(nil, "svc:private"))

	// Multiple capability values are merged; any of them may carry the service.
	require.True(t, hostsService(self(`{"svc:other":["100.100.1.2"]}`, `{"svc:private":["100.100.1.3"]}`), "svc:private"))
}

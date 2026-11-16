package tailnetgateway

import (
	"testing"

	"github.com/stretchr/testify/require"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tsnet"
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
	check := func(status *ipnstate.PeerStatus, service tailcfg.ServiceName) bool {
		t.Helper()
		ok, err := hostsService(status, service)
		require.NoError(t, err)
		return ok
	}

	require.True(t, check(approved, "svc:private"))
	require.False(t, check(approved, "svc:other"), "approval is per service")

	require.False(t, check(self(`{"svc:other":["100.100.1.2"]}`), "svc:private"))
	require.False(t, check(self(`{"svc:private":[]}`), "svc:private"), "a service with no addresses cannot be reached")
	require.False(t, check(&ipnstate.PeerStatus{}, "svc:private"), "no capability means not approved")
	require.False(t, check(nil, "svc:private"))

	// Multiple capability values are merged; any of them may carry the service.
	require.True(t, check(self(`{"svc:other":["100.100.1.2"]}`, `{"svc:private":["100.100.1.3"]}`), "svc:private"))

	// A malformed capability fails closed but reports why.
	ok, err := hostsService(self(`not json`), "svc:private")
	require.Error(t, err)
	require.False(t, ok)
}

// TestServiceModeLeavesTLSToTheGateway guards the fix for a slot ordering a
// certificate outside the issuance lease: if tsnet terminated TLS, in HTTPS
// mode or with TerminateTLS, it would order one for any client that connects
// before one is stored. The opt-in issuancetest package shows that against a
// real tsnet and ACME server.
func TestServiceModeLeavesTLSToTheGateway(t *testing.T) {
	t.Parallel()
	require.Equal(t, tsnet.ServiceModeTCP{Port: 443, TerminateTLS: false, PROXYProtocolVersion: 2}, serviceMode(),
		"tsnet forwards raw TCP with the peer's address and never terminates TLS")
}

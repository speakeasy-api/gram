package customdomains

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
)

func TestObserveDNS(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		aliases   map[string]string
		lookupErr error
		foreign   bool
		status    DNSConfigurationStatus
		method    DNSRoutingMethod
		wantErr   bool
	}{
		{name: "direct shared target", aliases: map[string]string{"domain.example.": "SHARED.EXAMPLE."}, status: DNSConfigurationMatched, method: DNSRoutingMethodSharedCNAME},
		{name: "customer intermediate", aliases: map[string]string{"domain.example.": "alias.example.", "alias.example.": "shared.example."}, status: DNSConfigurationMatched, method: DNSRoutingMethodSharedCNAME},
		{name: "apex addresses", status: DNSConfigurationMatched, method: DNSRoutingMethodARecord},
		{name: "proxy addresses", foreign: true, status: DNSConfigurationUnconfirmed, method: DNSRoutingMethodUnknown},
		{name: "shared alias with different address snapshots", aliases: map[string]string{"domain.example.": "shared.example."}, foreign: true, status: DNSConfigurationMatched, method: DNSRoutingMethodSharedCNAME},
		{name: "own target without trailing dot", aliases: map[string]string{"domain.example.": "abcdefghij.domains.example"}, foreign: true, status: DNSConfigurationMatched, method: DNSRoutingMethodPerDomainCNAME},
		{name: "own target through intermediate", aliases: map[string]string{"domain.example.": "alias.example.", "alias.example.": "ABCDEFGHIJ.DOMAINS.EXAMPLE."}, foreign: true, status: DNSConfigurationMatched, method: DNSRoutingMethodPerDomainCNAME},
		{name: "other target before shared", aliases: map[string]string{"domain.example.": "klmnopqrst.domains.example.", "klmnopqrst.domains.example.": "shared.example."}, status: DNSConfigurationWrongTarget, method: DNSRoutingMethodUnknown},
		{name: "NXDOMAIN", lookupErr: &net.DNSError{Err: "no such host", IsNotFound: true}, status: DNSConfigurationNotFound, method: DNSRoutingMethodUnknown},
		{name: "timeout", lookupErr: context.DeadlineExceeded, status: DNSConfigurationCheckFailed, method: DNSRoutingMethodUnknown, wantErr: true},
		{name: "loop", aliases: map[string]string{"domain.example.": "alias.example.", "alias.example.": "domain.example."}, status: DNSConfigurationCheckFailed, method: DNSRoutingMethodUnknown, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver := dns.NewMockResolver(dns.MockResolverConfig{
				LookupCNAMERecordFunc: func(_ context.Context, host string) (string, error) { return tc.aliases[host], tc.lookupErr },
				LookupCNAMEFunc: func(context.Context, string) (string, error) {
					return "", errors.New("canonical name lookup must not be used")
				},
				LookupNetIPFunc: func(_ context.Context, _, host string) ([]netip.Addr, error) {
					require.NotContains(t, []DNSRoutingMethod{DNSRoutingMethodSharedCNAME, DNSRoutingMethodPerDomainCNAME}, tc.method, "a recognized alias must not depend on address snapshots")
					ip := "192.0.2.1"
					if host == "domain.example." && tc.foreign {
						ip = "192.0.2.2"
					}
					return []netip.Addr{netip.MustParseAddr(ip)}, nil
				},
			})
			got, err := ObserveDNS(t.Context(), resolver, "domain.example", DNSRoutingTargets{SharedCNAME: "shared.example", DomainTarget: "abcdefghij.domains.example", TargetSuffix: "domains.example", ARecords: nil})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, DNSObservation{Status: tc.status, Method: tc.method}, got)
		})
	}
}

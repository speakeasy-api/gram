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

func TestObserveDNSWithoutTargetSuffix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		aliases map[string]string
	}{
		{name: "shared target", aliases: map[string]string{"domain.example.": "shared.example."}},
		{name: "token shaped intermediate", aliases: map[string]string{"domain.example.": "klmnopqrst.domains.example.", "klmnopqrst.domains.example.": "shared.example."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver := dns.NewMockResolver(dns.MockResolverConfig{
				LookupCNAMERecordFunc: func(_ context.Context, host string) (string, error) { return tc.aliases[host], nil },
			})
			got, err := ObserveDNS(t.Context(), resolver, "domain.example", DNSRoutingTargets{
				SharedCNAME: "shared.example", DomainTarget: "", TargetSuffix: "", ARecords: nil,
			})
			require.NoError(t, err)
			require.Equal(t, DNSObservation{Status: DNSConfigurationMatched, Method: DNSRoutingMethodSharedCNAME}, got)
		})
	}
}

func TestObserveDNSTargetAddressFailures(t *testing.T) {
	t.Parallel()
	const sharedTarget = "shared.example."
	const domainTarget = "abcdefghij.domains.example."
	matchingIP := netip.MustParseAddr("192.0.2.1")
	otherIP := netip.MustParseAddr("192.0.2.2")
	sharedErr := errors.New("shared target unavailable")
	domainErr := errors.New("domain target unavailable")
	for _, tc := range []struct {
		name            string
		staticAddresses []netip.Addr
		targetAddresses map[string][]netip.Addr
		targetErrors    map[string]error
		status          DNSConfigurationStatus
		wantErrors      []error
	}{
		{
			name:            "static address matches despite differing live targets",
			staticAddresses: []netip.Addr{matchingIP},
			targetAddresses: map[string][]netip.Addr{sharedTarget: {otherIP}, domainTarget: {otherIP}},
			status:          DNSConfigurationMatched,
		},
		{
			name:            "static address matches despite failed live targets",
			staticAddresses: []netip.Addr{matchingIP},
			targetErrors:    map[string]error{sharedTarget: sharedErr, domainTarget: domainErr},
			status:          DNSConfigurationMatched,
		},
		{
			name:            "other live target proves a match",
			targetAddresses: map[string][]netip.Addr{domainTarget: {matchingIP}},
			targetErrors:    map[string]error{sharedTarget: sharedErr},
			status:          DNSConfigurationMatched,
		},
		{
			name:            "unmatched static addresses cannot exclude failed target",
			staticAddresses: []netip.Addr{otherIP},
			targetErrors:    map[string]error{sharedTarget: sharedErr, domainTarget: domainErr},
			status:          DNSConfigurationCheckFailed, wantErrors: []error{sharedErr, domainErr},
		},
		{
			name:            "unmatched live target cannot exclude failed target",
			targetAddresses: map[string][]netip.Addr{sharedTarget: {otherIP}},
			targetErrors:    map[string]error{domainTarget: domainErr},
			status:          DNSConfigurationCheckFailed, wantErrors: []error{domainErr},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver := dns.NewMockResolver(dns.MockResolverConfig{
				LookupCNAMERecordFunc: func(context.Context, string) (string, error) { return "", nil },
				LookupNetIPFunc: func(_ context.Context, _, host string) ([]netip.Addr, error) {
					if host == "domain.example." {
						return []netip.Addr{matchingIP}, nil
					}
					return tc.targetAddresses[host], tc.targetErrors[host]
				},
			})
			got, err := ObserveDNS(t.Context(), resolver, "domain.example", DNSRoutingTargets{
				SharedCNAME: sharedTarget, DomainTarget: domainTarget, TargetSuffix: "domains.example", ARecords: tc.staticAddresses,
			})
			if len(tc.wantErrors) == 0 {
				require.NoError(t, err)
			} else {
				for _, wantErr := range tc.wantErrors {
					require.ErrorIs(t, err, wantErr)
				}
			}
			wantMethod := DNSRoutingMethodUnknown
			if tc.status == DNSConfigurationMatched {
				wantMethod = DNSRoutingMethodARecord
			}
			require.Equal(t, DNSObservation{Status: tc.status, Method: wantMethod}, got)
		})
	}
}

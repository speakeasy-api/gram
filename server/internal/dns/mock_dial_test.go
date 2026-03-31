package dns_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
)

func TestMockResolver_Resolver_LookupCNAME(t *testing.T) {
	t.Parallel()

	m := dns.NewMockResolver(dns.MockResolverConfig{
		LookupCNAMEFunc: func(_ context.Context, host string) (string, error) {
			return "cname." + host + ".", nil
		},
	})

	r := m.Resolver()
	got, err := r.LookupCNAME(t.Context(), "example.com")
	require.NoError(t, err)
	require.Equal(t, "cname.example.com.", got)
}

func TestMockResolver_Resolver_LookupHost(t *testing.T) {
	t.Parallel()

	m := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, network, _ string) ([]net.IP, error) {
			if network == "ip4" {
				return []net.IP{net.ParseIP("1.2.3.4")}, nil
			}
			return nil, nil
		},
	})

	r := m.Resolver()
	addrs, err := r.LookupHost(t.Context(), "example.com")
	require.NoError(t, err)
	require.Contains(t, addrs, "1.2.3.4")
}

func TestMockResolver_Resolver_LookupTXT(t *testing.T) {
	t.Parallel()

	m := dns.NewMockResolver(dns.MockResolverConfig{
		LookupTXTFunc: func(_ context.Context, name string) ([]string, error) {
			return []string{"v=spf1 include:example.com ~all"}, nil
		},
	})

	r := m.Resolver()
	txts, err := r.LookupTXT(t.Context(), "example.com")
	require.NoError(t, err)
	require.Equal(t, []string{"v=spf1 include:example.com ~all"}, txts)
}

func TestMockResolver_Resolver_LookupMX(t *testing.T) {
	t.Parallel()

	m := dns.NewMockResolver(dns.MockResolverConfig{
		LookupMXFunc: func(_ context.Context, _ string) ([]*net.MX, error) {
			return []*net.MX{
				{Host: "mail.example.com.", Pref: 10},
			}, nil
		},
	})

	r := m.Resolver()
	mxs, err := r.LookupMX(t.Context(), "example.com")
	require.NoError(t, err)
	require.Len(t, mxs, 1)
	require.Equal(t, "mail.example.com.", mxs[0].Host)
	require.Equal(t, uint16(10), mxs[0].Pref)
}

func TestMockResolver_Resolver_LookupNS(t *testing.T) {
	t.Parallel()

	m := dns.NewMockResolver(dns.MockResolverConfig{
		LookupNSFunc: func(_ context.Context, _ string) ([]*net.NS, error) {
			return []*net.NS{
				{Host: "ns1.example.com."},
			}, nil
		},
	})

	r := m.Resolver()
	nss, err := r.LookupNS(t.Context(), "example.com")
	require.NoError(t, err)
	require.Len(t, nss, 1)
	require.Equal(t, "ns1.example.com.", nss[0].Host)
}

func TestMockResolver_Resolver_LookupAddr(t *testing.T) {
	t.Parallel()

	m := dns.NewMockResolver(dns.MockResolverConfig{
		LookupAddrFunc: func(_ context.Context, _ string) ([]string, error) {
			return []string{"host.example.com."}, nil
		},
	})

	r := m.Resolver()
	names, err := r.LookupAddr(t.Context(), "1.2.3.4")
	require.NoError(t, err)
	require.Equal(t, []string{"host.example.com."}, names)
}

func TestMockResolver_Resolver_NilFuncReturnsError(t *testing.T) {
	t.Parallel()

	// Default config: all funcs return "not implemented" errors.
	m := dns.NewMockResolver(dns.MockResolverConfig{})

	r := m.Resolver()
	_, err := r.LookupTXT(t.Context(), "example.com")
	require.Error(t, err)

	var dnsErr *net.DNSError
	require.ErrorAs(t, err, &dnsErr)
}

func TestMockResolver_Resolver_FuncErrorReturnsSERVFAIL(t *testing.T) {
	t.Parallel()

	m := dns.NewMockResolver(dns.MockResolverConfig{
		LookupTXTFunc: func(context.Context, string) ([]string, error) {
			return nil, fmt.Errorf("mock DNS failure")
		},
	})

	r := m.Resolver()
	_, err := r.LookupTXT(t.Context(), "example.com")
	require.Error(t, err)

	var dnsErr *net.DNSError
	require.ErrorAs(t, err, &dnsErr)
}

func TestMockResolver_Resolver_LookupIPv6(t *testing.T) {
	t.Parallel()

	m := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, network, _ string) ([]net.IP, error) {
			if network == "ip6" {
				return []net.IP{net.ParseIP("2001:db8::1")}, nil
			}
			return nil, nil
		},
	})

	r := m.Resolver()
	ips, err := r.LookupIP(t.Context(), "ip6", "example.com")
	require.NoError(t, err)
	require.Len(t, ips, 1)
	require.Equal(t, "2001:db8::1", ips[0].String())
}

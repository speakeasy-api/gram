package dns_test

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
)

func TestIssueAllowsNoIssueTags(t *testing.T) {
	t.Parallel()

	require.True(t, dns.IssueAllows(nil, dns.LetsEncryptIssueDomain))
	require.True(t, dns.IssueAllows([]dns.CAA{{
		Flag:  0,
		Tag:   "iodef",
		Value: "mailto:dns@example.com",
	}}, dns.LetsEncryptIssueDomain))
}

func TestIssueAllowsLetsEncryptAmongOthers(t *testing.T) {
	t.Parallel()

	require.True(t, dns.IssueAllows([]dns.CAA{
		{Flag: 0, Tag: "issue", Value: "pki.goog"},
		{Flag: 0, Tag: "issue", Value: `letsencrypt.org; validationmethods=dns-01`},
	}, dns.LetsEncryptIssueDomain))
}

func TestIssueAllowsRejectsOtherIssuers(t *testing.T) {
	t.Parallel()

	require.False(t, dns.IssueAllows([]dns.CAA{
		{Flag: 0, Tag: "issue", Value: "pki.goog"},
		{Flag: 0, Tag: "issue", Value: "digicert.com"},
	}, dns.LetsEncryptIssueDomain))
	require.False(t, dns.IssueAllows([]dns.CAA{
		{Flag: 0, Tag: "issue", Value: ";"},
	}, dns.LetsEncryptIssueDomain))
}

func TestIssueAllowsRejectsCriticalUnknownTag(t *testing.T) {
	t.Parallel()

	require.False(t, dns.IssueAllows([]dns.CAA{
		{Flag: 1, Tag: "future-property", Value: "x"},
		{Flag: 0, Tag: "issue", Value: "letsencrypt.org"},
	}, dns.LetsEncryptIssueDomain))
}

func TestFindIssueRestrictionUsesClosestRRset(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupCAAFunc: func(_ context.Context, name string) ([]dns.CAA, error) {
			switch name {
			case "mcp.example.com":
				return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
			case "example.com":
				return []dns.CAA{{Flag: 0, Tag: "issue", Value: "pki.goog"}}, nil
			default:
				return nil, nil
			}
		},
	})

	restriction, err := dns.FindIssueRestriction(t.Context(), resolver, "mcp.example.com")
	require.NoError(t, err)
	require.Equal(t, "example.com", restriction.Name)
	require.Len(t, restriction.Records, 1)
	require.Equal(t, "pki.goog", restriction.Records[0].Value)
	require.False(t, dns.IssueAllows(restriction.Records, dns.LetsEncryptIssueDomain))
}

func TestFindIssueRestrictionAllowsWhenNoRecords(t *testing.T) {
	t.Parallel()

	restriction, err := dns.FindIssueRestriction(t.Context(), dns.NewMockResolver(dns.MockResolverConfig{}), "chat.example.com")
	require.NoError(t, err)
	require.Empty(t, restriction.Name)
	require.True(t, dns.IssueAllows(restriction.Records, dns.LetsEncryptIssueDomain))
}

func TestFindIssueRestrictionPropagatesLookupErrors(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupCAAFunc: func(context.Context, string) ([]dns.CAA, error) {
			return nil, &net.DNSError{Err: "server failure", IsNotFound: false}
		},
	})

	_, err := dns.FindIssueRestriction(t.Context(), resolver, "chat.example.com")
	require.Error(t, err)
}

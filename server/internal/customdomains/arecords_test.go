package customdomains_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
)

func TestParseExpectedARecordsValidatesAndDedupes(t *testing.T) {
	t.Parallel()

	addrs, err := customdomains.ParseExpectedARecords([]string{" 34.127.46.134 ", "34.83.69.209", "34.127.46.134", ""})
	require.NoError(t, err)
	require.Equal(t, []netip.Addr{
		netip.MustParseAddr("34.83.69.209"),
		netip.MustParseAddr("34.127.46.134"),
	}, addrs)
}

func TestParseExpectedARecordsRejectsNonIPv4(t *testing.T) {
	t.Parallel()

	_, err := customdomains.ParseExpectedARecords([]string{"2001:db8::1"})
	require.ErrorContains(t, err, "only IPv4 addresses are supported")

	_, err = customdomains.ParseExpectedARecords([]string{"not-an-ip"})
	require.ErrorContains(t, err, "invalid custom domain A record")
}

func TestFormatARecords(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"34.83.69.209"}, customdomains.FormatARecords([]netip.Addr{netip.MustParseAddr("34.83.69.209")}))
	require.Empty(t, customdomains.FormatARecords(nil))
}

func TestIsProbablyApexDomain(t *testing.T) {
	t.Parallel()

	require.True(t, customdomains.IsProbablyApexDomain("example.com"))
	require.True(t, customdomains.IsProbablyApexDomain("gemini-api-docs-mcp.dev"))
	require.True(t, customdomains.IsProbablyApexDomain("Example.COM."))
	require.True(t, customdomains.IsProbablyApexDomain("example.co.uk"), "two-part public suffixes still have a registrable apex")
	require.False(t, customdomains.IsProbablyApexDomain("chat.example.com"))
	require.False(t, customdomains.IsProbablyApexDomain("mcp.example.co.uk"))
	require.False(t, customdomains.IsProbablyApexDomain(""))
}

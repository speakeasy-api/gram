package customdomains

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseExpectedARecordsValidatesAndDedupes(t *testing.T) {
	t.Parallel()

	addrs, err := ParseExpectedARecords([]string{" 34.127.46.134 ", "34.83.69.209", "34.127.46.134", ""})
	require.NoError(t, err)
	require.Equal(t, []netip.Addr{
		netip.MustParseAddr("34.83.69.209"),
		netip.MustParseAddr("34.127.46.134"),
	}, addrs)
}

func TestParseExpectedARecordsRejectsNonIPv4(t *testing.T) {
	t.Parallel()

	_, err := ParseExpectedARecords([]string{"2001:db8::1"})
	require.ErrorContains(t, err, "only IPv4 addresses are supported")

	_, err = ParseExpectedARecords([]string{"not-an-ip"})
	require.ErrorContains(t, err, "invalid custom domain A record")
}

func TestFormatARecords(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"34.83.69.209"}, FormatARecords([]netip.Addr{netip.MustParseAddr("34.83.69.209")}))
	require.Empty(t, FormatARecords(nil))
}

func TestIsProbablyApexDomain(t *testing.T) {
	t.Parallel()

	require.True(t, IsProbablyApexDomain("example.com"))
	require.True(t, IsProbablyApexDomain("gemini-api-docs-mcp.dev"))
	require.True(t, IsProbablyApexDomain("Example.COM."))
	require.True(t, IsProbablyApexDomain("example.co.uk"), "two-part public suffixes still have a registrable apex")
	require.False(t, IsProbablyApexDomain("chat.example.com"))
	require.False(t, IsProbablyApexDomain("mcp.example.co.uk"))
	require.False(t, IsProbablyApexDomain(""))
}

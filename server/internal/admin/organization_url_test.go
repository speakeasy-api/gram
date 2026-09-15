package admin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func invalidOrganizationURLs() []string {
	return []string{
		"", "   ", "Example Company", "com", "co.uk", "github.io", "localhost", "app.localhost",
		"http://localhost", "ftp://example.com", "mailto:person@example.com", "https:example.com",
		"//example.com", "https:///example.com", "https://user:pass@example.com", "https://@example.com",
		"example.com:443", "https://example.com:443", "http://example.com:80", "https://example.com:",
		"127.0.0.1", "127.1", "2130706433", "0x7f000001", "0177.0.0.1", "example.123",
		"http://[::1]", "[2001:db8::1]", "*.example.com", "-bad.example.com", "bad-.example.com",
		"bad_label.example.com", ".example.com", "example..com", "example.com..", "https://example%2ecom",
		"https://example.com\\@other.com", "https://exa\tmple.com", "https://example.com/\npath",
		"https://b\u00fccher.de", "https://\u212a.com", "https://example\u3002com",
		"xn--a.com", "xn--.com", "xn--invalidpunycode-.com", "ab--cd.com", "https://example.com/\xff",
		strings.Repeat("a", 64) + ".com", strings.Repeat("a", 63) + "." + strings.Repeat("b", 33) + ".com",
		"example.com" + strings.Repeat(" ", 3990),
	}
}

func TestOrganizationHostname_RejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	for _, input := range invalidOrganizationURLs() {
		host, err := organizationHostname(input)
		require.Error(t, err, "input=%q", input)
		require.Empty(t, host, "input=%q", input)
	}
}

func TestOrganizationHostname_PreservesExactHost(t *testing.T) {
	t.Parallel()
	cases := []struct{ input, host string }{
		{"example.com", "example.com"},
		{"  HTTPS://EXAMPLE.COM./about?q=1#team  ", "example.com"},
		{"http://example.com", "example.com"},
		{"www.example.com", "www.example.com"},
		{"https://a.b.example.co.uk/path", "a.b.example.co.uk"},
		{"tenant.github.io", "tenant.github.io"},
		{"xn--bcher-kva.de", "xn--bcher-kva.de"},
		{"example.com/path?q=1#fragment", "example.com"},
		{"example.com" + strings.Repeat(" ", 3989), "example.com"},
		{strings.Repeat("a", 63) + "." + strings.Repeat("b", 32) + ".com", strings.Repeat("a", 63) + "." + strings.Repeat("b", 32) + ".com"},
	}
	for _, tc := range cases {
		host, err := organizationHostname(tc.input)
		require.NoError(t, err, "input=%q", tc.input)
		require.Equal(t, tc.host, host)
	}
}

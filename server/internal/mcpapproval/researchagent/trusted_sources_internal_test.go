package researchagent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrustedSourceCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "listed domain", url: "https://nvd.nist.gov/vuln/detail/CVE-2026-1234", want: "vulnerability database"},
		{name: "subdomain of a listed domain", url: "https://blog.owasp.org/top-ten", want: "security organization"},
		{name: "path-scoped entry matches its section", url: "https://github.com/advisories/GHSA-xxxx", want: "vulnerability database"},
		{name: "path-scoped entry vouches nothing outside it", url: "https://github.com/some-vendor/some-repo", want: ""},
		{name: "a sibling path sharing the prefix is outside it", url: "https://github.com/advisories-malicious/x", want: ""},
		{name: "the exact section page matches", url: "https://github.com/advisories", want: "vulnerability database"},
		{name: "lookalike suffix is not a subdomain", url: "https://notowasp.org/", want: ""},
		{name: "unlisted domain", url: "https://random-blog.example.com/post", want: ""},
		{name: "junk", url: "::not a url::", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, trustedSourceCategory(tt.url))
		})
	}
}

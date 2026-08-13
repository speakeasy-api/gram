package urls_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urls"
)

func TestIsAbsoluteHTTP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want bool
	}{
		{raw: "https://idp.example.com/docs", want: true},
		{raw: "http://idp.example.com", want: true},
		{raw: "https://idp.example.com:8443/docs?a=1#frag", want: true},
		// url.Parse lowercases the scheme, so an uppercase one is still http(s).
		{raw: "HTTPS://idp.example.com", want: true},
		{raw: "", want: false},
		{raw: "docs", want: false},
		{raw: "/relative/docs", want: false},
		{raw: "//idp.example.com/docs", want: false},
		{raw: "javascript:alert(1)", want: false},
		{raw: "mailto:legal@idp.example.com", want: false},
		{raw: "ftp://idp.example.com", want: false},
		{raw: "https://", want: false},
		{raw: "data:text/html,<script>alert(1)</script>", want: false},
		{raw: "https://idp.example.com\n", want: false},
		{raw: "ht tp://idp.example.com", want: false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, urls.IsAbsoluteHTTP(tc.raw), "IsAbsoluteHTTP(%q)", tc.raw)
	}
}

// The loopback carve-out is a security boundary: it decides which URLs Gram
// will send a token to in plaintext, so the cases that must stay rejected
// matter more than the ones that pass.
func TestIsAbsoluteHTTPSOrLoopback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want bool
	}{
		{raw: "https://idp.example.com/revoke", want: true},
		{raw: "https://idp.example.com:8443/revoke", want: true},
		{raw: "HTTPS://idp.example.com/revoke", want: true},

		// Loopback over plaintext: the token never crosses a network. This is
		// what lets the dev-idp harness and httptest upstreams work.
		{raw: "http://127.0.0.1:8080/revoke", want: true},
		{raw: "http://localhost:8080/revoke", want: true},
		{raw: "http://[::1]:8080/revoke", want: true},
		{raw: "http://127.9.9.9/revoke", want: true},

		// Plaintext to anywhere else would put a live token on the wire.
		{raw: "http://idp.example.com/revoke", want: false},
		// Adjacent to loopback but routable — 127.0.0.1 with a trailing label,
		// and a host that merely starts with "localhost", must not slip through
		// on a prefix match.
		{raw: "http://127.0.0.1.example.com/revoke", want: false},
		{raw: "http://localhost.example.com/revoke", want: false},
		{raw: "http://10.0.0.5/revoke", want: false},
		{raw: "http://169.254.169.254/latest/meta-data", want: false},

		{raw: "", want: false},
		{raw: "/relative/revoke", want: false},
		{raw: "//idp.example.com/revoke", want: false},
		{raw: "javascript:alert(1)", want: false},
		{raw: "ftp://idp.example.com", want: false},
		{raw: "https://", want: false},
		{raw: "http://", want: false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, urls.IsAbsoluteHTTPSOrLoopback(tc.raw), "IsAbsoluteHTTPSOrLoopback(%q)", tc.raw)
	}
}

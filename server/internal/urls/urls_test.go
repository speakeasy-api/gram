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

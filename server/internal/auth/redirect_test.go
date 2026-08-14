package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSiteOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "origin only", input: "https://app.example.com", want: "https://app.example.com"},
		{name: "trailing path is dropped", input: "http://localhost:3000/dashboard", want: "http://localhost:3000"},
		{name: "port is part of the origin", input: "https://localhost:5173", want: "https://localhost:5173"},
		{name: "case normalized", input: "HTTPS://App.Example.COM", want: "https://app.example.com"},
		{name: "default https port is dropped", input: "https://app.example.com:443", want: "https://app.example.com"},
		{name: "default http port is dropped", input: "http://app.example.com:80", want: "http://app.example.com"},
		{name: "non-default port is kept", input: "https://app.example.com:8443", want: "https://app.example.com:8443"},
		{name: "empty", input: "", want: ""},
		{name: "relative", input: "/dashboard", want: ""},
		{name: "scheme without host", input: "https://", want: ""},
		{name: "unparseable", input: "://nope", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, parseSiteOrigin(tt.input))
		})
	}
}

func TestSafeRedirectPath(t *testing.T) {
	t.Parallel()

	const origin = "https://app.example.com"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Ordinary destinations the dashboard actually asks for.
		{name: "rooted path", input: "/dashboard", want: "/dashboard"},
		{name: "path with query and fragment", input: "/projects?id=123#section", want: "/projects?id=123#section"},
		{name: "root", input: "/", want: "/"},
		{name: "disposition query", input: "/?disposition=assistants", want: "/?disposition=assistants"},
		{name: "same-origin absolute URL", input: "https://app.example.com/dashboard", want: "/dashboard"},
		{name: "same-origin absolute URL with query", input: "https://app.example.com/p?tab=settings", want: "/p?tab=settings"},
		{name: "same-origin absolute URL without a path", input: "https://app.example.com", want: "/"},
		{name: "same-origin absolute URL, mixed case host", input: "https://APP.example.com/dashboard", want: "/dashboard"},
		{name: "same-origin absolute URL spelling the default port", input: "https://app.example.com:443/dashboard", want: "/dashboard"},

		// An encoded slash is not a path separator to a browser, and the escaped
		// form is what goes back out in the Location header, so "/%2F%2Fevil.com"
		// stays a same-origin path rather than becoming "///evil.com".
		{name: "encoded slashes stay a path", input: "/%2F%2Fattacker.example.net", want: "/%2F%2Fattacker.example.net"},

		// AIS-428: a backslash after the leading slash is read by browsers as a
		// second slash, turning the value into a protocol-relative reference.
		{name: "backslash authority", input: `/\attacker.example.net`, want: ""},
		{name: "backslash then slash authority", input: `/\/attacker.example.net`, want: ""},
		{name: "double backslash authority", input: `\\attacker.example.net`, want: ""},
		{name: "backslash inside a path", input: `/dashboard\projects`, want: ""},
		{name: "encoded backslash is left alone", input: "/dashboard%5Cprojects", want: "/dashboard%5Cprojects"},

		// Other spellings of "leave this origin".
		{name: "protocol relative", input: "//attacker.example.net/phish", want: ""},
		{name: "protocol relative with extra slash", input: "///attacker.example.net", want: ""},
		{name: "foreign origin", input: "https://attacker.example.net/phish", want: ""},
		{name: "foreign origin on the allowed scheme-less form", input: "//app.example.com", want: ""},
		{name: "allowed host in userinfo", input: "https://app.example.com@attacker.example.net/", want: ""},
		{name: "scheme mismatch on the allowed host", input: "http://app.example.com/dashboard", want: ""},
		{name: "bare host", input: "attacker.example.net", want: ""},
		{name: "javascript scheme", input: "javascript:alert(1)", want: ""},
		{name: "data scheme", input: "data:text/html,<script>alert(1)</script>", want: ""},

		// Values with nowhere sensible to go.
		{name: "empty", input: "", want: ""},
		{name: "query only", input: "?tab=settings", want: ""},
		{name: "control character", input: "/\tdashboard", want: ""},

		// Percent-encoded separators stay encoded, so they cannot re-form an
		// authority once the browser reads the Location header.
		{name: "encoded double slash", input: "/%2F%2Fattacker.example.net", want: "/%2F%2Fattacker.example.net"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, safeRedirectPath(tt.input, origin))
		})
	}
}

// TestSafeRedirectPathWithoutOrigin covers a misconfigured or unparseable site
// URL: relative paths still work, and every absolute URL is refused rather than
// silently trusted.
func TestSafeRedirectPathWithoutOrigin(t *testing.T) {
	t.Parallel()

	require.Equal(t, "/dashboard", safeRedirectPath("/dashboard", ""))
	require.Empty(t, safeRedirectPath("https://app.example.com/dashboard", ""))
	require.Empty(t, safeRedirectPath("//app.example.com", ""))
}

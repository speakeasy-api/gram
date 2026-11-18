package consentpage

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFormActionSources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, redirect, sources string }{
		{"web", "https://client.example/callback?state=secret", "'self' https://client.example"},
		{"loopback", "http://127.0.0.1:43210/callback", "'self' http://127.0.0.1:43210"},
		{"ipv6", "http://[::1]:43210/callback", "'self' http://[::1]:43210"},
		{"native", "com.example.client:/oauth/callback", "'self' com.example.client:"},
		{"javascript", "javascript:alert(1)", "'self'"},
		{"fragment", "https://client.example/callback#fragment", "'self'"},
		{"empty fragment", "https://client.example/callback#", "'self'"},
		{"userinfo", "https://user:pass@client.example/callback", "'self'"},
		{"relative", "/callback", "'self'"},
		{"opaque https", "https:callback", "'self'"},
		{"empty hostname", "https://:443/callback", "'self'"},
		{"directive delimiter", "https://host.example;/callback", "'self'"},
		{"policy delimiter", "https://host.example,/callback", "'self'"},
		{"wildcard host", "https://*.example/callback", "'self'"},
		{"punycode host", "https://xn--bcher-kva.example/callback", "'self' https://xn--bcher-kva.example"},
		{"web port", "https://client.example:8443/callback", "'self' https://client.example:8443"},
	} {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); require.Equal(t, tc.sources, FormActionSources(tc.redirect)) })
	}
}

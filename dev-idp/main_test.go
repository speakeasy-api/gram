package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListenerScheme(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		certFile    string
		keyFile     string
		externalURL string
		want        string
		wantErr     string
	}{
		{name: "HTTP without TLS files", certFile: "", keyFile: "", externalURL: "", want: "http", wantErr: ""},
		{name: "HTTPS with TLS files", certFile: "local.pem", keyFile: "local-key.pem", externalURL: "", want: "https", wantErr: ""},
		{name: "missing key", certFile: "local.pem", keyFile: "", externalURL: "", want: "", wantErr: "ssl-cert-file and ssl-key-file must be provided together"},
		{name: "missing certificate", certFile: "", keyFile: "local-key.pem", externalURL: "", want: "", wantErr: "ssl-cert-file and ssl-key-file must be provided together"},
		{name: "matching HTTP URL", certFile: "", keyFile: "", externalURL: "http://localhost:35291", want: "http", wantErr: ""},
		{name: "matching HTTPS URL", certFile: "local.pem", keyFile: "local-key.pem", externalURL: "https://idp.example.ts.net:35291", want: "https", wantErr: ""},
		{name: "matching IPv6 HTTPS URL", certFile: "local.pem", keyFile: "local-key.pem", externalURL: "https://[::1]:35291", want: "https", wantErr: ""},
		{name: "HTTP URL with shared TLS files", certFile: "local.pem", keyFile: "local-key.pem", externalURL: "http://localhost:35291", want: "", wantErr: `external-url scheme "http" does not match listener scheme "https"`},
		{name: "HTTPS URL without TLS files", certFile: "", keyFile: "", externalURL: "https://idp.example.ts.net:35291", want: "", wantErr: `external-url scheme "https" does not match listener scheme "http"`},
		{name: "relative URL", certFile: "", keyFile: "", externalURL: "/oauth2-1", want: "", wantErr: "external-url must be an absolute HTTP(S) URL without credentials"},
		{name: "unsupported scheme", certFile: "", keyFile: "", externalURL: "ftp://idp.example.ts.net", want: "", wantErr: "external-url must be an absolute HTTP(S) URL without credentials"},
		{name: "URL credentials", certFile: "local.pem", keyFile: "local-key.pem", externalURL: "https://user@idp.example.ts.net", want: "", wantErr: "external-url must be an absolute HTTP(S) URL without credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme, err := listenerScheme(tc.certFile, tc.keyFile, tc.externalURL)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, scheme)
		})
	}
}

func TestDeriveExternalURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		addr   string
		scheme string
		want   string
	}{
		{name: "HTTP loopback", addr: "127.0.0.1:35291", scheme: "http", want: "http://127.0.0.1:35291"},
		{name: "HTTPS loopback", addr: "127.0.0.1:35291", scheme: "https", want: "https://127.0.0.1:35291"},
		{name: "HTTPS Tailnet", addr: "100.64.0.1:35291", scheme: "https", want: "https://100.64.0.1:35291"},
		{name: "HTTPS hostname", addr: "dev-machine.example.ts.net:35291", scheme: "https", want: "https://dev-machine.example.ts.net:35291"},
		{name: "HTTPS IPv6 loopback", addr: "[::1]:35291", scheme: "https", want: "https://[::1]:35291"},
		{name: "HTTPS wildcard", addr: ":35291", scheme: "https", want: "https://localhost:35291"},
		{name: "HTTPS IPv4 wildcard", addr: "0.0.0.0:35291", scheme: "https", want: "https://localhost:35291"},
		{name: "HTTPS IPv6 wildcard", addr: "[::]:35291", scheme: "https", want: "https://localhost:35291"},
		{name: "HTTP bare hostname", addr: "localhost", scheme: "http", want: "http://localhost"},
		{name: "HTTPS bare hostname", addr: "localhost", scheme: "https", want: "https://localhost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, deriveExternalURL(tc.addr, tc.scheme))
		})
	}
}

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListenerScheme(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		certFile string
		keyFile  string
		want     string
		wantErr  bool
	}{
		{name: "HTTP without TLS files", certFile: "", keyFile: "", want: "http", wantErr: false},
		{name: "HTTPS with TLS files", certFile: "local.pem", keyFile: "local-key.pem", want: "https", wantErr: false},
		{name: "missing key", certFile: "local.pem", keyFile: "", want: "", wantErr: true},
		{name: "missing certificate", certFile: "", keyFile: "local-key.pem", want: "", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme, err := listenerScheme(tc.certFile, tc.keyFile)
			if tc.wantErr {
				require.EqualError(t, err, "ssl-cert-file and ssl-key-file must be provided together")
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

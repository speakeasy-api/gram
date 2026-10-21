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

func TestListenerSchemeAllowsLoopbackHTTP(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", "127.255.255.255", "[::1]", "[::ffff:127.0.0.1]"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			scheme, err := listenerScheme("", "", "http://"+host+":35291")
			require.NoError(t, err)
			require.Equal(t, "http", scheme)
		})
	}
}

func TestListenerSchemeRejectsNonLoopbackHTTP(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"idp.example.ts.net", "100.64.0.1", "[fd7a:115c:a1e0::1]", "[::ffff:100.64.0.1]", "localhost.example.com", "127.0.0.1.example.com", "0.0.0.0"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			_, err := listenerScheme("", "", "http://"+host+":35291")
			require.EqualError(t, err, "external-url must use HTTPS unless its host is loopback")
		})
	}
}

func TestListenerSchemeRejectsQueryAndFragment(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"?x=1", "?", "#fragment", "#", "?x=1#fragment"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()

			_, err := listenerScheme("local.pem", "local-key.pem", "https://idp.example.ts.net:35291/"+suffix)
			require.EqualError(t, err, "external-url must not contain a query or fragment")
		})
	}
}

func TestListenerSchemeAllowsEscapedPathDelimiters(t *testing.T) {
	t.Parallel()

	scheme, err := listenerScheme("local.pem", "local-key.pem", "https://idp.example.ts.net:35291/tenant%3Fname%23id")
	require.NoError(t, err)
	require.Equal(t, "https", scheme)
}

func TestValidateListenerAddresses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		scheme         string
		publicAddress  string
		controlAddress string
		wantErr        string
	}{
		{name: "HTTPS public Tailnet with loopback control", scheme: "https", publicAddress: "100.64.0.1:35291", controlAddress: "127.0.0.1:35292", wantErr: ""},
		{name: "HTTPS public Tailnet with IPv6 loopback control", scheme: "https", publicAddress: "100.64.0.1:35291", controlAddress: "[::1]:35292", wantErr: ""},
		{name: "HTTPS public Tailnet with Tailnet control", scheme: "https", publicAddress: "100.64.0.1:35291", controlAddress: "100.64.0.1:35292", wantErr: "control-address: plain HTTP requires a loopback listener address"},
		{name: "HTTPS public Tailnet with wildcard control", scheme: "https", publicAddress: "100.64.0.1:35291", controlAddress: ":35292", wantErr: "control-address: plain HTTP requires a loopback listener address"},
		{name: "HTTPS public Tailnet with malformed control", scheme: "https", publicAddress: "100.64.0.1:35291", controlAddress: "localhost", wantErr: "control-address: plain HTTP requires a loopback listener address"},
		{name: "HTTP public loopback with loopback control", scheme: "http", publicAddress: "127.0.0.1:35291", controlAddress: "127.0.0.1:35292", wantErr: ""},
		{name: "HTTP public loopback with Tailnet control", scheme: "http", publicAddress: "127.0.0.1:35291", controlAddress: "100.64.0.1:35292", wantErr: "control-address: plain HTTP requires a loopback listener address"},
		{name: "HTTP public Tailnet with loopback control", scheme: "http", publicAddress: "100.64.0.1:35291", controlAddress: "127.0.0.1:35292", wantErr: "address: plain HTTP requires a loopback listener address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateListenerAddresses(tc.scheme, tc.publicAddress, tc.controlAddress)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateHTTPListenerAddress(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		address string
		wantErr bool
	}{
		{address: "localhost:35291", wantErr: false},
		{address: "127.0.0.1:35291", wantErr: false},
		{address: "127.255.255.255:35291", wantErr: false},
		{address: "[::1]:35291", wantErr: false},
		{address: "[::ffff:127.0.0.1]:35291", wantErr: false},
		{address: "100.64.0.1:35291", wantErr: true},
		{address: "[fd7a:115c:a1e0::1]:35291", wantErr: true},
		{address: "dev-machine.example.ts.net:35291", wantErr: true},
		{address: ":35291", wantErr: true},
		{address: "0.0.0.0:35291", wantErr: true},
		{address: "[::]:35291", wantErr: true},
		{address: "localhost", wantErr: true},
	} {
		t.Run(tc.address, func(t *testing.T) {
			t.Parallel()

			err := validateHTTPListenerAddress(tc.address)
			if tc.wantErr {
				require.EqualError(t, err, "plain HTTP requires a loopback listener address")
				return
			}
			require.NoError(t, err)
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

package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRejectsNonHTTPSServerURL(t *testing.T) {
	t.Parallel()
	for _, serverURL := range []string{"http://localhost:8080", "ftp://localhost", "localhost:8080", "/relative", "https:///no-host", "https://user:password@localhost", "https://localhost:65536"} {
		t.Run(serverURL, func(t *testing.T) {
			t.Parallel()
			for _, insecure := range []bool{false, true} {
				err := run(serverURL, "https://dashboard.example.test", insecure, slog.New(slog.NewTextHandler(io.Discard, nil)))
				require.ErrorContains(t, err, "server URL must be an absolute HTTPS URL")
			}
		})
	}
}

func TestClientURLsCanonicalOrigin(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ site, want string }{
		{"", "https://api.example.test"},
		{"https://APP.example.com:0443/path?q=1#fragment", "https://app.example.com"},
		{"HTTPS://APP.example.com:443", "https://app.example.com"},
		{"HTTP://APP.example.com:00080/path", "http://app.example.com"},
		{"https://APP.example.com:08443", "https://app.example.com:8443"},
		{"https://[2001:DB8::1]:0443/path", "https://[2001:db8::1]"},
		{"https://[2001:DB8::1]:08443", "https://[2001:db8::1]:8443"},
	} {
		t.Run(tt.site, func(t *testing.T) {
			t.Parallel()
			_, origin, err := clientURLs("https://API.example.test:0443", tt.site)
			require.NoError(t, err)
			require.Equal(t, tt.want, origin)
		})
	}
	for _, site := range []string{"/relative", "ftp://app.example.test", "https://app.example.test:65536", "https://app.example.test:port", "https://user:password@app.example.test"} {
		_, _, err := clientURLs("https://api.example.test", site)
		require.ErrorContains(t, err, "invalid site URL")
	}
}

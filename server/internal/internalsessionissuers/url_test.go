package internalsessionissuers_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
)

func TestURLRequiresHTTPSOutsideLocalDevelopment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		origin        string
		allowLoopback bool
		ok            bool
	}{
		{origin: "https://app.example.com", allowLoopback: false, ok: true},
		{origin: "https://localhost:8080", allowLoopback: false, ok: true},
		{origin: "http://localhost:8080", allowLoopback: false, ok: false},
		{origin: "http://localhost:8080", allowLoopback: true, ok: true},
		{origin: "http://app.example.com", allowLoopback: true, ok: false},
		{origin: "https://app.example.com/base", allowLoopback: false, ok: false},
	} {
		origin, err := url.Parse(tc.origin)
		require.NoError(t, err)
		issuer, err := internalsessionissuers.URL(origin, "org_1", tc.allowLoopback)
		if !tc.ok {
			require.Error(t, err, tc.origin)
			continue
		}
		require.NoError(t, err, tc.origin)
		require.Equal(t, tc.origin+"/oauth/idp/org/org_1", issuer.String())
	}
}

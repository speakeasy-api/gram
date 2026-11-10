package assets

import (
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformImagePublicURLTransport(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://admin.example.test")
	require.NoError(t, err)
	for _, raw := range []string{"https://public.example.test", "http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			site, err := url.Parse(raw)
			require.NoError(t, err)
			_, err = NewPlatformImageUploads(nil, nil, base, site, testenv.NewLogger(t))
			require.NoError(t, err)
		})
	}
	for _, raw := range []string{"http://public.example.test", "http://10.0.0.1", "http://localhost.evil.test", "https://user:pass@public.example.test", "ftp://public.example.test", "relative", ""} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			site, err := url.Parse(raw)
			require.NoError(t, err)
			_, err = NewPlatformImageUploads(nil, nil, base, site, testenv.NewLogger(t))
			require.Error(t, err)
		})
	}
	_, err = NewPlatformImageUploads(nil, nil, base, nil, testenv.NewLogger(t))
	require.Error(t, err)
}

package gram

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateServerURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         string
		environment string
		wantErr     string
	}{
		{name: "production HTTPS", raw: "https://api.example.com", environment: "prod"},
		{name: "local HTTP", raw: "http://localhost:8080", environment: "local"},
		{name: "relative", raw: "/api", environment: "local", wantErr: "absolute HTTP(S) URL"},
		{name: "unsupported scheme", raw: "ftp://api.example.com", environment: "local", wantErr: "absolute HTTP(S) URL"},
		{name: "userinfo", raw: "https://user:secret@api.example.com", environment: "prod", wantErr: "userinfo"},
		{name: "query", raw: "https://api.example.com?token=secret", environment: "prod", wantErr: "query"},
		{name: "forced query", raw: "https://api.example.com?", environment: "prod", wantErr: "query"},
		{name: "fragment", raw: "https://api.example.com/#secret", environment: "prod", wantErr: "fragment"},
		{name: "non-local HTTP", raw: "http://api.example.com", environment: "dev", wantErr: "HTTPS is required"},
		{name: "path is allowed", raw: "https://api.example.com/base", environment: "prod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parsed, err := url.Parse(tt.raw)
			require.NoError(t, err)
			err = validateServerURL(parsed, tt.environment)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

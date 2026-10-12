package gram

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCallbackOrigin(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://ai.example.com")
	require.NoError(t, err)
	platformHosts := map[string]string{"app.example.com": "https://app.example.com"}

	tests := []struct {
		name        string
		raw         string
		environment string
		want        string
		wantErr     string
	}{
		{name: "server host", raw: "https://ai.example.com", environment: "prod", want: "https://ai.example.com"},
		{name: "platform host with trailing slash", raw: "https://app.example.com/", environment: "prod", want: "https://app.example.com"},
		{name: "local HTTP", raw: "http://ai.example.com", environment: "local", want: "http://ai.example.com"},
		{name: "path", raw: "https://app.example.com/base", environment: "prod", wantErr: "without a path"},
		{name: "non-local HTTP", raw: "http://app.example.com", environment: "prod", wantErr: "HTTPS is required"},
		{name: "unserved host", raw: "https://other.example.com", environment: "prod", wantErr: "neither the server URL host nor a platform host"},
		{name: "relative", raw: "/callback", environment: "prod", wantErr: "absolute HTTP(S) URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseCallbackOrigin(tt.raw, serverURL, tt.environment, platformHosts)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.String())
		})
	}
}

package gram

import (
	"flag"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/mcp"
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

func TestParsePlatformHostsRefusesAuthenticationHost(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	authenticationHost, err := mcp.NewAuthenticationHost("https://auth.example.com", serverURL, "prod")
	require.NoError(t, err)

	parse := func(hosts ...string) (map[string]string, error) {
		set := flag.NewFlagSet("platform-hosts", flag.ContinueOnError)
		require.NoError(t, (&cli.StringSliceFlag{Name: "platform-hosts", Value: cli.NewStringSlice(hosts...)}).Apply(set))
		return parsePlatformHosts(cli.NewContext(nil, set, nil), authenticationHost)
	}

	hosts, err := parse("ai.example.com")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"ai.example.com": "https://ai.example.com"}, hosts)

	_, err = parse("ai.example.com", "auth.example.com")
	require.EqualError(t, err, "invalid platform hosts: auth.example.com is the authentication host")
}

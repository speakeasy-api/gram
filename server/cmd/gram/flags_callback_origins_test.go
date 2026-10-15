package gram

import (
	"flag"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
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
		{name: "platform host with non-default port", raw: "https://app.example.com:8443", environment: "prod", want: "https://app.example.com:8443"},
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

// The worker mints replacement issuer keys and the server serves and signs for
// the issuer, so both must derive the issuer origin from the same flag.
func TestServerAndWorkerDeriveTheSameIssuerOrigin(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://ai.example.com")
	require.NoError(t, err)
	args := []string{"--" + outboundCallbackURLFlag + "=https://app.example.com", "--" + platformHostsFlag + "=app.example.com"}

	origins := map[string]string{}
	for _, newCommand := range []func() *cli.Command{newStartCommand, newWorkerCommand} {
		command := newCommand()
		set := flag.NewFlagSet(command.Name, flag.ContinueOnError)
		for _, name := range []string{outboundCallbackURLFlag, platformHostsFlag} {
			require.NoError(t, requireFlag(t, command.Flags, name).Apply(set))
		}
		require.NoError(t, set.Parse(args))
		c := cli.NewContext(nil, set, nil)
		platformHosts, err := customdomains.ParsePlatformHosts(c.StringSlice(platformHostsFlag))
		require.NoError(t, err)
		callbackOrigins, err := callbackOriginsFromCLI(c, serverURL, "prod", platformHosts)
		require.NoError(t, err)
		origins[command.Name] = callbackOrigins.Outbound.String()
	}
	require.Equal(t, map[string]string{"start": "https://app.example.com", "worker": "https://app.example.com"}, origins)
}

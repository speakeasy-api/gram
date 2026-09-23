package gram

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestMCPJWKSBypassesTenantAndSessionMiddleware(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.String("environment", "local", "")
	flags.String("server-url", "https://gram.example", "")
	flags.String("site-url", "https://app.example", "")
	c := cli.NewContext(cli.NewApp(), flags, nil)
	serverURL, err := url.Parse("https://gram.example")
	require.NoError(t, err)
	host, err := mcp.NewAuthenticationHost("", serverURL, "local")
	require.NoError(t, err)
	mux, err := newMCPServerMux(c, testenv.NewLogger(t), nil, serverURL, host, nil, &background.Publishers{}, nil)
	require.NoError(t, err)
	mux.Handle(http.MethodGet, "/registered", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	// No DB/session services exist here. Public key requests must terminate
	// before custom-domain lookups or authentication on either ingress host.
	for _, authority := range []string{"gram.example", "custom.example"} {
		request := httptest.NewRequest(http.MethodGet, "https://"+authority+mcpauthz.JWKSPath, nil)
		request.Header.Set("Authorization", "invalid")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		require.JSONEq(t, `{"keys":[]}`, response.Body.String())
	}
}

func TestCallerAssertionFlagsPresentOnBothServingCommands(t *testing.T) {
	t.Parallel()
	for _, flags := range [][]cli.Flag{newStartCommand().Flags, mcpServerFlags()} {
		for _, name := range []string{"authz-private-key", "authz-public-keys"} {
			require.Contains(t, flagNames(flags), name)
		}
	}
}

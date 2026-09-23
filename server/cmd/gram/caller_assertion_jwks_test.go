package gram

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
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
	for _, authority := range []string{"gram.example", "tunnel.example", "custom.example"} {
		request := httptest.NewRequest(http.MethodGet, "https://"+authority+mcpauthz.JWKSPath, nil)
		request.Header.Set("Authorization", "invalid")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, strconv.Itoa(networkaccess.ServingPolicyVersion), response.Header().Get(networkaccess.ServingPolicyVersionHeader))
		require.JSONEq(t, `{"keys":[]}`, response.Body.String())
	}
}

func TestCallerAssertionFlagsPresentOnAllServingCommands(t *testing.T) {
	t.Parallel()
	for _, flags := range [][]cli.Flag{newStartCommand().Flags, mcpServerFlags(), privateIngressServerFlags()} {
		for _, name := range []string{"authz-private-key", "authz-public-keys", "authz-issuer-url", "tunnel-gateway-enabled"} {
			require.Contains(t, flagNames(flags), name)
		}
	}
}

func TestCallerAssertionConfigRequiredWithTunnelGateway(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	values := map[string]string{
		"authz-private-key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})),
		"authz-public-keys": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})),
		"authz-issuer-url":  "https://tunnel.example",
	}
	for _, missing := range []string{"", "all", "authz-private-key", "authz-public-keys", "authz-issuer-url"} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			flags.Bool("tunnel-gateway-enabled", true, "")
			flags.String("environment", "production", "")
			flags.String("server-url", "https://gram.example", "")
			for name, value := range values {
				if missing == name || missing == "all" {
					value = ""
				}
				flags.String(name, value, "")
			}
			c := cli.NewContext(cli.NewApp(), flags, nil)
			issuer, err := newCallerAssertions(c)
			if missing == "" {
				require.NoError(t, err)
				require.True(t, issuer.Enabled())
			} else {
				require.ErrorContains(t, err, "tunnel gateways require")
			}
			if missing == "all" {
				require.NoError(t, flags.Set("tunnel-gateway-enabled", "false"))
				issuer, err := newCallerAssertions(c)
				require.NoError(t, err)
				require.False(t, issuer.Enabled())
			}
		})
	}
}

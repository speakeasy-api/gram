package gram

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestKitWorkloadPrivateKeyCredentials(t *testing.T) {
	t.Parallel()
	c, private, err := kitWorkloadClientCredentials("private_key_jwt")
	require.NoError(t, err)
	require.Empty(t, c.Secret)
	require.Empty(t, c.JWKSURI)
	_, err = jwks.ValidateKeySource(c.AuthMethod, c.JWKS, c.JWKSURI)
	require.NoError(t, err)
	block, rest := pem.Decode(private)
	require.NotNil(t, block)
	require.Empty(t, rest)
	require.Equal(t, "PRIVATE KEY", block.Type)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	key, ok := parsed.(*ecdsa.PrivateKey)
	require.True(t, ok)
	require.Equal(t, elliptic.P256(), key.Curve)
	var jwks struct {
		Keys []map[string]string `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(c.JWKS, &jwks))
	require.Equal(t, []map[string]string{{"kty": "EC", "crv": "P-256", "use": "sig", "alg": "ES256", "x": base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))), "y": base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32)))}}, jwks.Keys)
	next, nextPrivate, err := kitWorkloadClientCredentials("private_key_jwt")
	require.NoError(t, err)
	require.NotEqual(t, private, nextPrivate)
	require.NotEqual(t, c.JWKS, next.JWKS)
}

func TestKitWorkloadAuthMethodConfiguration(t *testing.T) {
	t.Parallel()
	c, private, err := kitWorkloadClientCredentials("client_secret_basic")
	require.NoError(t, err)
	require.Equal(t, "client_secret_basic", c.AuthMethod)
	require.Empty(t, private)
	require.Empty(t, c.JWKS)
	for _, method := range []string{"", "client_secret_post", "PRIVATE_KEY_JWT", "private_key_jwt "} {
		_, _, err := kitWorkloadClientCredentials(method)
		require.Error(t, err)
	}
	found := false
	for _, f := range assistantKitRuntimeFlags() {
		if f.Names()[0] == "assistant-kit-mcp-auth-method" {
			found = true
			value, ok := f.(*cli.StringFlag)
			require.True(t, ok)
			require.Equal(t, "client_secret_basic", value.Value)
			require.Equal(t, []string{"GRAM_ASSISTANT_KIT_MCP_AUTH_METHOD"}, value.EnvVars)
		}
	}
	require.True(t, found, "authentication method flag must be registered")
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.String("assistant-kit-image", "kit:test", "")
	set.String("assistant-kit-mcp-auth-method", "invalid", "")
	require.ErrorContains(t, installKitLocalRuntimeFromCLI(cli.NewContext(nil, set, nil), nil, nil, nil, nil, nil), "invalid assistant-kit-mcp-auth-method")
}

func TestPrivateIngressRegistersAssistantRuntimeFlags(t *testing.T) {
	t.Parallel()
	flags := map[string]bool{}
	for _, f := range privateIngressServerFlags() {
		for _, name := range f.Names() {
			require.False(t, flags[name], "duplicate flag %s", name)
			flags[name] = true
		}
	}
	for _, f := range assistantRuntimeFlags() {
		require.True(t, flags[f.Names()[0]], "missing flag %s", f.Names()[0])
	}
	require.False(t, flags["assistant-kit-allow-insecure-inference"], "Kit requires its fixed HTTPS provider origin")
}

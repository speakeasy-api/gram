package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestResolveTokenEndpointAuthMethod_ExplicitWithSecret(t *testing.T) {
	t.Parallel()

	m, err := remotesessions.ResolveTokenEndpointAuthMethod("client_secret_basic", "s3cret")
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, m)

	m, err = remotesessions.ResolveTokenEndpointAuthMethod("client_secret_post", "s3cret")
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodPost, m)
}

func TestResolveTokenEndpointAuthMethod_ExplicitMissingSecretFails(t *testing.T) {
	t.Parallel()

	_, err := remotesessions.ResolveTokenEndpointAuthMethod("client_secret_basic", "")
	require.Error(t, err, "a declared confidential method without a secret is a misconfiguration")

	_, err = remotesessions.ResolveTokenEndpointAuthMethod("client_secret_post", "")
	require.Error(t, err, "a declared confidential method without a secret is a misconfiguration")
}

func TestResolveTokenEndpointAuthMethod_NoneIgnoresSecret(t *testing.T) {
	t.Parallel()

	m, err := remotesessions.ResolveTokenEndpointAuthMethod("none", "")
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodNone, m)

	m, err = remotesessions.ResolveTokenEndpointAuthMethod("none", "s3cret")
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodNone, m, "an explicit none wins even when a secret exists")
}

func TestResolveTokenEndpointAuthMethod_UnknownDefaultsBySecret(t *testing.T) {
	t.Parallel()

	m, err := remotesessions.ResolveTokenEndpointAuthMethod("", "s3cret")
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, m, "confidential clients with no stored method default to Basic")

	m, err = remotesessions.ResolveTokenEndpointAuthMethod("", "")
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodNone, m, "secret-less clients with no stored method are public")

	m, err = remotesessions.ResolveTokenEndpointAuthMethod("private_key_jwt", "s3cret")
	require.NoError(t, err)
	require.Equal(t, remotesessions.TokenEndpointAuthMethodBasic, m, "unrecognized methods with a secret fall back to Basic")
}

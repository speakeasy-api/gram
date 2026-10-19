package mcp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestAssistantWorkloadClientAuthentication(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	public, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "workload-test", Algorithm: "ES256", Use: "sig"}}})
	require.NoError(t, err)
	for _, method := range []string{"client_secret_basic", "client_secret_post"} {
		t.Run(method, func(t *testing.T) {
			keys, secret, hash, err := assistantWorkloadClientAuthentication(ProvisionAssistantWorkloadClientParams{AuthMethod: method})
			require.NoError(t, err)
			require.Nil(t, keys)
			require.NotEmpty(t, secret)
			require.True(t, hash.Valid)
			require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(secret)))
		})
	}
	keys, secret, hash, err := assistantWorkloadClientAuthentication(ProvisionAssistantWorkloadClientParams{AuthMethod: "private_key_jwt", JWKS: public})
	require.NoError(t, err)
	require.JSONEq(t, string(public), string(keys))
	require.Empty(t, secret)
	require.False(t, hash.Valid)
	private, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: key, KeyID: "workload-test", Algorithm: "ES256", Use: "sig"}}})
	require.NoError(t, err)
	for _, p := range []ProvisionAssistantWorkloadClientParams{
		{}, {AuthMethod: "none"}, {AuthMethod: "private_key_jwt"},
		{AuthMethod: "private_key_jwt", JWKS: private},
		{AuthMethod: "private_key_jwt", JWKS: public, Secret: strings.Repeat("x", 32)},
		{AuthMethod: "client_secret_basic", JWKS: public},
		{AuthMethod: "client_secret_basic", Secret: "short"},
		{AuthMethod: "client_secret_basic", Secret: strings.Repeat("x", 73)},
	} {
		_, _, _, err := assistantWorkloadClientAuthentication(p)
		require.Error(t, err)
	}
}

package jwks

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

func TestValidateKeySource_NullInlineNormalizedToAbsent(t *testing.T) {
	t.Parallel()

	normalized, err := ValidateKeySource(oauthwire.AuthMethodNone, json.RawMessage("null"), "")
	require.NoError(t, err)
	require.Nil(t, normalized)
}

func TestValidateKeySource_PrivateMaterialRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateKeySource(oauthwire.AuthMethodNone, json.RawMessage(`{"keys":[{"kty":"EC","d":"secret"}]}`), "")
	require.ErrorIs(t, err, ErrPrivateKeyMaterial)
}

func TestValidateKeySource_URIInvalid(t *testing.T) {
	t.Parallel()

	_, err := ValidateKeySource(oauthwire.AuthMethodNone, nil, "http://example.com/jwks.json")
	require.ErrorIs(t, err, ErrKeySourceURIInvalid)
}

func TestValidateKeySource_BothSourcesRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateKeySource(oauthwire.AuthMethodNone, keySetJSON(t, testKey(t, "a")), "https://example.com/jwks.json")
	require.ErrorIs(t, err, ErrKeySourceAmbiguous)
}

func TestValidateKeySource_PrivateKeyJWTRequiresASource(t *testing.T) {
	t.Parallel()

	_, err := ValidateKeySource(oauthwire.AuthMethodPrivateKeyJWT, nil, "")
	require.ErrorIs(t, err, ErrKeySourceMissing)

	// A JSON null is absent, not a source.
	_, err = ValidateKeySource(oauthwire.AuthMethodPrivateKeyJWT, json.RawMessage("null"), "")
	require.ErrorIs(t, err, ErrKeySourceMissing)
}

func TestValidateKeySource_PrivateKeyJWTNoUsableKeyRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateKeySource(oauthwire.AuthMethodPrivateKeyJWT, json.RawMessage(`{"keys":[]}`), "")
	require.ErrorIs(t, err, ErrNoUsableSigningKey)
}

func TestValidateKeySource_PrivateKeyJWTInlineAccepted(t *testing.T) {
	t.Parallel()

	set := keySetJSON(t, testKey(t, "a"))
	normalized, err := ValidateKeySource(oauthwire.AuthMethodPrivateKeyJWT, set, "")
	require.NoError(t, err)
	require.JSONEq(t, string(set), string(normalized))
}

func TestValidateKeySource_PrivateKeyJWTRemoteAccepted(t *testing.T) {
	t.Parallel()

	normalized, err := ValidateKeySource(oauthwire.AuthMethodPrivateKeyJWT, nil, "https://example.com/jwks.json")
	require.NoError(t, err)
	require.Nil(t, normalized)
}

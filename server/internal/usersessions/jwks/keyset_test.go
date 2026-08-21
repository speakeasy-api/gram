package jwks

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePublicOnly_NilAccepted(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidatePublicOnly(nil))
}

func TestValidatePublicOnly_PublicKeysAccepted(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidatePublicOnly(keySetJSON(t, testKey(t, "a"), testKey(t, "b"))))
}

func TestValidatePublicOnly_PrivateKeyRejected(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"keys":[{"kty":"EC","crv":"P-256","x":"AA","y":"AA","d":"secret"}]}`)
	require.ErrorIs(t, ValidatePublicOnly(raw), ErrPrivateKeyMaterial)
}

func TestValidatePublicOnly_SymmetricKeyRejected(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"keys":[{"kty":"oct","k":"c2VjcmV0"}]}`)
	require.ErrorIs(t, ValidatePublicOnly(raw), ErrSymmetricKeyMaterial)
}

func TestValidatePublicOnly_MalformedRejected(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, ValidatePublicOnly(json.RawMessage(`not json`)), ErrKeySetInvalid)
	require.ErrorIs(t, ValidatePublicOnly(json.RawMessage(`{"keys": 42}`)), ErrKeySetInvalid)
}

func TestParseKeySet_ParsesUsableKeys(t *testing.T) {
	t.Parallel()

	set, err := parseKeySet(keySetJSON(t, testKey(t, "a"), testKey(t, "b")))
	require.NoError(t, err)
	require.Len(t, set.Keys, 2)
	require.Equal(t, "a", set.Keys[0].KeyID)
	require.Equal(t, "b", set.Keys[1].KeyID)
}

func TestParseKeySet_EmptyDocumentRejected(t *testing.T) {
	t.Parallel()

	_, err := parseKeySet(nil)
	require.ErrorIs(t, err, ErrKeySetInvalid)
}

func TestParseKeySet_PrivateMaterialFatal(t *testing.T) {
	t.Parallel()

	// One offending key rejects the whole set: a source publishing private
	// material is broken in a way key-level tolerance must not paper over.
	good := testKey(t, "good")
	goodJSON, err := good.MarshalJSON()
	require.NoError(t, err)
	raw := json.RawMessage(`{"keys":[` + string(goodJSON) + `,{"kty":"EC","crv":"P-256","x":"AA","y":"AA","d":"secret"}]}`)

	_, err = parseKeySet(raw)
	require.ErrorIs(t, err, ErrPrivateKeyMaterial)
}

func TestParseKeySet_SkipsEncryptionUseKeys(t *testing.T) {
	t.Parallel()

	enc := testKey(t, "enc-key")
	enc.Use = "enc"
	set, err := parseKeySet(keySetJSON(t, testKey(t, "sig-key"), enc))
	require.NoError(t, err)
	require.Len(t, set.Keys, 1)
	require.Equal(t, "sig-key", set.Keys[0].KeyID)
}

func TestParseKeySet_SkipsDisallowedDeclaredAlgorithm(t *testing.T) {
	t.Parallel()

	unsigned := testKey(t, "none-key")
	unsigned.Algorithm = "none"
	set, err := parseKeySet(keySetJSON(t, unsigned, testKey(t, "sig-key")))
	require.NoError(t, err)
	require.Len(t, set.Keys, 1)
	require.Equal(t, "sig-key", set.Keys[0].KeyID)
}

func TestParseKeySet_KeepsKeysWithoutDeclaredAlgorithm(t *testing.T) {
	t.Parallel()

	// RFC 7517 makes alg optional and omission is common in the wild; the
	// allowlist rejects declared none/HS*, it never requires a declaration.
	bare := testKey(t, "bare")
	bare.Algorithm = ""
	bare.Use = ""
	set, err := parseKeySet(keySetJSON(t, bare))
	require.NoError(t, err)
	require.Len(t, set.Keys, 1)
}

func TestParseKeySet_SkipsUnparseableKeys(t *testing.T) {
	t.Parallel()

	// A key of an unsupported kty must not fail the set: real providers ship
	// exotic entries alongside their signing keys.
	good := testKey(t, "good")
	goodJSON, err := good.MarshalJSON()
	require.NoError(t, err)
	raw := json.RawMessage(`{"keys":[{"kty":"FUTURE","zzz":1},` + string(goodJSON) + `]}`)

	set, err := parseKeySet(raw)
	require.NoError(t, err)
	require.Len(t, set.Keys, 1)
	require.Equal(t, "good", set.Keys[0].KeyID)
}

func TestSelectKey_ByKid(t *testing.T) {
	t.Parallel()

	set, err := parseKeySet(keySetJSON(t, testKey(t, "a"), testKey(t, "b")))
	require.NoError(t, err)

	key, err := selectKey(set, "b")
	require.NoError(t, err)
	require.Equal(t, "b", key.KeyID)
}

func TestSelectKey_UnknownKid(t *testing.T) {
	t.Parallel()

	set, err := parseKeySet(keySetJSON(t, testKey(t, "a")))
	require.NoError(t, err)

	_, err = selectKey(set, "missing")
	require.ErrorIs(t, err, ErrKeyNotFound)
}

func TestSelectKey_NoKidSingleKey(t *testing.T) {
	t.Parallel()

	set, err := parseKeySet(keySetJSON(t, testKey(t, "only")))
	require.NoError(t, err)

	key, err := selectKey(set, "")
	require.NoError(t, err)
	require.Equal(t, "only", key.KeyID)
}

func TestSelectKey_NoKidMultipleKeysAmbiguous(t *testing.T) {
	t.Parallel()

	set, err := parseKeySet(keySetJSON(t, testKey(t, "a"), testKey(t, "b")))
	require.NoError(t, err)

	_, err = selectKey(set, "")
	require.ErrorIs(t, err, ErrKeyNotFound)
}

func TestAllowedSignatureAlgorithms_ReturnsCopy(t *testing.T) {
	t.Parallel()

	first := AllowedSignatureAlgorithms()
	first[0] = "none"
	require.NotEqual(t, first[0], AllowedSignatureAlgorithms()[0])
}

package encryption_test

import (
	"crypto/rand"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
)

func newDataKey(t *testing.T) []byte {
	t.Helper()

	key := make([]byte, encryption.DataKeyBytes)
	_, err := rand.Read(key)
	require.NoError(t, err)

	return key
}

func TestEnvelope_RoundTripsThroughItsStringForm(t *testing.T) {
	t.Parallel()

	key := newDataKey(t)
	id := uuid.New()

	sealed, err := encryption.SealEnvelope(key, id, []byte("node state"), []byte("aad"))
	require.NoError(t, err)

	encoded := sealed.String()
	require.True(t, strings.HasPrefix(encoded, encryption.EnvelopePrefix+id.String()+"$"))
	require.True(t, encryption.IsEnvelope(encoded))

	parsed, err := encryption.ParseEnvelope(encoded)
	require.NoError(t, err)
	require.Equal(t, id, parsed.DataKeyID)

	plaintext, err := encryption.OpenEnvelope(key, parsed, []byte("aad"))
	require.NoError(t, err)
	require.Equal(t, "node state", string(plaintext))
}

func TestOpenEnvelope_RefusesDifferentAssociatedData(t *testing.T) {
	t.Parallel()

	key := newDataKey(t)
	sealed, err := encryption.SealEnvelope(key, uuid.New(), []byte("secret"), []byte("org-a"))
	require.NoError(t, err)

	_, err = encryption.OpenEnvelope(key, sealed, []byte("org-b"))
	require.ErrorIs(t, err, encryption.ErrEnvelopeOpen)
}

func TestOpenEnvelope_RefusesDifferentKey(t *testing.T) {
	t.Parallel()

	sealed, err := encryption.SealEnvelope(newDataKey(t), uuid.New(), []byte("secret"), nil)
	require.NoError(t, err)

	_, err = encryption.OpenEnvelope(newDataKey(t), sealed, nil)
	require.ErrorIs(t, err, encryption.ErrEnvelopeOpen)
}

func TestOpenEnvelope_RefusesTruncatedCiphertext(t *testing.T) {
	t.Parallel()

	_, err := encryption.OpenEnvelope(newDataKey(t), encryption.Envelope{DataKeyID: uuid.New(), Sealed: []byte("short")}, nil)
	require.ErrorIs(t, err, encryption.ErrEnvelopeOpen)
}

func TestSealEnvelope_RefusesWrongKeySize(t *testing.T) {
	t.Parallel()

	_, err := encryption.SealEnvelope(make([]byte, 16), uuid.New(), []byte("secret"), nil)
	require.Error(t, err)
}

func TestParseEnvelope_RejectsMalformedValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{name: "client ciphertext", value: "c2VjcmV0"},
		{name: "missing separator", value: encryption.EnvelopePrefix + uuid.NewString()},
		{name: "invalid id", value: encryption.EnvelopePrefix + "not-a-uuid$c2VjcmV0"},
		{name: "invalid base64", value: encryption.EnvelopePrefix + uuid.NewString() + "$***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := encryption.ParseEnvelope(tc.value)
			require.ErrorIs(t, err, encryption.ErrNotEnvelope)
		})
	}
}

func TestIsEnvelope_DistinguishesClientCiphertext(t *testing.T) {
	t.Parallel()

	client, err := encryption.NewWithBytes(newDataKey(t))
	require.NoError(t, err)

	legacy, err := client.Encrypt([]byte("secret"))
	require.NoError(t, err)

	require.False(t, encryption.IsEnvelope(legacy))
}

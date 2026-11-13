package encryption_test

import (
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
)

func TestIsEmptyCiphertext_ClientCiphertext(t *testing.T) {
	t.Parallel()

	client, err := encryption.NewWithBytes(newDataKey(t))
	require.NoError(t, err)

	cases := []struct {
		name      string
		plaintext string
		wantEmpty bool
	}{
		{name: "empty", plaintext: "", wantEmpty: true},
		{name: "one byte", plaintext: "x", wantEmpty: false},
		{name: "two bytes", plaintext: "xy", wantEmpty: false},
		{name: "longer value", plaintext: "sk_live_0123456789abcdef", wantEmpty: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ciphertext, err := client.Encrypt([]byte(tc.plaintext))
			require.NoError(t, err)

			empty, err := encryption.IsEmptyCiphertext(ciphertext)
			require.NoError(t, err)
			require.Equal(t, tc.wantEmpty, empty)
		})
	}
}

func TestIsEmptyCiphertext_Envelope(t *testing.T) {
	t.Parallel()

	key := newDataKey(t)

	cases := []struct {
		name      string
		plaintext string
		wantEmpty bool
	}{
		{name: "empty", plaintext: "", wantEmpty: true},
		{name: "one byte", plaintext: "x", wantEmpty: false},
		{name: "longer value", plaintext: "sk_live_0123456789abcdef", wantEmpty: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelope, err := encryption.SealEnvelope(key, uuid.New(), []byte(tc.plaintext), []byte("associated data"))
			require.NoError(t, err)

			empty, err := encryption.IsEmptyCiphertext(envelope.String())
			require.NoError(t, err)
			require.Equal(t, tc.wantEmpty, empty)
		})
	}
}

func TestIsEmptyCiphertext_DoesNotAuthenticate(t *testing.T) {
	t.Parallel()

	// Nothing sealed these bytes; only their length matches an empty
	// plaintext's ciphertext, and that is all the check reads.
	forged := base64.StdEncoding.EncodeToString(make([]byte, 28))

	empty, err := encryption.IsEmptyCiphertext(forged)
	require.NoError(t, err)
	require.True(t, empty)
}

func TestIsEmptyCiphertext_RejectsMalformedValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{name: "empty string", value: ""},
		{name: "invalid base64", value: "***"},
		{name: "shorter than nonce and tag", value: base64.StdEncoding.EncodeToString(make([]byte, 27))},
		{name: "envelope with invalid base64", value: encryption.EnvelopePrefix + uuid.NewString() + "$***"},
		{name: "envelope shorter than nonce and tag", value: encryption.EnvelopePrefix + uuid.NewString() + "$" + base64.StdEncoding.EncodeToString(make([]byte, 27))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := encryption.IsEmptyCiphertext(tc.value)
			require.ErrorIs(t, err, encryption.ErrMalformedCiphertext)
		})
	}
}

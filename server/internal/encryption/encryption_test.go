package encryption_test

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestErrorCode_NamesEveryDecryptFailure(t *testing.T) {
	t.Parallel()

	client := testenv.NewEncryptionClient(t)
	cases := map[string]struct {
		ciphertext string
		code       string
	}{
		"not base64":       {ciphertext: "not base64!", code: "ENC-5"},
		"too short":        {ciphertext: base64.StdEncoding.EncodeToString([]byte("short")), code: "ENC-6"},
		"does not decrypt": {ciphertext: base64.StdEncoding.EncodeToString(make([]byte, 64)), code: "ENC-4"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := client.Decrypt(tc.ciphertext)
			require.Error(t, err)

			code, ok := encryption.ErrorCode(err)
			require.True(t, ok)
			require.Equal(t, tc.code, code)
		})
	}
}

func TestErrorCode_IgnoresOtherErrors(t *testing.T) {
	t.Parallel()

	_, ok := encryption.ErrorCode(errors.New("not an encryption error"))
	require.False(t, ok)
}

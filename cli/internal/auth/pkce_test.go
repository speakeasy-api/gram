package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPKCE_ChallengeIsS256OfVerifier(t *testing.T) {
	t.Parallel()

	a, err := NewPKCE()
	require.NoError(t, err)
	b, err := NewPKCE()
	require.NoError(t, err)

	// The server requires 43 to 128 characters.
	require.Len(t, a.Verifier, 43)
	sum := sha256.Sum256([]byte(a.Verifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), a.Challenge)
	require.NotEqual(t, a.Verifier, b.Verifier)
}

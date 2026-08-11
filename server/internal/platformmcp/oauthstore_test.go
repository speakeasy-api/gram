package platformmcp

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyPKCE(t *testing.T) {
	t.Parallel()

	verifier := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	require.NoError(t, verifyPKCE(verifier, challenge))
	weakHash := sha256.Sum256([]byte("a"))
	require.Error(t, verifyPKCE("a", base64.RawURLEncoding.EncodeToString(weakHash[:])))
	require.Error(t, verifyPKCE(strings.Repeat("a", 43), strings.Repeat("!", 43)))
}

func TestValidPKCES256Challenge(t *testing.T) {
	t.Parallel()

	require.True(t, validPKCES256Challenge(strings.Repeat("a", 43)))
	require.False(t, validPKCES256Challenge("short"))
	require.False(t, validPKCES256Challenge(strings.Repeat("!", 43)))
}

package access

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalResourceID_MatchesTheProjectLookupKeys(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0198a0b2-6c4e-7d3a-9f10-4b2c8e5d6a71", canonicalResourceID("0198A0B2-6C4E-7D3A-9F10-4B2C8E5D6A71"))
	require.Equal(t, "0198a0b2-6c4e-7d3a-9f10-4b2c8e5d6a71", canonicalResourceID("0198a0b2-6c4e-7d3a-9f10-4b2c8e5d6a71"))
	require.Equal(t, "legacy-server", canonicalResourceID("legacy-server"))
}

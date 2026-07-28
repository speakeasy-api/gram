package gcpkms

import (
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

// The stand-in generates a real key pair, so it can only stand in for the
// algorithms this package actually supports.
func TestNewLocalSigningClient_RejectsUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := NewLocalSigningClient(jose.PS256)
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}

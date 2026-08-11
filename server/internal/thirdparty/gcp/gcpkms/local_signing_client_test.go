package gcpkms

import (
	"context"
	"crypto/sha256"
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

// Real KMS calls fail once the caller's context is done. The stand-in has to do
// the same, or a cancellation test would pass against production and fail here.
func TestLocalSigningClient_HonoursCanceledContext(t *testing.T) {
	t.Parallel()

	client, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = client.GetPublicKey(ctx, testResourceName)
	require.ErrorIs(t, err, context.Canceled)

	digest := sha256.Sum256([]byte(ProbePayload))
	_, err = client.AsymmetricSign(ctx, testResourceName, jose.RS256, digest[:])
	require.ErrorIs(t, err, context.Canceled)
}

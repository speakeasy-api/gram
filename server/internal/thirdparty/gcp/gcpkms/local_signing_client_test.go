package gcpkms

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"path/filepath"
	"sync"
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

func TestPersistentLocalSigningClient_ReusesKeyAcrossClients(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "local-kms.pem")
	first, err := NewPersistentLocalSigningClient(jose.RS256, path)
	require.NoError(t, err)
	second, err := NewPersistentLocalSigningClient(jose.RS256, path)
	require.NoError(t, err)

	firstPublic, err := first.GetPublicKey(t.Context(), testResourceName)
	require.NoError(t, err)
	secondPublic, err := second.GetPublicKey(t.Context(), testResourceName)
	require.NoError(t, err)

	firstDER, err := x509.MarshalPKIXPublicKey(firstPublic.Key)
	require.NoError(t, err)
	secondDER, err := x509.MarshalPKIXPublicKey(secondPublic.Key)
	require.NoError(t, err)
	require.Equal(t, firstDER, secondDER)
}

func TestPersistentLocalSigningClient_ConcurrentCreationConverges(t *testing.T) {
	t.Parallel()

	const clients = 8
	path := filepath.Join(t.TempDir(), "local-kms.pem")
	start := make(chan struct{})
	var wg sync.WaitGroup
	keys := make([][]byte, clients)
	errs := make([]error, clients)
	for i := range clients {
		wg.Go(func() {
			<-start
			client, err := NewPersistentLocalSigningClient(jose.ES256, path)
			if err != nil {
				errs[i] = err
				return
			}
			public, err := client.GetPublicKey(t.Context(), testResourceName)
			if err != nil {
				errs[i] = err
				return
			}
			keys[i], errs[i] = x509.MarshalPKIXPublicKey(public.Key)
		})
	}
	close(start)
	wg.Wait()
	for i := range clients {
		require.NoError(t, errs[i])
		require.Equal(t, keys[0], keys[i])
	}
}
